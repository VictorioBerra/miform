package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// FieldPair represents a single submitted form field preserving order and display formatting.
type FieldPair struct {
	Name  string `json:"name"`  // Original input name
	Label string `json:"label"` // Humanized label for display
	Value string `json:"value"` // Formatted/joined string value
}

// ControlFields contains reserved Web3Forms-compatible form control parameters.
type ControlFields struct {
	AccessKey string `json:"access_key"`
	Subject   string `json:"subject"`
	FromName  string `json:"from_name"`
	ReplyTo   string `json:"reply_to"`
	CCEmail   string `json:"cc_email"`
	Redirect  string `json:"redirect"`
	Botcheck          string `json:"botcheck"`
	IntroText         string `json:"intro_text"`
	TurnstileResponse string `json:"turnstile_response"`
}

// RequestMetadata contains visitor attribution and header info.
type RequestMetadata struct {
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Referer   string `json:"referer"`
	Origin    string `json:"origin"`
}

// ParsedForm is the unified output of parsing urlencoded, multipart, or JSON submissions.
type ParsedForm struct {
	Controls ControlFields     `json:"controls"`
	Fields   []FieldPair       `json:"fields"` // Custom form fields in document order
	Data     map[string]any    `json:"data"`   // Flat map for database/API responses
	Metadata RequestMetadata   `json:"metadata"`
	IsAJAX   bool              `json:"is_ajax"` // True if client expects JSON response
}

var (
	camelCaseRegex  = regexp.MustCompile("([a-z0-9])([A-Z])")
	multiSpaceRegex = regexp.MustCompile(`\s+`)
	htmlLinkRegex   = regexp.MustCompile(`(?i)<\s*a\b[^>]*\bhref\s*=|(?i)<\s*/\s*a\s*>`)
)

// HasHTMLLink checks if a string contains raw HTML link tags (e.g. <a href=... or </a>).
func HasHTMLLink(s string) bool {
	return htmlLinkRegex.MatchString(s)
}

// ContainsHTMLLink checks if any submitted form field or control field contains raw HTML links.
func (p *ParsedForm) ContainsHTMLLink() bool {
	if p == nil {
		return false
	}
	if HasHTMLLink(p.Controls.Subject) || HasHTMLLink(p.Controls.IntroText) {
		return true
	}
	for _, f := range p.Fields {
		if HasHTMLLink(f.Value) {
			return true
		}
	}
	for _, v := range p.Data {
		if s, ok := v.(string); ok && HasHTMLLink(s) {
			return true
		}
	}
	return false
}

// ParseRequest parses an HTTP form submission preserving field order and normalizing data.
func ParseRequest(r *http.Request) (*ParsedForm, error) {
	if r == nil {
		return nil, errors.New("nil request")
	}

	metadata := extractMetadata(r)
	isAJAX := detectAJAX(r)

	contentType := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)

	var orderedEntries []rawEntry
	var err error

	switch {
	case mediaType == "application/json":
		orderedEntries, err = parseJSONBody(r)
	case mediaType == "multipart/form-data":
		orderedEntries, err = parseMultipartBody(r)
	default:
		// Default to application/x-www-form-urlencoded
		orderedEntries, err = parseURLEncodedBody(r)
	}

	if err != nil {
		return nil, err
	}

	return buildParsedForm(orderedEntries, metadata, isAJAX), nil
}

type rawEntry struct {
	Key   string
	Value string
}

func parseURLEncodedBody(r *http.Request) ([]rawEntry, error) {
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	// Restore body for any subsequent readers if needed
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	rawStr := string(bodyBytes)
	if rawStr == "" {
		return nil, nil
	}

	var entries []rawEntry
	pairs := strings.Split(rawStr, "&")
	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		key, err := url.QueryUnescape(kv[0])
		if err != nil {
			key = kv[0]
		}
		val := ""
		if len(kv) == 2 {
			unescapedVal, err := url.QueryUnescape(strings.ReplaceAll(kv[1], "+", " "))
			if err != nil {
				val = kv[1]
			} else {
				val = unescapedVal
			}
		}
		entries = append(entries, rawEntry{Key: strings.TrimSpace(key), Value: strings.TrimSpace(val)})
	}

	return entries, nil
}

func parseMultipartBody(r *http.Request) ([]rawEntry, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		// Fallback to standard ParseMultipartForm
		if parseErr := r.ParseMultipartForm(10 << 20); parseErr != nil {
			return nil, fmt.Errorf("failed to parse multipart form: %w", parseErr)
		}
		var entries []rawEntry
		for key, vals := range r.MultipartForm.Value {
			for _, val := range vals {
				entries = append(entries, rawEntry{Key: strings.TrimSpace(key), Value: strings.TrimSpace(val)})
			}
		}
		return entries, nil
	}

	var entries []rawEntry
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("multipart read error: %w", err)
		}

		name := part.FormName()
		if name == "" {
			continue
		}

		// Read part value (non-file text values)
		buf, err := io.ReadAll(io.LimitReader(part, 1<<20)) // 1MB limit per field
		if err != nil {
			continue
		}

		entries = append(entries, rawEntry{
			Key:   strings.TrimSpace(name),
			Value: strings.TrimSpace(string(buf)),
		})
	}

	return entries, nil
}

func parseJSONBody(r *http.Request) ([]rawEntry, error) {
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read json body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return nil, nil
	}

	var rawMap map[string]any
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		return nil, fmt.Errorf("invalid json payload: %w", err)
	}

	var entries []rawEntry

	// Support nested Web3Forms Submissions API payload: { "form_id": "...", "fields": { ... }, "subject": "..." }
	var fieldsMap map[string]any
	if nested, ok := rawMap["fields"].(map[string]any); ok {
		fieldsMap = nested
		// Top-level controls
		for k, v := range rawMap {
			if k != "fields" && k != "metadata" {
				entries = append(entries, rawEntry{Key: k, Value: fmt.Sprint(v)})
			}
		}
	} else {
		fieldsMap = rawMap
	}

	for k, v := range fieldsMap {
		switch val := v.(type) {
		case []any:
			var strParts []string
			for _, item := range val {
				strParts = append(strParts, strings.TrimSpace(fmt.Sprint(item)))
			}
			entries = append(entries, rawEntry{Key: k, Value: strings.Join(strParts, ", ")})
		case []string:
			entries = append(entries, rawEntry{Key: k, Value: strings.Join(val, ", ")})
		default:
			if val != nil {
				entries = append(entries, rawEntry{Key: k, Value: strings.TrimSpace(fmt.Sprint(val))})
			}
		}
	}

	return entries, nil
}

func buildParsedForm(entries []rawEntry, meta RequestMetadata, isAJAX bool) *ParsedForm {
	parsed := &ParsedForm{
		Data:     make(map[string]any),
		Metadata: meta,
		IsAJAX:   isAJAX,
	}

	// Group repeated keys (e.g. multi-value checkboxes) preserving first occurrence order
	type keyValues struct {
		values []string
	}
	grouped := make(map[string]*keyValues)
	var orderedKeys []string

	for _, entry := range entries {
		if entry.Key == "" {
			continue
		}
		if _, exists := grouped[entry.Key]; !exists {
			grouped[entry.Key] = &keyValues{}
			orderedKeys = append(orderedKeys, entry.Key)
		}
		if entry.Value != "" {
			grouped[entry.Key].values = append(grouped[entry.Key].values, entry.Value)
		}
	}

	for _, key := range orderedKeys {
		valSlice := grouped[key].values
		joinedVal := strings.Join(valSlice, ", ")

		lowerKey := strings.ToLower(key)

		// Check for reserved control keys
		switch lowerKey {
		case "access_key", "apikey", "form_id":
			if parsed.Controls.AccessKey == "" {
				parsed.Controls.AccessKey = joinedVal
			}
			continue
		case "subject":
			if parsed.Controls.Subject == "" {
				parsed.Controls.Subject = joinedVal
			}
			continue
		case "from_name":
			if parsed.Controls.FromName == "" {
				parsed.Controls.FromName = joinedVal
			}
			continue
		case "replyto", "reply_to":
			if parsed.Controls.ReplyTo == "" {
				parsed.Controls.ReplyTo = joinedVal
			}
			continue
		case "ccemail", "cc":
			if parsed.Controls.CCEmail == "" {
				parsed.Controls.CCEmail = joinedVal
			}
			continue
		case "redirect":
			if parsed.Controls.Redirect == "" {
				parsed.Controls.Redirect = joinedVal
			}
			continue
		case "botcheck":
			parsed.Controls.Botcheck = joinedVal
			continue
		case "intro_text", "preheader":
			if parsed.Controls.IntroText == "" {
				parsed.Controls.IntroText = joinedVal
			}
			continue
		case "cf-turnstile-response", "g-recaptcha-response", "turnstile", "turnstile_response", "h-captcha-response":
			if parsed.Controls.TurnstileResponse == "" {
				parsed.Controls.TurnstileResponse = joinedVal
			}
			continue
		}

		// Handle visitor email: captures reply-to if not explicitly set, AND preserves in Fields table
		if (lowerKey == "email" || lowerKey == "user_email") && parsed.Controls.ReplyTo == "" {
			parsed.Controls.ReplyTo = joinedVal
		}

		label := HumanizeLabel(key)
		pair := FieldPair{
			Name:  key,
			Label: label,
			Value: joinedVal,
		}

		parsed.Fields = append(parsed.Fields, pair)
		parsed.Data[key] = joinedVal
	}

	return parsed
}

// HumanizeLabel converts raw input names like `company_name` or `quoteTopic` into `Company Name` or `Quote Topic`.
func HumanizeLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// Common abbreviations
	switch strings.ToLower(name) {
	case "id":
		return "ID"
	case "ip":
		return "IP"
	case "url":
		return "URL"
	case "msg":
		return "Message"
	}

	// If already capitalized with space, keep clean
	if strings.Contains(name, " ") {
		parts := strings.Fields(name)
		for i, p := range parts {
			if len(p) > 0 && unicode.IsLower(rune(p[0])) {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return strings.Join(parts, " ")
	}

	// Handle camelCase
	name = camelCaseRegex.ReplaceAllString(name, "$1 $2")

	// Replace underscores and dashes with space
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = multiSpaceRegex.ReplaceAllString(name, " ")

	// Capitalize each word
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}

func extractMetadata(r *http.Request) RequestMetadata {
	ip := r.Header.Get("CF-Connecting-IP")
	if ip == "" {
		ip = r.Header.Get("X-Forwarded-For")
		if ip != "" {
			parts := strings.Split(ip, ",")
			ip = strings.TrimSpace(parts[0])
		}
	}
	if ip == "" {
		ip = r.RemoteAddr
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
	}

	return RequestMetadata{
		IP:        ip,
		UserAgent: r.Header.Get("User-Agent"),
		Referer:   r.Header.Get("Referer"),
		Origin:    r.Header.Get("Origin"),
	}
}

func detectAJAX(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	requestedWith := r.Header.Get("X-Requested-With")

	if strings.Contains(accept, "application/json") {
		return true
	}
	if strings.Contains(contentType, "application/json") {
		return true
	}
	if strings.EqualFold(requestedWith, "XMLHttpRequest") {
		return true
	}

	return false
}

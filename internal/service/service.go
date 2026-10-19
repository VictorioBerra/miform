package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"miform/internal/mailer"
	"miform/internal/parser"
	"miform/internal/turnstile"
)

var (
	ErrFormNotFound      = errors.New("form not found")
	ErrFormInactive      = errors.New("form is currently inactive")
	ErrDomainNotAllowed  = errors.New("submissions from this domain are not allowed")
	ErrMissingAccessKey  = errors.New("missing access_key or form identifier")
	ErrTurnstileMissing  = errors.New("captcha verification failed: missing turnstile token")
	ErrTurnstileFailed   = errors.New("captcha verification failed: invalid token")
	ErrTurnstileConfig   = errors.New("captcha verification failed: turnstile secret not configured")
	ErrSpamHTMLLinks     = errors.New("form submission rejected: HTML links are not allowed")
)

// SubmissionResult is the structured result returned after processing a form submission.
type SubmissionResult struct {
	FormID       string         `json:"form_id"`
	SubmissionID string         `json:"submission_id"`
	Status       string         `json:"status"` // "sent", "spam", "failed"
	RedirectURL  string         `json:"redirect_url"`
	IsAJAX       bool           `json:"is_ajax"`
	Message      string         `json:"message"`
	Data         map[string]any `json:"data,omitempty"`
}

// SubmissionService coordinates form resolution, domain validation, persistence, and email dispatch.
type SubmissionService struct {
	app               core.App
	mailer            *mailer.Mailer
	turnstileVerifier turnstile.Verifier
}

// New creates a new SubmissionService.
func New(app core.App) *SubmissionService {
	return &SubmissionService{
		app:               app,
		mailer:            mailer.New(app),
		turnstileVerifier: turnstile.New(nil),
	}
}

// WithTurnstileVerifier sets a custom Turnstile verifier (e.g. for testing).
func (s *SubmissionService) WithTurnstileVerifier(v turnstile.Verifier) *SubmissionService {
	s.turnstileVerifier = v
	return s
}

// ResolveForm locates a Form record by access_key or PocketBase record id.
func (s *SubmissionService) ResolveForm(identifier string) (*core.Record, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, ErrMissingAccessKey
	}

	// 1. Try resolving by access_key
	form, err := s.app.FindFirstRecordByData("forms", "access_key", identifier)
	if err == nil && form != nil {
		if !form.GetBool("is_active") {
			return nil, ErrFormInactive
		}
		return form, nil
	}

	// 2. Try resolving by PocketBase record id
	form, err = s.app.FindRecordById("forms", identifier)
	if err == nil && form != nil {
		if !form.GetBool("is_active") {
			return nil, ErrFormInactive
		}
		return form, nil
	}

	return nil, ErrFormNotFound
}

// Process handles a submitted form payload end-to-end.
func (s *SubmissionService) Process(ctx context.Context, formIdentifier string, parsed *parser.ParsedForm) (*SubmissionResult, error) {
	if formIdentifier == "" {
		formIdentifier = parsed.Controls.AccessKey
	}
	if formIdentifier == "" {
		return nil, ErrMissingAccessKey
	}

	form, err := s.ResolveForm(formIdentifier)
	if err != nil {
		return nil, err
	}

	// 1. Domain whitelist verification
	allowedDomains := form.GetString("allowed_domains")
	if !ValidateDomain(allowedDomains, parsed.Metadata.Referer, parsed.Metadata.Origin) {
		return nil, ErrDomainNotAllowed
	}

	// 2. Turnstile Verification (if enabled on the form)
	if form.GetBool("turnstile_enabled") {
		secretKey := strings.TrimSpace(form.GetString("turnstile_secret_key"))
		if secretKey == "" {
			secretKey = strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY"))
		}
		if secretKey == "" {
			s.app.Logger().Error("turnstile enabled on form but no secret key configured", "form_id", form.Id)
			return nil, ErrTurnstileConfig
		}

		token := strings.TrimSpace(parsed.Controls.TurnstileResponse)
		if token == "" {
			return nil, ErrTurnstileMissing
		}

		res, err := s.turnstileVerifier.Verify(ctx, token, parsed.Metadata.IP, secretKey)
		if err != nil {
			s.app.Logger().Error("turnstile verification request failed", "err", err, "form_id", form.Id)
			return nil, ErrTurnstileFailed
		}
		if !res.Success {
			s.app.Logger().Warn("turnstile verification rejected", "codes", res.ErrorCodes, "form_id", form.Id)
			return nil, ErrTurnstileFailed
		}
	}

	// Determine redirect URL
	redirectURL := parsed.Controls.Redirect
	if redirectURL == "" {
		redirectURL = form.GetString("redirect_url")
	}
	if redirectURL == "" {
		redirectURL = "/submit/success"
	}

	submissionsColl, err := s.app.FindCollectionByNameOrId("submissions")
	if err != nil {
		return nil, fmt.Errorf("submissions collection not found: %w", err)
	}

	subject := mailer.ExtractSubject(form, parsed)
	replyTo := mailer.ExtractReplyTo(parsed)

	// 2. Anti-Spam Check: Raw HTML Links
	if parsed.ContainsHTMLLink() {
		s.app.Logger().Warn("submission rejected: raw HTML links detected", "form_id", form.Id)

		spamRecord := core.NewRecord(submissionsColl)
		spamRecord.Set("form", form.Id)
		spamRecord.Set("data", parsed.Data)
		spamRecord.Set("ordered_fields", parsed.Fields)
		spamRecord.Set("metadata", parsed.Metadata)
		spamRecord.Set("subject", subject)
		spamRecord.Set("reply_to", replyTo)
		spamRecord.Set("status", "spam")
		spamRecord.Set("error_message", "rejected: raw HTML links are not allowed")

		if saveErr := s.app.Save(spamRecord); saveErr != nil {
			s.app.Logger().Error("failed to save spam submission", "err", saveErr)
		}

		return nil, ErrSpamHTMLLinks
	}

	// 3. Honeypot check (botcheck)
	if parsed.Controls.Botcheck != "" {
		// Bot detected: save record as spam and do not send email
		spamRecord := core.NewRecord(submissionsColl)
		spamRecord.Set("form", form.Id)
		spamRecord.Set("data", parsed.Data)
		spamRecord.Set("ordered_fields", parsed.Fields)
		spamRecord.Set("metadata", parsed.Metadata)
		spamRecord.Set("subject", subject)
		spamRecord.Set("reply_to", replyTo)
		spamRecord.Set("status", "spam")

		if saveErr := s.app.Save(spamRecord); saveErr != nil {
			s.app.Logger().Error("failed to save spam submission", "err", saveErr)
		}

		return &SubmissionResult{
			FormID:       form.Id,
			SubmissionID: spamRecord.Id,
			Status:       "spam",
			RedirectURL:  redirectURL,
			IsAJAX:       parsed.IsAJAX,
			Message:      "Form submission received successfully.",
			Data:         parsed.Data,
		}, nil
	}

	// 3. Normal Human Submission
	record := core.NewRecord(submissionsColl)
	record.Set("form", form.Id)
	record.Set("data", parsed.Data)
	record.Set("ordered_fields", parsed.Fields)
	record.Set("metadata", parsed.Metadata)
	record.Set("subject", subject)
	record.Set("reply_to", replyTo)

	// 4. Send notification email
	mailErr := s.mailer.SendNotification(form, parsed)
	if mailErr != nil {
		record.Set("status", "failed")
		record.Set("error_message", mailErr.Error())
		if saveErr := s.app.Save(record); saveErr != nil {
			s.app.Logger().Error("failed to save failed submission", "err", saveErr)
		}
		return nil, fmt.Errorf("failed to send notification email: %w", mailErr)
	}

	record.Set("status", "sent")
	if err := s.app.Save(record); err != nil {
		return nil, fmt.Errorf("failed to save submission: %w", err)
	}

	// 5. Send autoresponder if enabled
	if form.GetBool("autoresponder_enabled") {
		if autoErr := s.mailer.SendAutoresponder(form, parsed); autoErr != nil {
			s.app.Logger().Warn("failed to send autoresponder", "err", autoErr, "submission_id", record.Id)
		}
	}

	return &SubmissionResult{
		FormID:       form.Id,
		SubmissionID: record.Id,
		Status:       "sent",
		RedirectURL:  redirectURL,
		IsAJAX:       parsed.IsAJAX,
		Message:      "Form submission received successfully.",
		Data:         parsed.Data,
	}, nil
}

// ProcessRecord processes a submission record created directly through the PocketBase REST API.
func (s *SubmissionService) ProcessRecord(record *core.Record) error {
	if record.GetString("status") != "pending" {
		return nil // Already processed or marked as spam
	}

	formId := record.GetString("form")
	form, err := s.app.FindRecordById("forms", formId)
	if err != nil {
		record.Set("status", "failed")
		record.Set("error_message", "Associated form record not found: "+err.Error())
		_ = s.app.Save(record)
		return err
	}

	// Reconstruct parsed form from record data
	parsed := &parser.ParsedForm{
		Controls: parser.ControlFields{
			Subject: record.GetString("subject"),
			ReplyTo: record.GetString("reply_to"),
		},
		Data: make(map[string]any),
	}

	// Reconstruct Data
	rawBytes, _ := json.Marshal(record.Get("data"))
	_ = json.Unmarshal(rawBytes, &parsed.Data)

	// Reconstruct Fields
	rawFieldsBytes, _ := json.Marshal(record.Get("ordered_fields"))
	_ = json.Unmarshal(rawFieldsBytes, &parsed.Fields)

	// Reconstruct Metadata
	rawMetaBytes, _ := json.Marshal(record.Get("metadata"))
	_ = json.Unmarshal(rawMetaBytes, &parsed.Metadata)

	// Anti-Spam Check: Raw HTML Links
	if parsed.ContainsHTMLLink() {
		record.Set("status", "spam")
		record.Set("error_message", "rejected: raw HTML links are not allowed")
		_ = s.app.Save(record)
		return nil
	}

	// Send notification
	mailErr := s.mailer.SendNotification(form, parsed)
	if mailErr != nil {
		record.Set("status", "failed")
		record.Set("error_message", mailErr.Error())
		_ = s.app.Save(record)
		return mailErr
	}

	record.Set("status", "sent")
	_ = s.app.Save(record)

	// Send autoresponder if enabled
	if form.GetBool("autoresponder_enabled") {
		_ = s.mailer.SendAutoresponder(form, parsed)
	}

	return nil
}

// ValidateDomain verifies whether the origin or referer matches the form's allowed_domains rule.
func ValidateDomain(allowedDomainsStr string, refererStr string, originStr string) bool {
	allowedDomainsStr = strings.TrimSpace(allowedDomainsStr)
	if allowedDomainsStr == "" {
		return true // No restrictions configured
	}

	refHost := extractHost(refererStr)
	origHost := extractHost(originStr)

	// If restrictions are configured but no origin/referer was provided
	if refHost == "" && origHost == "" {
		for _, part := range strings.Split(allowedDomainsStr, ",") {
			if strings.TrimSpace(part) == "*" {
				return true
			}
		}
		return false
	}

	rules := strings.Split(allowedDomainsStr, ",")
	for _, rule := range rules {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}

		if rule == "*" {
			return true
		}

		if refHost != "" && matchHostRule(rule, refHost) {
			return true
		}
		if origHost != "" && matchHostRule(rule, origHost) {
			return true
		}
	}

	return false
}

func extractHost(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func matchHostRule(rule, host string) bool {
	rule = strings.ToLower(rule)
	host = strings.ToLower(host)

	// Exact match with or without port (e.g. "example.com" or "localhost:3000")
	if rule == host {
		return true
	}

	hostOnly := host
	if colonIdx := strings.Index(host, ":"); colonIdx != -1 {
		hostOnly = host[:colonIdx]
	}

	if rule == hostOnly {
		return true
	}

	// Wildcard port matching (e.g. "localhost:*")
	if strings.HasSuffix(rule, ":*") {
		rulePrefix := strings.TrimSuffix(rule, ":*")
		if hostOnly == rulePrefix {
			return true
		}
	}

	// Wildcard subdomain matching (e.g. "*.example.com")
	if strings.HasPrefix(rule, "*.") {
		domainSuffix := rule[1:] // e.g. ".example.com"
		if strings.HasSuffix(hostOnly, domainSuffix) {
			return true
		}
	}

	return false
}

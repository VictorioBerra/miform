package mailer

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	textTemplate "text/template"
	"time"

	"miform/internal/parser"
)

// RenderField represents a single field prepared for template rendering.
type RenderField struct {
	Name       string
	Label      string
	Value      string
	IsMultiple bool
	Items      []string
}

// NotificationData contains all data required to render the admin notification email.
type NotificationData struct {
	FormName    string
	IntroText   string
	Fields      []RenderField
	IP          string
	Referer     string
	UserAgent   string
	SubmittedAt string
}

// AutoresponderData contains data to render the visitor confirmation email.
type AutoresponderData struct {
	FormName string
	Message  string
}

// PrepareRenderFields transforms parser.FieldPair slice into template-ready RenderField slice.
func PrepareRenderFields(fields []parser.FieldPair) []RenderField {
	var result []RenderField
	for _, f := range fields {
		rf := RenderField{
			Name:  f.Name,
			Label: f.Label,
			Value: f.Value,
		}
		if strings.Contains(f.Value, ", ") {
			parts := strings.Split(f.Value, ", ")
			if len(parts) > 1 {
				rf.IsMultiple = true
				rf.Items = parts
			}
		}
		result = append(result, rf)
	}
	return result
}

const htmlNotificationTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>New Form Submission: {{ .FormName }}</title>
</head>
<body style="margin: 0; padding: 24px 12px; background-color: #f1f5f9; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; color: #1e293b;">
  <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%" style="max-width: 620px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; border: 1px solid #e2e8f0; overflow: hidden; box-shadow: 0 1px 3px 0 rgba(0,0,0,0.05);">
    <!-- Header -->
    <tr>
      <td style="background-color: #0f172a; padding: 24px 28px;">
        <span style="display: inline-block; background-color: #38bdf8; color: #0f172a; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; padding: 3px 8px; border-radius: 4px; margin-bottom: 8px;">New Submission</span>
        <h1 style="margin: 0; color: #ffffff; font-size: 20px; font-weight: 700; line-height: 1.3;">{{ .FormName }}</h1>
        <p style="margin: 6px 0 0; color: #94a3b8; font-size: 13px;">Received {{ .SubmittedAt }}</p>
      </td>
    </tr>

    <!-- Body Container -->
    <tr>
      <td style="padding: 24px 28px;">
        {{- if .IntroText }}
        <div style="background-color: #f0f9ff; border-left: 4px solid #0284c7; padding: 12px 16px; margin-bottom: 20px; border-radius: 0 4px 4px 0;">
          <p style="margin: 0; color: #0369a1; font-size: 14px; line-height: 1.5; white-space: pre-wrap;">{{ .IntroText }}</p>
        </div>
        {{- end }}

        <!-- Fields Table -->
        <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%" style="border-collapse: collapse; border: 1px solid #e2e8f0; border-radius: 6px; overflow: hidden;">
          {{- range $i, $field := .Fields }}
          <tr style="background-color: {{ if (even $i) }}#ffffff{{ else }}#f8fafc{{ end }};">
            <td style="padding: 12px 14px; width: 32%; font-weight: 600; color: #475569; font-size: 13px; border-bottom: 1px solid #e2e8f0; vertical-align: top;">
              {{ $field.Label }}
            </td>
            <td style="padding: 12px 14px; color: #0f172a; font-size: 14px; border-bottom: 1px solid #e2e8f0; vertical-align: top; word-break: break-word;">
              {{- if $field.IsMultiple }}
                {{- range $val := $field.Items }}
                <span style="display: inline-block; background-color: #e0e7ff; color: #3730a3; padding: 2px 8px; border-radius: 4px; margin: 2px 4px 2px 0; font-size: 12px; font-weight: 500;">{{ $val }}</span>
                {{- end }}
              {{- else }}
                <div style="white-space: pre-wrap; line-height: 1.5;">{{ $field.Value }}</div>
              {{- end }}
            </td>
          </tr>
          {{- end }}
        </table>

        <!-- Metadata Section -->
        <div style="margin-top: 24px; padding-top: 16px; border-top: 1px dashed #cbd5e1;">
          <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%" style="font-size: 12px; color: #64748b; line-height: 1.6;">
            {{- if .IP }}
            <tr>
              <td style="width: 25%; font-weight: 600; padding: 2px 0;">Submitter IP:</td>
              <td style="padding: 2px 0; font-family: monospace; color: #334155;">{{ .IP }}</td>
            </tr>
            {{- end }}
            {{- if .Referer }}
            <tr>
              <td style="width: 25%; font-weight: 600; padding: 2px 0;">Page Referer:</td>
              <td style="padding: 2px 0; word-break: break-all;"><a href="{{ .Referer }}" style="color: #0284c7; text-decoration: none;">{{ .Referer }}</a></td>
            </tr>
            {{- end }}
            {{- if .UserAgent }}
            <tr>
              <td style="width: 25%; font-weight: 600; padding: 2px 0;">User Agent:</td>
              <td style="padding: 2px 0; word-break: break-all; color: #64748b;">{{ .UserAgent }}</td>
            </tr>
            {{- end }}
          </table>
        </div>
      </td>
    </tr>

    <!-- Footer -->
    <tr>
      <td style="background-color: #f8fafc; padding: 14px 28px; text-align: center; border-top: 1px solid #e2e8f0;">
        <p style="margin: 0; font-size: 11px; color: #94a3b8;">Delivered by MiForm</p>
      </td>
    </tr>
  </table>
</body>
</html>`

const textNotificationTmpl = `==================================================
NEW FORM SUBMISSION: {{ .FormName }}
==================================================
Received: {{ .SubmittedAt }}
{{- if .IntroText }}

{{ .IntroText }}
--------------------------------------------------
{{- end }}

{{ range .Fields }}
{{ .Label }}:
{{- if .IsMultiple }}
{{ range .Items }}- {{ . }}
{{ end }}
{{- else }}
{{ .Value }}
{{ end }}
{{ end }}
--------------------------------------------------
SUBMISSION METADATA:
{{- if .IP }}
- Submitter IP: {{ .IP }}
{{- end }}
{{- if .Referer }}
- Page Referer: {{ .Referer }}
{{- end }}
{{- if .UserAgent }}
- User Agent:   {{ .UserAgent }}
{{- end }}
==================================================
`

const htmlAutoresponderTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>We received your submission</title>
</head>
<body style="margin: 0; padding: 24px 12px; background-color: #f1f5f9; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; color: #1e293b;">
  <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%" style="max-width: 580px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; border: 1px solid #e2e8f0; overflow: hidden; box-shadow: 0 1px 3px 0 rgba(0,0,0,0.05);">
    <tr>
      <td style="background-color: #0f172a; padding: 20px 24px;">
        <h1 style="margin: 0; color: #ffffff; font-size: 18px; font-weight: 700;">{{ .FormName }}</h1>
      </td>
    </tr>
    <tr>
      <td style="padding: 24px; font-size: 15px; line-height: 1.6; color: #334155;">
        <div style="white-space: pre-wrap;">{{ .Message }}</div>
      </td>
    </tr>
    <tr>
      <td style="background-color: #f8fafc; padding: 12px 24px; text-align: center; border-top: 1px solid #e2e8f0;">
        <p style="margin: 0; font-size: 11px; color: #94a3b8;">{{ .FormName }}</p>
      </td>
    </tr>
  </table>
</body>
</html>`

const textAutoresponderTmpl = `Hello,

{{ .Message }}

Best regards,
{{ .FormName }}
`

var (
	parsedHTMLNotificationTmpl *template.Template
	parsedTextNotificationTmpl *textTemplate.Template
	parsedHTMLAutoresponderTmpl *template.Template
	parsedTextAutoresponderTmpl *textTemplate.Template
)

func init() {
	funcs := template.FuncMap{
		"even": func(i int) bool { return i%2 == 0 },
	}

	var err error
	parsedHTMLNotificationTmpl, err = template.New("notification_html").Funcs(funcs).Parse(htmlNotificationTmpl)
	if err != nil {
		panic(fmt.Sprintf("failed to parse htmlNotificationTmpl: %v", err))
	}

	parsedTextNotificationTmpl, err = textTemplate.New("notification_text").Parse(textNotificationTmpl)
	if err != nil {
		panic(fmt.Sprintf("failed to parse textNotificationTmpl: %v", err))
	}

	parsedHTMLAutoresponderTmpl, err = template.New("autoresponder_html").Parse(htmlAutoresponderTmpl)
	if err != nil {
		panic(fmt.Sprintf("failed to parse htmlAutoresponderTmpl: %v", err))
	}

	parsedTextAutoresponderTmpl, err = textTemplate.New("autoresponder_text").Parse(textAutoresponderTmpl)
	if err != nil {
		panic(fmt.Sprintf("failed to parse textAutoresponderTmpl: %v", err))
	}
}

// RenderNotificationHTML generates the HTML string for the notification email.
func RenderNotificationHTML(data NotificationData) (string, error) {
	if data.SubmittedAt == "" {
		data.SubmittedAt = time.Now().UTC().Format("Jan 02, 2006 15:04:05 UTC")
	}
	var buf bytes.Buffer
	if err := parsedHTMLNotificationTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderNotificationText generates the plain text string for the notification email.
func RenderNotificationText(data NotificationData) (string, error) {
	if data.SubmittedAt == "" {
		data.SubmittedAt = time.Now().UTC().Format("Jan 02, 2006 15:04:05 UTC")
	}
	var buf bytes.Buffer
	if err := parsedTextNotificationTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderAutoresponderHTML generates the HTML string for the autoresponder email.
func RenderAutoresponderHTML(data AutoresponderData) (string, error) {
	var buf bytes.Buffer
	if err := parsedHTMLAutoresponderTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderAutoresponderText generates the plain text string for the autoresponder email.
func RenderAutoresponderText(data AutoresponderData) (string, error) {
	var buf bytes.Buffer
	if err := parsedTextAutoresponderTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

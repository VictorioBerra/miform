package mailer

import (
	"strings"
	"testing"

	"miform/internal/parser"
)

func TestParseAddressList(t *testing.T) {
	raw := "admin@example.com, Support Team <support@example.com>,  sales@example.com  "
	addrs := ParseAddressList(raw)
	if len(addrs) != 3 {
		t.Fatalf("expected 3 addresses, got %d", len(addrs))
	}
	if addrs[0].Address != "admin@example.com" {
		t.Errorf("expected admin@example.com, got %s", addrs[0].Address)
	}
	if addrs[1].Name != "Support Team" || addrs[1].Address != "support@example.com" {
		t.Errorf("expected Support Team <support@example.com>, got %v", addrs[1])
	}
	if addrs[2].Address != "sales@example.com" {
		t.Errorf("expected sales@example.com, got %s", addrs[2].Address)
	}
}

func TestPrepareRenderFields(t *testing.T) {
	fields := []parser.FieldPair{
		{Name: "full_name", Label: "Full Name", Value: "John Smith"},
		{Name: "lines", Label: "Lines", Value: "Health, Life"},
	}

	rendered := PrepareRenderFields(fields)
	if len(rendered) != 2 {
		t.Fatalf("expected 2 rendered fields, got %d", len(rendered))
	}

	if rendered[0].Label != "Full Name" || rendered[0].Value != "John Smith" || rendered[0].IsMultiple {
		t.Errorf("unexpected first field: %+v", rendered[0])
	}

	if rendered[1].Label != "Lines" || !rendered[1].IsMultiple || len(rendered[1].Items) != 2 {
		t.Errorf("unexpected second field: %+v", rendered[1])
	}
}

func TestRenderNotificationHTML(t *testing.T) {
	data := NotificationData{
		FormName:  "Contact Form",
		IntroText: "Please reply within 24 hours.",
		Fields: []RenderField{
			{Label: "Name", Value: "Alice Walker"},
			{Label: "Lines", IsMultiple: true, Items: []string{"Health", "Auto"}},
		},
		IP:          "198.51.100.22",
		Referer:     "https://example.com/quote",
		UserAgent:   "Mozilla/5.0 Test",
		SubmittedAt: "Oct 17, 2026 18:00:00 UTC",
	}

	html, err := RenderNotificationHTML(data)
	if err != nil {
		t.Fatalf("failed to render HTML notification: %v", err)
	}

	if !strings.Contains(html, "Contact Form") {
		t.Errorf("expected FormName in HTML")
	}
	if !strings.Contains(html, "Please reply within 24 hours.") {
		t.Errorf("expected IntroText in HTML")
	}
	if !strings.Contains(html, "Alice Walker") {
		t.Errorf("expected Alice Walker in HTML")
	}
	if !strings.Contains(html, "Health") || !strings.Contains(html, "Auto") {
		t.Errorf("expected list values in HTML")
	}
	if !strings.Contains(html, "198.51.100.22") {
		t.Errorf("expected IP in HTML")
	}
	if !strings.Contains(html, "https://example.com/quote") {
		t.Errorf("expected Referer in HTML")
	}
}

func TestRenderNotificationText(t *testing.T) {
	data := NotificationData{
		FormName:  "Leads Form",
		IntroText: "Important lead received",
		Fields: []RenderField{
			{Label: "First Name", Value: "Bob"},
			{Label: "Services", IsMultiple: true, Items: []string{"Solar", "Batteries"}},
		},
		IP:          "203.0.113.5",
		Referer:     "https://leads.example.com",
		UserAgent:   "Go-http-client",
		SubmittedAt: "Oct 17, 2026 19:00:00 UTC",
	}

	text, err := RenderNotificationText(data)
	if err != nil {
		t.Fatalf("failed to render Text notification: %v", err)
	}

	if !strings.Contains(text, "NEW FORM SUBMISSION: Leads Form") {
		t.Errorf("expected header in text")
	}
	if !strings.Contains(text, "Important lead received") {
		t.Errorf("expected intro text in text")
	}
	if !strings.Contains(text, "First Name:\nBob") {
		t.Errorf("expected First Name in text")
	}
	if !strings.Contains(text, "- Solar\n- Batteries") {
		t.Errorf("expected list items in text")
	}
	if !strings.Contains(text, "Submitter IP: 203.0.113.5") {
		t.Errorf("expected IP in text")
	}
}

func TestRenderAutoresponder(t *testing.T) {
	data := AutoresponderData{
		FormName: "Contact Form",
		Message:  "Thanks for contacting us. Our team will review your request.",
	}

	html, err := RenderAutoresponderHTML(data)
	if err != nil {
		t.Fatalf("failed to render autoresponder HTML: %v", err)
	}
	if !strings.Contains(html, "Contact Form") || !strings.Contains(html, "team will review") {
		t.Errorf("unexpected autoresponder HTML: %s", html)
	}

	text, err := RenderAutoresponderText(data)
	if err != nil {
		t.Fatalf("failed to render autoresponder text: %v", err)
	}
	if !strings.Contains(text, "Contact Form") || !strings.Contains(text, "team will review") {
		t.Errorf("unexpected autoresponder text: %s", text)
	}
}

func TestExtractReplyTo(t *testing.T) {
	// Case 1: parsed.Controls.ReplyTo is set
	p1 := &parser.ParsedForm{
		Controls: parser.ControlFields{ReplyTo: "direct@example.com"},
	}
	if got := ExtractReplyTo(p1); got != "direct@example.com" {
		t.Errorf("expected direct@example.com, got %s", got)
	}

	// Case 2: Data has lowercase "email"
	p2 := &parser.ParsedForm{
		Data: map[string]any{"email": "custom@example.com"},
	}
	if got := ExtractReplyTo(p2); got != "custom@example.com" {
		t.Errorf("expected custom@example.com, got %s", got)
	}

	// Case 3: Data has uppercase "EMAIL"
	p3 := &parser.ParsedForm{
		Data: map[string]any{"EMAIL": "upper@example.com"},
	}
	if got := ExtractReplyTo(p3); got != "upper@example.com" {
		t.Errorf("expected upper@example.com, got %s", got)
	}

	// Case 4: No email provided
	p4 := &parser.ParsedForm{
		Data: map[string]any{"name": "No Email User"},
	}
	if got := ExtractReplyTo(p4); got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

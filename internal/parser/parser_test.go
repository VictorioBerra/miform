package parser

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestParseURLEncodedForm(t *testing.T) {
	// Simulate URL-encoded form submission
	rawBody := "access_key=test-contact-key&name=Jane+Doe&company=Solar+Tech&email=jane%40example.com&phone=555-1234&interest=Solar+thermal&message=Need+300+collectors&redirect=https%3A%2F%2Fexample.com%2Fthanks"
	req, _ := http.NewRequest("POST", "/submit", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("CF-Connecting-IP", "203.0.113.195")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://example.com/contact")

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Controls.AccessKey != "test-contact-key" {
		t.Errorf("expected access_key 'test-contact-key', got '%s'", parsed.Controls.AccessKey)
	}
	if parsed.Controls.Redirect != "https://example.com/thanks" {
		t.Errorf("expected redirect 'https://example.com/thanks', got '%s'", parsed.Controls.Redirect)
	}
	if parsed.Controls.ReplyTo != "jane@example.com" {
		t.Errorf("expected reply_to 'jane@example.com', got '%s'", parsed.Controls.ReplyTo)
	}
	if parsed.Metadata.IP != "203.0.113.195" {
		t.Errorf("expected IP '203.0.113.195', got '%s'", parsed.Metadata.IP)
	}
	if parsed.IsAJAX {
		t.Errorf("expected IsAJAX to be false")
	}

	// Verify custom fields order and labels
	expectedFields := []struct {
		Name  string
		Label string
		Value string
	}{
		{"name", "Name", "Jane Doe"},
		{"company", "Company", "Solar Tech"},
		{"email", "Email", "jane@example.com"},
		{"phone", "Phone", "555-1234"},
		{"interest", "Interest", "Solar thermal"},
		{"message", "Message", "Need 300 collectors"},
	}

	if len(parsed.Fields) != len(expectedFields) {
		t.Fatalf("expected %d fields, got %d", len(expectedFields), len(parsed.Fields))
	}

	for i, exp := range expectedFields {
		f := parsed.Fields[i]
		if f.Name != exp.Name || f.Label != exp.Label || f.Value != exp.Value {
			t.Errorf("field %d: expected {%s, %s, %s}, got {%s, %s, %s}", i, exp.Name, exp.Label, exp.Value, f.Name, f.Label, f.Value)
		}
	}
}

func TestMultiValueCheckboxes(t *testing.T) {
	// Simulate form submission with repeated checkbox name "lines"
	rawBody := "access_key=test-leads-key&name=Bob+Smith&agency=Acme+Insurance&email=bob%40acme.com&territory=Missouri&lines=health&lines=liability&lines=auto&message=Check+territory"
	req, _ := http.NewRequest("POST", "/submit", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify "lines" aggregated into single comma-separated value
	foundLines := false
	for _, f := range parsed.Fields {
		if f.Name == "lines" {
			foundLines = true
			if f.Label != "Lines" {
				t.Errorf("expected label 'Lines', got '%s'", f.Label)
			}
			if f.Value != "health, liability, auto" {
				t.Errorf("expected value 'health, liability, auto', got '%s'", f.Value)
			}
		}
	}

	if !foundLines {
		t.Errorf("expected 'lines' field in parsed output")
	}
}

func TestSpecialFields(t *testing.T) {
	// Form has 'msg' for message and camelCase inputs
	rawBody := "access_key=test-agency-key&name=Alice&phone=555-4321&email=alice%40example.com&topic=Employee+benefit+programs&msg=Looking+for+census+details"
	req, _ := http.NewRequest("POST", "/submit", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msgLabel string
	for _, f := range parsed.Fields {
		if f.Name == "msg" {
			msgLabel = f.Label
		}
	}

	if msgLabel != "Message" {
		t.Errorf("expected 'msg' to humanize to 'Message', got '%s'", msgLabel)
	}
}

func TestJSONSubmissionAndAJAX(t *testing.T) {
	jsonPayload := `{
		"access_key": "test-agency-key",
		"name": "Jane",
		"email": "jane@example.com",
		"topic": "Small group health insurance",
		"msg": "Quote please"
	}`

	req, _ := http.NewRequest("POST", "/submit", strings.NewReader(jsonPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !parsed.IsAJAX {
		t.Errorf("expected IsAJAX to be true")
	}
	if parsed.Controls.AccessKey != "test-agency-key" {
		t.Errorf("expected access_key 'test-agency-key', got '%s'", parsed.Controls.AccessKey)
	}
	if parsed.Controls.ReplyTo != "jane@example.com" {
		t.Errorf("expected reply_to 'jane@example.com', got '%s'", parsed.Controls.ReplyTo)
	}
}

func TestNestedSubmissionsAPIJSON(t *testing.T) {
	// Web3Forms Submissions API format
	jsonPayload := `{
		"form_id": "0a1b2c3d",
		"subject": "New CRM Lead",
		"fields": {
			"full_name": "Sarah Connor",
			"company_name": "Cyberdyne",
			"email": "sarah@resistance.org"
		}
	}`

	req, _ := http.NewRequest("POST", "/api/submissions", strings.NewReader(jsonPayload))
	req.Header.Set("Content-Type", "application/json")

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Controls.AccessKey != "0a1b2c3d" {
		t.Errorf("expected access_key '0a1b2c3d', got '%s'", parsed.Controls.AccessKey)
	}
	if parsed.Controls.Subject != "New CRM Lead" {
		t.Errorf("expected subject 'New CRM Lead', got '%s'", parsed.Controls.Subject)
	}
	if parsed.Controls.ReplyTo != "sarah@resistance.org" {
		t.Errorf("expected reply_to 'sarah@resistance.org', got '%s'", parsed.Controls.ReplyTo)
	}

	// Verify labels for snake_case
	labels := make(map[string]string)
	for _, f := range parsed.Fields {
		labels[f.Name] = f.Label
	}

	if labels["full_name"] != "Full Name" {
		t.Errorf("expected 'Full Name', got '%s'", labels["full_name"])
	}
	if labels["company_name"] != "Company Name" {
		t.Errorf("expected 'Company Name', got '%s'", labels["company_name"])
	}
}

func TestMultipartFormData(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("access_key", "test-key")
	_ = writer.WriteField("botcheck", "")
	_ = writer.WriteField("first_name", "John")
	_ = writer.WriteField("phone_number", "555-9876")
	_ = writer.WriteField("email", "john@test.com")
	_ = writer.Close()

	req, _ := http.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Controls.AccessKey != "test-key" {
		t.Errorf("expected access_key 'test-key', got '%s'", parsed.Controls.AccessKey)
	}
	if len(parsed.Fields) != 3 {
		t.Fatalf("expected 3 custom fields, got %d", len(parsed.Fields))
	}
	if parsed.Fields[0].Label != "First Name" {
		t.Errorf("expected 'First Name', got '%s'", parsed.Fields[0].Label)
	}
}

func TestHoneypotDetection(t *testing.T) {
	rawBody := "access_key=test&botcheck=1&name=SpamBot&email=bot@spammer.com"
	req, _ := http.NewRequest("POST", "/submit", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Controls.Botcheck != "1" {
		t.Errorf("expected botcheck '1', got '%s'", parsed.Controls.Botcheck)
	}
}

func TestTurnstileResponseExtraction(t *testing.T) {
	rawBody := "access_key=test&name=User&cf-turnstile-response=cf-token-123"
	req, _ := http.NewRequest("POST", "/submit", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parsed, err := ParseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Controls.TurnstileResponse != "cf-token-123" {
		t.Errorf("expected turnstile_response 'cf-token-123', got '%s'", parsed.Controls.TurnstileResponse)
	}

	for _, f := range parsed.Fields {
		if f.Name == "cf-turnstile-response" {
			t.Errorf("cf-turnstile-response should not be in Fields slice")
		}
	}
}

func TestHasHTMLLink(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Hello, I need a quote.", false},
		{"Visit our website at https://example.com for info.", false},
		{"option A < option B", false},
		{"<a href=elitevisathailand.org/ru/>elitevisathailand.org/ru/</a>", true},
		{`<a href="https://spam.com">click here</a>`, true},
		{`<a target='_blank' href='http://spam.com'>`, true},
		{`<A HREF='http://spam.com'>`, true},
		{`check this out: </a>`, true},
		{"Для тех, кто планирует долгосрочное проживание в Таиланде, Elite Visa — отличное решение. Стоимость программы зависит от её длительности, советую ознакомиться с информацией elitevisathailand.org, где thailand elite visa как получить <a href=elitevisathailand.org/ru/>elitevisathailand.org/ru/</a> можно найти актуальные данные.", true},
	}

	for _, tt := range tests {
		got := HasHTMLLink(tt.input)
		if got != tt.expected {
			t.Errorf("HasHTMLLink(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestContainsHTMLLink(t *testing.T) {
	cleanForm := &ParsedForm{
		Fields: []FieldPair{
			{Name: "name", Label: "Name", Value: "John Doe"},
			{Name: "message", Label: "Message", Value: "Hello there"},
		},
		Data: map[string]any{"name": "John Doe", "message": "Hello there"},
	}
	if cleanForm.ContainsHTMLLink() {
		t.Errorf("expected clean form not to contain HTML links")
	}

	spamForm := &ParsedForm{
		Fields: []FieldPair{
			{Name: "name", Label: "Name", Value: "Spammer"},
			{Name: "message", Label: "Message", Value: "Check <a href=http://spam.com>link</a>"},
		},
		Data: map[string]any{"name": "Spammer", "message": "Check <a href=http://spam.com>link</a>"},
	}
	if !spamForm.ContainsHTMLLink() {
		t.Errorf("expected spam form to contain HTML links")
	}
}

func TestHumanizeLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"first_name", "First Name"},
		{"phoneNumber", "Phone Number"},
		{"postal-code", "Postal Code"},
		{"id", "ID"},
		{"url", "URL"},
		{"ip", "IP"},
		{"msg", "Message"},
		{"Already Capitalized", "Already Capitalized"},
		{"alllowercase with spaces", "Alllowercase With Spaces"},
	}

	for _, tt := range tests {
		got := HumanizeLabel(tt.input)
		if got != tt.expected {
			t.Errorf("HumanizeLabel(%q) = %q; expected %q", tt.input, got, tt.expected)
		}
	}
}

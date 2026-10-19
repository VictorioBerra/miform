package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"

	"miform/internal/api"
	"miform/internal/service"
	"miform/internal/turnstile"
)

const testDataDir = "../../pb_data"

func seedTestForms(app core.App) error {
	formsColl, err := app.FindCollectionByNameOrId("forms")
	if err != nil {
		return err
	}

	sites := []map[string]any{
		{
			"access_key":       "test-contact-key",
			"name":             "Contact Form",
			"website_url":      "https://example.com/contact",
			"allowed_domains":  "example.com, *.example.com, localhost:*",
			"recipient_emails": "recipient@example.com",
			"email_subject":    "New Inquiry",
			"sender_name":      "Website Notifications",
			"intro_text":       "You received a new inquiry:",
			"redirect_url":     "https://example.com/thanks",
			"is_active":        true,
		},
		{
			"access_key":       "test-leads-key",
			"name":             "Leads Form",
			"website_url":      "https://leads.example.com",
			"allowed_domains":  "leads.example.com, *.example.com, localhost:*",
			"recipient_emails": "recipient@example.com",
			"email_subject":    "New Lead",
			"sender_name":      "Leads Notifications",
			"intro_text":       "You received a new lead:",
			"redirect_url":     "https://leads.example.com/thanks",
			"is_active":        true,
		},
		{
			"access_key":       "test-agency-key",
			"name":             "Quote Form",
			"website_url":      "https://quote.example.com",
			"allowed_domains":  "quote.example.com, *.example.com, localhost:*",
			"recipient_emails": "quotes@example.com",
			"email_subject":    "New Quote Request",
			"sender_name":      "Quote Notifications",
			"intro_text":       "You received a new quote request:",
			"redirect_url":     "https://quote.example.com/#quote",
			"is_active":        true,
		},
	}

	for _, site := range sites {
		record := core.NewRecord(formsColl)
		record.Load(site)
		if err := app.Save(record); err != nil {
			return err
		}
	}
	return nil
}

func setupTestApp(t testing.TB) (*tests.TestApp, *api.Handler, *service.SubmissionService) {
	app, err := tests.NewTestApp(testDataDir)
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}

	if err := seedTestForms(app); err != nil {
		t.Fatalf("failed to seed test forms: %v", err)
	}

	svc := service.New(app)
	handler := api.NewHandler(svc)

	app.OnRecordAfterCreateSuccess("submissions").BindFunc(func(e *core.RecordEvent) error {
		return svc.ProcessRecord(e.Record)
	})

	return app, handler, svc
}

func TestURLEncodedSubmission(t *testing.T) {
	app, handler, _ := setupTestApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "URL-encoded submission with redirect",
		Method:                http.MethodPost,
		URL:                   "/submit",
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		Body: strings.NewReader(
			"access_key=test-contact-key&name=Jane+Doe&company=Solar+Tech&email=jane%40example.com&phone=555-1234&interest=Solar+thermal&message=Need+300+collectors&redirect=https%3A%2F%2Fexample.com%2Fthanks",
		),
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Referer":      "https://example.com/contact",
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.POST("/submit", handler.Submit)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if res.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected 303 See Other, got %d", res.StatusCode)
			}
			loc := res.Header.Get("Location")
			if loc != "https://example.com/thanks" {
				t.Errorf("expected redirect location https://example.com/thanks, got %s", loc)
			}

			if app.TestMailer.TotalSend() != 1 {
				t.Fatalf("expected 1 email sent, got %d", app.TestMailer.TotalSend())
			}

			lastMsg := app.TestMailer.LastMessage()
			if len(lastMsg.To) == 0 || lastMsg.To[0].Address != "recipient@example.com" {
				t.Errorf("expected To: recipient@example.com, got %v", lastMsg.To)
			}
			if lastMsg.Headers["Reply-To"] != "jane@example.com" {
				t.Errorf("expected Reply-To: jane@example.com, got %s", lastMsg.Headers["Reply-To"])
			}
			if !strings.Contains(lastMsg.HTML, "Need 300 collectors") {
				t.Errorf("expected message body in email HTML")
			}

			// Verify record in submissions collection
			subRecord, err := app.FindFirstRecordByData("submissions", "reply_to", "jane@example.com")
			if err != nil {
				t.Fatalf("failed to find submission in db: %v", err)
			}
			if subRecord.GetString("status") != "sent" {
				t.Errorf("expected status 'sent', got %s", subRecord.GetString("status"))
			}
		},
		ExpectedStatus: http.StatusSeeOther,
	}

	scenario.Test(t)
}

func TestJSONAJAXSubmission(t *testing.T) {
	app, handler, _ := setupTestApp(t)
	defer app.Cleanup()

	payload := `{
		"access_key": "test-leads-key",
		"first_name": "Bob",
		"last_name": "Smith",
		"email": "bob@leads.com",
		"phone": "555-9876",
		"company_name": "Acme Solar",
		"services": ["Commercial", "Financing"]
	}`

	scenario := tests.ApiScenario{
		Name:                  "AJAX JSON submission",
		Method:                http.MethodPost,
		URL:                   "/submit",
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		Body:                  strings.NewReader(payload),
		Headers: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
			"Referer":      "https://leads.example.com/",
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.POST("/submit", handler.Submit)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if res.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d", res.StatusCode)
			}

			bodyBytes, _ := io.ReadAll(res.Body)
			var respMap map[string]any
			if err := json.Unmarshal(bodyBytes, &respMap); err != nil {
				t.Fatalf("failed to parse JSON response: %v", err)
			}

			if respMap["success"] != true {
				t.Errorf("expected success: true, got %v", respMap["success"])
			}

			if app.TestMailer.TotalSend() != 1 {
				t.Fatalf("expected 1 email sent, got %d", app.TestMailer.TotalSend())
			}

			lastMsg := app.TestMailer.LastMessage()
			if len(lastMsg.To) == 0 || lastMsg.To[0].Address != "recipient@example.com" {
				t.Errorf("expected To: recipient@example.com, got %v", lastMsg.To)
			}
			if lastMsg.Headers["Reply-To"] != "bob@leads.com" {
				t.Errorf("expected Reply-To: bob@leads.com, got %s", lastMsg.Headers["Reply-To"])
			}
			if !strings.Contains(lastMsg.HTML, "Commercial") || !strings.Contains(lastMsg.HTML, "Financing") {
				t.Errorf("expected services in email HTML")
			}
		},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"success":true`},
	}

	scenario.Test(t)
}

func TestPathIDSubmission(t *testing.T) {
	app, handler, _ := setupTestApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "Path /submit/{id} urlencoded submission",
		Method:                http.MethodPost,
		URL:                   "/submit/test-agency-key",
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		Body: strings.NewReader(
			"first_name=Alice&last_name=Walker&email=alice%40example.com&phone=555-4321&company=Walker+LLC&lines=Property&lines=Liability&coverage_details=Need+general+liability&best_time_to_reach=Morning&preferred_contact_method=Email",
		),
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Referer":      "https://quote.example.com/",
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.POST("/submit/{id}", handler.Submit)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if res.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected 303 See Other, got %d", res.StatusCode)
			}
			loc := res.Header.Get("Location")
			expectedRedirect := "https://quote.example.com/#quote"
			if loc != expectedRedirect {
				t.Errorf("expected redirect %s, got %s", expectedRedirect, loc)
			}

			if app.TestMailer.TotalSend() != 1 {
				t.Fatalf("expected 1 email sent, got %d", app.TestMailer.TotalSend())
			}

			lastMsg := app.TestMailer.LastMessage()
			if len(lastMsg.To) == 0 || lastMsg.To[0].Address != "quotes@example.com" {
				t.Errorf("expected To: quotes@example.com, got %v", lastMsg.To)
			}
			if lastMsg.Headers["Reply-To"] != "alice@example.com" {
				t.Errorf("expected Reply-To: alice@example.com, got %s", lastMsg.Headers["Reply-To"])
			}
		},
		ExpectedStatus: http.StatusSeeOther,
	}

	scenario.Test(t)
}

func TestHoneypotSpamHandling(t *testing.T) {
	app, handler, _ := setupTestApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "Honeypot filled submission marked as spam",
		Method:                http.MethodPost,
		URL:                   "/submit",
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		Body: strings.NewReader(
			"access_key=test-contact-key&name=BotUser&email=spambot%40evil.com&botcheck=spam_value&message=Spam",
		),
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Referer":      "https://example.com/contact",
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.POST("/submit", handler.Submit)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			// Zero emails sent!
			if app.TestMailer.TotalSend() != 0 {
				t.Fatalf("expected 0 emails sent for honeypot spam, got %d", app.TestMailer.TotalSend())
			}

			// Record created with status = "spam"
			spamRecord, err := app.FindFirstRecordByData("submissions", "reply_to", "spambot@evil.com")
			if err != nil {
				t.Fatalf("expected spam record in db, got error: %v", err)
			}
			if spamRecord.GetString("status") != "spam" {
				t.Errorf("expected status 'spam', got %s", spamRecord.GetString("status"))
			}
		},
		ExpectedStatus: http.StatusSeeOther,
	}

	scenario.Test(t)
}

func TestDisallowedDomainRejection(t *testing.T) {
	app, handler, _ := setupTestApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "Submission from disallowed domain rejected with 403",
		Method:                http.MethodPost,
		URL:                   "/submit",
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		Body: strings.NewReader(
			`{"access_key":"test-contact-key","name":"Hacker","email":"hack@attacker.com"}`,
		),
		Headers: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
			"Referer":      "https://attacker.evil.com/phish",
			"Origin":       "https://attacker.evil.com",
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.POST("/submit", handler.Submit)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if app.TestMailer.TotalSend() != 0 {
				t.Fatalf("expected 0 emails sent, got %d", app.TestMailer.TotalSend())
			}
		},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{`"success":false`},
	}

	scenario.Test(t)
}

func TestRawHTMLLinkDenial(t *testing.T) {
	app, handler, _ := setupTestApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "Submission with raw HTML links denied with 400",
		Method:                http.MethodPost,
		URL:                   "/submit",
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		Body: strings.NewReader(
			`{"access_key":"test-contact-key","name":"Kt","email":"spammer@example.com","msg":"Check our site <a href=elitevisathailand.org/ru/>elitevisathailand.org</a> for details."}`,
		),
		Headers: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
			"Referer":      "https://example.com/contact",
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.POST("/submit", handler.Submit)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			// Zero emails sent!
			if app.TestMailer.TotalSend() != 0 {
				t.Fatalf("expected 0 emails sent for raw HTML spam, got %d", app.TestMailer.TotalSend())
			}

			// Record created with status = "spam"
			spamRecord, err := app.FindFirstRecordByData("submissions", "reply_to", "spammer@example.com")
			if err != nil {
				t.Fatalf("expected spam record in db, got error: %v", err)
			}
			if spamRecord.GetString("status") != "spam" {
				t.Errorf("expected status 'spam', got %s", spamRecord.GetString("status"))
			}
			if spamRecord.GetString("error_message") != "rejected: raw HTML links are not allowed" {
				t.Errorf("expected error_message 'rejected: raw HTML links are not allowed', got %s", spamRecord.GetString("error_message"))
			}
		},
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{`"success":false`, "HTML links are not allowed"},
	}

	scenario.Test(t)
}

func TestSuccessConfirmationPage(t *testing.T) {
	app, handler, _ := setupTestApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "GET /submit/success renders HTML confirmation page",
		Method:                http.MethodGet,
		URL:                   "/submit/success",
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.GET("/submit/success", handler.Success)
		},
		ExpectedStatus: http.StatusOK,
		ExpectedContent: []string{
			"Thank you!",
			"Your message has been received successfully",
		},
	}

	scenario.Test(t)
}

func TestDirectRecordCreationParity(t *testing.T) {
	app, _, svc := setupTestApp(t)
	defer app.Cleanup()

	form, err := svc.ResolveForm("test-contact-key")
	if err != nil {
		t.Fatalf("failed to resolve form: %v", err)
	}

	submissionsColl, err := app.FindCollectionByNameOrId("submissions")
	if err != nil {
		t.Fatalf("failed to find submissions collection: %v", err)
	}

	record := core.NewRecord(submissionsColl)
	record.Set("form", form.Id)
	record.Set("data", map[string]any{"name": "Direct User", "email": "direct@example.com"})
	record.Set("status", "pending")

	if err := app.Save(record); err != nil {
		t.Fatalf("failed to save record: %v", err)
	}

	if app.TestMailer.TotalSend() != 1 {
		t.Fatalf("expected exactly 1 email sent for direct record creation, got %d", app.TestMailer.TotalSend())
	}

	savedRecord, err := app.FindRecordById("submissions", record.Id)
	if err != nil {
		t.Fatalf("failed to find saved record: %v", err)
	}
	if savedRecord.GetString("status") != "sent" {
		t.Errorf("expected status 'sent', got '%s'", savedRecord.GetString("status"))
	}
}

type mockTurnstileVerifier struct {
	validTokens map[string]bool
}

func (m *mockTurnstileVerifier) Verify(ctx context.Context, token, clientIP, secret string) (*turnstile.Response, error) {
	if m.validTokens[token] {
		return &turnstile.Response{Success: true}, nil
	}
	return &turnstile.Response{Success: false, ErrorCodes: []string{"invalid-input-response"}}, nil
}

func TestTurnstileVerification(t *testing.T) {
	app, handler, svc := setupTestApp(t)
	defer app.Cleanup()

	// Configure mock Turnstile verifier
	mockVerifier := &mockTurnstileVerifier{
		validTokens: map[string]bool{"valid-turnstile-token": true},
	}
	svc.WithTurnstileVerifier(mockVerifier)

	// Enable Turnstile on test-agency-key form
	form, err := svc.ResolveForm("test-agency-key")
	if err != nil {
		t.Fatalf("failed to resolve form: %v", err)
	}
	form.Set("turnstile_enabled", true)
	form.Set("turnstile_secret_key", "form-specific-secret")
	if err := app.Save(form); err != nil {
		t.Fatalf("failed to enable turnstile on form: %v", err)
	}

	t.Run("fails when token is missing", func(t *testing.T) {
		app.TestMailer.Reset()

		scenario := tests.ApiScenario{
			Name:                  "Turnstile missing token rejected with 400",
			Method:                http.MethodPost,
			URL:                   "/submit",
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
			DisableTestAppCleanup: true,
			Body: strings.NewReader(
				`{"access_key":"test-agency-key","name":"Alice","email":"alice@example.com"}`,
			),
			Headers: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
				"Referer":      "https://quote.example.com/",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.POST("/submit", handler.Submit)
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if app.TestMailer.TotalSend() != 0 {
					t.Fatalf("expected 0 emails sent, got %d", app.TestMailer.TotalSend())
				}
			},
			ExpectedStatus:  http.StatusBadRequest,
			ExpectedContent: []string{`"success":false`, "missing turnstile token"},
		}

		scenario.Test(t)
	})

	t.Run("fails when token is invalid", func(t *testing.T) {
		app.TestMailer.Reset()

		scenario := tests.ApiScenario{
			Name:                  "Turnstile invalid token rejected with 400",
			Method:                http.MethodPost,
			URL:                   "/submit",
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
			DisableTestAppCleanup: true,
			Body: strings.NewReader(
				`{"access_key":"test-agency-key","name":"Alice","email":"alice@example.com","cf-turnstile-response":"bogus-token"}`,
			),
			Headers: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
				"Referer":      "https://quote.example.com/",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.POST("/submit", handler.Submit)
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if app.TestMailer.TotalSend() != 0 {
					t.Fatalf("expected 0 emails sent, got %d", app.TestMailer.TotalSend())
				}
			},
			ExpectedStatus:  http.StatusBadRequest,
			ExpectedContent: []string{`"success":false`, "invalid token"},
		}

		scenario.Test(t)
	})

	t.Run("succeeds when valid token is provided", func(t *testing.T) {
		app.TestMailer.Reset()

		scenario := tests.ApiScenario{
			Name:                  "Turnstile valid token succeeds with 200",
			Method:                http.MethodPost,
			URL:                   "/submit",
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
			DisableTestAppCleanup: true,
			Body: strings.NewReader(
				`{"access_key":"test-agency-key","name":"Alice","email":"alice@example.com","cf-turnstile-response":"valid-turnstile-token"}`,
			),
			Headers: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
				"Referer":      "https://quote.example.com/",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.POST("/submit", handler.Submit)
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if app.TestMailer.TotalSend() != 1 {
					t.Fatalf("expected exactly 1 email sent, got %d", app.TestMailer.TotalSend())
				}
			},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{`"success":true`},
		}

		scenario.Test(t)
	})
}

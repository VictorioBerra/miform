package turnstile_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"miform/internal/turnstile"
)

func TestTurnstileVerify_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if r.Form.Get("secret") != "test-secret" {
			t.Errorf("expected secret 'test-secret', got '%s'", r.Form.Get("secret"))
		}
		if r.Form.Get("response") != "valid-token" {
			t.Errorf("expected response 'valid-token', got '%s'", r.Form.Get("response"))
		}
		if r.Form.Get("remoteip") != "1.2.3.4" {
			t.Errorf("expected remoteip '1.2.3.4', got '%s'", r.Form.Get("remoteip"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "challenge_ts": "2026-10-20T16:00:00.000Z", "hostname": "example.com"}`))
	}))
	defer ts.Close()

	client := turnstile.New(ts.Client()).WithVerifyURL(ts.URL)
	res, err := client.Verify(context.Background(), "valid-token", "1.2.3.4", "test-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success: true, got false")
	}
	if res.Hostname != "example.com" {
		t.Errorf("expected hostname 'example.com', got '%s'", res.Hostname)
	}
}

func TestTurnstileVerify_Failed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": false, "error-codes": ["invalid-input-response"]}`))
	}))
	defer ts.Close()

	client := turnstile.New(ts.Client()).WithVerifyURL(ts.URL)
	res, err := client.Verify(context.Background(), "invalid-token", "1.2.3.4", "test-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Errorf("expected success: false, got true")
	}
	if len(res.ErrorCodes) != 1 || res.ErrorCodes[0] != "invalid-input-response" {
		t.Errorf("expected error code 'invalid-input-response', got %v", res.ErrorCodes)
	}
}

func TestTurnstileVerify_MissingParams(t *testing.T) {
	client := turnstile.New(nil)

	if _, err := client.Verify(context.Background(), "", "1.2.3.4", "secret"); err == nil {
		t.Errorf("expected error for empty token")
	}

	if _, err := client.Verify(context.Background(), "token", "1.2.3.4", ""); err == nil {
		t.Errorf("expected error for empty secret")
	}
}

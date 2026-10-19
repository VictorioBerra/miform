package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultVerifyURL is Cloudflare's Turnstile siteverify API endpoint.
const DefaultVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Response represents Cloudflare's siteverify API JSON response.
type Response struct {
	Success     bool      `json:"success"`
	ErrorCodes  []string  `json:"error-codes"`
	ChallengeTS time.Time `json:"challenge_ts"`
	Hostname    string    `json:"hostname"`
	Action      string    `json:"action"`
	Cdata       string    `json:"cdata"`
}

// Verifier defines the interface for validating Turnstile response tokens.
type Verifier interface {
	Verify(ctx context.Context, token, clientIP, secret string) (*Response, error)
}

// Client implements Verifier using an HTTP client.
type Client struct {
	httpClient *http.Client
	verifyURL  string
}

// New creates a new Turnstile verification client.
func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		verifyURL:  DefaultVerifyURL,
	}
}

// WithVerifyURL overrides the endpoint URL (primarily for testing).
func (c *Client) WithVerifyURL(urlStr string) *Client {
	c.verifyURL = urlStr
	return c
}

// Verify sends the token, secret, and optional client IP to Cloudflare's siteverify endpoint.
func (c *Client) Verify(ctx context.Context, token, clientIP, secret string) (*Response, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("missing turnstile secret key")
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("missing turnstile response token")
	}

	data := url.Values{}
	data.Set("secret", secret)
	data.Set("response", token)
	if clientIP != "" {
		data.Set("remoteip", clientIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.verifyURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create turnstile request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("turnstile request failed: %w", err)
	}
	defer resp.Body.Close()

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode turnstile response: %w", err)
	}

	return &result, nil
}

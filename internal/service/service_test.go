package service

import (
	"testing"
)

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name           string
		allowedDomains string
		referer        string
		origin         string
		expected       bool
	}{
		{
			name:           "empty allowed domains allows any",
			allowedDomains: "",
			referer:        "https://anywhere.com/form",
			origin:         "https://anywhere.com",
			expected:       true,
		},
		{
			name:           "wildcard star allows any",
			allowedDomains: "*",
			referer:        "https://randomsite.org/index.html",
			origin:         "",
			expected:       true,
		},
		{
			name:           "exact domain match via referer",
			allowedDomains: "app.example.com, mysite.com",
			referer:        "https://app.example.com/site/#contact",
			origin:         "",
			expected:       true,
		},
		{
			name:           "exact domain match via origin",
			allowedDomains: "leads.example.com",
			referer:        "",
			origin:         "https://leads.example.com",
			expected:       true,
		},
		{
			name:           "wildcard subdomain match",
			allowedDomains: "*.example.com",
			referer:        "https://sub.example.com/contact",
			origin:         "",
			expected:       true,
		},
		{
			name:           "localhost port wildcard match",
			allowedDomains: "localhost:*",
			referer:        "http://localhost:8080/test",
			origin:         "http://localhost:8080",
			expected:       true,
		},
		{
			name:           "disallowed domain rejected",
			allowedDomains: "mysite.com",
			referer:        "https://evil-hacker.com/phishing",
			origin:         "https://evil-hacker.com",
			expected:       false,
		},
		{
			name:           "empty referer and origin rejected when restrictions exist",
			allowedDomains: "mysite.com",
			referer:        "",
			origin:         "",
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateDomain(tt.allowedDomains, tt.referer, tt.origin)
			if got != tt.expected {
				t.Errorf("ValidateDomain(%q, %q, %q) = %v; want %v",
					tt.allowedDomains, tt.referer, tt.origin, got, tt.expected)
			}
		})
	}
}

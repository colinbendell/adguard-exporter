package worker

import (
	"testing"
)

func TestGetETLDPlusOne(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "subdomain of .com domain",
			input:    "www.example.com",
			expected: "example.com",
		},
		{
			name:     "multiple subdomains",
			input:    "api.v2.example.com",
			expected: "example.com",
		},
		{
			name:     "domain with trailing dot",
			input:    "www.example.com.",
			expected: "example.com",
		},
		{
			name:     "already eTLD+1",
			input:    "example.com",
			expected: "example.com",
		},
		{
			name:     "co.uk TLD",
			input:    "www.example.co.uk",
			expected: "example.co.uk",
		},
		{
			name:     "github.io domain",
			input:    "user.github.io",
			expected: "user.github.io",
		},
		{
			name:     "IP address",
			input:    "192.168.1.1",
			expected: "192.168.1.1",
		},
		{
			name:     "IPv6 address",
			input:    "2001:db8:85a3:0:0:8a2e:370:7334",
			expected: "2001:db8:85a3:0:0:8a2e:370:7334",
		},
		{
			name:     "localhost",
			input:    "localhost",
			expected: "localhost",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single label domain",
			input:    "internal",
			expected: "internal",
		},
		{
			name:     "subdomain of .org domain",
			input:    "subdomain.example.org",
			expected: "example.org",
		},
		{
			name:     "complex public suffix",
			input:    "www.example.gov.uk",
			expected: "example.gov.uk",
		},
		{
			name:     "custom gTLD",
			input:    "dns.google",
			expected: "dns.google",
		},
		{
			name:     "3p hosts",
			input:    "shoesbycolin.myshopify.com",
			expected: "shoesbycolin.myshopify.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getETLDPlusOne(tt.input)
			if result != tt.expected {
				t.Errorf("getETLDPlusOne(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

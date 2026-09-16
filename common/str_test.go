package common

import (
	"strings"
	"testing"
)

func TestMaskSensitiveInfo(t *testing.T) {
	googleKey := "AIza" + strings.Repeat("A", 35)    // Google API key: AIza + 35 chars
	awsKey := "AKIAIOSFODNN7EXAMPLE"                 // AWS docs example: AKIA + 16 uppercase alphanumerics
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + // Bearer token containing dots
		"eyJzdWIiOiIxMjM0NTY3ODkwIn0." +
		"dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "url",
			input:    "request failed for https://api.test.org/v1/users/123?key=secret",
			expected: "request failed for https://***.org/***/***/***?key=***",
		},
		{
			name:     "url with subdomain and country code tld",
			input:    "see https://sub.domain.co.uk/path/to/resource",
			expected: "see https://***.***.co.uk/***/***/***",
		},
		{
			name:     "bare domains",
			input:    "mail from user@api.openai.com or www.example.co.uk",
			expected: "mail from user@***.***.com or ***.***.co.uk",
		},
		{
			name:     "ipv4",
			input:    "connect to 192.168.1.100:8080 timed out",
			expected: "connect to ***.***.***.***:8080 timed out",
		},
		{
			name:     "api key with prefix",
			input:    "api_key:" + googleKey,
			expected: "api_key:***",
		},
		{
			name:     "sk token",
			input:    "auth failed with sk-abcd1234efgh for request",
			expected: "auth failed with sk-abcd******** for request",
		},
		{
			name:     "sk anthropic style token",
			input:    "key sk-ant-api03-abcdefghijklmnop leaked",
			expected: "key sk-ant******** leaked",
		},
		{
			name:     "sk project style token",
			input:    "sk-proj-abcdefghijklmnopqrstuvwxyz",
			expected: "sk-proj********",
		},
		{
			name:     "google api key",
			input:    "key " + googleKey + " rejected",
			expected: "key AIza******** rejected",
		},
		{
			name:     "aws access key id",
			input:    "credential " + awsKey + " exposed",
			expected: "credential AKIA******** exposed",
		},
		{
			name:     "github personal token",
			input:    "token ghp_abcdefghijklmnopqrstuvwxyz012345 rejected",
			expected: "token ghp_******** rejected",
		},
		{
			name:     "github fine grained token",
			input:    "github_pat_11ABCDEFG0123456789_abcdefghij",
			expected: "github_pat_********",
		},
		{
			name:     "bearer token",
			input:    "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123",
			expected: "Authorization: Bearer ********",
		},
		{
			name:     "bearer jwt is masked as a whole",
			input:    "Authorization: Bearer " + jwt,
			expected: "Authorization: Bearer ********",
		},
		{
			name:     "bearer github token",
			input:    "Authorization: Bearer ghp_abcdefghijklmnopqrstuvwxyz012345",
			expected: "Authorization: Bearer ********",
		},
		{
			name: "mixed sensitive information",
			input: "upstream https://api.openai.com/v1/keys returned 401 for " +
				"api_key:sk-proj-abcdefghijklmnopqrst at 10.0.0.7 with Bearer ghp_abcdefghijklmnopqrstuvwxyz012345",
			expected: "upstream https://***.com/***/*** returned 401 for " +
				"api_key:*** at ***.***.***.*** with Bearer ********",
		},
		{
			name:     "nothing to mask",
			input:    "invalid_request: field prompt is required",
			expected: "invalid_request: field prompt is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskSensitiveInfo(tt.input); got != tt.expected {
				t.Errorf("MaskSensitiveInfo(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

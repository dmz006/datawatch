package config

import "testing"

func TestNormalizePublicURL(t *testing.T) {
	valid := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"whitespace only counts as empty", "  \t ", ""},
		{"https with port", "https://host.tailnet.ts.net:8081", "https://host.tailnet.ts.net:8081"},
		{"http with trailing slash", "http://127.0.0.1:8081/", "http://127.0.0.1:8081"},
		{"mixed-case scheme", "HtTpS://Host.Example.com:8081", "HtTpS://Host.Example.com:8081"},
		{"surrounding spaces", "  https://proxy.example.com/mcp//  ", "https://proxy.example.com/mcp"},
		{"bracketed IPv6 host", "http://[::1]:8081", "http://[::1]:8081"},
	}
	for _, c := range valid {
		t.Run("valid/"+c.name, func(t *testing.T) {
			got, err := NormalizePublicURL(c.in)
			if err != nil {
				t.Fatalf("NormalizePublicURL(%q) error: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("NormalizePublicURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
	invalid := []struct{ name, in string }{
		{"ftp scheme", "ftp://x"},
		{"no scheme with port", "host:8081"},
		{"scheme-relative", "//host"},
		{"relative path", "/relative"},
		{"javascript scheme", "javascript:alert(1)"},
		{"user-info", "https://user:pw@host"},
		{"user without password", "https://user@host"},
		{"query", "https://host?x=1"},
		{"empty query marker", "https://host?"},
		{"fragment", "https://host#f"},
		{"empty fragment marker", "https://host#"},
		{"no host", "https://"},
		{"port only host", "https://:8081"},
		{"space in host", "https://ho st"},
	}
	for _, c := range invalid {
		t.Run("invalid/"+c.name, func(t *testing.T) {
			if got, err := NormalizePublicURL(c.in); err == nil {
				t.Errorf("NormalizePublicURL(%q) = %q, want an error", c.in, got)
			}
		})
	}
}

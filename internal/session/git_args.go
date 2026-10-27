package session

import (
	"fmt"
	"strings"
)

// checkGitArg rejects values that git could read as an option or that carry
// control characters. kind names the value in the error.
func checkGitArg(kind, v string) error {
	if v == "" {
		return fmt.Errorf("invalid git %s: empty", kind)
	}
	if v[0] == '-' {
		return fmt.Errorf("invalid git %s %q: leading '-'", kind, v)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("invalid git %s: control character", kind)
		}
	}
	return nil
}

// validateGitRef applies git check-ref-format rules to a tag, branch or
// remote name.
func validateGitRef(kind, v string) error {
	if err := checkGitArg(kind, v); err != nil {
		return err
	}
	if strings.ContainsAny(v, " ~^:?*[\\") {
		return fmt.Errorf("invalid git %s %q: forbidden character", kind, v)
	}
	if v == "@" || strings.Contains(v, "..") || strings.Contains(v, "@{") ||
		strings.Contains(v, "//") || strings.HasPrefix(v, "/") ||
		strings.HasSuffix(v, "/") || strings.HasSuffix(v, ".") ||
		strings.HasSuffix(v, ".lock") {
		return fmt.Errorf("invalid git %s %q: malformed ref", kind, v)
	}
	for _, part := range strings.Split(v, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return fmt.Errorf("invalid git %s %q: malformed ref", kind, v)
		}
	}
	return nil
}

// validateGitRemoteName accepts a remote name: a ref-like name without '/'.
func validateGitRemoteName(v string) error {
	if err := validateGitRef("remote name", v); err != nil {
		return err
	}
	if strings.Contains(v, "/") {
		return fmt.Errorf("invalid git remote name %q: '/' not allowed", v)
	}
	return nil
}

// validateGitURL accepts a remote URL that cannot be read as an option.
func validateGitURL(v string) error {
	return checkGitArg("url", v)
}

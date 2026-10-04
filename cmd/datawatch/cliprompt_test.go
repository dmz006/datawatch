// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3d) — cliPrompt's "%s [%s]: " prompt echoed an existing secret value
// (password, bearer token, API key, HMAC secret) in plaintext every time
// `datawatch setup` re-ran on an already-configured field. cliPromptSecret
// is the fix for the 13 real call sites that passed a secret as the
// default; these tests cover both the new function and confirm the
// original cliPrompt (still correct and still used for ~45 non-secret
// fields) was not changed.

package main

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
)

// capturePromptOutput runs fn with a reader fed by input and returns
// (everything fn printed to stdout, fn's return value).
func capturePromptOutput(t *testing.T, input string, fn func(reader *bufio.Reader) string) (string, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	reader := bufio.NewReader(strings.NewReader(input))
	result := fn(reader)

	w.Close() //nolint:errcheck
	os.Stdout = oldStdout
	out, _ := io.ReadAll(r)
	return string(out), result
}

func TestCliPromptSecret_DoesNotEchoExistingValue(t *testing.T) {
	const secret = "supersecret-token-abc123"
	printed, got := capturePromptOutput(t, "\n", func(reader *bufio.Reader) string {
		return cliPromptSecret(reader, "Bot token", secret)
	})
	if strings.Contains(printed, secret) {
		t.Fatalf("prompt echoed the secret value in plaintext: %q", printed)
	}
	if !strings.Contains(printed, "unchanged") {
		t.Errorf("expected the prompt to indicate an unchanged value is kept, got: %q", printed)
	}
	// The critical round-trip check: pressing Enter with no input must
	// still return the original secret unchanged, even though the
	// displayed prompt text is different now.
	if got != secret {
		t.Errorf("return value = %q, want original secret %q unchanged", got, secret)
	}
}

func TestCliPromptSecret_TypedValueOverridesExisting(t *testing.T) {
	printed, got := capturePromptOutput(t, "brand-new-token\n", func(reader *bufio.Reader) string {
		return cliPromptSecret(reader, "Bot token", "old-secret-value")
	})
	if strings.Contains(printed, "old-secret-value") {
		t.Fatalf("prompt echoed the old secret value: %q", printed)
	}
	if got != "brand-new-token" {
		t.Errorf("return value = %q, want the typed replacement", got)
	}
}

func TestCliPromptSecret_EmptyExisting_PromptsPlainLikeCliPrompt(t *testing.T) {
	// When there's no existing secret to protect, the prompt should look
	// exactly like a never-configured field -- no "[unchanged...]" noise.
	printed, got := capturePromptOutput(t, "\n", func(reader *bufio.Reader) string {
		return cliPromptSecret(reader, "Bot token", "")
	})
	if got != "" {
		t.Errorf("return value = %q, want empty", got)
	}
	if strings.Contains(printed, "unchanged") {
		t.Errorf("should not mention 'unchanged' when there was nothing configured: %q", printed)
	}
	if !strings.Contains(printed, "Bot token: ") {
		t.Errorf("expected a plain %q prompt, got: %q", "Bot token: ", printed)
	}
}

// Regression: the original cliPrompt (still used for ~45 non-secret
// fields -- hostnames, addresses, ports, binary paths) must be completely
// unchanged by this fix. It SHOULD keep echoing its default value; that's
// correct and expected for a non-secret field like a hostname.
func TestCliPrompt_StillEchoesNonSecretDefault(t *testing.T) {
	printed, got := capturePromptOutput(t, "\n", func(reader *bufio.Reader) string {
		return cliPrompt(reader, "SMTP server hostname", "smtp.gmail.com")
	})
	if !strings.Contains(printed, "smtp.gmail.com") {
		t.Errorf("expected cliPrompt to still show the non-secret default, got: %q", printed)
	}
	if got != "smtp.gmail.com" {
		t.Errorf("return value = %q, want the default unchanged", got)
	}
}

func TestCliPrompt_TypedValueStillOverrides(t *testing.T) {
	_, got := capturePromptOutput(t, "smtp.example.com\n", func(reader *bufio.Reader) string {
		return cliPrompt(reader, "SMTP server hostname", "smtp.gmail.com")
	})
	if got != "smtp.example.com" {
		t.Errorf("return value = %q, want the typed replacement", got)
	}
}

// Confirms every real secret-field call site in the setup wizard was
// actually migrated off the plain cliPrompt -- a grep-shaped regression
// test so a future added secret field (or a revert of one of the 13)
// doesn't silently reintroduce the echo.
func TestSetupWizard_NoSecretFieldUsesPlainCliPrompt(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Skip("main.go not readable in this test environment")
	}
	text := string(src)
	secretFields := []string{
		`cfg.Telegram.Token)`, `cfg.Discord.Token)`, `cfg.Slack.Token)`,
		`cfg.Matrix.AccessToken)`, `cfg.Twilio.AuthToken)`, `cfg.Ntfy.Token)`,
		`cfg.Email.Password)`, `cfg.Webhook.Token)`, `cfg.GitHubWebhook.Secret)`,
		`cfg.Server.Token)`, `cfg.MCP.Token)`, `cfg.DNSChannel.Secret)`,
		`cfg.OpenWebUI.APIKey)`,
	}
	for _, field := range secretFields {
		idx := strings.Index(text, field)
		if idx < 0 {
			t.Errorf("expected call site assigning %s not found -- did it move or get renamed?", field)
			continue
		}
		// Look at the ~40 chars immediately before the field reference:
		// it must say cliPromptSecret(, not a bare cliPrompt(.
		start := idx - 60
		if start < 0 {
			start = 0
		}
		window := text[start:idx]
		if strings.Contains(window, "cliPrompt(reader,") && !strings.Contains(window, "cliPromptSecret(reader,") {
			t.Errorf("%s is still passed through plain cliPrompt, not cliPromptSecret", field)
		}
	}
}

package session

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

func TestFirstPromptContext_PrefixedOnceAndFlagged(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "fp01")

	first := mgr.prependPlatformContextOnce(sess, "pi-prompt", "do the task")
	if !strings.HasPrefix(first, platformcontext.PlatformContext) || !strings.HasSuffix(first, "do the task") {
		t.Fatalf("first prompt not prefixed: %q", first)
	}
	if !sess.PlatformContextDelivered {
		t.Fatal("flag not set after first prompt")
	}
	if second := mgr.prependPlatformContextOnce(sess, "pi-prompt", "next"); second != "next" {
		t.Fatalf("second prompt was modified: %q", second)
	}
}

func TestFirstPromptContext_RulesFileBackendNotPrefixed(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "fp02")
	sess.PlatformContextLocalFile = true // local file written: no prefix
	if got := mgr.prependPlatformContextOnce(sess, "claude-code", "task"); got != "task" {
		t.Fatalf("rules_file backend prefixed: %q", got)
	}
	if sess.PlatformContextDelivered {
		t.Fatal("flag set for rules_file backend")
	}
}

func TestFirstPromptContext_SettingOffDisables(t *testing.T) {
	mgr := newTestManager(t)
	mgr.platformContextFirstPromptOff = true
	sess := newTestSession(t, mgr, "fp03")
	if got := mgr.prependPlatformContextOnce(sess, "pi-prompt", "task"); got != "task" {
		t.Fatalf("prefixed with setting off: %q", got)
	}
	if sess.PlatformContextDelivered {
		t.Fatal("flag set with setting off")
	}
	if FirstPromptContext("pi-prompt", false, false) != "" {
		t.Fatal("FirstPromptContext returned text with setting off")
	}
}

func TestPlatformContextFirstPromptFallback_LocalFileWrittenNoPrefix(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "fb01")
	sess.PlatformContextLocalFile = true
	if got := mgr.prependPlatformContextOnce(sess, "claude-code", "task"); got != "task" {
		t.Fatalf("prefixed despite local file: %q", got)
	}
	if sess.PlatformContextDelivered {
		t.Fatal("flag set despite local file")
	}
}

func TestPlatformContextFirstPromptFallback_LocalFileRefusedPrefixOnce(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "fb02")
	got := mgr.prependPlatformContextOnce(sess, "claude-code", "task")
	if !strings.HasPrefix(got, platformcontext.PlatformContext) || !strings.HasSuffix(got, "task") {
		t.Fatalf("fallback prefix missing: %q", got)
	}
	if !sess.PlatformContextDelivered {
		t.Fatal("flag not set")
	}
}

func TestPlatformContextFirstPromptFallback_SecondPromptNoPrefix(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "fb03")
	mgr.prependPlatformContextOnce(sess, "claude-code", "task")
	if got := mgr.prependPlatformContextOnce(sess, "claude-code", "next"); got != "next" {
		t.Fatalf("second prompt modified: %q", got)
	}
}

func TestPlatformContextFirstPromptFallback_DisabledNoPrefix(t *testing.T) {
	mgr := newTestManager(t)
	mgr.platformContextFirstPromptOff = true
	sess := newTestSession(t, mgr, "fb04")
	if got := mgr.prependPlatformContextOnce(sess, "claude-code", "task"); got != "task" {
		t.Fatalf("prefixed while disabled: %q", got)
	}
	if sess.PlatformContextDelivered {
		t.Fatal("flag set while disabled")
	}
}

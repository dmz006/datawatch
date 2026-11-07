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

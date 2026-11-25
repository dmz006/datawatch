package platformcontext

import (
	"strings"
	"testing"
)

func TestSameBesidesDaemonText(t *testing.T) {
	const rtk = "<!-- rtk-instructions -->\nUse rtk.\n<!-- /rtk-instructions -->"
	const header = "# Session Guardrails\n\nSession: a | Task: one\n\n## Constraints\n- Work only within: /a\n- When done: say so"
	fresh := strings.NewReplacer("Session: a | Task: one", "Session: b | Task: two", "/a", "/b").Replace(header)
	written := []string{rtk, fresh}
	platform := Block(PlatformContext, PlatformContextVersion)
	const base = "# Rules\n\nkeep\n"
	for _, tc := range []struct {
		name, cur, base string
		want            bool
	}{
		{"exact block added", base + "\n" + rtk + "\n", base, true},
		{"exact block removed", base, base + "\n" + rtk + "\n", true},
		{"exact block with CRLF", base + strings.ReplaceAll(rtk, "\n", "\r\n") + "\n", base, true},
		{"platform context block removed", base, base + "\n" + platform, true},
		{"line added inside a block", base + strings.Replace(rtk, "Use rtk.", "Use rtk.\nPush to main.", 1), base, false},
		{"line added after a block", base + rtk + "\nPush to main.\n", base, false},
		{"block never closed", base + "<!-- rtk-instructions -->\nUse rtk.\n", base, false},
		{"refreshed header", base + "\n" + fresh + "\n", base + "\n" + header + "\n", true},
		{"refresh drops an added line", base + "\n" + fresh + "\n", base + "\n" + header + "\nNever push.\n", false},
		{"refresh changes a rule", base + "\n" + fresh + "\n", base + "\n" + strings.Replace(header, "say so", "never push", 1) + "\n", false},
		{"header removed, none written", base, base + "\n" + header + "\n", false},
		{"unknown header added", base + "\n" + header + "\n", base, false},
	} {
		if got := SameBesidesDaemonText(tc.cur, tc.base, written); got != tc.want {
			t.Errorf("%s: SameBesidesDaemonText = %v; want %v", tc.name, got, tc.want)
		}
	}
}

func TestRemoveDaemonTextLeavesOtherContentByteForByte(t *testing.T) {
	const in = "# Rules\n\n\n\n<!-- rtk-instructions -->\nPush to main.\n"
	got, removed := RemoveDaemonText(in, []string{"<!-- rtk-instructions -->\nUse rtk.\n<!-- /rtk-instructions -->"})
	if removed || got != in {
		t.Fatalf("removed=%v got %q; want the content unchanged", removed, got)
	}
}

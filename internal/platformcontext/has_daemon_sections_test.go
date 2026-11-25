package platformcontext

import "testing"

func TestHasDaemonSections(t *testing.T) {
	with := map[string]string{
		"platform context": "rules\n<!-- datawatch:platform-context v1 -->\nx\n<!-- /datawatch:platform-context -->\n",
		"rtk":              "rules\n<!-- rtk-instructions -->\nx\n<!-- /rtk-instructions -->\n",
		"cross-session":    "rules\n<!-- cross-session-communication-rule -->\nx\n<!-- /cross-session-communication-rule -->\n",
		"memory block":     "rules\n<!-- datawatch:memory-instructions -->\nx\n<!-- /datawatch:memory-instructions -->\n",
		"memory section":   "# Memory & Knowledge (datawatch)\nx\n",
		"guardrails":       "# Session Guardrails\nx\n",
	}
	for name, content := range with {
		if !HasDaemonSections(content) {
			t.Errorf("%s: HasDaemonSections = false; want true", name)
		}
	}
	// Text stripping only re-flows blank lines in is not a daemon section.
	for _, content := range []string{"", "rules\n", "a\n\n\n\nb\n\n", "## Session Guardrails\nx\n", "steps:\n  - run: x\n"} {
		if HasDaemonSections(content) {
			t.Errorf("HasDaemonSections(%q) = true; want false", content)
		}
	}
}

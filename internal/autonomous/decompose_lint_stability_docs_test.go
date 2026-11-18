package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocsHowtoStabilityRules asserts that the decompose-lint how-to names
// the four newest verification-script stability rules by id, so the
// operator-facing docs cannot silently drop a rule id that findings, config
// (`disabled_rules`, same-id replacement) and the plan all still carry. It
// checks the canonical docs/ tree and the embedded copy under
// internal/server/web/docs that `make sync-docs` mirrors from it.
func TestDocsHowtoStabilityRules(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	rel := filepath.Join("..", "..", "docs", "howto", "automaton-decompose-lint.md")
	want := []string{
		"artefact_files_allowed",
		"final_gate_artefact_allowed",
		"verify_script_unknown_flag",
		"verify_script_text_not_in_spec",
	}
	for _, candidate := range []string{
		filepath.Join(wd, rel),
		filepath.Join(wd, "..", "server", "web", "docs", "howto", "automaton-decompose-lint.md"),
	} {
		b, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatalf("reading %s: %v", candidate, err)
		}
		docs := string(b)
		for _, id := range want {
			if !strings.Contains(docs, id) {
				t.Errorf("%s: how-to does not name the stability rule id %q", candidate, id)
			}
		}
	}
}

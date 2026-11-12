package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fingerprintWorktreeManager returns a manager that reads the rules from the
// working tree, so no git runner is involved.
func fingerprintWorktreeManager() *Manager {
	return &Manager{cfg: Config{Automation: AutomationConfig{RulesReviewSource: config.RulesReviewSourceWorktree}}}
}

func writeFingerprintFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

var fingerprintHexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestReviewRulesFingerprintSameInputsSameValue(t *testing.T) {
	m := fingerprintWorktreeManager()
	dir := t.TempDir()
	writeFingerprintFile(t, dir, "AGENT.md", "rules text")
	writeFingerprintFile(t, dir, "CONTEXT.md", "context text")
	prd := &PRD{ID: "fp1", ProjectDir: dir}

	first := m.reviewRulesFingerprint(context.Background(), prd)
	second := m.reviewRulesFingerprint(context.Background(), prd)
	if !fingerprintHexPattern.MatchString(first) {
		t.Fatalf("fingerprint %q is not a 64-character hex SHA-256", first)
	}
	if first != second {
		t.Errorf("same inputs gave different values: %q then %q", first, second)
	}
}

func TestReviewRulesFingerprintChangesWhenRulesFileEdited(t *testing.T) {
	m := fingerprintWorktreeManager()
	dir := t.TempDir()
	writeFingerprintFile(t, dir, "AGENT.md", "rules text A")
	prd := &PRD{ID: "fp2", ProjectDir: dir}
	before := m.reviewRulesFingerprint(context.Background(), prd)

	writeFingerprintFile(t, dir, "AGENT.md", "rules text B")
	after := m.reviewRulesFingerprint(context.Background(), prd)
	if before == "" || after == "" {
		t.Fatalf("fingerprints must not be empty: %q, %q", before, after)
	}
	if before == after {
		t.Error("editing one byte of the rules file did not change the fingerprint")
	}
}

func TestReviewRulesFingerprintChangesWhenContextFileEdited(t *testing.T) {
	m := fingerprintWorktreeManager()
	dir := t.TempDir()
	writeFingerprintFile(t, dir, "AGENT.md", "rules")
	writeFingerprintFile(t, dir, "CONTEXT.md", "context one")
	prd := &PRD{ID: "fp3", ProjectDir: dir}
	before := m.reviewRulesFingerprint(context.Background(), prd)

	writeFingerprintFile(t, dir, "CONTEXT.md", "context two")
	if after := m.reviewRulesFingerprint(context.Background(), prd); before == after {
		t.Error("editing the context file did not change the fingerprint")
	}
}

func TestReviewRulesFingerprintIgnoresFileThatIsNotRulesOrContext(t *testing.T) {
	m := fingerprintWorktreeManager()
	dir := t.TempDir()
	writeFingerprintFile(t, dir, "AGENT.md", "rules")
	writeFingerprintFile(t, dir, "notes.txt", "one")
	prd := &PRD{ID: "fp4", ProjectDir: dir}
	before := m.reviewRulesFingerprint(context.Background(), prd)

	writeFingerprintFile(t, dir, "notes.txt", "two")
	writeFingerprintFile(t, dir, "other.go", "package other")
	if after := m.reviewRulesFingerprint(context.Background(), prd); before != after {
		t.Errorf("a file that is not a rules or context file changed the fingerprint: %q then %q", before, after)
	}
}

func TestReviewRulesFingerprintEmptyWithoutProjectDirOrRulesFile(t *testing.T) {
	m := fingerprintWorktreeManager()
	if got := m.reviewRulesFingerprint(context.Background(), &PRD{ID: "fp5"}); got != "" {
		t.Errorf("no project directory: fingerprint = %q, want empty", got)
	}
	if got := m.reviewRulesFingerprint(context.Background(), &PRD{ID: "fp5", ProjectDir: t.TempDir()}); got != "" {
		t.Errorf("no rules file: fingerprint = %q, want empty", got)
	}
	// The default source is the base commit; the same two cases are empty there.
	base := &Manager{cfg: Config{}}
	if got := base.reviewRulesFingerprint(context.Background(), &PRD{ID: "fp5"}); got != "" {
		t.Errorf("base-commit source, no project directory: fingerprint = %q, want empty", got)
	}
}

func TestReviewRulesFingerprintUnreadableFileDiffersFromReadable(t *testing.T) {
	m := fingerprintWorktreeManager()
	readable := t.TempDir()
	writeFingerprintFile(t, readable, "AGENT.md", "rules")
	unreadable := t.TempDir()
	// A directory named like the rules file exists but cannot be read as a file.
	if err := os.Mkdir(filepath.Join(unreadable, "AGENT.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := m.reviewRulesFingerprint(context.Background(), &PRD{ID: "fp6", ProjectDir: readable})
	b := m.reviewRulesFingerprint(context.Background(), &PRD{ID: "fp6", ProjectDir: unreadable})
	if b == "" {
		t.Fatal("a present but unreadable rules file must still give a fingerprint")
	}
	if a == b {
		t.Error("an unreadable file gave the same fingerprint as a readable one")
	}
}

func TestReviewRulesFingerprintAppendsNoDecision(t *testing.T) {
	// Worktree source.
	m := fingerprintWorktreeManager()
	dir := t.TempDir()
	writeFingerprintFile(t, dir, "AGENT.md", "rules")
	prd := &PRD{ID: "fp7", ProjectDir: dir}
	_ = m.reviewRulesFingerprint(context.Background(), prd)
	if len(prd.Decisions) != 0 {
		t.Errorf("worktree source appended Decisions: %+v", prd.Decisions)
	}

	// Base-commit source whose git read fails: the fallback to the working
	// tree must be silent, unlike rulesReviewSuffix.
	bm, g, bprd, _ := rrSetup(t, Config{}, map[string]string{"AGENT.md": "worker rules"})
	g.showErr = errors.New("git show failed")
	got := bm.reviewRulesFingerprint(context.Background(), bprd)
	if got == "" {
		t.Error("the fallback to the working-tree text should still give a fingerprint")
	}
	if len(bprd.Decisions) != 0 {
		t.Errorf("base-commit read error appended Decisions: %+v", bprd.Decisions)
	}
	if got := rrDecisions(bprd, "rules_review_source_fallback"); len(got) != 0 {
		t.Errorf("a fallback Decision was recorded: %+v", got)
	}
}

func TestReviewRulesFingerprintBaseCommitHashesEmbeddedContent(t *testing.T) {
	m, g, prd, _ := rrSetup(t, Config{}, map[string]string{"AGENT.md": rrWorkerTxt})
	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText

	first := m.reviewRulesFingerprint(context.Background(), prd)
	if !fingerprintHexPattern.MatchString(first) {
		t.Fatalf("fingerprint %q is not a 64-character hex SHA-256", first)
	}
	want := JudgeVerdictRulesHash(baseCommitRulesReviewText(
		[]baseRulesFile{{Path: "AGENT.md", Content: rrBaseText}}, rrBaseSHA) + m.generatedArtefactReviewText(prd))
	if first != want {
		t.Errorf("fingerprint = %q, want the hash of the embedded base-commit text %q", first, want)
	}

	// The worker rewriting the working-tree copy does not change it.
	writeFingerprintFile(t, prd.ProjectDir, "AGENT.md", "WORKER REWRITE AGAIN")
	if again := m.reviewRulesFingerprint(context.Background(), prd); again != first {
		t.Error("a working-tree edit changed a base-commit fingerprint")
	}

	// A change to the content at the base commit does.
	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText + " Also update the docs."
	if changed := m.reviewRulesFingerprint(context.Background(), prd); changed == first {
		t.Error("changing the rules text at the base commit did not change the fingerprint")
	}
	if len(prd.Decisions) != 0 {
		t.Errorf("base-commit fingerprint appended Decisions: %+v", prd.Decisions)
	}
}

func TestReviewRulesFingerprintBaseCommitEmptyWhenNoFileAtBase(t *testing.T) {
	m, _, prd, _ := rrSetup(t, Config{}, map[string]string{"AGENT.md": rrWorkerTxt})
	if got := m.reviewRulesFingerprint(context.Background(), prd); got != "" {
		t.Errorf("no rules file at the base commit: fingerprint = %q, want empty", got)
	}
}

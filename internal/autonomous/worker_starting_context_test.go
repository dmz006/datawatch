package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// TestStartingContextInput_Fields exercises every field of
// StartingContextInput by passing each through the builder, so adding or
// dropping a field changes what a test must assert.
func TestStartingContextInput_Fields(t *testing.T) {
	in := StartingContextInput{
		Role:        "worker",
		Profile:     workerprofile.WorkerProfile{},
		TaskSpec:    "do the task",
		Constraints: []string{"one"},
		RulesDigest: "digest",
		RulesFull:   "full",
	}
	if in.Role != "worker" || in.Profile.Name != "" || in.TaskSpec != "do the task" ||
		len(in.Constraints) != 1 || in.Constraints[0] != "one" ||
		in.RulesDigest != "digest" || in.RulesFull != "full" {
		t.Fatalf("StartingContextInput round-trip: %+v", in)
	}
}

func TestWorkerStartingContext_Minimal(t *testing.T) {
	out := BuildWorkerStartingContext(StartingContextInput{
		Role:        "worker",
		Profile:     mustProfile(t, "minimal"),
		TaskSpec:    "fix the failing test",
		Constraints: []string{"stay in the project directory", "no new dependencies"},
		RulesDigest: "SHOULD NOT APPEAR AT MINIMAL",
		RulesFull:   "SHOULD NOT APPEAR AT MINIMAL",
	})

	for _, want := range []string{
		"Role\nworker",
		"Profile\nprofile=\"minimal\" context=minimal mcp=none ",
		"Constraints\n- stay in the project directory\n- no new dependencies",
		"Task\nfix the failing test\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("minimal level missing %q\noutput:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SHOULD NOT APPEAR") {
		t.Errorf("minimal level must not carry digest or full rules:\n%s", out)
	}
	if strings.Contains(out, "Rules digest") || strings.Contains(out, "Rules\n") {
		t.Errorf("minimal level must have no rules sections:\n%s", out)
	}
	if !strings.HasSuffix(out, "fix the failing test\n") {
		t.Errorf("task spec must be the last content:\n%s", out)
	}
}

func TestWorkerStartingContext_Digest(t *testing.T) {
	const digestText = "Rules digest (generated, 2 sections, 1 mandatory lines):\nSec A\n  must do X"
	out := BuildWorkerStartingContext(StartingContextInput{
		Role:        "worker",
		Profile:     mustProfile(t, "digest"),
		TaskSpec:    "write the report",
		Constraints: []string{"plain English only"},
		RulesDigest: digestText,
		RulesFull:   "SHOULD NOT APPEAR AT DIGEST LEVEL",
	})

	if !strings.Contains(out, "Rules digest\n"+digestText+"\n\n") {
		t.Errorf("digest level must carry the rules digest verbatim:\n%s", out)
	}
	if strings.Contains(out, "SHOULD NOT APPEAR") {
		t.Errorf("digest level must not carry full rules:\n%s", out)
	}
	// The digest level must not open a bare "Rules" heading line.
	if strings.Contains(out, "\nRules\n") {
		t.Errorf("digest level leaked a full-rules section:\n%s", out)
	}
}

func TestWorkerStartingContext_Full(t *testing.T) {
	const (
		digestText = "Rules digest (generated, 1 sections, 2 mandatory lines):\nSec A\n  must do X\n  never break Y"
		fullText   = "# Sec A\nfull body of the rules file"
	)
	out := BuildWorkerStartingContext(StartingContextInput{
		Role:        "worker",
		Profile:     mustProfile(t, "full"),
		TaskSpec:    "ship the feature",
		Constraints: []string{},
		RulesDigest: digestText,
		RulesFull:   fullText,
	})

	for _, want := range []string{
		"Constraints\n(none)\n",
		"Rules digest\n" + digestText + "\n\n",
		"Rules\n" + fullText + "\n\n",
		"Task\nship the feature\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("full level missing %q\noutput:\n%s", want, out)
		}
	}
	order := map[string]int{
		"digest": strings.Index(out, "\n\nRules digest\n"),
		"full":   strings.Index(out, "\n\nRules\n"),
		"task":   strings.Index(out, "\n\nTask\n"),
	}
	if order["digest"] == -1 || order["full"] == -1 || order["task"] == -1 ||
		order["digest"] >= order["full"] || order["full"] >= order["task"] {
		t.Fatalf("full level must order digest before full rules before task:\n%s", out)
	}
}

func TestWorkerStartingContext_PrefixIdenticalForDifferentTaskSpecs(t *testing.T) {
	base := StartingContextInput{
		Role:        "worker",
		Profile:     mustProfile(t, "full"),
		Constraints: []string{"constraint a", "constraint b"},
		RulesDigest: "digest body",
		RulesFull:   "full body",
	}
	a := BuildWorkerStartingContext(StartingContextInput{
		Role: base.Role, Profile: base.Profile, Constraints: base.Constraints,
		RulesDigest: base.RulesDigest, RulesFull: base.RulesFull, TaskSpec: "spec one",
	})
	b := BuildWorkerStartingContext(StartingContextInput{
		Role: base.Role, Profile: base.Profile, Constraints: base.Constraints,
		RulesDigest: base.RulesDigest, RulesFull: base.RulesFull, TaskSpec: "spec two",
	})

	preA, seen := splitBeforeHeading(a)
	if !seen {
		t.Fatalf("first output has no 'Task' heading:\n%s", a)
	}
	preB, seen := splitBeforeHeading(b)
	if !seen {
		t.Fatalf("second output has no 'Task' heading:\n%s", b)
	}
	if preA != preB {
		t.Errorf("prefix before the Task heading differs for differing task specs:\nA:\n%s\n---\nB:\n%s", preA, preB)
	}
	if strings.Contains(preA, "spec one") || strings.Contains(preA, "spec two") {
		t.Errorf("task spec leaked into the prefix:\n%s", preA)
	}
}

func TestWorkerStartingContext_StableAcrossRepeatedCalls(t *testing.T) {
	in := StartingContextInput{
		Role:        "reviewer",
		Profile:     mustProfile(t, "datawatch-tools"),
		TaskSpec:    "review the branch",
		Constraints: []string{"no source changes"},
		RulesDigest: "digest body",
		RulesFull:   "full body",
	}
	first := BuildWorkerStartingContext(in)
	for i := 0; i < 25; i++ {
		if got := BuildWorkerStartingContext(in); got != first {
			t.Fatalf("output changed on call %d:\nfirst:\n%s\n---\nrepeated:\n%s", i, first, got)
		}
	}
}

// splitBeforeHeading returns the text before the first line that is exactly
// the heading h, and whether such a heading line exists.
func splitBeforeHeading(out string) (string, bool) {
	const h = "Task\n"
	i := strings.Index(out, h)
	if i == -1 {
		return "", false
	}
	return out[:i], true
}

func mustProfile(t *testing.T, name string) workerprofile.WorkerProfile {
	t.Helper()
	p, ok := workerprofile.Builtin(name)
	if !ok {
		t.Fatalf("no built-in profile %q", name)
	}
	return p
}

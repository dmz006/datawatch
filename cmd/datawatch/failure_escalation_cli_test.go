package main

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

// parsePolicyFlags registers the policy flags on a bare command, parses args
// and returns the policy they describe.
func parsePolicyFlags(t *testing.T, args ...string) (*cobra.Command, failureEscalationFlags) {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	var f failureEscalationFlags
	addFailureEscalationPolicyFlags(cmd, &f)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags %v: %v", args, err)
	}
	return cmd, f
}

func TestFailureEscalationCLIPolicyFromFlagsSetsOnlyGivenFields(t *testing.T) {
	cmd, f := parsePolicyFlags(t, "--max-failures", "4", "--enabled=false", "--steps", "pi-prompt:ollama/qwen3.8:27b@2,-")
	p, err := policyFromFlags(cmd, f)
	if err != nil {
		t.Fatalf("policyFromFlags: %v", err)
	}
	if p.Enabled == nil || *p.Enabled {
		t.Errorf("enabled = %v, want a set false", p.Enabled)
	}
	if p.MaxFailures != 4 {
		t.Errorf("max_failures = %d, want 4", p.MaxFailures)
	}
	if len(p.Steps) != 2 || p.Steps[0].Backend != "pi-prompt" || p.Steps[0].Model != "ollama/qwen3.8:27b" || p.Steps[0].Attempts != 2 {
		t.Errorf("steps = %+v", p.Steps)
	}
	if p.Steps[1].Backend != "" {
		t.Errorf("a - step must use the normal worker, got %+v", p.Steps[1])
	}
}

func TestFailureEscalationCLIPolicyFromFlagsUnsetFieldsInherit(t *testing.T) {
	cmd, f := parsePolicyFlags(t)
	p, err := policyFromFlags(cmd, f)
	if err != nil {
		t.Fatalf("policyFromFlags: %v", err)
	}
	if p.Enabled != nil || p.MaxFailures != 0 || p.Steps != nil {
		t.Fatalf("no flags given: policy = %+v, want every field unset", p)
	}
}

func TestFailureEscalationCLIPolicyFromFlagsRejectsBadSteps(t *testing.T) {
	cmd, f := parsePolicyFlags(t, "--steps", "claude-code")
	if _, err := policyFromFlags(cmd, f); err == nil {
		t.Fatal("steps without a colon must be refused before any request")
	}
}

func TestFailureEscalationCLISetNeedsSomethingToSet(t *testing.T) {
	cmd := newAutonomousFailureEscalationSetCmd()
	cmd.SetArgs([]string{"p1"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("set with no policy flags must fail before calling the daemon")
	}
}

func TestFailureEscalationCLIResolveReviewValidatesAction(t *testing.T) {
	cmd := newAutonomousResolveReviewCmd()
	cmd.SetArgs([]string{"p1", "--task", "t1", "--action", "explode"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("an action other than retry or cancel must be refused before calling the daemon")
	}
}

func TestFailureEscalationCLIResolveReviewRequiresTask(t *testing.T) {
	cmd := newAutonomousResolveReviewCmd()
	cmd.SetArgs([]string{"p1", "--action", "cancel"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("--task is required")
	}
}

func TestFailureEscalationCLISplitReviewFiles(t *testing.T) {
	got := splitReviewFiles(" a.go, ,b.go,")
	if !reflect.DeepEqual(got, []string{"a.go", "b.go"}) {
		t.Fatalf("splitReviewFiles = %v", got)
	}
	if splitReviewFiles("") != nil {
		t.Fatal("an empty list must be nil")
	}
}

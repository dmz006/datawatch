package autonomous

import (
	"context"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/resources"
)

var guardBase = []string{"env", "-u", "TMUX", "-u", "TMUX_TMPDIR", "judge", "--once"}

// runGate runs classifyGate with a runner that records the command built from
// the launch carried in its context.
func runScopedGate(t *testing.T, scope, proc bool) (GuardrailVerdict, []string, string) {
	t.Helper()
	stubVerifyHost(t, scope, proc)
	var argv []string
	var path string
	m := &Manager{}
	m.SetResourceCeiling(config.DefaultResourceLimits(), func(*PRD, *Task) resources.Override {
		return resources.Override{Set: true, MB: 3000}
	})
	m.SetGuardrailClassify(
		func(b, mo string) inference.VerificationMode { return inference.VerificationMode("classify") },
		func(ctx context.Context, b, mo, p string) VerificationResult {
			argv, path = GuardrailRunArgv(ctx, guardBase)
			return VerificationResult{OK: true, Severity: "info"}
		})
	v := m.classifyGate(context.Background(), gateInv, GuardrailVerdict{Outcome: "pass"})
	return v, argv, path
}

func TestGuardrailScope_ScopePathWrapsCommand(t *testing.T) {
	v, argv, path := runScopedGate(t, true, true)
	if path != VerifyCeilingScope || v.CeilingPath != VerifyCeilingScope || v.CeilingMB != 3000 {
		t.Fatalf("path=%q verdict=%q/%d", path, v.CeilingPath, v.CeilingMB)
	}
	if len(argv) < 3 || argv[0] != "/usr/bin/systemd-run" || argv[1] != "--user" || argv[2] != "--scope" {
		t.Fatalf("argv = %v", argv)
	}
	if !reflect.DeepEqual(argv[len(argv)-len(guardBase):], guardBase) {
		t.Fatalf("wrapped command = %v, want suffix %v", argv, guardBase)
	}
	found := false
	for _, a := range argv {
		if a == "MemoryMax=3000M" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no MemoryMax=3000M in %v", argv)
	}
}

func TestGuardrailScope_WatchdogPathRunsBare(t *testing.T) {
	v, argv, path := runScopedGate(t, false, true)
	if path != VerifyCeilingWatchdog || v.CeilingPath != VerifyCeilingWatchdog || v.CeilingMB != 3000 {
		t.Fatalf("path=%q verdict=%q/%d", path, v.CeilingPath, v.CeilingMB)
	}
	if !reflect.DeepEqual(argv, guardBase) {
		t.Fatalf("argv = %v, want bare %v", argv, guardBase)
	}
}

func TestGuardrailScope_VerdictUnchanged(t *testing.T) {
	v, _, _ := runScopedGate(t, true, true)
	if v.Outcome != "pass" || v.Mode != "classify" {
		t.Fatalf("verdict changed: %+v", v)
	}
}

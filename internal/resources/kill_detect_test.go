package resources

import (
	"errors"
	"testing"
)

type fakeCgroup map[string]string

func (f fakeCgroup) ReadFile(p string) ([]byte, error) {
	if v, ok := f[p]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("missing")
}

func TestDetectScopeKill(t *testing.T) {
	ev := func(n string) string { return "low 0\nhigh 0\nmax 3\noom 1\noom_kill " + n + "\n" }
	tests := []struct {
		name  string
		files fakeCgroup
		want  KillInfo
	}{
		{"killed", fakeCgroup{"/cg/memory.events": ev("1"), "/cg/memory.peak": "2147483648\n"}, KillInfo{true, 2048, "unknown"}},
		{"not killed", fakeCgroup{"/cg/memory.events": ev("0"), "/cg/memory.peak": "104857600\n"}, KillInfo{false, 100, "unknown"}},
		{"peak only", fakeCgroup{"/cg/memory.peak": "52428800"}, KillInfo{false, 50, "unknown"}},
		{"nothing", fakeCgroup{}, KillInfo{false, 0, "unknown"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectScopeKill(tc.files, "/cg"); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestDetectWatchdogKill(t *testing.T) {
	got := DetectWatchdogKill(Result{Killed: true, PeakKB: 3 * 1024, LargestName: "node"})
	if want := (KillInfo{true, 3, "node"}); got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	got = DetectWatchdogKill(Result{PeakKB: 2048})
	if want := (KillInfo{false, 2, "unknown"}); got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestKillReasonExact(t *testing.T) {
	if KillReason != "the process used more memory than the limit" {
		t.Fatal(KillReason)
	}
}

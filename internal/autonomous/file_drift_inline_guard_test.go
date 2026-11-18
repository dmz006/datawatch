package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDriftRunnerInlineGuardTable(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "inside")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		dir  string
		ok   bool
	}{
		{"inside base", sub, true},
		{"traversal", "../..", false},
		{"relative traversal", "../../etc", false},
		{"dot", ".", false},
		{"empty", "", false},
		{"dash prefixed", "-C", false},
		{"NUL injected", sub + "\x00/evil", false},
		{"non directory", file, false},
		{"missing", filepath.Join(base, "nope"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			swapDriftGitExec(t, func(_ context.Context, _ []string) (string, error) {
				calls++
				return "", nil
			})
			_, err := defaultDriftGitRunner(context.Background(), tc.dir, "diff", "--name-only", "HEAD")
			if tc.ok && (err != nil || calls != 1) {
				t.Fatalf("want exec, err=%v calls=%d", err, calls)
			}
			if !tc.ok && (err == nil || calls != 0) {
				t.Fatalf("want refusal before exec, err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestDriftRunnerRejectsNULArgBeforeExec(t *testing.T) {
	calls := 0
	swapDriftGitExec(t, func(_ context.Context, _ []string) (string, error) { calls++; return "", nil })
	if _, err := defaultDriftGitRunner(context.Background(), t.TempDir(), "log", "a\x00b..HEAD"); err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDriftRevisionGuardsNoExec(t *testing.T) {
	bad := []string{"a\x00b", "-p", "--output=x", "/etc/passwd"}
	for _, rev := range bad {
		calls := 0
		fake := func(_ context.Context, _ string, _ ...string) (string, error) { calls++; return "", nil }
		if f, err := CommittedFilesSince(context.Background(), t.TempDir(), rev, fake); f != nil || err != nil {
			t.Fatalf("CommittedFilesSince %q: %v %v", rev, f, err)
		}
		if s, err := CommittedLineStatsSince(context.Background(), t.TempDir(), rev, fake); s != nil || err != nil {
			t.Fatalf("CommittedLineStatsSince %q: %v %v", rev, s, err)
		}
		if calls != 0 {
			t.Fatalf("rev %q reached the runner %d times", rev, calls)
		}
	}
}

func TestDriftCaptureNULDirNoExec(t *testing.T) {
	calls := 0
	swapDriftGitExec(t, func(_ context.Context, _ []string) (string, error) { calls++; return "", nil })
	d := "x\x00y"
	if CapturePreTaskDirtyFiles(context.Background(), d, nil) != nil || CapturePostVerifySHA(context.Background(), d, nil) != "" {
		t.Fatal("expected empty results")
	}
	if _, err := CommittedFilesSince(context.Background(), d, "abc123", nil); err == nil && calls != 0 {
		t.Fatal("exec reached")
	}
	if calls != 0 {
		t.Fatalf("calls=%d", calls)
	}
}

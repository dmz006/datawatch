package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDetectForeignFileDrift(t *testing.T) {
	cases := []struct {
		name      string
		planned   []string
		untracked []string
		dirty     []string
		committed []string
		want      []string
	}{
		{
			name:      "dirty-before file committed, not planned → flagged",
			planned:   []string{"a.go"},
			dirty:     []string{"operator.go"},
			committed: []string{"a.go", "operator.go"},
			want:      []string{"operator.go"},
		},
		{
			name:      "dirty-before file committed but planned → not flagged",
			planned:   []string{"a.go", "operator.go"},
			dirty:     []string{"operator.go"},
			committed: []string{"a.go", "operator.go"},
			want:      nil,
		},
		{
			name:      "clean task touching only planned files → nil",
			planned:   []string{"a.go", "b.go"},
			committed: []string{"a.go", "b.go"},
			want:      nil,
		},
		{
			name:      "untracked-before file committed → flagged",
			planned:   []string{"a.go"},
			untracked: []string{"notes/scratch.md"},
			committed: []string{"a.go", "notes/scratch.md"},
			want:      []string{"notes/scratch.md"},
		},
		{
			name:      "./x vs x normalization on planned side",
			planned:   []string{"./internal/x.go"},
			dirty:     []string{"internal/x.go"},
			committed: []string{"internal/x.go"},
			want:      nil,
		},
		{
			name:      "./x vs x normalization on pre/committed side, sorted + deduped",
			planned:   []string{"a.go"},
			dirty:     []string{"./z.go", "y.go"},
			untracked: []string{"z.go"},
			committed: []string{"z.go", "./y.go", "y.go"},
			want:      []string{"y.go", "z.go"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{FilesPlanned: tc.planned}
			got := DetectForeignFileDrift(task, tc.untracked, tc.dirty, tc.committed)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyFileDrift_PartitionsCommittedFiles(t *testing.T) {
	task := &Task{FilesPlanned: []string{"a.go", "expected.go"}}
	preExisting, inRun := ClassifyFileDrift(task,
		[]string{"scratch.md"},          // preTaskUntracked
		[]string{"d.go", "expected.go"}, // preTaskDirty (expected.go also planned)
		[]string{"a.go", "scratch.md", "d.go", "expected.go", "new.go", "worker_extra.go"})
	if !reflect.DeepEqual(preExisting, []string{"d.go", "scratch.md"}) {
		t.Fatalf("preExisting = %v, want [d.go scratch.md]", preExisting)
	}
	// In-run = everything committed that was NOT pre-dirty/untracked —
	// the worker's own edits, planned (a.go) or not.
	if !reflect.DeepEqual(inRun, []string{"a.go", "new.go", "worker_extra.go"}) {
		t.Fatalf("inRun = %v, want [a.go new.go worker_extra.go]", inRun)
	}
}

func TestClassifyFileDrift_PlannedPreExistingFileInNeitherGroup(t *testing.T) {
	task := &Task{FilesPlanned: []string{"pre.txt"}}
	preExisting, inRun := ClassifyFileDrift(task,
		nil, []string{"pre.txt"}, []string{"pre.txt", "other.txt"})
	if preExisting != nil {
		t.Fatalf("preExisting = %v, want nil (pre-dirty but planned)", preExisting)
	}
	if !reflect.DeepEqual(inRun, []string{"other.txt"}) {
		t.Fatalf("inRun = %v, want [other.txt]", inRun)
	}
}

func TestClassifyFileDrift_EmptyAndNilSides(t *testing.T) {
	for name, tc := range map[string]struct {
		task             *Task
		preTaskUntracked []string
		preTaskDirty     []string
		committed        []string
		wantPre, wantIn  []string
	}{
		"no committed":   {task: &Task{FilesPlanned: []string{"a.go"}}, preTaskDirty: []string{"d.go"}, committed: nil, wantPre: nil, wantIn: nil},
		"nil task":       {task: nil, preTaskDirty: []string{"d.go"}, committed: []string{"x.go"}, wantPre: nil, wantIn: []string{"x.go"}},
		"untracked-only": {task: &Task{}, preTaskUntracked: []string{"s.md"}, committed: []string{"s.md", "n.go"}, wantPre: []string{"s.md"}, wantIn: []string{"n.go"}},
	} {
		t.Run(name, func(t *testing.T) {
			pre, in := ClassifyFileDrift(tc.task, tc.preTaskUntracked, tc.preTaskDirty, tc.committed)
			if !reflect.DeepEqual(pre, tc.wantPre) {
				t.Fatalf("preExisting = %v, want %v", pre, tc.wantPre)
			}
			if !reflect.DeepEqual(in, tc.wantIn) {
				t.Fatalf("inRun = %v, want %v", in, tc.wantIn)
			}
		})
	}
}

func TestClassifyFileDrift_NormalizesAndDedupes(t *testing.T) {
	task := &Task{FilesPlanned: []string{"./internal/planned.go"}}
	pre, in := ClassifyFileDrift(task,
		[]string{"./s.md", "s.md"}, // untracked, duplicated/normalized
		[]string{"a/../d.go"},      // dirty, path-cleaned
		[]string{"./s.md", "a/../d.go", "x.go", "./x.go", "internal/planned.go", "internal/planned.go"})
	if !reflect.DeepEqual(pre, []string{"d.go", "s.md"}) {
		t.Fatalf("preExisting = %v, want [d.go s.md]", pre)
	}
	if !reflect.DeepEqual(in, []string{"internal/planned.go", "x.go"}) {
		t.Fatalf("inRun = %v, want [internal/planned.go x.go]", in)
	}
}

func TestClassifyFileDrift_MatchesDetectForeignFileDrift(t *testing.T) {
	tasks := []*Task{nil, {FilesPlanned: nil}, {FilesPlanned: []string{"a.go", "./b/c.go"}}}
	untracked := []string{"s.md", "n/1.md"}
	dirty := []string{"d.go", "./b/c.go"}
	committed := []string{"a.go", "s.md", "d.go", "b/c.go", "n/1.md", "free.go"}
	for _, task := range tasks {
		pre, in := ClassifyFileDrift(task, untracked, dirty, committed)
		if got, want := pre, DetectForeignFileDrift(task, untracked, dirty, committed); !reflect.DeepEqual(got, want) {
			t.Fatalf("preExisting %v != DetectForeignFileDrift %v (task=%+v)", got, want, task)
		}
		// Disjoint, complete partition except planned-pre files: every
		// committed path lands in exactly one group or is planned.
		preSet := map[string]bool{}
		for _, f := range pre {
			preSet[f] = true
		}
		for _, f := range in {
			if preSet[f] {
				t.Fatalf("%q in both groups", f)
			}
		}
	}
}

func TestResolveDriftMode_DefaultIsBlock(t *testing.T) {
	if DefaultConfig().DriftMode != "block" {
		t.Fatalf("DefaultConfig().DriftMode = %q, want \"block\"", DefaultConfig().DriftMode)
	}
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd := newBlockOnFileDriftPRD(t, m)
	if got := m.resolveDriftMode(prd); got != "block" {
		t.Fatalf("resolveDriftMode default = %q, want \"block\"", got)
	}
}

func TestResolveDriftMode_Warn(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DriftMode = "warn"
	m, _ := NewManager(t.TempDir(), cfg, nil)
	prd := newBlockOnFileDriftPRD(t, m)
	if got := m.resolveDriftMode(prd); got != "warn" {
		t.Fatalf("resolveDriftMode = %q, want \"warn\"", got)
	}
}

func TestResolveDriftMode_UnknownOrEmptyMeansBlock(t *testing.T) {
	for _, mode := range []string{"", "garbage", "BLOCK", "WARN", " warn ", "blocking"} {
		cfg := DefaultConfig()
		cfg.DriftMode = mode
		m, _ := NewManager(t.TempDir(), cfg, nil)
		prd := newBlockOnFileDriftPRD(t, m)
		want := "block"
		if mode == "WARN" || strings.TrimSpace(mode) == "warn" {
			want = "warn"
		}
		if got := m.resolveDriftMode(prd); got != want {
			t.Errorf("DriftMode=%q: got %q, want %q", mode, got, want)
		}
	}
}

func driftTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "drift-test"},
		{"config", "user.email", "drift-test@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		driftGit(t, dir, args...)
	}
	return dir
}

func driftGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func driftWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCapturePreTaskDirtyFiles(t *testing.T) {
	dir := driftTestRepo(t)
	driftWrite(t, dir, "unstaged.txt", "1\n")
	driftWrite(t, dir, "staged.txt", "1\n")
	driftWrite(t, dir, "clean.txt", "1\n")
	driftGit(t, dir, "add", "unstaged.txt", "staged.txt", "clean.txt")
	driftGit(t, dir, "commit", "-q", "-m", "init")

	driftWrite(t, dir, "unstaged.txt", "2\n")
	driftWrite(t, dir, "staged.txt", "2\n")
	driftGit(t, dir, "add", "staged.txt")

	got := CapturePreTaskDirtyFiles(context.Background(), dir, nil)
	want := []string{"staged.txt", "unstaged.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := CapturePreTaskDirtyFiles(context.Background(), "", nil); got != nil {
		t.Fatalf("empty dir: got %v, want nil", got)
	}
	if got := CapturePreTaskDirtyFiles(context.Background(), t.TempDir(), nil); got != nil {
		t.Fatalf("non-repo dir: got %v, want nil", got)
	}
}

func TestCommittedFilesSince(t *testing.T) {
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n")
	driftGit(t, dir, "add", "base.txt")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	pre := driftGit(t, dir, "rev-parse", "HEAD")

	driftWrite(t, dir, "one.txt", "1\n")
	driftWrite(t, dir, "shared.txt", "1\n")
	driftGit(t, dir, "add", "one.txt", "shared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "first")
	driftWrite(t, dir, "two.txt", "1\n")
	driftWrite(t, dir, "shared.txt", "2\n")
	driftGit(t, dir, "add", "two.txt", "shared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "second")

	got, err := CommittedFilesSince(context.Background(), dir, pre, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"one.txt", "shared.txt", "two.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, err := CommittedFilesSince(context.Background(), dir, "", nil); got != nil || err != nil {
		t.Fatalf("empty preSHA: got %v, %v; want nil, nil", got, err)
	}
}

func TestCapturePostVerifySHA(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := driftTestRepo(t)
	driftWrite(t, dir, "a.txt", "a")
	driftGit(t, dir, "add", "a.txt")
	driftGit(t, dir, "commit", "-q", "-m", "c1")
	want := driftGit(t, dir, "rev-parse", "HEAD")
	if got := CapturePostVerifySHA(context.Background(), dir, nil); got != want {
		t.Fatalf("CapturePostVerifySHA = %q, want %q", got, want)
	}
	// Best-effort: empty dir / non-repo yield "", never an error.
	if got := CapturePostVerifySHA(context.Background(), "", nil); got != "" {
		t.Fatalf("empty dir: got %q, want \"\"", got)
	}
	if got := CapturePostVerifySHA(context.Background(), t.TempDir(), nil); got != "" {
		t.Fatalf("non-repo dir: got %q, want \"\"", got)
	}
	// A failing runner (injected) also just yields "".
	if got := CapturePostVerifySHA(context.Background(), dir, func(context.Context, string, ...string) (string, error) {
		return "", context.Canceled
	}); got != "" {
		t.Fatalf("failing runner: got %q, want \"\"", got)
	}
}

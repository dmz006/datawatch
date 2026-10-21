package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// undeclaredEditAlertRecorder captures SetAlertSink calls.
type undeclaredEditAlertRecorder struct {
	mu   sync.Mutex
	rows []string
}

func (r *undeclaredEditAlertRecorder) fn(level, title, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, level+"|"+title+"|"+body)
}

func (r *undeclaredEditAlertRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.rows...)
}

// undeclaredFixture builds a Manager (NewManager over a fresh data dir,
// config mode as given) whose PRD points at a real temp git repo with
// a.go committed. The returned task plans a.go and carries the
// captured PreTaskSHA/PreTaskDirtyFiles exactly as the spawn path sets
// them; audit and alert recorders are already installed on the manager.
func undeclaredFixture(t *testing.T, mode string, planned []string) (*Manager, *PRD, *Task, string, *driftAuditRecorder, *undeclaredEditAlertRecorder) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.UndeclaredEditMode = mode
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := driftTestRepo(t)
	driftWrite(t, dir, "a.go", "package main\n")
	driftGit(t, dir, "add", "a.go")
	driftGit(t, dir, "commit", "-q", "-m", "base")

	prd, err := m.CreatePRD("undeclared-edit", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	task := &Task{ID: "task-1", PRDID: prd.ID, FilesPlanned: planned}
	task.PreTaskSHA = driftGit(t, dir, "rev-parse", "HEAD")
	task.PreTaskDirtyFiles = CapturePreTaskDirtyFiles(context.Background(), dir, nil)
	audit := &driftAuditRecorder{}
	m.SetAuditFn(audit.fn)
	alerts := &undeclaredEditAlertRecorder{}
	m.SetAlertSink(alerts.fn)
	return m, prd, task, dir, audit, alerts
}

// workerCommit simulates the worker's session result: its planned a.go
// changes plus an unplanned b.go it created (3 added lines).
func workerCommit(t *testing.T, dir string) {
	t.Helper()
	driftWrite(t, dir, "a.go", "package main\n\ntype a int\n")
	driftWrite(t, dir, "b.go", "package main\n\ntype b int\n")
	driftGit(t, dir, "add", "a.go", "b.go")
	driftGit(t, dir, "commit", "-q", "-m", "task work")
}

func TestUndeclaredEdit_WarnMode(t *testing.T) {
	m, prd, task, dir, audit, alerts := undeclaredFixture(t, "warn", []string{"a.go"})
	workerCommit(t, dir)

	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("warn mode must never block")
	}
	if task.Status == TaskBlocked {
		t.Fatal("warn mode must not block the task")
	}
	// b.go recorded with lines > 0; planned a.go not flagged.
	if len(task.UndeclaredEdits) != 1 {
		t.Fatalf("UndeclaredEdits = %v, want exactly one entry (b.go)", task.UndeclaredEdits)
	}
	if task.UndeclaredEdits[0].Path != "b.go" {
		t.Fatalf("UndeclaredEdits[0].Path = %q, want \"b.go\"", task.UndeclaredEdits[0].Path)
	}
	if task.UndeclaredEdits[0].LinesChanged <= 0 {
		t.Fatalf("b.go LinesChanged = %d, want > 0", task.UndeclaredEdits[0].LinesChanged)
	}
	// PRD decision recorded.
	found := false
	for _, d := range prd.Decisions {
		if d.Kind == "undeclared_edit_detected" {
			found = true
			if !strings.Contains(d.Note, "b.go") {
				t.Fatalf("decision note missing file: %q", d.Note)
			}
		}
	}
	if !found {
		t.Fatal("no undeclared_edit_detected decision recorded")
	}
	// Audit event fired.
	if n := len(audit.acts); n == 0 || audit.acts[n-1] != "autonomous_undeclared_edit_detected" {
		t.Fatalf("audit actions %v, want trailing autonomous_undeclared_edit_detected", audit.acts)
	}
	// Alert fired exactly once at warn level.
	rows := alerts.all()
	if len(rows) != 1 || !strings.HasPrefix(rows[0], "warn|") || !strings.Contains(rows[0], "b.go") {
		t.Fatalf("alerts = %v, want exactly one warn-level alert naming b.go", rows)
	}
	// The check must not mutate the repo beyond the worker's own commit.
	if status := strings.TrimSpace(driftGit(t, dir, "status", "--porcelain")); status != "" {
		t.Fatalf("unexpected repo status after check: %q", status)
	}
}

func TestUndeclaredEdit_BlockMode(t *testing.T) {
	m, prd, task, dir, audit, alerts := undeclaredFixture(t, "block", []string{"a.go"})
	workerCommit(t, dir)

	if !m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("block mode must return true (halt the walk)")
	}
	if task.Status != TaskBlocked {
		t.Fatalf("status = %s, want %s", task.Status, TaskBlocked)
	}
	if !strings.HasPrefix(task.Error, "undeclared_edit:") || !strings.Contains(task.Error, "b.go") {
		t.Fatalf("error = %q, want \"undeclared_edit:\" naming b.go", task.Error)
	}
	// Findings still recorded on the task, decision + audit + alert all
	// fired before the block.
	if len(task.UndeclaredEdits) != 1 || task.UndeclaredEdits[0].Path != "b.go" {
		t.Fatalf("UndeclaredEdits = %v, want one entry for b.go", task.UndeclaredEdits)
	}
	found := false
	for _, d := range prd.Decisions {
		if d.Kind == "undeclared_edit_detected" {
			found = true
		}
	}
	if !found {
		t.Fatal("no undeclared_edit_detected decision recorded")
	}
	if n := len(audit.acts); n == 0 || audit.acts[n-1] != "autonomous_undeclared_edit_detected" {
		t.Fatalf("audit actions %v, want trailing autonomous_undeclared_edit_detected", audit.acts)
	}
	if rows := alerts.all(); len(rows) != 1 {
		t.Fatalf("alerts = %v, want exactly one alert", rows)
	}
}

func TestUndeclaredEdit_OffMode(t *testing.T) {
	m, prd, task, dir, audit, alerts := undeclaredFixture(t, "off", []string{"a.go"})
	workerCommit(t, dir)
	// Off must be a full no-op: zero git calls at all.
	m.driftGit = func(context.Context, string, ...string) (string, error) {
		t.Fatal("off mode must make zero git calls")
		return "", nil
	}

	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("off mode must never block")
	}
	if task.Status == TaskBlocked || task.Error != "" || len(task.UndeclaredEdits) != 0 {
		t.Fatalf("off mode recorded findings: status=%s error=%q edits=%v", task.Status, task.Error, task.UndeclaredEdits)
	}
	for _, d := range prd.Decisions {
		if d.Kind == "undeclared_edit_detected" {
			t.Fatal("off mode must record no decisions")
		}
	}
	if n := len(audit.acts); n != 0 {
		t.Fatalf("off mode must emit no audit events: %v", audit.acts)
	}
	if rows := alerts.all(); len(rows) != 0 {
		t.Fatalf("off mode must raise no alerts: %v", rows)
	}
}

func TestUndeclaredEdit_AllDeclared(t *testing.T) {
	m, prd, task, dir, audit, alerts := undeclaredFixture(t, "warn", []string{"a.go", "b.go"})
	workerCommit(t, dir)

	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("fully declared work must not block")
	}
	if task.Status == TaskBlocked || len(task.UndeclaredEdits) != 0 {
		t.Fatalf("fully declared work recorded findings: status=%s edits=%v", task.Status, task.UndeclaredEdits)
	}
	for _, d := range prd.Decisions {
		if d.Kind == "undeclared_edit_detected" {
			t.Fatal("no undeclared_edit_detected decision expected")
		}
	}
	if n := len(audit.acts); n != 0 {
		t.Fatalf("no audit events expected: %v", audit.acts)
	}
	if rows := alerts.all(); len(rows) != 0 {
		t.Fatalf("no alerts expected: %v", rows)
	}
}

func TestEffectiveUndeclaredEditMode(t *testing.T) {
	// Manager at the default (warn) config.
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		given string
		want  string
	}{
		{"", "warn"},         // per-call empty → configured default (warn)
		{"WARN", "warn"},     // normalized to lowercase
		{" block ", "block"}, // trimmed
		{"off", "off"},
		{"block", "block"},
		{"bogus", "warn"}, // unknown values fall back to warn
	}
	for _, tc := range cases {
		if got := m.EffectiveUndeclaredEditMode(tc.given); got != tc.want {
			t.Fatalf("EffectiveUndeclaredEditMode(%q) = %q, want %q", tc.given, got, tc.want)
		}
	}
	// Configured modes flow through when the per-call value is empty,
	// and a bogus configured value still resolves to warn.
	for _, cfgMode := range []string{"off", "block", ""} {
		cfg := DefaultConfig()
		cfg.UndeclaredEditMode = cfgMode
		mm, err := NewManager(t.TempDir(), cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := "warn"
		if cfgMode == "off" || cfgMode == "block" {
			want = cfgMode
		}
		if got := mm.EffectiveUndeclaredEditMode(""); got != want {
			t.Fatalf("config %q: EffectiveUndeclaredEditMode(\"\") = %q, want %q", cfgMode, got, want)
		}
		// An explicit per-call value always wins over the config.
		if got := mm.EffectiveUndeclaredEditMode("block"); got != "block" {
			t.Fatalf("config %q: per-call \"block\" resolved to %q", cfgMode, got)
		}
	}
}

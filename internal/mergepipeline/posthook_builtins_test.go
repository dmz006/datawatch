package mergepipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakes: no test in this file touches gh or a real remote. The tag
// built-in's presence check runs git against a throwaway local repository
// in a temporary directory.

type fakeBuiltinShip struct {
	mu    sync.Mutex
	calls []string
	fail  string // a call name that returns an error
}

func (f *fakeBuiltinShip) note(c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if f.fail == c {
		return errors.New(c + " refused")
	}
	return nil
}

func (f *fakeBuiltinShip) callsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

func (f *fakeBuiltinShip) Tag(_ context.Context, _, tag, _ string) error {
	return f.note("tag " + tag)
}
func (f *fakeBuiltinShip) Push(_ context.Context, _, b, tag string) error {
	return f.note("push " + b + " " + tag)
}
func (f *fakeBuiltinShip) Deploy(_ context.Context, _ string) error { return f.note("deploy") }
func (f *fakeBuiltinShip) Hook(_ context.Context, _ string, s Step) (string, error) {
	return "hook ran", f.note("hook " + s.Name)
}
func (f *fakeBuiltinShip) FileIssue(_ context.Context, _, repo, title, _ string) error {
	return f.note("issue " + repo + " " + title)
}

type fakeNotifier struct {
	mu    sync.Mutex
	texts []string
	err   error
}

func (f *fakeNotifier) Notify(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	return f.err
}

// localGitRepo makes a throwaway git repository with one commit in a
// temporary directory. It never points at a remote.
func localGitRepo(t *testing.T, tags ...string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("symbolic-ref", "HEAD", "refs/heads/main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "-q", "-m", "initial")
	for _, tag := range tags {
		run("tag", tag)
	}
	return dir
}

func TestPostShipRenderSubstitutesOnlyKnownPlaceholders(t *testing.T) {
	got := render("{project} v{version} {other}", "acme", "1.2.3")
	if want := "acme v1.2.3 {other}"; got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
	if got := render("no placeholders", "p", "v"); got != "no placeholders" {
		t.Fatalf("render changed plain text: %q", got)
	}
}

const testRegistry = `# Registry

| # | Item | Status | Depends on |
|---|------|--------|------------|
| 100 | Some other row | open | - |
| 101 | post-ship actions v9.9.9 | open | 100 |
`

func registryRowSettings(file, pattern string) PostShipSettings {
	return PostShipSettings{
		Enabled:            true,
		RegistryFile:       file,
		RegistryRowPattern: pattern,
	}
}

func TestRegistryRowReplacesStatusCell(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "REGISTRY.md"), []byte(testRegistry), 0o644); err != nil {
		t.Fatal(err)
	}
	note, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("REGISTRY.md", "post-ship actions v{version}"))
	if err != nil {
		t.Fatalf("RunRegistryRow: %v", err)
	}
	if want := "updated the registry row"; note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "REGISTRY.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := `# Registry

| # | Item | Status | Depends on |
|---|------|--------|------------|
| 100 | Some other row | open | - |
| 101 | post-ship actions v9.9.9 | Shipped 9.9.9 | 100 |
`
	if string(data) != want {
		t.Fatalf("registry = %q, want %q", data, want)
	}
	// no temporary file may be left behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "REGISTRY.md" {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestRegistryRowRecognizesStatusCellByValue(t *testing.T) {
	dir := t.TempDir()
	// no header row: the status cell must be recognized by its value
	body := "| 7 | do the thing | in progress | - |\n"
	if err := os.WriteFile(filepath.Join(dir, "rows.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunRegistryRow(context.Background(), dir, "acme", "1.0.0",
		registryRowSettings("rows.md", "do the thing")); err != nil {
		t.Fatalf("RunRegistryRow: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "rows.md"))
	if want := "| 7 | do the thing | Shipped 1.0.0 | - |\n"; string(data) != want {
		t.Fatalf("rows = %q, want %q", data, want)
	}
}

func TestRegistryRowRequiresExactlyOneMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "REGISTRY.md"), []byte(testRegistry), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("REGISTRY.md", "nothing matches this")); err == nil {
		t.Fatal("zero matches must fail")
	} else if !strings.Contains(err.Error(), "0 lines") {
		t.Fatalf("error does not name the count: %v", err)
	}
	if _, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("REGISTRY.md", "| open |")); err == nil {
		t.Fatal("two matches must fail")
	} else if !strings.Contains(err.Error(), "2 lines") {
		t.Fatalf("error does not name the count: %v", err)
	}
}

func TestRegistryRowStaysInsideTheProject(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "registry-outside.md")
	defer func() { _ = os.Remove(outside) }()
	for _, file := range []string{"../registry-outside.md", "/tmp/registry-outside.md"} {
		_, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
			registryRowSettings(file, "post-ship actions v9.9.9"))
		if err == nil {
			t.Fatalf("registry_file %q must be rejected", file)
		}
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("a file was created outside the project directory")
	}
}

func TestRegistryRowMissingPieces(t *testing.T) {
	dir := t.TempDir()
	if _, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("", "post-ship actions")); err == nil {
		t.Fatal("an empty registry_file must fail")
	}
	if _, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("REGISTRY.md", "")); err == nil {
		t.Fatal("an empty registry_row_pattern must fail")
	}
	if _, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("NOPE.md", "post-ship actions")); err == nil {
		t.Fatal("a missing registry file must fail")
	}
	// a matched row with no recognizable status cell fails
	body := "| 7 | do the thing | - |\n"
	if err := os.WriteFile(filepath.Join(dir, "rows.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("rows.md", "do the thing")); err == nil {
		t.Fatal("a row with no status cell must fail")
	}
}

func TestParityIssueFilesThroughTheShipRunner(t *testing.T) {
	ship := &fakeBuiltinShip{}
	note, err := RunParityIssue(context.Background(), ship, t.TempDir(), "acme", "9.9.9", PostShipSettings{
		CompanionRepository: "dmz006/app",
		IssueTitleTemplate:  "feat(i18n): parity for {project} {version}",
		NotifyTemplate:      "Shipped {version} of {project}.",
	})
	if err != nil {
		t.Fatalf("RunParityIssue: %v", err)
	}
	if want := "filed the parity issue in dmz006/app"; note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	calls := ship.callsSeen()
	if len(calls) != 1 || calls[0] != "issue dmz006/app feat(i18n): parity for acme 9.9.9" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestParityIssueRequiresItsSettings(t *testing.T) {
	dir := t.TempDir()
	if _, err := RunParityIssue(context.Background(), nil, dir, "acme", "9.9.9",
		PostShipSettings{CompanionRepository: "dmz006/app", IssueTitleTemplate: "t"}); err == nil {
		t.Fatal("a nil ship runner must fail")
	}
	if _, err := RunParityIssue(context.Background(), &fakeBuiltinShip{}, dir, "acme", "9.9.9",
		PostShipSettings{IssueTitleTemplate: "t"}); err == nil {
		t.Fatal("a missing companion_repository must fail")
	}
	if _, err := RunParityIssue(context.Background(), &fakeBuiltinShip{}, dir, "acme", "9.9.9",
		PostShipSettings{CompanionRepository: "dmz006/app"}); err == nil {
		t.Fatal("a missing issue_title_template must fail")
	}
}

func TestParityIssueRedactsRunnerErrors(t *testing.T) {
	ship := &leakyIssueShip{}
	_, err := RunParityIssue(context.Background(), ship, t.TempDir(), "acme", "9.9.9",
		PostShipSettings{CompanionRepository: "dmz006/app", IssueTitleTemplate: "t"})
	if err == nil {
		t.Fatal("a failing runner must fail the hook")
	}
	if strings.Contains(err.Error(), "ghp_") || strings.Contains(err.Error(), "secret=leaked") {
		t.Fatalf("error leaked credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error was not redacted: %v", err)
	}
}

// leakyIssueShip fails FileIssue with error text full of credentials, to
// prove the built-in redacts before returning it.
type leakyIssueShip struct{ fakeBuiltinShip }

func (f *leakyIssueShip) FileIssue(_ context.Context, _, repo, title, _ string) error {
	return errors.New(`gh: HTTP 401: token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ secret=leaked`)
}

func TestTagTagsOnlyWhenTheTagIsAbsent(t *testing.T) {
	ship := &fakeBuiltinShip{}
	dir := localGitRepo(t)
	note, err := RunTag(context.Background(), ship, dir, "9.9.9")
	if err != nil {
		t.Fatalf("RunTag (absent): %v", err)
	}
	if want := "tagged v9.9.9"; note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	if calls := ship.callsSeen(); len(calls) != 1 || calls[0] != "tag v9.9.9" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestTagSkipsAnExistingTag(t *testing.T) {
	ship := &fakeBuiltinShip{}
	dir := localGitRepo(t, "v9.9.9")
	note, err := RunTag(context.Background(), ship, dir, "9.9.9")
	if err != nil {
		t.Fatalf("an existing tag is skipped, not failed: %v", err)
	}
	if !strings.Contains(note, "already exists") || !strings.Contains(note, "skipped") {
		t.Fatalf("note = %q", note)
	}
	if calls := ship.callsSeen(); len(calls) != 0 {
		t.Fatalf("an existing tag must not call the runner: %v", calls)
	}
}

func TestTagRequiresItsWiring(t *testing.T) {
	dir := localGitRepo(t)
	if _, err := RunTag(context.Background(), nil, dir, "9.9.9"); err == nil {
		t.Fatal("a nil ship runner must fail")
	}
	if _, err := RunTag(context.Background(), &fakeBuiltinShip{}, dir, ""); err == nil {
		t.Fatal("an empty version must fail")
	}
}

func TestNotifySendsTheRenderedTemplate(t *testing.T) {
	n := &fakeNotifier{}
	note, err := RunNotify(context.Background(), n, "acme", "9.9.9",
		PostShipSettings{NotifyTemplate: "shipped {version} of {project}"})
	if err != nil {
		t.Fatalf("RunNotify: %v", err)
	}
	if want := "sent the notification"; note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	if len(n.texts) != 1 || n.texts[0] != "shipped 9.9.9 of acme" {
		t.Fatalf("texts = %v", n.texts)
	}
}

func TestNotifyFailsWithoutAWiring(t *testing.T) {
	if _, err := RunNotify(context.Background(), nil, "acme", "9.9.9",
		PostShipSettings{NotifyTemplate: "hi"}); err == nil {
		t.Fatal("a nil notifier must fail")
	} else if !strings.Contains(err.Error(), "notifier") {
		t.Fatalf("error is not clear: %v", err)
	}
	if _, err := RunNotify(context.Background(), &fakeNotifier{}, "acme", "9.9.9",
		PostShipSettings{}); err == nil {
		t.Fatal("an empty notify_template must fail")
	}
	n := &fakeNotifier{err: errors.New("send failed: token: abc123def456")}
	if _, err := RunNotify(context.Background(), n, "acme", "9.9.9",
		PostShipSettings{NotifyTemplate: "hi"}); err == nil {
		t.Fatal("a failing notifier must fail the hook")
	} else if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error was not redacted: %v", err)
	}
}

func TestBuiltinErrorsAreRedacted(t *testing.T) {
	dir := t.TempDir()
	// a missing file whose name looks like a credential: the error text
	// must come back redacted, never raw
	_, err := RunRegistryRow(context.Background(), dir, "acme", "9.9.9",
		registryRowSettings("token abc123def456.md", "x"))
	if err == nil {
		t.Fatal("a missing registry file must fail")
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error text did not pass through redact: %v", err)
	}
	if strings.Contains(err.Error(), "abc123def456") {
		t.Fatalf("credential-looking text leaked: %v", err)
	}
}

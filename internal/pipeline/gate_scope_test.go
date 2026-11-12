package pipeline

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGateScopeRunner is the in-memory GateScopeRunner used by the unit
// tests: unknown patterns resolve to no packages, and errors are injected
// per test.
type fakeGateScopeRunner struct {
	list       map[string][]string // package pattern -> resolved import paths
	imports    map[string][]string // import path -> one-level imports
	changed    []string
	listErrs   map[string]string // package pattern -> injected error
	importErr  error
	changedErr error
}

func (f fakeGateScopeRunner) ListPackages(dir string, patterns ...string) ([]string, error) {
	_ = dir
	for _, p := range patterns {
		if msg, ok := f.listErrs[p]; ok {
			return nil, errors.New(msg)
		}
		if v, ok := f.list[p]; ok {
			return append([]string(nil), v...), nil
		}
	}
	return nil, nil
}

func (f fakeGateScopeRunner) PackageImports(dir string) (map[string][]string, error) {
	_ = dir
	if f.importErr != nil {
		return nil, f.importErr
	}
	return f.imports, nil
}

func (f fakeGateScopeRunner) ChangedFiles(dir, baseSHA string) ([]string, error) {
	return f.changed, f.changedErr
}

func gateScopeHasPackage(t *testing.T, plan GateScopePlan, pkg string) {
	t.Helper()
	for _, p := range plan.Packages {
		if p == pkg {
			return
		}
	}
	t.Errorf("plan packages %v do not contain %s", plan.Packages, pkg)
}

func gateScopeNotPackage(t *testing.T, plan GateScopePlan, pkg string) {
	t.Helper()
	for _, p := range plan.Packages {
		if p == pkg {
			t.Errorf("plan packages %v must not contain %s", plan.Packages, pkg)
			return
		}
	}
}

func gateScopeHasReason(t *testing.T, plan GateScopePlan, pkg, reason string) {
	t.Helper()
	for _, r := range plan.Reasons {
		if r.Package == pkg && r.Reason == reason {
			return
		}
	}
	t.Errorf("no (package=%s, reason=%q) in %v", pkg, reason, plan.Reasons)
}

func TestGateScope_Parse(t *testing.T) {
	for _, v := range []string{"", GateScopeFull, GateScopeTouchedPlusSmoke, GateScopeTouched} {
		got, err := ParseGateScope(v)
		if err != nil || got != v {
			t.Errorf("ParseGateScope(%q) = (%q, %v); want pass-through", v, got, err)
		}
	}
	_, err := ParseGateScope("everything")
	if err == nil {
		t.Fatal("an unrecognized scope must be an error")
	}
	for _, want := range []string{GateScopeFull, GateScopeTouchedPlusSmoke, GateScopeTouched} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name %q (got %q)", want, err)
		}
	}
}

func TestGateScope_ResolveOrder(t *testing.T) {
	cases := []struct {
		name       string
		prd        string
		global     string
		want       string
		wantSource string
	}{
		{"unset uses the built-in default", "", "", GateScopeFull, "default"},
		{"automaton beats global", GateScopeTouched, GateScopeTouchedPlusSmoke, GateScopeTouched, "automaton"},
		{"global beats default", "", GateScopeTouchedPlusSmoke, GateScopeTouchedPlusSmoke, "global"},
		{"empty automaton inherits global", "", GateScopeTouched, GateScopeTouched, "global"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scope, source := ResolveGateScope(c.prd, c.global)
			if scope != c.want || source != c.wantSource {
				t.Errorf("ResolveGateScope(%q, %q) = (%q, %q); want (%q, %q)",
					c.prd, c.global, scope, source, c.want, c.wantSource)
			}
		})
	}
	if _, err := ParseGateScope("bogus"); err == nil {
		t.Error("an invalid value must be caught by ParseGateScope")
	}
}

func TestGateScope_TouchedPackagesFromDiff(t *testing.T) {
	runner := fakeGateScopeRunner{
		changed: []string{
			"cmd/app/main.go",
			"alpha/alpha.go",
			"alpha/alpha_test.go",
			"nested/inner/inner.go",
			"deleted/pkg/removed.go", // directory no longer has a package
			"docs/notes.md",          // unattributable: must not widen scope
		},
		list: map[string][]string{
			"./cmd/app":      {"example.com/gs/cmd/app"},
			"./alpha":        {"example.com/gs/alpha"},
			"./nested/inner": {"example.com/gs/nested/inner"},
			// "./deleted/pkg" deliberately absent: no package there.
		},
	}
	plan := PlanGateScope(runner, GateScopeRequest{Dir: "/x", BaseSHA: "base", Scope: GateScopeTouched})
	if plan.Full || plan.NothingToRun || plan.FallbackReason != "" {
		t.Fatalf("expected a scoped plan, got %+v", plan)
	}
	gateScopeHasPackage(t, plan, "example.com/gs/cmd/app")
	gateScopeHasPackage(t, plan, "example.com/gs/alpha")
	gateScopeHasPackage(t, plan, "example.com/gs/nested/inner")
	gateScopeNotPackage(t, plan, "example.com/gs/deleted/pkg")
	if len(plan.Packages) != 3 {
		t.Errorf("unexpected extra packages: %v", plan.Packages)
	}
	gateScopeHasReason(t, plan, "example.com/gs/alpha", "changed")
	if plan.ChangedFilesTotal != 6 || len(plan.ChangedFiles) != 6 {
		t.Errorf("changed files not recorded: total=%d listed=%v", plan.ChangedFilesTotal, plan.ChangedFiles)
	}

	// The recording cap keeps the total count.
	cappy := fakeGateScopeRunner{changed: make([]string, 300)}
	for i := range cappy.changed {
		cappy.changed[i] = "pkg/d.go"
	}
	plan = PlanGateScope(cappy, GateScopeRequest{Dir: "/x", Scope: GateScopeTouched})
	if plan.ChangedFilesTotal != 300 || len(plan.ChangedFiles) != maxChangedFiles {
		t.Errorf("cap not applied: total=%d listed=%d", plan.ChangedFilesTotal, len(plan.ChangedFiles))
	}
}

func TestGateScope_ImportersOneLevelOnly(t *testing.T) {
	runner := fakeGateScopeRunner{
		changed: []string{"a/a.go"},
		list:    map[string][]string{"./a": {"example.com/gs/a"}},
		imports: map[string][]string{
			"example.com/gs/a": {"fmt"},
			"example.com/gs/b": {"example.com/gs/a", "fmt"},
			"example.com/gs/c": {"example.com/gs/b"},
		},
	}
	plan := PlanGateScope(runner, GateScopeRequest{Dir: "/x", Scope: GateScopeTouched})
	gateScopeHasPackage(t, plan, "example.com/gs/a")
	gateScopeHasPackage(t, plan, "example.com/gs/b")
	gateScopeNotPackage(t, plan, "example.com/gs/c") // no recursion
	gateScopeHasReason(t, plan, "example.com/gs/a", "changed")
	gateScopeHasReason(t, plan, "example.com/gs/b", "imports example.com/gs/a")
}

func TestGateScope_SmokeSetScope(t *testing.T) {
	runner := fakeGateScopeRunner{
		changed: []string{"a/a.go"},
		list: map[string][]string{
			"./a":            {"example.com/gs/a"},
			"./examples/web": {"example.com/gs/web"},
			// "no-such-pkg" deliberately absent: matches nothing.
		},
		imports: map[string][]string{
			"example.com/gs/a": {"fmt"},
		},
	}
	smoke := []string{"./examples/web", "no-such-pkg"}
	plan := PlanGateScope(runner, GateScopeRequest{
		Dir: "/x", Scope: GateScopeTouchedPlusSmoke, SmokePatterns: smoke,
	})
	if plan.Full || plan.FallbackReason != "" {
		t.Fatalf("expected a scoped plan, got %+v", plan)
	}
	gateScopeHasPackage(t, plan, "example.com/gs/web")
	gateScopeHasReason(t, plan, "example.com/gs/web", "smoke set")
	foundWarning := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "no-such-pkg") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("an unmatched smoke pattern must be recorded as a warning: %v", plan.Warnings)
	}

	// The same configuration under the touched scope must not pick up smoke.
	plain := PlanGateScope(runner, GateScopeRequest{
		Dir: "/x", Scope: GateScopeTouched, SmokePatterns: smoke,
	})
	gateScopeNotPackage(t, plain, "example.com/gs/web")
}

func TestGateScope_FinalAndFullScope(t *testing.T) {
	runner := fakeGateScopeRunner{
		changed: []string{"a/a.go"},
		list:    map[string][]string{"./a": {"example.com/gs/a"}},
	}
	final := PlanGateScope(runner, GateScopeRequest{
		Dir: "/x", Scope: GateScopeTouched, IsFinal: true,
	})
	if !final.Full {
		t.Fatal("a final gate task must run the full suite")
	}
	if !strings.Contains(final.FullReason, "full suite") {
		t.Errorf("missing explanation in %q", final.FullReason)
	}

	full := PlanGateScope(runner, GateScopeRequest{Dir: "/x", Scope: GateScopeFull})
	if !full.Full || !strings.Contains(full.FullReason, "scope is full") {
		t.Errorf("full-scope plan: %+v", full)
	}

	inherit := PlanGateScope(runner, GateScopeRequest{Dir: "/x"})
	if !inherit.Full || inherit.Scope != GateScopeFull {
		t.Errorf("an empty scope must inherit the full default: %+v", inherit)
	}
}

func TestGateScope_ModuleFileWidensToFull(t *testing.T) {
	for _, file := range []string{"go.mod", "go.sum"} {
		runner := fakeGateScopeRunner{
			changed: []string{file, "a/a.go"},
			list:    map[string][]string{"./a": {"example.com/gs/a"}},
		}
		plan := PlanGateScope(runner, GateScopeRequest{Dir: "/x", Scope: GateScopeTouched})
		if !plan.Full {
			t.Errorf("%s change must widen to full: %+v", file, plan)
		}
		if plan.FullReason == "" {
			t.Errorf("the widening reason must be recorded for %s", file)
		}
		if plan.FallbackReason != "" {
			t.Errorf("%s change is a widening, not a failure fallback: %q", file, plan.FallbackReason)
		}
	}
}

func TestGateScope_RunnerErrorFallsBackToFull(t *testing.T) {
	cases := []struct {
		name   string
		runner fakeGateScopeRunner
		req    GateScopeRequest
	}{
		{"diff failure", fakeGateScopeRunner{changedErr: errors.New("git is unavailable")},
			GateScopeRequest{Dir: "/x", Scope: GateScopeTouched}},
		{"directory listing failure", fakeGateScopeRunner{
			changed:  []string{"a/a.go"},
			listErrs: map[string]string{"./a": "go toolchain broken"},
		}, GateScopeRequest{Dir: "/x", Scope: GateScopeTouched}},
		{"smoke listing failure", fakeGateScopeRunner{
			changed:  []string{"a/a.go"},
			listErrs: map[string]string{"./examples/web": "go toolchain broken"},
		}, GateScopeRequest{Dir: "/x", Scope: GateScopeTouchedPlusSmoke, SmokePatterns: []string{"./examples/web"}}},
		{"import graph failure", fakeGateScopeRunner{
			changed:   []string{"a/a.go"},
			list:      map[string][]string{"./a": {"example.com/gs/a"}},
			importErr: errors.New("no import graph"),
		}, GateScopeRequest{Dir: "/x", Scope: GateScopeTouched}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := PlanGateScope(c.runner, c.req)
			if !plan.Full {
				t.Fatalf("expected a full-suite fallback, got %+v", plan)
			}
			if plan.FallbackReason == "" {
				t.Fatal("a fallback reason must be recorded")
			}
			t.Logf("fallback reason: %q", plan.FallbackReason)
		})
	}
}

func TestGateScope_DocsOnlyNothingToRun(t *testing.T) {
	runner := fakeGateScopeRunner{
		changed: []string{"docs/notes.md", "README.md"},
		list:    map[string][]string{"./examples/web": {"example.com/gs/web"}},
	}
	plan := PlanGateScope(runner, GateScopeRequest{Dir: "/x", Scope: GateScopeTouched})
	if !plan.NothingToRun || len(plan.Packages) != 0 || plan.Full {
		t.Fatalf("a docs-only touched task must be marked nothing-to-run: %+v", plan)
	}
	if !strings.Contains(strings.Join(plan.Warnings, " "), "nothing to run") {
		t.Errorf("the nothing-to-run reason is missing: %v", plan.Warnings)
	}

	// Under touched-plus-smoke the smoke set still runs.
	smoked := PlanGateScope(runner, GateScopeRequest{
		Dir: "/x", Scope: GateScopeTouchedPlusSmoke, SmokePatterns: []string{"./examples/web"},
	})
	if smoked.NothingToRun {
		t.Fatalf("the smoke set must still run: %+v", smoked)
	}
	gateScopeHasPackage(t, smoked, "example.com/gs/web")
}

func TestGateScope_ScopedTestCommand(t *testing.T) {
	pkgs := []string{"example.com/gs/a", "example.com/gs/web"}

	cmd, ok, reason := ScopedTestCommand("go test ./...", []string{"example.com/gs/a"})
	if !ok {
		t.Fatalf("ok=false: %s", reason)
	}
	if cmd != "go test 'example.com/gs/a'" {
		t.Errorf("cmd = %q", cmd)
	}

	cmd, ok, reason = ScopedTestCommand("go test -count=1 ./...", pkgs)
	if !ok {
		t.Fatalf("ok=false: %s", reason)
	}
	if !strings.Contains(cmd, "'-count=1'") || !strings.Contains(cmd, "example.com/gs/web") ||
		strings.Contains(cmd, "./...") {
		t.Errorf("cmd = %q", cmd)
	}

	// A -run pattern value that is not a package path must be kept.
	cmd, ok, reason = ScopedTestCommand("go test -run '^Alpha$' ./...", []string{"example.com/gs/a"})
	if !ok {
		t.Fatalf("ok=false: %s", reason)
	}
	if !strings.Contains(cmd, "'^Alpha$'") {
		t.Errorf("the -run value must be kept: %q", cmd)
	}

	// Compound command: the non-Go segment is untouched, go test narrowed.
	compound := "go test ./... && node --test internal/server/web/*.test.js"
	cmd, ok, reason = ScopedTestCommand(compound, pkgs)
	if !ok {
		t.Fatalf("ok=false: %s", reason)
	}
	if !strings.Contains(cmd, "node --test internal/server/web/*.test.js") ||
		!strings.Contains(cmd, "example.com/gs/web") {
		t.Errorf("cmd = %q", cmd)
	}
	if strings.Count(cmd, "example.com/gs/") != 2 {
		t.Errorf("both scoped packages must appear: %q", cmd)
	}

	// Two go test segments: both are narrowed.
	two := "go test ./a && go test -count=1 ./b"
	cmd, ok, reason = ScopedTestCommand(two, []string{"example.com/gs/a"})
	if !ok {
		t.Fatalf("ok=false: %s", reason)
	}
	if strings.Count(cmd, "example.com/gs/a") != 2 {
		t.Errorf("both go test segments must be narrowed: %q", cmd)
	}

	for _, bad := range []string{
		"go test ./...; true",
		"go test ./... | tee out.log",
		"echo hi && node --test x.test.js", // no go test segment
		"go test $(pwd) ./...",
	} {
		_, ok, reason := ScopedTestCommand(bad, pkgs)
		if ok {
			t.Errorf("%q must be rejected", bad)
		}
		if reason == "" {
			t.Errorf("rejection of %q needs a reason", bad)
		}
	}
}

func TestGateScope_ScopedVetCommand(t *testing.T) {
	got := ScopedVetCommand([]string{"example.com/gs/a", "example.com/gs/web"})
	want := "go vet 'example.com/gs/a' 'example.com/gs/web'"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := ScopedVetCommand(nil); got != "go vet ./..." {
		t.Errorf("got %q", got)
	}
}

func TestGateScope_RestrictBaseline(t *testing.T) {
	baseline := TestResult{
		ExitCode:        1,
		FailedTests:     []string{"example.com/gs/alpha.TestA", "example.com/gs/beta.TestB", "example.com/gs/beta"},
		PackageFailures: []string{"example.com/gs/beta"},
		PassCount:       4,
		FailCount:       3,
		TestsFound:      7,
	}
	plan := RestrictBaseline(baseline, []string{"example.com/gs/alpha"})
	if len(plan.FailedTests) != 1 || plan.FailedTests[0] != "example.com/gs/alpha.TestA" {
		t.Errorf("FailedTests = %v", plan.FailedTests)
	}
	if len(plan.PackageFailures) != 0 {
		t.Errorf("PackageFailures = %v", plan.PackageFailures)
	}
	if plan.FailCount != 1 || plan.PassCount != 4 || plan.TestsFound != 5 {
		t.Errorf("counts: pass=%d fail=%d found=%d", plan.PassCount, plan.FailCount, plan.TestsFound)
	}
	if plan.ExitCode != 1 {
		t.Errorf("a remaining failure must keep the non-zero exit: %d", plan.ExitCode)
	}

	// Compared against the scoped current run, the untouched "beta"
	// failures must not read as an improvement.
	current := TestResult{
		ExitCode: 1, FailedTests: []string{"example.com/gs/alpha.TestA"},
		PassCount: 3, FailCount: 1, TestsFound: 4,
	}
	if got := CompareResults(plan, current); got.Regression || got.Improved || got.Invalid {
		t.Fatalf("expected a stable comparison of the restricted baseline: %+v", got)
	}

	// Restricting to packages with no baseline failures yields a clean
	// baseline that ClassifyBaseline accepts.
	clean := RestrictBaseline(baseline, []string{"example.com/gs/web"})
	if len(clean.FailedTests) != 0 || len(clean.PackageFailures) != 0 || clean.ExitCode != 0 {
		t.Errorf("clean baseline: %v exit=%d", clean.FailedTests, clean.ExitCode)
	}
	if valid, reason := ClassifyBaseline(clean); !valid {
		t.Errorf("the clean restricted baseline must be usable: %s", reason)
	}
}

func TestGateScope_VetOutputParsing(t *testing.T) {
	output := `# example.com/gs/beta
example.com/gs/beta/beta.go:10:5: printf format %d has arg x (type string)
# example.com/gs/alpha
# example.com/gs/alpha.test
vet: alpha.go:20:3: unreachable code
# example.com/gs/clean [no test files]
`
	got := VetFailuresFromOutput(output)
	want := []string{"example.com/gs/alpha", "example.com/gs/beta"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if got := VetFailuresFromOutput("# example.com/gs/clean [no test files]\n"); len(got) != 0 {
		t.Errorf("a bare header is not a failure: %v", got)
	}
	if got := VetFailuresFromOutput(""); len(got) != 0 {
		t.Errorf("empty output has no failures: %v", got)
	}

	diff := VetRegressions([]string{"example.com/gs/alpha"}, []string{"example.com/gs/alpha", "example.com/gs/beta"})
	if len(diff) != 1 || diff[0] != "vet:example.com/gs/beta" {
		t.Errorf("diff = %v", diff)
	}
	if got := VetRegressions(nil, []string{"example.com/gs/alpha"}); len(got) != 1 || got[0] != "vet:example.com/gs/alpha" {
		t.Errorf("diff = %v", got)
	}
	if got := VetRegressions([]string{"example.com/gs/alpha"}, []string{"example.com/gs/alpha"}); len(got) != 0 {
		t.Errorf("diff = %v", got)
	}
}

// listImportsRunner pairs the real go-list-backed runner with a fixed set
// of changed files, so the real runner's mapping is exercised end to end.
type listImportsRunner struct {
	listImports GateScopeRunner
	changed     []string
}

func (r listImportsRunner) ListPackages(dir string, patterns ...string) ([]string, error) {
	return r.listImports.ListPackages(dir, patterns...)
}

func (r listImportsRunner) PackageImports(dir string) (map[string][]string, error) {
	return r.listImports.PackageImports(dir)
}

func (r listImportsRunner) ChangedFiles(dir, baseSHA string) ([]string, error) {
	return r.changed, nil
}

// TestGateScope_RealModuleScopePlan runs the go-list-backed runner against
// a small real module in a temp directory.
func TestGateScope_RealModuleScopePlan(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("go.mod", "module example.com/gs\n\ngo 1.21\n")
	mustWrite("alpha/alpha.go", "package alpha\n\nfunc Alpha() int { return 1 }\n")
	mustWrite("beta/beta.go",
		"package beta\n\nimport \"example.com/gs/alpha\"\n\nfunc Beta() int { return alpha.Alpha() }\n")

	runner := listImportsRunner{listImports: NewGateScopeListRunner(), changed: []string{"alpha/alpha.go"}}
	plan := PlanGateScope(runner, GateScopeRequest{Dir: dir, Scope: GateScopeTouched})
	if plan.Full || plan.FallbackReason != "" {
		t.Fatalf("expected a scoped plan from a real module, got %+v", plan)
	}
	gateScopeHasPackage(t, plan, "example.com/gs/alpha")
	gateScopeHasPackage(t, plan, "example.com/gs/beta")
	gateScopeHasReason(t, plan, "example.com/gs/alpha", "changed")
	gateScopeHasReason(t, plan, "example.com/gs/beta", "imports example.com/gs/alpha")
}

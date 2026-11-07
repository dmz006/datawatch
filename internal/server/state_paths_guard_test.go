package server

// Static guard: blocks new state paths derived from the process home
// directory ($HOME) outside the closed allow list.
//
// Datawatch-owned state lives under the configured data directory (see
// statePath / dataDirPath in state_paths.go). Since the home-derived
// defaults were moved onto the data directory, a direct os.UserHomeDir /
// os.Getenv("HOME") call in a new server handler is how the drift happened
// before; this test is the backstop.
//
// The scan covers every non-test .go file directly in internal/server and
// flags any call to os.UserHomeDir(...) or os.Getenv("HOME"). A flagged call
// is classified exactly one of two ways, in the same test file:
//
//  - statePathAllowList (an allow-list constant): the call reads the
//    operator's own workspace or their own folders — never datawatch-owned
//    state. Every line carries a one-line reason, and every line must
//    match a current hit (a stale line is a failure, so the list cannot
//    outlive its reason).
//  - statePathReportedBlockers: datawatch-owned home-derived state paths
//    the scan still finds. These are NOT allowed; per the task they are
//    reported as blockers. They are deliberately kept out of the allow
//    list so the blocker stays visible, and each line must also match a
//    current hit (if the code moves, the blocker must be re-reviewed).
//
// Any flagged call in a file/function on NEITHER list is a new,
// unclassified home-derived state path and fails the test: report it as a
// blocker, do not add it to the allow list.
//
// Classification of every current hit (verified against current code):
//
// ALLOWS — operator-owned reads, never datawatch state:
//   - api.go handleStartSession / handleWS — default project directory for a
//     new session: the operator's own workspace, not daemon state.
//   - api.go handleFiles / handleFilesMkdir / handleFilesDownload — file
//     browser on the operator's own workspace.
//   - bl333_file_service.go sessionRoot / fileServiceRoot /
//     handleFilesJSONUpload / handleFilesUpload / handleFilesDelete — user
//     file roots: operator-chosen folders (FileServiceRoot/RootPath, or
//     their tilde prefixes).
//   - session_progress.go piSessionsRootServer — pi tool session JSONL logs,
//     read-only, PI_CODING_AGENT_DIR environment override checked first.
//   - v52710_channel_info.go scanStaleMCPJSON — read-only staleness check of
//     the operator's own project MCP file ($HOME/.mcp.json).
//
// BLOCKER — datawatch-owned state, reported, not allowed:
//   - state_paths.go dataDirPath — the designated single-source data
//     directory path builder, but with an empty dataDir it falls back to
//     config.DefaultConfig().DataDir, which internal/config builds as
//     $HOME/.datawatch from os.UserHomeDir() (internal/config/config.go,
//     DefaultConfig: `DataDir: filepath.Join(home, ".datawatch")`). So
//     datawatch-owned state is still home-derived by default, and the
//     home read happens inside internal/config, which this scan does not
//     cover — the guard cannot see it, which is exactly why the home use
//     stays classified as a reported blocker instead of an allow-list
//     exemption. Until the config package derives the default data
//     directory without $HOME (or the default is operator-declared), this
//     is open work, not a permission.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// A classified call site: the file, its enclosing top-level function, and a
// one-line reason. Adding a line without a stated reason is a review
// rejection.
type statePathEntry struct {
	file   string
	fun    string
	reason string
}

// statePathAllowList is the closed allow list for home-derived path calls.
// Format: one line per entry, "file<TAB>function<TAB>one-line reason". A
// row matching no current hit is a stale-entry failure; a hit matching no
// row (and no blocker row) is a new-state blocker. Blank lines are ignored.
const statePathAllowList = `
	api.go	handleStartSession	default project directory for a new session — the operator's own workspace, not daemon state
	api.go	handleWS	default project directory for a new session — the operator's own workspace, not daemon state
	api.go	handleFiles	file browser on the operator's own workspace, not daemon state
	api.go	handleFilesMkdir	file browser on the operator's own workspace, not daemon state
	api.go	handleFilesDownload	file browser on the operator's own workspace, not daemon state
	bl333_file_service.go	sessionRoot	user file root — operator-chosen folder (FileServiceRoot/RootPath) or its ~/... tilde prefix
	bl333_file_service.go	fileServiceRoot	user file root — operator-chosen folder (FileServiceRoot/RootPath) or its ~/... tilde prefix
	bl333_file_service.go	handleFilesJSONUpload	user file root — operator-chosen folder (FileServiceRoot/RootPath) or its ~/... tilde prefix
	bl333_file_service.go	handleFilesUpload	user file root — operator-chosen folder (FileServiceRoot/RootPath) or its ~/... tilde prefix
	bl333_file_service.go	handleFilesDelete	user file root — operator-chosen folder (FileServiceRoot/RootPath) or its ~/... tilde prefix
	session_progress.go	piSessionsRootServer	pi tool session JSONL logs, read-only; PI_CODING_AGENT_DIR environment override is checked first
	v52710_channel_info.go	scanStaleMCPJSON	read-only staleness check of the operator's own project MCP file ($HOME/.mcp.json)
`

// statePathReportedBlockers records datawatch-owned home-derived state
// paths the scan still finds. These are reported blockers, NOT exemptions:
// the point of keeping them in a separate constant is that they stay
// visible as open work and can never be mistaken for an allowed read. The
// test fails if a blocker row stops matching a current hit, so a change
// that removes (or moves) the home-derived state forces a re-classification
// instead of silently retiring the blocker.
const statePathReportedBlockers = `
	state_paths.go	dataDirPath	BLOCKER: with an empty dataDir it falls back to config.DefaultConfig().DataDir, which internal/config builds as $HOME/.datawatch from os.UserHomeDir() — datawatch-owned state is still home-derived, and the fallback lives in internal/config, outside this scan; report, do not allow-list
`

// parseStatePathList splits an "file<TAB>function<TAB>reason" constant into
// entries, failing on malformed lines.
func parseStatePathList(t *testing.T, name, raw string) []statePathEntry {
	t.Helper()
	var out []statePathEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimLeft(line, "\t ")
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
			t.Fatalf("%s: malformed line %q — expected \"file<TAB>function<TAB>one-line reason\"", name, line)
		}
		out = append(out, statePathEntry{file: fields[0], fun: fields[1], reason: fields[2]})
	}
	if len(out) == 0 {
		t.Fatalf("%s is empty — the constant must be listed explicitly, not inferred", name)
	}
	return out
}

func TestStatePathGuardNoHomeStateOutsideAllowList(t *testing.T) {
	type hit struct {
		file string
		fun  string
		line int
	}

	// go test runs with the package directory as the working directory.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var hits []hit

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if !importsPlainOS(file) {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "os" {
				return true
			}
			flagged := sel.Sel.Name == "UserHomeDir"
			if sel.Sel.Name == "Getenv" && len(call.Args) == 1 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value == `"HOME"` {
					flagged = true
				}
			}
			if !flagged {
				return true
			}
			enclosing, ok := currentFunc(file, call.Pos())
			if !ok {
				t.Fatalf("%s: flagged call at line %d has no enclosing function; review it", name, fset.Position(call.Pos()).Line)
			}
			hits = append(hits, hit{file: name, fun: enclosing, line: fset.Position(call.Pos()).Line})
			return true
		})
	}

	if len(hits) == 0 {
		t.Fatal("scan found no os.UserHomeDir / os.Getenv(\"HOME\") calls at all — scan machinery is broken or the classification has gone fully stale")
	}

	allowed := parseStatePathList(t, "statePathAllowList", statePathAllowList)
	blockers := parseStatePathList(t, "statePathReportedBlockers", statePathReportedBlockers)

	key := func(f, fn string) string { return f + "\x00" + fn }
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[key(a.file, a.fun)] = true
	}
	blockerSet := make(map[string]bool, len(blockers))
	for _, b := range blockers {
		if allowedSet[key(b.file, b.fun)] {
			t.Fatalf("state path guard: %s :: %s appears in BOTH statePathAllowList and statePathReportedBlockers — it is one thing: allow it (with a true reason) or keep it a blocker", b.file, b.fun)
		}
		blockerSet[key(b.file, b.fun)] = true
	}
	hitSet := make(map[string]bool, len(hits))
	for _, h := range hits {
		hitSet[key(h.file, h.fun)] = true
	}

	var unclassified []hit
	for _, h := range hits {
		if !allowedSet[key(h.file, h.fun)] && !blockerSet[key(h.file, h.fun)] {
			unclassified = append(unclassified, h)
		}
	}
	if len(unclassified) > 0 {
		sort.Slice(unclassified, func(i, j int) bool { return unclassified[i].line < unclassified[j].line })
		t.Errorf("state path guard: %d flagged call(s) are on NEITHER list. A datawatch-owned state path like this is a new blocker — report it, do not add it to the allow list:", len(unclassified))
		for _, h := range unclassified {
			t.Errorf("  %s:%d in %s", h.file, h.line, h.fun)
		}
	}
	for _, a := range allowed {
		if !hitSet[key(a.file, a.fun)] {
			t.Errorf("state path guard: stale allow-list entry %s :: %s (%s) — no current hit matches; remove the row", a.file, a.fun, a.reason)
		}
	}
	for _, b := range blockers {
		if !hitSet[key(b.file, b.fun)] {
			t.Errorf("state path guard: blocker %s :: %s (%s) no longer matches a current hit — the code moved or the home-derived state is gone; re-classify it deliberately instead of letting the blocker retire silently", b.file, b.fun, b.reason)
		}
		// Keep the blocker in the record: every run must name it.
		t.Logf("RECORDED BLOCKER (reported, not allowed): %s :: %s — %s", b.file, b.fun, b.reason)
	}
}

// importsPlainOS reports whether file imports the standard library "os"
// package under its default name. An aliased or dot import would hide
// os.* calls from the scan; neither pattern exists in this package, so if
// one ever appears the scan simply never sees it and the guard must be
// extended at the same time.
func importsPlainOS(file *ast.File) bool {
	if file == nil || file.Imports == nil {
		return false
	}
	for _, imp := range file.Imports {
		if imp == nil || imp.Path == nil || imp.Path.Value != `"os"` {
			continue
		}
		if imp.Name != nil && imp.Name.Name != "os" {
			return false
		}
		return true
	}
	return false
}

// currentFunc returns the name of the innermost top-level function (method
// set function) whose body contains the given position.
func currentFunc(file *ast.File, pos token.Pos) (string, bool) {
	var name string
	var best int
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || fd.Name == nil {
			continue
		}
		lo, hi := fd.Pos(), fd.Body.End()
		if pos < lo || pos > hi {
			continue
		}
		size := int(hi - lo)
		if best == 0 || size < best {
			best = size
			name = fd.Name.Name
		}
	}
	return name, name != ""
}

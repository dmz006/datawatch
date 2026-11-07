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
// is permitted only if its file and enclosing function appear in
// statePathAllowList below, and every allow-list entry must match a current
// hit (a stale entry is a failure, so the list cannot outlive its reason).
//
// Classification of every remaining hit (verified against current code):
//
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
//   - state_paths.go dataDirPath — the designated single-source data
//     directory path builder; its only home use expands a leading "~/..."
//     the operator wrote, and it is never called when an explicit data
//     directory is configured. New handlers must build state paths through
//     this, never through os.UserHomeDir directly.
//
// Any datawatch-owned state path found in a file/function not listed here
// is a blocker, not an allow-list addition.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// One-line reasons for the allow list. Entries reference these constants;
// adding a row without a stated reason is a review rejection.
const (
	reasonAPIProjectDir   = "default project directory for a new session — the operator's own workspace, not daemon state"
	reasonAPIFileBrowser  = "file browser on the operator's own workspace, not daemon state"
	reasonFileServiceRoot = "user file root — operator-chosen folder (FileServiceRoot/RootPath) or its ~/... tilde prefix"
	reasonPiSessionLogs   = "pi tool session JSONL logs, read-only; PI_CODING_AGENT_DIR environment override is checked first"
	reasonOperatorMCPFile = "read-only staleness check of the operator's own project MCP file ($HOME/.mcp.json)"
	reasonDataDirSource   = "designated single-source data dir path builder; home only expands a ~/... prefix the operator wrote"
)

// statePathAllowList is the closed allow list for home-derived path calls.
// File and function must both match a current hit (see the scan below); a
// row matching nothing is a stale-entry failure, a hit matching no row is a
// new-state blocker.
var statePathAllowList = []struct {
	file   string
	fun    string
	reason string
}{
	{file: "api.go", fun: "handleStartSession", reason: reasonAPIProjectDir},
	{file: "api.go", fun: "handleWS", reason: reasonAPIProjectDir},
	{file: "api.go", fun: "handleFiles", reason: reasonAPIFileBrowser},
	{file: "api.go", fun: "handleFilesMkdir", reason: reasonAPIFileBrowser},
	{file: "api.go", fun: "handleFilesDownload", reason: reasonAPIFileBrowser},
	{file: "bl333_file_service.go", fun: "sessionRoot", reason: reasonFileServiceRoot},
	{file: "bl333_file_service.go", fun: "fileServiceRoot", reason: reasonFileServiceRoot},
	{file: "bl333_file_service.go", fun: "handleFilesJSONUpload", reason: reasonFileServiceRoot},
	{file: "bl333_file_service.go", fun: "handleFilesUpload", reason: reasonFileServiceRoot},
	{file: "bl333_file_service.go", fun: "handleFilesDelete", reason: reasonFileServiceRoot},
	{file: "session_progress.go", fun: "piSessionsRootServer", reason: reasonPiSessionLogs},
	{file: "v52710_channel_info.go", fun: "scanStaleMCPJSON", reason: reasonOperatorMCPFile},
	{file: "state_paths.go", fun: "dataDirPath", reason: reasonDataDirSource},
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
		t.Fatal("scan found no os.UserHomeDir / os.Getenv(\"HOME\") calls at all — scan machinery is broken or the allow list has gone fully stale")
	}

	key := func(f, fn string) string { return f + "\x00" + fn }
	allowed := make(map[string]bool, len(statePathAllowList))
	for _, a := range statePathAllowList {
		allowed[key(a.file, a.fun)] = true
	}
	hitSet := make(map[string]bool, len(hits))
	for _, h := range hits {
		hitSet[key(h.file, h.fun)] = true
	}

	var unallowed []hit
	for _, h := range hits {
		if !allowed[key(h.file, h.fun)] {
			unallowed = append(unallowed, h)
		}
	}
	if len(unallowed) > 0 {
		sort.Slice(unallowed, func(i, j int) bool { return unallowed[i].line < unallowed[j].line })
		t.Errorf("state path guard: %d flagged call(s) outside the allow list. A datawatch-owned state path here is a blocker — do not add it to the allow list:\n", len(unallowed))
		for _, h := range unallowed {
			t.Errorf("  %s:%d in %s", h.file, h.line, h.fun)
		}
	}
	for _, a := range statePathAllowList {
		if !hitSet[key(a.file, a.fun)] {
			t.Errorf("state path guard: stale allow-list entry %s :: %s (%s) — no current hit matches; remove the row", a.file, a.fun, a.reason)
		}
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

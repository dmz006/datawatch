package server

// Static guard: blocks new state paths derived from the process home
// directory ($HOME) outside the closed allow list.
//
// Datawatch-owned state lives under the configured data directory (see
// statePath / dataDirPath in state_paths.go). A direct os.UserHomeDir /
// os.Getenv("HOME") call in a new server handler is how home-derived state
// drifts back in; this test is the backstop.
//
// The scan covers every non-test .go file directly in internal/server and
// flags any call to os.UserHomeDir(...) or os.Getenv("HOME"). A flagged
// call is permitted ONLY if its file and enclosing function appear in the
// statePathAllowList constant in this file; each allow-list line carries a
// one-line reason, and a line that matches no current hit is a stale-entry
// failure, so the list cannot outlive its reason.
//
// A flagged call on NO allow-list line is a blocker and FAILS the test.
//
// Operator decision 2026-11-07, quoted: "a - allow" — in response to the
// question of whether to allow-list the single dataDirPath builder in the
// state path guard, keeping the default data directory home-derived, or
// make the default home-free. dataDirPath is the one sanctioned place that
// reads the home directory: only to expand an operator-written ~/ or ~
// tilde data directory prefix, or the home-derived default data directory
// (config.DefaultConfig().DataDir, $HOME/.datawatch). It is therefore on
// the allow list below; the home read that builds the default itself lives
// in internal/config (config.go, DefaultConfig), which this scan does not
// cover.
//
// Allow-list classification of every other current hit (verified against
// current code — all operator-owned reads, never datawatch state):
//   - api.go handleStartSession / handleWS — default project directory for
//     a new session: the operator's own workspace, not daemon state.
//   - api.go handleFiles / handleFilesMkdir / handleFilesDownload — file
//     browser on the operator's own workspace.
//   - bl333_file_service.go sessionRoot / fileServiceRoot /
//     handleFilesJSONUpload / handleFilesUpload / handleFilesDelete — user
//     file roots: operator-chosen folders (FileServiceRoot/RootPath, or
//     their tilde prefixes).
//   - session_progress.go piSessionsRootServer — pi tool session JSONL
//     logs, read-only, PI_CODING_AGENT_DIR environment override checked
//     first.
//   - v52710_channel_info.go scanStaleMCPJSON — read-only staleness check
//     of the operator's own project MCP file ($HOME/.mcp.json).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// statePathAllowList is the closed allow list for home-derived path calls.
// Format: one line per entry, "file<TAB>function<TAB>one-line reason". A
// line matching no current hit is a stale-entry failure. A flagged hit
// matching no line is a datawatch-owned (or unclassified) home-derived
// state path and fails the test as a blocker: report it, do not add it
// here. Blank lines are ignored.
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
	state_paths.go	dataDirPath	single-source builder for datawatch-owned state paths; reads the home directory only to expand an operator-written tilde data directory or the default data directory
	session_progress.go	piSessionsRootServer	pi tool session JSONL logs, read-only; PI_CODING_AGENT_DIR environment override is checked first
	v52710_channel_info.go	scanStaleMCPJSON	read-only staleness check of the operator's own project MCP file ($HOME/.mcp.json)
`

// blockerRationale explains any currently known non-allowed hit(s) in the
// failure message so the report is accurate. Keys are "file\x00function".
// Empty as of the 2026-11-07 operator decision ("a - allow"): state_paths.go
// dataDirPath is on the allow list, so there is no known deliberate blocker.
var blockerRationale = map[string]string{}

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

	// statePathAllowList is the closed allow list: file<TAB>function<TAB>reason.
	allowed := map[string]string{} // file+\x00+function -> reason
	order := []string{}
	for _, line := range strings.Split(statePathAllowList, "\n") {
		line = strings.TrimLeft(line, "\t ")
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
			t.Fatalf("statePathAllowList: malformed line %q — expected \"file<TAB>function<TAB>one-line reason\"", line)
		}
		k := fields[0] + "\x00" + fields[1]
		if _, dup := allowed[k]; dup {
			t.Fatalf("statePathAllowList: duplicate entry %s", k)
		}
		allowed[k] = fields[2]
		order = append(order, k)
	}
	if len(allowed) == 0 {
		t.Fatal("statePathAllowList is empty — the constant must be listed explicitly, not inferred")
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].file != hits[j].file {
			return hits[i].file < hits[j].file
		}
		if hits[i].line != hits[j].line {
			return hits[i].line < hits[j].line
		}
		return hits[i].fun < hits[j].fun
	})

	failures := 0
	fail := func(format string, args ...any) {
		failures++
		t.Errorf(format, args...)
	}

	// 1. Every flagged hit must be on the allow list; otherwise it is a
	//    blocker: a datawatch-owned (or unclassified) home-derived state
	//    path that must be reported, not exempted.
	for _, h := range hits {
		k := h.file + "\x00" + h.fun
		if _, ok := allowed[k]; ok {
			continue
		}
		if why, known := blockerRationale[k]; known {
			fail("state path guard BLOCKER (known, deliberately failing): %s:%d in %s — %s. Remove it from the code (or make the default home-free) rather than allow-listing it", h.file, h.line, h.fun, why)
		} else {
			fail("state path guard BLOCKER (new, unclassified): %s:%d in %s calls os.UserHomeDir / os.Getenv(\"HOME\") and is not in statePathAllowList. A datawatch-owned state path like this is not allowed — report it as a blocker and fix it in code; do not add it to the allow list", h.file, h.line, h.fun)
		}
	}

	// 2. Anti-staleness: an allow-list line with no current hit means the
	//    code moved or the home read is gone — re-classify deliberately
	//    instead of letting the entry outlive its reason.
	hitSet := map[string]bool{}
	for _, h := range hits {
		hitSet[h.file+"\x00"+h.fun] = true
	}
	for _, k := range order {
		if !hitSet[k] {
			parts := strings.SplitN(k, "\x00", 2)
			fail("state path guard stale allow-list entry %s :: %s (%s) — no current hit matches; remove the line", parts[0], parts[1], allowed[k])
		}
	}

	if failures > 0 {
		t.Logf("state path guard: %d failure(s) across %d flagged hit(s); every violating hit above is a reported blocker, not an exemption", failures, len(hits))
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

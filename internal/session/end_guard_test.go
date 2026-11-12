package session

// Guards that keep every session end attributed. A session ended without a
// caller and a reason shows up in the journal as an anonymous end, so these
// tests fail when new code calls the bare Kill wrappers or closes a tmux pane
// outside the one helper that records it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// endGuardMarker, placed on the call's line or the line above it, excuses a
// call to one of endGuardKillNames that is not a session end. It must be
// followed by a short justification.
const endGuardMarker = "end-reason-ok:"

// endGuardKillNames are the method names that end a session or close its pane
// without a reason. Calls to them belong on Manager.EndSession.
var endGuardKillNames = map[string]bool{
	"Kill":            true,
	"KillTmuxSession": true,
	"KillAll":         true,
}

// endGuardPaneKillFile and endGuardPaneKillHelper name the one function that
// may call tmux.KillSession, and the file it lives in.
const (
	endGuardPaneKillFile   = "end_reason.go"
	endGuardPaneKillHelper = "killPane"
)

// endGuardParse parses one Go file with its comments.
func endGuardParse(t *testing.T, fset *token.FileSet, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return f
}

// endGuardMarkedLines returns the lines that carry the marker with a
// non-empty justification.
func endGuardMarkedLines(fset *token.FileSet, f *ast.File) map[int]bool {
	marked := map[int]bool{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			i := strings.Index(c.Text, endGuardMarker)
			if i < 0 {
				continue
			}
			why := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(c.Text[i+len(endGuardMarker):]), "*/"))
			if why == "" {
				continue
			}
			marked[fset.Position(c.Pos()).Line] = true
		}
	}
	return marked
}

// TestSessionEndGuardNoBareKillCallers fails when a file outside internal/session
// calls Kill, KillTmuxSession or KillAll without an end-reason-ok marker.
func TestSessionEndGuardNoBareKillCallers(t *testing.T) {
	sessionDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	scanned := 0
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if dir, err := filepath.Abs(filepath.Dir(path)); err != nil || dir == sessionDir {
				return err
			}
			scanned++
			fset := token.NewFileSet()
			f := endGuardParse(t, fset, path)
			marked := endGuardMarkedLines(fset, f)
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !endGuardKillNames[sel.Sel.Name] {
					return true
				}
				line := fset.Position(call.Pos()).Line
				if marked[line] || marked[line-1] {
					return true
				}
				violations = append(violations, fmt.Sprintf("%s:%d: %s call without %s marker", path, line, sel.Sel.Name, endGuardMarker))
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if scanned == 0 {
		t.Fatal("no Go files scanned outside internal/session; the guard would pass vacuously")
	}
	if len(violations) > 0 {
		t.Errorf("bare session-end calls; use Manager.EndSession with an EndReason, or mark a non-session call with %q and a justification:\n%s",
			endGuardMarker, strings.Join(violations, "\n"))
	}
}

// TestSessionEndGuardEveryTmuxPaneKillIsLogged fails when tmux.KillSession is
// called from anywhere but the pane-kill helper, which journals the pane and
// the reason before it closes the pane.
func TestSessionEndGuardEveryTmuxPaneKillIsLogged(t *testing.T) {
	var stray []string
	helperCalls := 0
	for _, path := range []string{"manager.go", endGuardPaneKillFile} {
		fset := token.NewFileSet()
		f := endGuardParse(t, fset, path)
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			inHelper := path == endGuardPaneKillFile && isFunc && fn.Name.Name == endGuardPaneKillHelper
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !endGuardIsTmuxKill(call) {
					return true
				}
				if inHelper {
					helperCalls++
					return true
				}
				stray = append(stray, fmt.Sprintf("%s:%d", path, fset.Position(call.Pos()).Line))
				return true
			})
		}
	}
	if helperCalls == 0 {
		t.Fatalf("%s no longer closes a pane through tmux.KillSession; the pane-kill guard has nothing to check", endGuardPaneKillHelper)
	}
	if len(stray) > 0 {
		t.Errorf("tmux.KillSession called outside %s; close panes through it so the pane and reason are journaled:\n%s",
			endGuardPaneKillHelper, strings.Join(stray, "\n"))
	}
}

// endGuardIsTmuxKill reports whether call is <x>.tmux.KillSession(...).
func endGuardIsTmuxKill(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "KillSession" {
		return false
	}
	switch recv := sel.X.(type) {
	case *ast.Ident:
		return recv.Name == "tmux"
	case *ast.SelectorExpr:
		return recv.Sel.Name == "tmux"
	}
	return false
}

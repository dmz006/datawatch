package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// paneDoors is every function that creates a live session pane, found by
// calling startWorkerPane. A new session door must be added here with how it
// takes a capacity lease, so a start path nobody listed cannot appear unseen.
var paneDoors = map[string]string{
	"internal/session/manager.go startSession": "admitted: only Manager.Start calls it, and Start takes the lease first (or the REST handler already took it and sets PreAdmitted)",
	"internal/session/manager.go Restart":      "OPEN GAP: relaunches an ended session in place and takes no lease (reached by POST /api/sessions/{id}/restart and MCP restart_session); the operator decides the class before it is closed",
}

// openLeaseGaps are the doors above that do not take a lease today. The test
// logs them on every run so the gap stays visible; closing one means removing
// it here and from the reason above.
var openLeaseGaps = map[string]bool{
	"internal/session/manager.go Restart": true,
}

// functionsCalling returns "file func" for every non-test function in
// internal/session whose body calls a function or method named callee.
func functionsCalling(t *testing.T, callee string) []string {
	t.Helper()
	root, err := filepath.Abs("../../internal/session")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil || !strings.Contains(string(src), callee+"(") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Dir(filepath.Dir(root)), path)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			found := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					found = found || fn.Sel.Name == callee
				case *ast.Ident:
					found = found || fn.Name == callee
				}
				return true
			})
			if found {
				out = append(out, rel+" "+fd.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// Every function that opens a session pane is on the audited list, and the one
// that goes through admission is reached only from Manager.Start. Together with
// TestSessionStartPathsAreAudited (every caller of Start) this closes the loop:
// a session cannot start without being admitted unless it is named here.
func TestEverySessionPaneDoorIsAdmittedOrNamed(t *testing.T) {
	doors := functionsCalling(t, "startWorkerPane")
	seen := map[string]bool{}
	for _, d := range doors {
		seen[d] = true
		if _, ok := paneDoors[d]; !ok {
			t.Errorf("unlisted session pane door: %s\n\tit creates a session pane; take a capacity lease first (Manager.Start does) and add it to paneDoors with how it admits", d)
		}
	}
	for d := range paneDoors {
		if !seen[d] {
			t.Errorf("listed pane door no longer exists (remove it or fix its key): %s", d)
		}
	}
	callers := functionsCalling(t, "startSession")
	var other []string
	for _, c := range callers {
		if c != "internal/session/manager.go Start" && c != "internal/session/manager.go startSession" {
			other = append(other, c)
		}
	}
	if len(other) > 0 {
		t.Errorf("startSession is the unadmitted half of a start and only Manager.Start may call it; also called from: %v", other)
	}
	for d := range openLeaseGaps {
		t.Logf("OPEN GAP, no capacity lease: %s: %s", d, paneDoors[d])
	}
}

// Every audited start path states how it is admitted.
func TestAuditedStartPathsEachGiveAReason(t *testing.T) {
	for k, reason := range auditedSessionStarts {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s has no stated admission class or reason", k)
		}
	}
	for k, reason := range paneDoors {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s has no stated admission", k)
		}
	}
}

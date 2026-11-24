package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// startCall is one call of the session manager's Start in non-test source.
type startCall struct{ file, fn, recv string }

func (c startCall) key() string { return c.file + " " + c.fn + " " + c.recv }

// scanSessionStarts walks the repository and returns every call that looks
// like Manager.Start(ctx, task, group, dir, ...): a method named Start with at
// least four arguments whose first is a context. It also returns the files
// that set the PreAdmitted option.
func scanSessionStarts(t *testing.T) (starts []startCall, preAdmitted []string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	preSet := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "web", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil || !strings.Contains(string(src), "Start(") && !strings.Contains(string(src), "PreAdmitted") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					sel, ok := x.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Start" || len(x.Args) < 4 {
						return true
					}
					first := types.ExprString(x.Args[0])
					if !strings.Contains(strings.ToLower(first), "ctx") && !strings.HasPrefix(first, "context.") {
						return true
					}
					starts = append(starts, startCall{rel, fd.Name.Name, types.ExprString(sel.X)})
				case *ast.KeyValueExpr:
					if id, ok := x.Key.(*ast.Ident); ok && id.Name == "PreAdmitted" {
						preSet[rel] = true
					}
				case *ast.AssignStmt:
					for _, l := range x.Lhs {
						if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "PreAdmitted" {
							preSet[rel] = true
						}
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for f := range preSet {
		preAdmitted = append(preAdmitted, f)
	}
	sort.Strings(preAdmitted)
	return starts, preAdmitted
}

// auditedSessionStarts is every non-test caller of Manager.Start. Each one is
// admitted by Manager.Start itself (the chokepoint), in the class named, unless
// it sets PreAdmitted, which only the REST start handler may do. Adding a
// caller means adding a line here: a conscious, reviewed decision. An entry
// with class "worker" restarts work whose lease its original session holds.
var auditedSessionStarts = map[string]string{
	"cmd/datawatch/main.go runWorkerBootstrapTask mgr":        "operator: a worker daemon's own bootstrap task, accounted on that daemon's ledger",
	"cmd/datawatch/main.go runStart mgr":                      "worker class at the head of the queue for a worker session (session.RestartAdmission), operator for any other session: rate-limit profile fallback restart",
	"cmd/datawatch/main.go runExitHookRestart mgr":            "worker class at the head of the queue for a worker session (session.RestartAdmission), operator for any other session: exit-hook restart",
	"cmd/datawatch/main.go runScheduler mgr":                  "operator, or supervisor for a schedule whose launch role is supervisor",
	"internal/mcp/server.go handleStartSession s.manager":     "operator: MCP start without an LLM entry",
	"internal/pipeline/session_adapter.go StartSession a.mgr": "operator: pipeline adapter start",
	"internal/router/router.go handleNew r.manager":           "operator: chat new verb",
	"internal/server/api.go executeCommand s.manager":         "operator: command text new verb",
	"internal/server/api.go handleWS s.manager":               "operator: federation WebSocket new session",
	"internal/server/api.go handleStartSession s.manager":     "REST start: admits itself (PreAdmitted), class from the daemon marker",
	"internal/session/manager.go processScheduledItems m":     "operator, or supervisor for a schedule whose launch role is supervisor",
	"internal/supervisorloop/restart.go Check r.Sessions":     "interface call: the daemon wires no implementation yet; any implementation must start through Manager.Start",
}

// Every session start path admits or sits on this audited list, and only the
// REST handler may declare a start already admitted.
func TestSessionStartPathsAreAudited(t *testing.T) {
	starts, pre := scanSessionStarts(t)
	seen := map[string]bool{}
	for _, c := range starts {
		seen[c.key()] = true
		if _, ok := auditedSessionStarts[c.key()]; !ok {
			t.Errorf("unaudited session start path: %s\n\tManager.Start admits it by default; add it to auditedSessionStarts with its class and the reason", c.key())
		}
	}
	for k := range auditedSessionStarts {
		if !seen[k] {
			t.Errorf("audited start path no longer exists (remove it or fix its key): %s", k)
		}
	}
	if len(pre) != 1 || pre[0] != "internal/server/api.go" {
		t.Errorf("PreAdmitted is set in %v; only internal/server/api.go (the REST start handler, which admits itself) may set it", pre)
	}
}

// The REST start handler must never trust one_shot (or any request field) for
// the admission decision.
func TestStartHandlerAdmissionIgnoresOneShot(t *testing.T) {
	src, err := os.ReadFile("../../internal/server/api.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "admitClass := startAdmissionClass(r)")
	if i < 0 {
		t.Fatal("handleStartSession no longer decides the class with startAdmissionClass(r)")
	}
	window := s[i : i+900]
	if strings.Contains(window, "req.OneShot") {
		t.Fatalf("the admission decision reads req.OneShot:\n%s", window)
	}
	if strings.Contains(s, "!req.OneShot && s.capacityAdmit") {
		t.Fatal("the one_shot admission skip is back")
	}
}

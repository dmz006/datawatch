package router_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

// auditCommRouter returns a router whose loopback is an httptest server that
// records the last /api/audit query it was sent.
func auditCommRouter(t *testing.T) (*router.Router, *url.Values, *int) {
	t.Helper()
	var got url.Values
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"count":0,"entries":[]}`))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := router.NewRouter("h", "g", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, &got, &hits
}

func TestAuditQuery_CommFilters(t *testing.T) {
	r, got, hits := auditCommRouter(t)

	t.Run("every word is forwarded under its filter name", func(t *testing.T) {
		out := r.HandleTestMessage("audit actor=daemon action=autonomous_budget_* subject=claude session_id=s1 " +
			"since=2026-10-22T12:00:00Z until=2026-10-22T13:00:00Z limit=5")
		if len(out) == 0 || !strings.Contains(out[0], "audit") {
			t.Fatalf("reply = %v", out)
		}
		for k, v := range map[string]string{
			"actor": "daemon", "action": "autonomous_budget_*", "subject": "claude",
			"session_id": "s1", "since": "2026-10-22T12:00:00Z", "until": "2026-10-22T13:00:00Z", "limit": "5",
		} {
			if got.Get(k) != v {
				t.Errorf("forwarded %s = %q, want %q", k, got.Get(k), v)
			}
		}
	})

	t.Run("unknown words are dropped", func(t *testing.T) {
		r.HandleTestMessage("audit bogus=1 subject=t1")
		if _, ok := (*got)["bogus"]; ok || got.Get("subject") != "t1" {
			t.Errorf("query = %v", *got)
		}
	})

	t.Run("invalid since or until is rejected before any request", func(t *testing.T) {
		before := *hits
		for _, name := range []string{"since", "until"} {
			out := r.HandleTestMessage("audit " + name + "=yesterday")
			if len(out) == 0 || !strings.Contains(out[0], "invalid "+name) {
				t.Errorf("%s=yesterday: reply = %v", name, out)
			}
		}
		if *hits != before {
			t.Errorf("a rejected query still reached the daemon")
		}
	})
}

// cliAuditFlags returns the filter names the datawatch audit command
// registers, read from its source because package main cannot be imported.
func cliAuditFlags(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../../cmd/datawatch/cli_sx_parity.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "newAuditCmd" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasSuffix(sel.Sel.Name, "Var") {
				return true
			}
			if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				name, _ := strconv.Unquote(lit.Value)
				names = append(names, strings.ReplaceAll(name, "-", "_"))
			}
			return true
		})
	}
	return names
}

// A filter added to one surface only changes that surface's set and fails
// this test until every surface carries it.
func TestAuditQuery_FilterParityAcrossSurfaces(t *testing.T) {
	sets := map[string][]string{
		"audit": audit.QueryParamNames,
		"rest":  server.AuditQueryParams(),
		"mcp":   mcp.AuditQueryToolArgs(),
		"cli":   cliAuditFlags(t),
		"comm":  router.AuditCommKeys(),
	}
	want := []string{"action", "actor", "limit", "session_id", "since", "subject", "until"}
	for name, set := range sets {
		got := slices.Clone(set)
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s surface filters = %v, want %v", name, got, want)
		}
	}
	if len(sets) != 5 {
		t.Errorf("expected 5 surfaces, got %d", len(sets))
	}
}

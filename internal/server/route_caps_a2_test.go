// Design A2 (docs/plans/historical-plans/2026-09-02-sec-design-a-authz-scoping.md
// §A2) — "make capabilities opt-out, not opt-in." An audit of every route
// registered on apiMux (v8.39.23) found 13 real gaps across 11 handlers
// that had ZERO capability check at all: any authenticated caller (a
// federation peer today, a scoped session once Design A3 lands) could
// reach them with no gate beyond "authenticated." All 13 are fixed by
// this commit; TestA2_EveryRegisteredRouteHandlerIsCapabilityGated below
// is the structural regression test that keeps a future route from
// silently landing ungated again, without requiring a full rewrite of
// the ~440 existing, working s.fedCap(...) call sites into a new
// declarative map (the original design's proposal) — the call-graph walk
// below gives the same "opt-out, not opt-in" guarantee at far lower risk.

package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// routeCapsExceptions lists every apiMux-registered handler that
// legitimately has no s.fedCap(...) call anywhere it can reach, with why.
var routeCapsExceptions = map[string]string{
	"handleAuthNonce": "SEC-006 — mints a nonce standing in for whichever " +
		"identity already authenticated through fedAuthMiddleware; a caller " +
		"needs no capability beyond proving it IS that identity.",
	"handleProxy": "BL394 §6 — uses its own equivalent-but-different gate, " +
		"checkProxyAuth (admin/federation token OR a peer-scoped proxy " +
		"token bound to the exact peer in the path), not the general cap " +
		"model.",
}

// handlerFuncs parses every non-test .go file directly under dir (not
// subpackages) and returns every func (s *Server) NAME(...) body found.
func parseServerFuncs(t *testing.T, dir string) map[string]*ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	out := make(map[string]*ast.FuncDecl)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			id, ok := star.X.(*ast.Ident)
			if !ok || id.Name != "Server" {
				continue
			}
			out[fn.Name.Name] = fn
		}
	}
	return out
}

// reachesFedCap reports whether fn's body calls s.fedCap(...) directly, or
// transitively via any s.<method>(...) call this package also defines.
func reachesFedCap(name string, funcs map[string]*ast.FuncDecl, visited map[string]bool) bool {
	if visited[name] {
		return false // cycle guard
	}
	visited[name] = true
	fn, ok := funcs[name]
	if !ok || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != "s" {
			return true
		}
		if sel.Sel.Name == "fedCap" {
			found = true
			return false
		}
		if reachesFedCap(sel.Sel.Name, funcs, visited) {
			found = true
			return false
		}
		return true
	})
	return found
}

func TestA2_EveryRegisteredRouteHandlerIsCapabilityGated(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	re := regexp.MustCompile(`apiMux\.HandleFunc\("[^"]+",\s*api\.([a-zA-Z0-9_]+)`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	if len(matches) < 200 {
		t.Fatalf("expected at least 200 apiMux.HandleFunc registrations, found %d — regex may be stale", len(matches))
	}
	seen := make(map[string]bool)
	for _, m := range matches {
		seen[m[1]] = true
	}

	funcs := parseServerFuncs(t, ".")

	var ungated []string
	for handler := range seen {
		if reason, isException := routeCapsExceptions[handler]; isException {
			if reason == "" {
				t.Errorf("handler %q is listed as an exception with no reason", handler)
			}
			continue
		}
		if !reachesFedCap(handler, funcs, map[string]bool{}) {
			ungated = append(ungated, handler)
		}
	}
	if len(ungated) > 0 {
		t.Errorf("Design A2 regression: %d apiMux-registered handler(s) have NO fedCap(...) call reachable "+
			"anywhere in their call graph, and are not in routeCapsExceptions: %v\n"+
			"Either add an s.fedCap(w, r, federation.Cap...) check, or add a justified entry to "+
			"routeCapsExceptions in route_caps_a2_test.go.", len(ungated), ungated)
	}
}

// ─── functional coverage for the 13 gaps this commit closes ────────────────

func TestA2_PreviouslyUngatedRoutes_BarePeerGets403(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	if err := store.Add(&multiserver.Entry{
		Name:         "peer-bare",
		URL:          "http://peer-bare:8080",
		Token:        "peer-bare-token",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"federation-peer"}, // SEC-009 minimal default: health:read + federation:self only
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	cases := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"guardrails", http.MethodGet, "/api/autonomous/guardrails", s.handleAutonomousGuardrails},
		{"guardrail_profiles list", http.MethodGet, "/api/autonomous/guardrail_profiles", s.handleAutonomousGuardrailProfiles},
		{"guardrail_profiles create", http.MethodPost, "/api/autonomous/guardrail_profiles", s.handleAutonomousGuardrailProfiles},
		{"discussion-subs list", http.MethodGet, "/api/discussion-subs", s.handleDiscussionSubs},
		{"discussion-subs subscribe", http.MethodPost, "/api/discussion-subs", s.handleDiscussionSubs},
		{"evals compat", http.MethodGet, "/api/evals/runs", s.handleEvalsCompat},
		{"exit-hooks list", http.MethodGet, "/api/exit-hooks", s.handleExitHooks},
		{"exit-hooks create", http.MethodPost, "/api/exit-hooks", s.handleExitHooks},
		{"lsp servers", http.MethodGet, "/api/lsp/servers", s.handleLSPServers},
		{"matrix status", http.MethodGet, "/api/matrix/status", s.handleMatrixStatus},
		{"matrix test", http.MethodPost, "/api/matrix/test", s.handleMatrixTest},
		{"observer config get", http.MethodGet, "/api/observer/config", s.handleObserverConfig},
		{"observer config put", http.MethodPut, "/api/observer/config", s.handleObserverConfig},
		{"opencode models", http.MethodGet, "/api/opencode/models", s.handleOpenCodeModels},
		{"opencode providers get", http.MethodGet, "/api/opencode/providers", s.handleOpenCodeProviders},
		{"opencode providers put (secret write)", http.MethodPut, "/api/opencode/providers", s.handleOpenCodeProviders},
		{"queue list", http.MethodGet, "/api/queue", s.handleQueue},
		{"queue push", http.MethodPost, "/api/queue/push", s.handleQueue},
		{"result-store list", http.MethodGet, "/api/result-store", s.handleResultStore},
		{"result-store get one", http.MethodGet, "/api/result-store/some-name", s.handleResultStore},
		{"result-store upsert", http.MethodPost, "/api/result-store", s.handleResultStore},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			req.Header.Set("Authorization", "Bearer peer-bare-token")
			rr := httptest.NewRecorder()
			s.fedAuthMiddleware(c.handler).ServeHTTP(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Errorf("%s %s with a bare federation-peer (no grant) should be 403, got %d body=%s",
					c.method, c.path, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestA2_GrantedPeerPassesCapCheck confirms the explicit-grant path still
// works post-fix: a peer with the specific capability clears fedCap (may
// still hit a downstream 503/other error from a nil test-fixture subsystem,
// but must NOT be 401/403).
func TestA2_GrantedPeerPassesCapCheck(t *testing.T) {
	s, store, groups := newFedTestServer(t)
	if err := groups.Add(&federation.CapabilityGroup{
		Name: "a2-test-group",
		Caps: []string{
			federation.CapAutonomousRead,
			federation.CapQueueRead,
			federation.CapResultsList,
			federation.CapSecretsList,
		},
	}); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if err := store.Add(&multiserver.Entry{
		Name:         "peer-granted",
		URL:          "http://peer-granted:8080",
		Token:        "peer-granted-token",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"a2-test-group"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"guardrails (CapAutonomousRead)", "/api/autonomous/guardrails", s.handleAutonomousGuardrails},
		{"queue list (CapQueueRead)", "/api/queue", s.handleQueue},
		{"result-store list (CapResultsList)", "/api/result-store", s.handleResultStore},
		{"opencode providers get (CapSecretsList)", "/api/opencode/providers", s.handleOpenCodeProviders},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			req.Header.Set("Authorization", "Bearer peer-granted-token")
			rr := httptest.NewRecorder()
			s.fedAuthMiddleware(c.handler).ServeHTTP(rr, req)
			if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
				t.Errorf("%s with the matching capability grant should clear the cap check (got %d), body=%s",
					c.path, rr.Code, rr.Body.String())
			}
		})
	}
}

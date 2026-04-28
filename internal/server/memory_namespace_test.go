// BL101 — REST handler test for cross-profile namespace expansion on
// /api/memory/search.

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// nsMemAPI captures the args passed to SearchInNamespaces so we can
// assert the handler resolved namespaces correctly via the project
// store before calling.
type nsMemAPI struct {
	gotNS         []string
	gotQuery      string
	calledNS      bool
	calledFallback bool
}

func (n *nsMemAPI) Stats() map[string]interface{}                                      { return nil }
func (n *nsMemAPI) ListRecent(string, int) ([]map[string]interface{}, error)           { return nil, nil }
func (n *nsMemAPI) ListFiltered(string, string, string, int) ([]map[string]interface{}, error) {
	return nil, nil
}
func (n *nsMemAPI) Search(q string, _ int) ([]map[string]interface{}, error) {
	n.calledFallback = true
	n.gotQuery = q
	return []map[string]interface{}{{"source": "Search"}}, nil
}
func (n *nsMemAPI) SearchInNamespaces(q string, ns []string, _ int) ([]map[string]interface{}, error) {
	n.calledNS = true
	n.gotQuery = q
	n.gotNS = ns
	return []map[string]interface{}{{"source": "SearchInNamespaces"}}, nil
}
func (n *nsMemAPI) Delete(int64) error                       { return nil }
func (n *nsMemAPI) SetPinned(int64, bool) error              { return nil }
func (n *nsMemAPI) Remember(string, string) (int64, error)   { return 0, nil }
func (n *nsMemAPI) Export(io.Writer) error                   { return nil }
func (n *nsMemAPI) Import(io.Reader) (int, error)            { return 0, nil }
func (n *nsMemAPI) WALRecent(int) []map[string]interface{}   { return nil }
func (n *nsMemAPI) Reindex() (int, error)                    { return 0, nil }
func (n *nsMemAPI) ListLearnings(string, string, int) ([]map[string]interface{}, error) {
	return nil, nil
}
func (n *nsMemAPI) Research(string, int) ([]map[string]interface{}, error) { return nil, nil }

func nsTestServer(t *testing.T, mem MemoryAPI) (*Server, *profile.ProjectStore) {
	t.Helper()
	dir := t.TempDir()
	ps, err := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{memoryAPI: mem, projectStore: ps}
	return s, ps
}

func TestMemorySearch_NoProfile_FallsBackToSearch(t *testing.T) {
	mem := &nsMemAPI{}
	s, _ := nsTestServer(t, mem)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/search?q=foo", nil)
	rr := httptest.NewRecorder()
	s.handleMemorySearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if mem.gotNS != nil {
		t.Errorf("namespace path used unexpectedly: %v", mem.gotNS)
	}
	if mem.gotQuery != "foo" {
		t.Errorf("query=%q want foo", mem.gotQuery)
	}
}

func TestMemorySearch_WithProfile_ResolvesNamespaces(t *testing.T) {
	mem := &nsMemAPI{}
	s, ps := nsTestServer(t, mem)

	// Two profiles with mutual opt-in sharing.
	_ = ps.Create(&profile.ProjectProfile{
		Name:           "alpha",
		Git:            profile.GitSpec{URL: "https://g/a"},
		ImagePair:      profile.ImagePair{Agent: "agent-claude"},
		Memory:         profile.MemorySpec{Mode: profile.MemorySyncBack, Namespace: "ns-a", SharedWith: []string{"beta"}},
	})
	_ = ps.Create(&profile.ProjectProfile{
		Name:           "beta",
		Git:            profile.GitSpec{URL: "https://g/b"},
		ImagePair:      profile.ImagePair{Agent: "agent-claude"},
		Memory:         profile.MemorySpec{Mode: profile.MemorySyncBack, Namespace: "ns-b", SharedWith: []string{"alpha"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/memory/search?q=foo&profile=alpha", nil)
	rr := httptest.NewRecorder()
	s.handleMemorySearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(mem.gotNS) != 2 {
		t.Fatalf("expected 2 namespaces (alpha + beta mutual), got %v", mem.gotNS)
	}
	// Order isn't guaranteed; assert set membership.
	seen := map[string]bool{}
	for _, ns := range mem.gotNS {
		seen[ns] = true
	}
	if !seen["ns-a"] || !seen["ns-b"] {
		t.Errorf("expected ns-a + ns-b in resolved set, got %v", mem.gotNS)
	}

	// Response body should reflect the SearchInNamespaces source.
	var body []map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if len(body) == 0 || body[0]["source"] != "SearchInNamespaces" {
		t.Errorf("body should come from SearchInNamespaces: %v", body)
	}
}

func TestMemorySearch_UnknownProfile_StillResolves(t *testing.T) {
	// A profile name with no matching record should resolve to no
	// namespaces (not an error). Asserts the handler doesn't crash
	// when the operator typos a profile.
	mem := &nsMemAPI{}
	s, _ := nsTestServer(t, mem)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/search?q=foo&profile=does-not-exist", nil)
	rr := httptest.NewRecorder()
	s.handleMemorySearch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !mem.calledNS {
		t.Error("namespace path should still be invoked even for unknown profile")
	}
	if mem.calledFallback {
		t.Error("fallback Search must not be called when ?profile= is set")
	}
}

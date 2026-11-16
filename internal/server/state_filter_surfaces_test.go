package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/planrecords"
)

// surfList is the wire the MCP list tool and the chat plan-records verb
// send: one GET per kind, the state value straight through and untouched.
func surfList(t *testing.T, r *planRecordsRig, path string) (int, string, int, []string) {
	t.Helper()
	rec := prCall(t, r.s, http.MethodGet, path, nil)
	var body struct {
		Error   string `json:"error"`
		Total   int    `json:"total"`
		Records []struct {
			Title string `json:"title"`
			State string `json:"state"`
		} `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: response is not JSON: %v (%s)", path, err, rec.Body.String())
	}
	titles := make([]string, 0, len(body.Records))
	for i := range body.Records {
		titles = append(titles, body.Records[i].Title)
	}
	return rec.Code, body.Error, body.Total, titles
}

func surfGet(t *testing.T, r *planRecordsRig, path string) (int, string) {
	t.Helper()
	rec := prCall(t, r.s, http.MethodGet, path, nil)
	return rec.Code, rec.Body.String()
}

// TestMCPStateFilterSurface covers the routes the MCP plan-record tools
// (list and search) proxy: single state keeps its exact meaning, a
// comma-separated list is honored, and an unknown state is refused with
// the same plain message naming the states that are valid.
func TestMCPStateFilterSurface(t *testing.T) {
	r := newPlanRecordsRig(t)
	r.create(t, "registry-rows", map[string]any{"title": "mcp open row", "state": "open"})
	r.create(t, "registry-rows", map[string]any{"title": "mcp blocked row", "state": "blocked"})
	r.create(t, "registry-rows", map[string]any{"title": "mcp shipped row", "state": "shipped"})
	r.create(t, "findings", map[string]any{"title": "mcp open finding", "state": "open"})
	r.create(t, "findings", map[string]any{"title": "mcp triaged finding", "state": "triaged"})

	rowWords := strings.Join(stateWords([]planrecords.Kind{planrecords.KindRegistryRow}), ", ")
	findWords := strings.Join(stateWords([]planrecords.Kind{planrecords.KindFinding}), ", ")
	unionWords := strings.Join(stateWords(nil), ", ")
	has := func(titles []string, want string) bool {
		for _, x := range titles {
			if x == want {
				return true
			}
		}
		return false
	}

	// Single state means exactly that state.
	code, err, _, titles := surfList(t, r, "/api/plan-records/registry-rows?state=open")
	if code != http.StatusOK || err != "" || len(titles) != 1 || titles[0] != "mcp open row" {
		t.Fatalf("single state: code=%d err=%q titles=%v", code, err, titles)
	}
	// A single state is matched without trimming.
	code, err, _, _ = surfList(t, r, "/api/plan-records/registry-rows?state=%20open")
	if code != http.StatusBadRequest || !strings.Contains(err, `state " open" is not a valid state`) {
		t.Fatalf("padded single: code=%d err=%q", code, err)
	}
	// A comma-separated list keeps every state it admits.
	code, err, _, titles = surfList(t, r, "/api/plan-records/registry-rows?state=open,%20blocked")
	if code != http.StatusOK || err != "" || !has(titles, "mcp open row") || !has(titles, "mcp blocked row") || has(titles, "mcp shipped row") {
		t.Fatalf("list: code=%d err=%q titles=%v", code, err, titles)
	}
	// Repeated and empty parts are dropped.
	code, err, _, titles = surfList(t, r, "/api/plan-records/registry-rows?state=blocked,%20open,open,%2C")
	if code != http.StatusOK || err != "" || len(titles) != 2 {
		t.Fatalf("dedupe: code=%d err=%q titles=%v", code, err, titles)
	}

	// shipped is a registry-row state and a finding is not.
	code, err, _, titles = surfList(t, r, "/api/plan-records/registry-rows?state=shipped")
	if code != http.StatusOK || !has(titles, "mcp shipped row") {
		t.Fatalf("rows shipped: code=%d err=%q titles=%v", code, err, titles)
	}
	code, err, _, _ = surfList(t, r, "/api/plan-records/findings?state=shipped")
	if code != http.StatusBadRequest || !strings.Contains(err, fmt.Sprintf(`state "shipped" is not a valid state; use one of %s`, findWords)) {
		t.Fatalf("findings shipped: code=%d err=%q", code, err)
	}

	// A state neither kind admits is refused with the same plain message,
	// alone or in a list.
	for _, path := range []string{
		"/api/plan-records/registry-rows?state=plumbing",
		"/api/plan-records/registry-rows?state=open,plumbing",
	} {
		code, err, _, _ = surfList(t, r, path)
		if code != http.StatusBadRequest || !strings.Contains(err, fmt.Sprintf(`state "plumbing" is not a valid state; use one of %s`, rowWords)) {
			t.Fatalf("%s: code=%d err=%q", path, code, err)
		}
	}
	code, err, _, _ = surfList(t, r, "/api/plan-records/findings?state=open,plumbing")
	if code != http.StatusBadRequest || !strings.Contains(err, fmt.Sprintf(`state "plumbing" is not a valid state; use one of %s`, findWords)) {
		t.Fatalf("findings list: code=%d err=%q", code, err)
	}

	// The search route shares the same filter across both kinds' states:
	// there shipped is admitted because rows use it.
	code, body := surfGet(t, r, "/api/plan-records/search?q=mcp&state=open,shipped,triaged")
	if code != http.StatusOK {
		t.Fatalf("search list: code=%d body=%s", code, body)
	}
	var hits struct {
		Error string `json:"error"`
		Hits  []struct {
			Title string `json:"title"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(body), &hits); err != nil {
		t.Fatalf("search list: body is not JSON: %v", err)
	}
	if hits.Error != "" || len(hits.Hits) != 4 { // the blocked row is out of the open,shipped,triaged filter
		t.Fatalf("search list: error=%q hits=%d body=%s", hits.Error, len(hits.Hits), body)
	}
	if strings.Contains(body, "mcp blocked row") {
		t.Fatalf("search list: blocked row leaked through the state filter: %s", body)
	}
	code, body = surfGet(t, r, "/api/plan-records/search?q=mcp&state=plumbing")
	want := `state \"plumbing\" is not a valid state; use one of ` + unionWords
	if code != http.StatusBadRequest || !strings.Contains(body, want) {
		t.Fatalf("search refuse: code=%d body=%s want=%s", code, body, want)
	}
}

// TestCommStateFilterSurface covers the wire the chat plan-records verb
// speaks: both kinds are asked per state, a state one kind admits still
// shows, an unknown state is refused in plain words on every kind, and the
// verb's small page size still applies.
func TestCommStateFilterSurface(t *testing.T) {
	r := newPlanRecordsRig(t)
	r.create(t, "registry-rows", map[string]any{"title": "comm open row", "state": "open"})
	r.create(t, "registry-rows", map[string]any{"title": "comm blocked row", "state": "blocked"})
	r.create(t, "registry-rows", map[string]any{"title": "comm shipped row", "state": "shipped"})
	r.create(t, "findings", map[string]any{"title": "comm triaged finding", "state": "triaged"})

	has := func(titles []string, want string) bool {
		for _, x := range titles {
			if x == want {
				return true
			}
		}
		return false
	}

	// A state one kind admits is a hit there and refused on the other one.
	code, err, _, titles := surfList(t, r, "/api/plan-records/registry-rows?state=shipped")
	if code != http.StatusOK || !has(titles, "comm shipped row") {
		t.Fatalf("rows shipped: code=%d err=%q titles=%v", code, err, titles)
	}
	code, err, _, _ = surfList(t, r, "/api/plan-records/findings?state=shipped")
	if code != http.StatusBadRequest || err == "" {
		t.Fatalf("findings shipped: code=%d err=%q", code, err)
	}

	// A comma-separated list spans what each kind admits.
	code, err, _, titles = surfList(t, r, "/api/plan-records/registry-rows?state=open,shipped")
	if code != http.StatusOK || err != "" || !has(titles, "comm open row") || !has(titles, "comm shipped row") || has(titles, "comm blocked row") {
		t.Fatalf("rows list: code=%d err=%q titles=%v", code, err, titles)
	}

	// An unknown state is refused in plain words, naming the states that
	// are valid for that kind.
	code, err, _, _ = surfList(t, r, "/api/plan-records/registry-rows?state=open,plumbing")
	if code != http.StatusBadRequest || !strings.Contains(err, `state "plumbing" is not a valid state; use one of `) {
		t.Fatalf("rows refuse: code=%d err=%q", code, err)
	}
	code, err, _, _ = surfList(t, r, "/api/plan-records/findings?state=plumbing")
	if code != http.StatusBadRequest || !strings.Contains(err, `state "plumbing" is not a valid state; use one of `) {
		t.Fatalf("findings refuse: code=%d err=%q", code, err)
	}

	// The verb's one-line-per-kind page size still applies.
	code, err, total, titles := surfList(t, r, "/api/plan-records/registry-rows?limit=2")
	if code != http.StatusOK || err != "" || total != 3 || len(titles) != 2 {
		t.Fatalf("limit: code=%d err=%q total=%d titles=%v", code, err, total, titles)
	}
}

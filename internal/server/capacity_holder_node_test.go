package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// TestCapacityStatusShowsHeldNode checks that /api/capacity, the body the MCP
// capacity_status tool and the CLI read, names the node each holder's lease
// sits on, both for the whole ledger and for one PRD's scoped view, and that
// the text path renders the same node beside each holder.
func TestCapacityStatusShowsHeldNode(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	l.SetLimit("llm:pi", 4)
	s.SetCapacity(l)

	for _, c := range []struct{ holder, node string }{{"task-a", "gpu-a"}, {"task-b", "gpu-b"}} {
		req := capacity.Request{Holder: c.holder, PRDID: "prd-a", Pools: []string{"llm:pi"}, Node: c.node}
		if err := l.Acquire(context.Background(), req, 0, nil, nil); err != nil {
			t.Fatalf("acquire %s: %v", c.holder, err)
		}
	}

	type poolJSON struct {
		Name      string   `json:"name"`
		Holders   []string `json:"holders"`
		HoldNodes []string `json:"hold_nodes"`
	}
	type leaseJSON struct {
		Holder string `json:"holder"`
		Node   string `json:"node"`
	}
	decode := func(t *testing.T, target string) (map[string]poolJSON, []leaseJSON, []byte) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d body %s", target, rec.Code, rec.Body.String())
		}
		var resp struct {
			Pools  []poolJSON  `json:"pools"`
			Leases []leaseJSON `json:"leases"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		byName := map[string]poolJSON{}
		for _, p := range resp.Pools {
			byName[p.Name] = p
		}
		return byName, resp.Leases, rec.Body.Bytes()
	}

	check := func(t *testing.T, target string) []byte {
		t.Helper()
		pools, leases, body := decode(t, target)
		p, ok := pools["llm:pi"]
		if !ok {
			t.Fatalf("%s: llm:pi missing: %s", target, body)
		}
		if got, want := strings.Join(p.HoldNodes, ","), "gpu-a,gpu-b"; got != want {
			t.Fatalf("%s: hold_nodes = %v, want %s (holders %v)", target, p.HoldNodes, want, p.Holders)
		}
		nodeOf := map[string]string{}
		for _, ls := range leases {
			nodeOf[ls.Holder] = ls.Node
		}
		if nodeOf["task-a"] != "gpu-a" || nodeOf["task-b"] != "gpu-b" {
			t.Fatalf("%s: lease nodes = %v", target, nodeOf)
		}
		return body
	}

	body := check(t, "/api/capacity")
	// The comm and CLI text path folds this same body into its line.
	text, err := autonomous.FormatCapacityText(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "llm:pi: 2/4 held by task-a on gpu-a,task-b on gpu-b") {
		t.Fatalf("text path missing held node:\n%s", text)
	}
	// The PRD-scoped view carries the node too.
	check(t, "/api/capacity?prd_id=prd-a")
}

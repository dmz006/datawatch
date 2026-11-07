package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// countingTagReader is a session tag reader with a snapshot method that
// counts how often the snapshot is built and how often the per-Automaton
// method is called.
type countingTagReader struct {
	snapshots   int
	perPRDCalls int
}

func (c *countingTagReader) SystemTagsByPRD(string) []string {
	c.perPRDCalls++
	return []string{"never-used"}
}

func (c *countingTagReader) SystemTagSnapshot() func(string) []string {
	c.snapshots++
	return func(string) []string { return []string{"node-b", "node-a", "node-b"} }
}

func TestPRDListTakesTagSnapshotOnce(t *testing.T) {
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reader := &countingTagReader{}
	mgr.SetInheritedTagReader(reader)
	for i := 0; i < 100; i++ {
		if _, err := mgr.CreatePRD(fmt.Sprintf("spec %d", i), t.TempDir(), "", "", autonomous.EffortNormal); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{autonomousMgr: newTestAPI(mgr)}

	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds?limit=200", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if reader.snapshots != 1 {
		t.Errorf("snapshot built %d times for a 100-Automaton list, want 1", reader.snapshots)
	}
	if reader.perPRDCalls != 0 {
		t.Errorf("per-Automaton reader called %d times, want 0", reader.perPRDCalls)
	}
	body := rec.Body.String()
	var resp struct {
		PRDs []struct {
			InheritedTags []string `json:"inherited_tags"`
		} `json:"prds"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.PRDs) != 100 {
		t.Fatalf("got %d rows, want 100 (body %.200s)", len(resp.PRDs), body)
	}
	for i, p := range resp.PRDs {
		if len(p.InheritedTags) != 2 || p.InheritedTags[0] != "node-a" || p.InheritedTags[1] != "node-b" {
			t.Fatalf("row %d inherited_tags = %v, want [node-a node-b]", i, p.InheritedTags)
		}
	}
}

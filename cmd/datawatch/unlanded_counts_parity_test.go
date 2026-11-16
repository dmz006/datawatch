package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
)

// The unlanded-work counts section must read the same on every surface: the
// REST body, the CLI (raw JSON and text), the MCP tool, the chat verb and the
// shape the web app reads. One fixture of Automaton branches feeds all of them.

func unlandedParityFixture(now time.Time) []autonomous.UnlandedBranch {
	return []autonomous.UnlandedBranch{
		{AutomatonID: "a1", Branch: "automaton/a1", Title: "Landed docs", Recommendation: autonomous.UnlandedLanded, LastActivity: now.Add(-2 * time.Hour)},
		{AutomatonID: "a2", Branch: "automaton/a2", Title: "Old replaced work", Recommendation: autonomous.UnlandedSuperseded, LastActivity: now.Add(-10*24*time.Hour - 3*time.Hour)},
		{AutomatonID: "a3", Branch: "automaton/a3", Title: "Newer replaced work", Recommendation: autonomous.UnlandedSuperseded, LastActivity: now.Add(-3 * 24 * time.Hour)},
		{AutomatonID: "a4", Branch: "automaton/a4", Title: "Needs a call", Recommendation: autonomous.UnlandedNeedsDecision, LastActivity: now.Add(-30 * time.Minute)},
		{AutomatonID: "a5", Branch: "automaton/a5", Title: "Still open", Recommendation: "merge"},
	}
}

type parityCounts struct {
	ByState map[string]int
	ByAge   map[string]int
	Oldest  string
}

func countsFromList(t *testing.T, surface string, body []byte) parityCounts {
	t.Helper()
	l, err := autonomous.ParseUnlandedWorkList(body)
	if err != nil {
		t.Fatalf("%s: %v (%s)", surface, err, body)
	}
	if l.Counts == nil {
		t.Fatalf("%s: no counts section: %s", surface, body)
	}
	p := parityCounts{ByState: l.Counts.ByState, ByAge: l.Counts.ByAge}
	if l.Counts.OldestPending != nil {
		p.Oldest = l.Counts.OldestPending.Branch
	}
	return p
}

// countsFromWebShape reads the body the way the web app does: plain JSON
// objects keyed by the documented names.
func countsFromWebShape(t *testing.T, body []byte) parityCounts {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	c, _ := data["counts"].(map[string]any)
	if c == nil {
		t.Fatalf("web shape: no counts object: %s", body)
	}
	toInts := func(v any) map[string]int {
		m := map[string]int{}
		obj, _ := v.(map[string]any)
		for k, n := range obj {
			f, _ := n.(float64)
			m[k] = int(f)
		}
		return m
	}
	p := parityCounts{ByState: toInts(c["by_state"]), ByAge: toInts(c["by_age"])}
	if o, ok := c["oldest_pending"].(map[string]any); ok {
		p.Oldest, _ = o["branch"].(string)
	}
	return p
}

func TestUnlandedCountsParity_EverySurfaceSameCounts(t *testing.T) {
	now := time.Now()
	restBody, err := json.Marshal(autonomous.NewUnlandedWorkList(unlandedParityFixture(now), now))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/unlanded-work" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(restBody)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := writeReloadTestConfig(home+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatal(err)
	}

	want := parityCounts{
		ByState: map[string]int{autonomous.LandingLanded: 1, autonomous.LandingPending: 2, autonomous.LandingLive: 1, autonomous.LandingUnlanded: 1},
		ByAge:   map[string]int{autonomous.AgeUnderDay: 2, autonomous.AgeUnderWeek: 1, autonomous.AgeOlder: 2},
		Oldest:  "automaton/a2",
	}
	got := map[string]parityCounts{"rest": countsFromList(t, "rest", restBody), "web": countsFromWebShape(t, restBody)}

	cliJSON, err := runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	got["cli"] = countsFromList(t, "cli", []byte(cliJSON))

	m := mcp.New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), mcp.Options{Version: "test"})
	m.SetWebPort(port)
	mcpOut, err := m.Invoke(context.Background(), "autonomous_unlanded_work", nil)
	if err != nil {
		t.Fatal(err)
	}
	got["mcp"] = countsFromList(t, "mcp", []byte(mcpOut))

	for surface, c := range got {
		if !reflect.DeepEqual(c, want) {
			t.Errorf("%s counts = %+v, want %+v", surface, c, want)
		}
	}

	// The text surfaces (CLI and chat verb) render the same counts lines.
	wantLines := []string{
		fmt.Sprintf("Counts: %d landed, %d pending, %d live, %d unlanded", 1, 2, 1, 1),
		fmt.Sprintf("Age: %d under one day, %d under one week, %d older", 2, 1, 2),
		"Oldest pending: Old replaced work (automaton/a2), 10d 3h",
	}
	cliText, err := runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := router.NewRouter("h", "g", nil, nil, 0, nil)
	r.SetWebPort(port)
	commText := strings.Join(r.HandleTestMessage("autonomous unlanded-work"), "\n")
	for _, line := range wantLines {
		if !strings.Contains(cliText, line) {
			t.Errorf("CLI text lacks %q:\n%s", line, cliText)
		}
		if !strings.Contains(commText, line) {
			t.Errorf("chat reply lacks %q:\n%s", line, commText)
		}
	}
}

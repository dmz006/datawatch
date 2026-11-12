// Cost series REST tests (internal/costreport series over GET /api/cost/series).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/spendledger"
)

func getCostSeries(t *testing.T, s *Server, query string) (*httptest.ResponseRecorder, costreport.Series) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cost/series"+query, nil)
	rr := httptest.NewRecorder()
	s.handleCostSeries(rr, req)
	var out costreport.Series
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rr, out
}

func seriesLedger(now time.Time) fakeSpendLedger {
	return fakeSpendLedger{
		{SessionID: "s1", AutomatonID: "a1", Role: "worker", Backend: "aider", StartedAt: now.Add(-3 * time.Hour), TokensIn: 100, TokensOut: 50},
		{SessionID: "s2", AutomatonID: "a1", Role: "verification", Backend: "aider", StartedAt: now.Add(-2 * time.Hour), TokensIn: 10, TokensOut: 5},
		{SessionID: "s3", AutomatonID: "a2", Role: "worker", Backend: "ollama", StartedAt: now.Add(-90 * time.Minute), TokensIn: 7, TokensOut: 3},
		{SessionID: "s4", Role: "other", Backend: "ollama", StartedAt: now.Add(-30 * time.Minute), TokensIn: 1, TokensOut: 1},
		{SessionID: "old", AutomatonID: "a1", Role: "worker", Backend: "aider", StartedAt: now.Add(-10 * 24 * time.Hour), TokensIn: 999, TokensOut: 999},
	}
}

func seriesGroup(t *testing.T, s costreport.Series, key string) costreport.SeriesGroup {
	t.Helper()
	for _, g := range s.Groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("group %q missing: %+v", key, s.Groups)
	return costreport.SeriesGroup{}
}

func TestCostSeries_DefaultsGroupByAutomaton(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = seriesLedger(time.Now())
	rr, got := getCostSeries(t, s, "?window=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}
	if got.GroupBy != costreport.SeriesByAutomaton || got.Bucket != costreport.SeriesBucketHour {
		t.Fatalf("group_by=%q bucket=%q", got.GroupBy, got.Bucket)
	}
	if g := seriesGroup(t, got, "a1"); g.Tokens != 165 || g.Sessions != 2 {
		t.Errorf("a1 = %+v", g)
	}
	if g := seriesGroup(t, got, costreport.UnattributedKey); g.Tokens != 2 {
		t.Errorf("unattributed = %+v", g)
	}
	if len(got.Groups) != 3 {
		t.Errorf("want 3 groups (the 10-day-old entry is outside the window), got %d", len(got.Groups))
	}
	if g := seriesGroup(t, got, "a1"); len(g.Points) != len(got.Buckets) {
		t.Errorf("points %d != buckets %d", len(g.Points), len(got.Buckets))
	}
}

func TestCostSeries_GroupByModelCarriesUnits(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = seriesLedger(time.Now())
	rr, got := getCostSeries(t, s, "?group_by=model&window=24h&bucket=hour")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	aider := seriesGroup(t, got, "aider")
	if aider.Tokens != 165 {
		t.Fatalf("aider = %+v", aider)
	}
	// ollama is a local backend, so its money is a tokens-only "none" unit;
	// aider is metered, so its money is a dollars unit — never one number.
	ollama := seriesGroup(t, got, "ollama")
	if len(ollama.UnitTotals) != 1 || ollama.UnitTotals[0].Unit != costreport.UnitNone || ollama.UnitTotals[0].Dollars != nil {
		t.Errorf("ollama units wrong: %+v", ollama.UnitTotals)
	}
	if len(aider.UnitTotals) != 1 || aider.UnitTotals[0].Unit != costreport.UnitDollars {
		t.Errorf("aider units wrong: %+v", aider.UnitTotals)
	}
	if len(got.TotalsByUnit) != 2 {
		t.Errorf("series totals should hold two separate units: %+v", got.TotalsByUnit)
	}
}

func TestCostSeries_GroupByProjectWithoutManagerIsUnattributed(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = seriesLedger(time.Now())
	rr, got := getCostSeries(t, s, "?group_by=project&window=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(got.Groups) != 1 || got.Groups[0].Key != costreport.UnattributedKey {
		t.Fatalf("want one unattributed group, got %+v", got.Groups)
	}
}

func TestCostSeries_EmptyWindowAndNoLedger(t *testing.T) {
	s := bl90Server(t)
	rr, got := getCostSeries(t, s, "?window=24h") // no ledger wired
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(got.Groups) != 0 || len(got.Buckets) == 0 {
		t.Fatalf("want no groups and an x axis, got %+v", got)
	}
	if !strings.Contains(rr.Body.String(), `"groups":[]`) {
		t.Errorf("groups must serialize as [], got %s", rr.Body.String())
	}
	s.costReportLedger = fakeSpendLedger{}
	if _, got = getCostSeries(t, s, ""); len(got.Groups) != 0 {
		t.Fatalf("empty ledger should give no groups: %+v", got.Groups)
	}
}

func TestCostSeries_DefaultWindowUsesDayBuckets(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = seriesLedger(time.Now())
	rr, got := getCostSeries(t, s, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got.Bucket != costreport.SeriesBucketDay {
		t.Errorf("the default 7-day window should bucket by day, got %q", got.Bucket)
	}
}

func TestCostSeries_BadParams(t *testing.T) {
	s := bl90Server(t)
	for name, q := range map[string]string{
		"group_by":       "?group_by=week",
		"window":         "?window=soon",
		"window zero":    "?window=0h",
		"bucket":         "?bucket=minute",
		"bucket auto":    "?bucket=auto",
		"too many hours": "?window=90d&bucket=hour",
	} {
		rr, _ := getCostSeries(t, s, q)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d body=%s", name, rr.Code, rr.Body.String())
		}
	}
}

func TestCostSeries_RejectsNonGet(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/cost/series", nil)
	rr := httptest.NewRecorder()
	s.handleCostSeries(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
}

func TestCostSeries_NoManagerIs503(t *testing.T) {
	s := bl90Server(t)
	s.manager = nil
	rr, _ := getCostSeries(t, s, "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rr.Code)
	}
}

func TestCostSeries_RouteRegistered(t *testing.T) {
	src, err := os.ReadFile("server.go") // tests run in the package directory
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"/api/cost/series", api.handleCostSeries`) {
		t.Fatal("GET /api/cost/series is not registered in server.go")
	}
}

func TestAutomatonProject(t *testing.T) {
	cases := []struct {
		name string
		prd  *autonomous.PRD
		want string
	}{
		{"nil", nil, ""},
		{"tag wins over dir", &autonomous.PRD{SystemTags: []string{"automation:x", "project:alpha"}, ProjectDir: "/work/beta"}, "alpha"},
		{"blank tag falls to dir", &autonomous.PRD{SystemTags: []string{"project:  "}, ProjectDir: "/work/beta/"}, "beta"},
		{"dir only", &autonomous.PRD{ProjectDir: "/work/gamma"}, "gamma"},
		{"root dir", &autonomous.PRD{ProjectDir: "/"}, ""},
		{"nothing", &autonomous.PRD{SystemTags: []string{"automation:x"}}, ""},
	}
	for _, c := range cases {
		if got := automatonProject(c.prd); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCostSeriesProjectOf(t *testing.T) {
	of := costSeriesProjectOf([]*autonomous.PRD{
		{ID: "a1", SystemTags: []string{"project:alpha"}},
		{ID: "a2", ProjectDir: "/work/alpha"},
		{ID: "b1", ProjectDir: "/work/beta"},
		{ID: "none"},
		nil,
	})
	if of("a1") != "alpha" || of("a2") != "alpha" || of("b1") != "beta" {
		t.Fatalf("lookup wrong: %q %q %q", of("a1"), of("a2"), of("b1"))
	}
	if of("none") != "" || of("unknown") != "" {
		t.Fatal("an Automaton without a project must resolve to blank")
	}

	// the builder groups two Automata of one project onto a single line
	now := time.Now()
	s, err := costreport.BuildSeries(costreport.SeriesRequest{GroupBy: costreport.SeriesByProject, Window: 24 * time.Hour},
		[]spendledger.Entry{
			{SessionID: "s1", AutomatonID: "a1", Role: "worker", StartedAt: now.Add(-time.Hour), TokensIn: 10},
			{SessionID: "s2", AutomatonID: "a2", Role: "worker", StartedAt: now.Add(-time.Hour), TokensIn: 20},
			{SessionID: "s3", AutomatonID: "b1", Role: "worker", StartedAt: now.Add(-time.Hour), TokensIn: 40},
		},
		costreport.SeriesOptions{Now: now, ProjectOf: of})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Groups) != 2 || s.Groups[0].Key != "alpha" || s.Groups[0].Tokens != 30 || s.Groups[1].Key != "beta" {
		t.Fatalf("project grouping wrong: %+v", s.Groups)
	}
}

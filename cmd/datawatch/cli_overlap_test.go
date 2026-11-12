package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// overlapCLISample is the overlap read body: one Automaton, one registry row
// and one unmerged branch, best match first, with the threshold and mode.
const overlapCLISample = `{"warnings":[
  {"kind": "automaton", "id": "prd-77", "status": "running", "score": 0.82,
   "shared_terms": ["paging", "ingestion"],
   "reason": "Automaton prd-77 already describes this plan.",
   "read_hint": "datawatch autonomous get prd-77"},
  {"kind": "registry", "row": 4, "score": 0.61,
   "reason": "An existing Automaton plan is already on the roadmap (row 4).",
   "read_hint": "docs/plans/README.md row 4"},
  {"kind": "branch", "branch": "automaton/feat-paging", "score": 0.55,
   "reason": "An unmerged Automaton branch already carries this work (automaton/feat-paging).",
   "read_hint": "git log main..automaton/feat-paging"}
],"threshold": 0.5,"mode": "on"}`

// queryValues parses the query the fake daemon recorded.
func queryValues(t *testing.T, raw string) url.Values {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("query %q: %v", raw, err)
	}
	return q
}

func TestOverlapCLI_FlagsAreSentAsQuery(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"warnings":[],"threshold":0.5,"mode":"on"}`)
	if _, err := runLaneFillCmd(t, newAutonomousOverlapCmd(),
		[]string{"--title", "Add paging", "--spec", "Page the ingestion path"}); err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/overlap" {
		t.Errorf("path=%s; want the overlap route", d.path)
	}
	q := queryValues(t, d.query)
	if q.Get("title") != "Add paging" || q.Get("spec") != "Page the ingestion path" {
		t.Errorf("query=%q; want title and spec from the flags", d.query)
	}
}

func TestOverlapCLI_PositionalIsTheSpec(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"warnings":[],"threshold":0.5,"mode":"on"}`)
	if _, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), []string{"Page the ingestion path"}); err != nil {
		t.Fatal(err)
	}
	q := queryValues(t, d.query)
	if q.Get("spec") != "Page the ingestion path" {
		t.Errorf("query=%q; want the positional argument sent as spec", d.query)
	}
	if _, ok := q["title"]; ok {
		t.Errorf("query=%q; no title was given, so none may be sent", d.query)
	}
}

func TestOverlapCLI_SpecFlagAloneNeedsNoPositional(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"warnings":[],"threshold":0.5,"mode":"on"}`)
	if _, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), []string{"--spec", "Page the ingestion path"}); err != nil {
		t.Fatal(err)
	}
	if got := queryValues(t, d.query).Get("spec"); got != "Page the ingestion path" {
		t.Errorf("query=%q; want spec from --spec", d.query)
	}
}

func TestOverlapCLI_SpecGivenTwiceIsRefused(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, overlapCLISample)
	_, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), []string{"--spec", "one", "two"})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("expected a refusal for a spec given two ways, got %v", err)
	}
	if d.calls != 0 {
		t.Errorf("nothing may be sent, saw %d calls", d.calls)
	}
}

func TestOverlapCLI_NoTitleAndNoSpecIsRefused(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, overlapCLISample)
	_, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "--title") {
		t.Errorf("expected a refusal naming --title, got %v", err)
	}
	if d.calls != 0 {
		t.Errorf("nothing may be sent, saw %d calls", d.calls)
	}
}

func TestOverlapCLI_HumanOutputOneLinePerWarning(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, overlapCLISample)
	out, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), []string{"Page the ingestion path"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"automaton prd-77 status running score 0.82: Automaton prd-77 already describes this plan.\n",
		"registry row 4 score 0.61: An existing Automaton plan is already on the roadmap (row 4).\n",
		"branch automaton/feat-paging score 0.55: An unmerged Automaton branch already carries this work (automaton/feat-paging).\n",
	}
	if out != strings.Join(want, "") {
		t.Errorf("output mismatch\nwant:\n%s\ngot:\n%s", strings.Join(want, ""), out)
	}
}

func TestOverlapCLI_JSONPassesBodyUnchanged(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, overlapCLISample)
	out, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), []string{"--json", "Page the ingestion path"})
	if err != nil {
		t.Fatal(err)
	}
	if out != overlapCLISample {
		t.Errorf("--json must print the body untouched\nGot: %s", out)
	}
}

func TestOverlapCLI_EmptyResultSaysNoneFound(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"warnings":[],"threshold":0.5,"mode":"on"}`)
	out, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), []string{"Page the ingestion path"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/overlap" {
		t.Errorf("path=%s; want the overlap route", d.path)
	}
	if out != "No overlaps found.\n" {
		t.Errorf("empty result must print one line saying none were found, got %q", out)
	}
}

func TestOverlapCLI_DaemonErrorPassesThrough(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusInternalServerError, "overlap search failed")
	_, err := runLaneFillCmd(t, newAutonomousOverlapCmd(), []string{"Page the ingestion path"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "overlap search failed") {
		t.Errorf("expected the daemon error to pass through, got %v", err)
	}
}

func TestOverlapCLI_Registered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	if !names["overlap"] {
		t.Error("overlap is not registered under autonomous")
	}
	if !strings.Contains(newAutonomousCmd().Long, "overlap [--title T] [--spec S | <spec>] [--json]") {
		t.Error("the autonomous help block lacks the overlap line")
	}
}

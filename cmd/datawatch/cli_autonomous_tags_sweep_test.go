// Flag-to-request tests for the `autonomous tags sweep` command: the REST
// route, the dry_run query, the table output and the --json pass-through.
// They use the captureDaemon / withCaptureServer helpers of
// cli_autonomous_subscriptions_test.go.
package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const tagsSweepReply = `{"sessions":{"examined":12,"changed":3,"unchanged":9,"failed":0},"automata":{"examined":5,"changed":2,"unchanged":3,"failed":0},"automations":{"examined":4,"changed":0,"unchanged":4,"failed":0},"tasks":{"examined":30,"changed":4,"unchanged":26,"failed":0},"templates":{"examined":2,"changed":1,"unchanged":1,"failed":0},"decision_records":{"examined":7,"changed":0,"unchanged":7,"failed":1},"questions":{"examined":1,"changed":1,"unchanged":0,"failed":0}}`

// TestTagsSweepRegistered confirms `sweep` hangs under the tags command (so
// both `autonomous tags sweep` and `autonomous tag-list sweep` reach it) and
// carries its two flags.
func TestTagsSweepRegistered(t *testing.T) {
	tags, _, err := newAutonomousCmd().Find([]string{"tags"})
	if err != nil || tags.Name() != "tag-list" {
		t.Fatalf("Find(tags) = %v, %v", tags, err)
	}
	sweep, _, err := tags.Find([]string{"sweep"})
	if err != nil || sweep.Name() != "sweep" {
		t.Fatalf("Find(sweep) = %v, %v", sweep, err)
	}
	if _, err := sweep.Flags().GetBool("dry-run"); err != nil {
		t.Fatalf("--dry-run flag missing: %v", err)
	}
	if _, err := sweep.Flags().GetBool("json"); err != nil {
		t.Fatalf("--json flag missing: %v", err)
	}
	if _, _, err := newAutonomousCmd().Find([]string{"tag-list", "sweep"}); err != nil {
		t.Fatalf("tag-list sweep not found: %v", err)
	}
}

// TestTagsSweepRouteAndQuery maps each CLI form onto the REST call the
// daemon gets: POST /api/autonomous/tags/sweep with no body, and
// dry_run=true only for the --dry-run form.
func TestTagsSweepRouteAndQuery(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		query string
	}{
		{"plain", []string{"tags", "sweep"}, ""},
		{"dry-run", []string{"tags", "sweep", "--dry-run"}, "dry_run=true"},
		{"alias-dry-run", []string{"tag-list", "sweep", "--dry-run"}, "dry_run=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: tagsSweepReply}
			withCaptureServer(t, c)
			if err := runSubCmd(t, newAutonomousCmd(), tc.args...); err != nil {
				t.Fatalf("%v: %v", tc.args, err)
			}
			if c.gotMethod != http.MethodPost {
				t.Errorf("method = %q, want POST", c.gotMethod)
			}
			if c.gotPath != "/api/autonomous/tags/sweep" {
				t.Errorf("path = %q, want /api/autonomous/tags/sweep", c.gotPath)
			}
			if c.gotQuery != tc.query {
				t.Errorf("query = %q, want %q", c.gotQuery, tc.query)
			}
			if len(c.gotRaw) > 0 {
				t.Errorf("unexpected body %q", c.gotRaw)
			}
		})
	}
}

// TestTagsSweepTableOutput confirms the default printout is the short
// per-kind table: the header, every kind named, and the tally numbers.
func TestTagsSweepTableOutput(t *testing.T) {
	c := &captureDaemon{replyBody: tagsSweepReply}
	withCaptureServer(t, c)
	var err error
	out := captureStdout(t, func() { err = runSubCmd(t, newAutonomousCmd(), "tags", "sweep") })
	if err != nil {
		t.Fatalf("tags sweep: %v", err)
	}
	for _, want := range []string{"EXAMINED", "CHANGED", "UNCHANGED", "FAILED",
		"sessions", "automata", "automations", "tasks", "templates",
		"decision records", "held questions", "12", "30", "7"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output misses %q:\n%s", want, out)
		}
	}
}

// TestTagsSweepJSONOutput confirms --json prints the SweepResult reply
// untouched (the daemon's JSON, not the table).
func TestTagsSweepJSONOutput(t *testing.T) {
	c := &captureDaemon{replyBody: tagsSweepReply}
	withCaptureServer(t, c)
	var err error
	out := captureStdout(t, func() { err = runSubCmd(t, newAutonomousCmd(), "tags", "sweep", "--json") })
	if err != nil {
		t.Fatalf("tags sweep --json: %v", err)
	}
	if !strings.Contains(out, `"decision_records"`) {
		t.Errorf("--json output is not the raw SweepResult:\n%s", out)
	}
	if strings.Contains(out, "EXAMINED\t") || strings.Contains(out, "KIND") {
		t.Errorf("--json output should not be the table:\n%s", out)
	}
}

// TestTagsSweepTallyRoundTrip decodes the reply shape the CLI expects and
// checks every kind's four counters survive the round trip.
func TestTagsSweepTallyRoundTrip(t *testing.T) {
	var res tagsSweepResult
	if err := json.Unmarshal([]byte(tagsSweepReply), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string][4]int{
		"sessions":         {12, 3, 9, 0},
		"automata":         {5, 2, 3, 0},
		"automations":      {4, 0, 4, 0},
		"tasks":            {30, 4, 26, 0},
		"templates":        {2, 1, 1, 0},
		"decision_records": {7, 0, 7, 1},
		"questions":        {1, 1, 0, 0},
	}
	got := map[string][4]int{
		"sessions":         {res.Sessions.Examined, res.Sessions.Changed, res.Sessions.Unchanged, res.Sessions.Failed},
		"automata":         {res.Automata.Examined, res.Automata.Changed, res.Automata.Unchanged, res.Automata.Failed},
		"automations":      {res.Automations.Examined, res.Automations.Changed, res.Automations.Unchanged, res.Automations.Failed},
		"tasks":            {res.Tasks.Examined, res.Tasks.Changed, res.Tasks.Unchanged, res.Tasks.Failed},
		"templates":        {res.Templates.Examined, res.Templates.Changed, res.Templates.Unchanged, res.Templates.Failed},
		"decision_records": {res.DecisionRecords.Examined, res.DecisionRecords.Changed, res.DecisionRecords.Unchanged, res.DecisionRecords.Failed},
		"questions":        {res.Questions.Examined, res.Questions.Changed, res.Questions.Unchanged, res.Questions.Failed},
	}
	for k, w := range want {
		if !reflect.DeepEqual(got[k], w) {
			t.Errorf("%s = %v, want %v", k, got[k], w)
		}
	}
}

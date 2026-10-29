// CLI coverage for `automation status`: each row's pending member inserts
// are printed below the table, and the PENDING column carries the daemon's
// pending_members count (which includes held inserts).
package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// tableCols splits one tabwriter line on the 2-space column padding, so cells
// that themselves contain spaces (e.g. CURRENT "m1 (Do it)") stay intact.
var tableCols = regexp.MustCompile(` {2,}`)

func TestAutomationCLIStatusShowsPendingInserts(t *testing.T) {
	// First row has one held insert (already counted in pending_members);
	// the second row has none at all.
	c := &captureDaemon{replyBody: `{"automations":[
{"id":"ab12","name":"chain","status":"active","current_member_id":"m1","current_prd_title":"Do it",
 "pending_members":1,"pending_inserts":[
   {"id":"9c30ef8e","prd_id":"p-77","reason":"tests found a gap","actor":"agent:7","status":"pending"}]},
{"id":"o77d","name":"quiet","status":"paused","pending_members":0,"pending_inserts":[]}]}`}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status") })
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if c.gotMethod != http.MethodGet || c.gotPath != "/api/autonomous/status" {
		t.Errorf("proxied %s %s, want GET /api/autonomous/status", c.gotMethod, c.gotPath)
	}

	// The table is printed first.
	for _, want := range []string{"ID", "NAME", "STATUS", "PENDING", "ab12", "chain", "active"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output lacks %q:\n%s", want, out)
		}
	}

	// Then the pending inserts section for the row that has them.
	for _, want := range []string{
		"Pending inserts of ab12 (chain):",
		"9c30ef8e",
		"p-77",
		"tests found a gap",
		"datawatch automation approve ab12 9c30ef8e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// The row without inserts prints no insert lines at all.
	if strings.Contains(out, "Pending inserts of o77d") {
		t.Errorf("row without inserts got a pending section:\n%s", out)
	}
	if strings.Contains(out, "approve o77d") {
		t.Errorf("row without inserts suggests an approve command:\n%s", out)
	}
}

func TestAutomationCLIStatusPendingColumnCountsInserts(t *testing.T) {
	// One Automation, two held inserts and no pending member: the daemon
	// reports pending_members=2 (the two inserts), and the PENDING cell must
	// match that value.
	c := &captureDaemon{replyBody: `{"automations":[
{"id":"ab12","name":"chain","status":"active","current_member_id":"m1","current_prd_title":"Do it",
 "pending_members":2,"pending_inserts":[
   {"id":"9c30ef8e","prd_id":"p-77","reason":"tests found a gap","actor":"agent:7","status":"pending"},
   {"id":"d1f0a0be","prd_id":"p-78","reason":"operator asked","actor":"dmz","status":"pending"}]}]}`}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status") })
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	// Parse the table by columns: find the PENDING header, then read that
	// column of the ab12 row.
	var headerCols, rowCols []string
	foundRow := false
	for _, l := range strings.Split(out, "\n") {
		fields := tableCols.Split(l, -1)
		if len(fields) == 0 || fields[0] == "" {
			continue
		}
		if fields[0] == "ID" && strings.Contains(l, "PENDING") {
			headerCols = fields
		}
		if fields[0] == "ab12" {
			rowCols = fields
			foundRow = true
		}
	}
	if headerCols == nil {
		t.Fatalf("output lacks the table header:\n%s", out)
	}
	if !foundRow {
		t.Fatalf("output lacks the ab12 table row:\n%s", out)
	}
	pendingCol := -1
	for i, h := range headerCols {
		if h == "PENDING" {
			pendingCol = i
			break
		}
	}
	if pendingCol < 0 || pendingCol >= len(rowCols) {
		t.Fatalf("PENDING column not found in header %v / row %v", headerCols, rowCols)
	}
	if got := rowCols[pendingCol]; got != "2" {
		t.Errorf("PENDING cell = %q, want 2 (the daemon's pending_members counting the inserts)", got)
	}

	// The --json form includes the row's pending_inserts.
	out = captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status", "--json") })
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("status --json is not a JSON array: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("json rows = %d, want 1: %s", len(rows), out)
	}
	pi, ok := rows[0]["pending_inserts"].([]any)
	if !ok || len(pi) != 2 {
		t.Errorf("ab12 row pending_inserts = %v, want two entries: %s", rows[0]["pending_inserts"], out)
	}
	if got := rows[0]["pending_members"].(float64); got != 2 {
		t.Errorf("ab12 row pending_members = %v, want 2", rows[0]["pending_members"])
	}
}

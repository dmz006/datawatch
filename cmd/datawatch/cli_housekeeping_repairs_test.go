package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const housekeepingRepairsCLIBody = `{"grace_seconds":120,"groups":[
 {"group":"task_chain","mode":"report","would":[{"subject":"t1","kind":"would","found":"chain stalled","done":"would repair (report mode; nothing changed)","undo":"reset it"}],"did":[],"failed":[],"skipped_unsafe":[]},
 {"group":"lane_fill","mode":"on","would":[],"did":[{"subject":"l1","kind":"did","found":"idle lane","done":"started next step","undo":"cancel the step"}],"failed":[],"skipped_unsafe":[]}
]}`

func housekeepingRepairsDaemon(t *testing.T, status int, body string) (method, path *string) {
	t.Helper()
	var m, p string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, p = r.Method, r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return &m, &p
}

func runHousekeepingRepairsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := &cobra.Command{Use: "autonomous", SilenceUsage: true, SilenceErrors: true}
	c.AddCommand(newAutonomousHousekeepingRepairsCmd())
	c.SetArgs(append([]string{"housekeeping-repairs"}, args...))
	out := &strings.Builder{}
	c.SetOut(out)
	err := c.Execute()
	return out.String(), err
}

func TestHousekeepingRepairsCLIRegistered(t *testing.T) {
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "housekeeping-repairs" {
			return
		}
	}
	t.Error("housekeeping-repairs is not registered under autonomous")
}

func TestHousekeepingRepairsCLIText(t *testing.T) {
	method, path := housekeepingRepairsDaemon(t, 200, housekeepingRepairsCLIBody)
	out, err := runHousekeepingRepairsCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if *method != http.MethodGet || *path != "/api/autonomous/housekeeping-repairs" {
		t.Errorf("request %s %s", *method, *path)
	}
	for _, frag := range []string{"task_chain: mode report", "would do 1", "lane_fill: mode on", "did 1", "undo: reset it", "undo: cancel the step"} {
		if !strings.Contains(out, frag) {
			t.Errorf("output lacks %q:\n%s", frag, out)
		}
	}
}

func TestHousekeepingRepairsCLIJSONAndErrors(t *testing.T) {
	housekeepingRepairsDaemon(t, 200, housekeepingRepairsCLIBody)
	out, err := runHousekeepingRepairsCmd(t, "--json")
	if err != nil || out != housekeepingRepairsCLIBody {
		t.Errorf("--json must pass the body untouched: err=%v out=%q", err, out)
	}
	housekeepingRepairsDaemon(t, 503, "autonomous disabled")
	if _, err := runHousekeepingRepairsCmd(t); err == nil {
		t.Error("a server failure must return an error")
	}
	if _, err := runHousekeepingRepairsCmd(t, "extra"); err == nil {
		t.Error("an extra argument must be refused")
	}
}

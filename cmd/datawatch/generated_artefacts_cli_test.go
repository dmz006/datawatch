// generated_artefacts_cli_test.go — fakes the daemon and checks the
// request shape of generated-artefacts, change-record and generated.
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

type genArtReq struct{ method, uri, body string }

func genArtDaemon(t *testing.T) func() genArtReq {
	t.Helper()
	var mu sync.Mutex
	var got genArtReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = genArtReq{r.Method, r.URL.RequestURI(), string(b)}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return func() genArtReq { mu.Lock(); defer mu.Unlock(); return got }
}

func runGenArt(args ...string) error {
	c := &cobra.Command{Use: "autonomous", SilenceUsage: true, SilenceErrors: true}
	c.AddCommand(newAutonomousGeneratedArtefactsCmd(), newAutonomousChangeRecordCmd(), newAutonomousGeneratedCmd())
	c.SetArgs(args)
	return c.Execute()
}

func TestGeneratedArtefactsCLIRequests(t *testing.T) {
	cases := []struct {
		args         []string
		method, uri  string
		bodyContains string
	}{
		{[]string{"generated-artefacts"}, "GET", "/api/autonomous/generated-artefacts", ""},
		{[]string{"generated-artefacts", "get"}, "GET", "/api/autonomous/generated-artefacts", ""},
		{[]string{"generated-artefacts", "set", "gate_report", "off"}, "PUT", "/api/config", `"autonomous.generated_artefacts.gate_report":false`},
		{[]string{"generated-artefacts", "set", "parity_draft", "on"}, "PUT", "/api/config", `"autonomous.generated_artefacts.parity_draft":true`},
		{[]string{"change-record", "p1"}, "GET", "/api/autonomous/prds/p1/change-record", ""},
		{[]string{"generated", "p1", "gate_report"}, "GET", "/api/autonomous/prds/p1/generated/gate_report", ""},
		{[]string{"generated", "p1", "gate_report", "--regenerate"}, "POST", "/api/autonomous/prds/p1/generated/gate_report/regenerate", ""},
	}
	for _, tc := range cases {
		get := genArtDaemon(t)
		if err := runGenArt(tc.args...); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		g := get()
		if g.method != tc.method || g.uri != tc.uri || !strings.Contains(g.body, tc.bodyContains) {
			t.Errorf("%v: got %+v, want %s %s body~%q", tc.args, g, tc.method, tc.uri, tc.bodyContains)
		}
	}
}

func TestGeneratedArtefactsCLIUsageErrors(t *testing.T) {
	genArtDaemon(t)
	for _, args := range [][]string{
		{"generated-artefacts", "set", "gate_report"},
		{"generated-artefacts", "set", "gate_report", "maybe"},
		{"generated-artefacts", "bogus"},
		{"change-record"},
		{"generated", "p1"},
	} {
		if err := runGenArt(args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestGeneratedArtefactsCLIHelpSaysAutomaton(t *testing.T) {
	if strings.Contains(newAutonomousChangeRecordCmd().Short, "PRD") {
		t.Error("help must say Automaton, not PRD")
	}
}

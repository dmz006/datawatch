// Low-bandwidth list flags for `session list` and `alerts` (BL432 / GH#242).

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// recordingDaemon starts a fake daemon that records each request's path
// and query and answers with body. serverURL is pointed at it for the test.
func recordingDaemon(t *testing.T, body string) *[]*url.URL {
	t.Helper()
	var mu sync.Mutex
	var seen []*url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return &seen
}

func sessionListSubcmd(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range newSessionCmd().Commands() {
		if c.Name() == "list" {
			return c
		}
	}
	t.Fatal("session list subcommand missing")
	return nil
}

func runCmd(t *testing.T, c *cobra.Command, args ...string) error {
	t.Helper()
	c.SetArgs(args)
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c.Execute()
}

func TestSessionListQuery_MapsNewFlags(t *testing.T) {
	c := sessionListSubcmd(t)
	if err := c.ParseFlags([]string{"--summary", "--states", "active,failed", "--since", "1700000000", "--name", "w-*"}); err != nil {
		t.Fatal(err)
	}
	q := sessionListQuery(c)
	want := url.Values{"view": {"summary"}, "states": {"active,failed"}, "since": {"1700000000"}, "name": {"w-*"}}
	if q.Encode() != want.Encode() {
		t.Errorf("query = %q, want %q", q.Encode(), want.Encode())
	}
}

func TestSessionListQuery_EmptyWithoutFlags(t *testing.T) {
	c := sessionListSubcmd(t)
	if q := sessionListQuery(c); len(q) != 0 {
		t.Errorf("want empty query, got %q", q.Encode())
	}
	if got := sessionsPath(nil); got != "/api/sessions" {
		t.Errorf("sessionsPath(nil) = %q", got)
	}
}

func TestSessionListCmd_SummaryHitsAPI(t *testing.T) {
	seen := recordingDaemon(t, `[]`)
	if err := runCmd(t, newSessionCmd(), "list", "--summary", "--states", "running"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	u := (*seen)[0]
	if u.Path != "/api/sessions" || u.Query().Get("view") != "summary" || u.Query().Get("states") != "running" {
		t.Errorf("request = %s", u)
	}
}

func TestAlertsCmd_LimitAndUnreadMapToPagingParams(t *testing.T) {
	seen := recordingDaemon(t, `{"alerts":[],"unread_count":3,"total":9,"has_more":false}`)
	if err := runCmd(t, newAlertsCmd(), "--limit", "5", "--unread", "--system"); err != nil {
		t.Fatal(err)
	}
	q := (*seen)[0].Query()
	if q.Get("limit") != "5" || q.Get("unread") != "1" || q.Get("source") != "system" {
		t.Errorf("query = %q", q.Encode())
	}
}

func TestAlertsCmd_LimitZeroIsSent(t *testing.T) {
	seen := recordingDaemon(t, `{"alerts":[],"unread_count":0,"total":0}`)
	if err := runCmd(t, newAlertsCmd(), "--limit", "0"); err != nil {
		t.Fatal(err)
	}
	if q := (*seen)[0].Query(); !q.Has("limit") || q.Get("limit") != "0" {
		t.Errorf("limit=0 not forwarded: %q", q.Encode())
	}
}

func TestAlertsCmd_NoFlagsKeepsLegacyRequest(t *testing.T) {
	seen := recordingDaemon(t, `{"alerts":[],"unread_count":0}`)
	if err := runCmd(t, newAlertsCmd()); err != nil {
		t.Fatal(err)
	}
	if u := (*seen)[0]; u.Path != "/api/alerts" || u.RawQuery != "" {
		t.Errorf("legacy request changed: %s", u)
	}
}

func TestAlertsCmd_NegativeLimitRejected(t *testing.T) {
	recordingDaemon(t, `{}`)
	err := runCmd(t, newAlertsCmd(), "--limit", "-1")
	if err == nil || !strings.Contains(err.Error(), "--limit") {
		t.Errorf("err = %v, want --limit error", err)
	}
}

func TestAlertsCmd_CountHitsCountEndpoint(t *testing.T) {
	seen := recordingDaemon(t, `{"unread_count":2,"total":4}`)
	if err := runCmd(t, newAlertsCmd(), "--count"); err != nil {
		t.Fatal(err)
	}
	if u := (*seen)[0]; u.Path != "/api/alerts/count" {
		t.Errorf("path = %q, want /api/alerts/count", u.Path)
	}
}

func TestAlertsCmd_CountReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "alert store not available", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	if err := printAlertsCount(srv.URL + "/api/alerts"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v, want HTTP 503", err)
	}
}

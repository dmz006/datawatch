package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestProgressKindComm_Set(t *testing.T) {
	for _, msg := range []string{
		"autonomous set-kind p1 bug",
		"autonomous set_kind p1 bug",
		"autonomous set-kind p1 BUG",
	} {
		path, got := commSetLLMBody(t, msg)
		if path != "/api/autonomous/prds/p1/set_kind" || got["kind"] != "bug" || len(got) != 1 {
			t.Errorf("%q: path=%s body=%v", msg, path, got)
		}
	}
}

func TestProgressKindComm_BadKindShowsUsageAndSendsNothing(t *testing.T) {
	calls := 0
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous set-kind p1 chore"), " ")
	if !strings.Contains(got, "usage: autonomous set-kind <prd-id> <feature|bug|other>") {
		t.Errorf("expected the usage message, got %q", got)
	}
	if !strings.Contains(got, "chore") {
		t.Errorf("expected the bad kind to be named, got %q", got)
	}
	if calls != 0 {
		t.Errorf("a bad kind must not reach the daemon, got %d calls", calls)
	}
}

func TestProgressKindComm_MissingArgsShowUsage(t *testing.T) {
	r := &Router{}
	for _, msg := range []string{"autonomous set-kind", "autonomous set-kind p1"} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage") {
			t.Errorf("%q: expected usage, got %q", msg, got)
		}
	}
}

func TestProgressKindComm_HelpListsVerb(t *testing.T) {
	r := &Router{}
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(help, "set-kind <id> <feature|bug|other>") {
		t.Errorf("help missing set-kind: %q", help)
	}
}

func TestProgressKindComm_DaemonRefusalIsReported(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous set-kind nope other"), " ")
	if !strings.Contains(got, "set-kind failed") {
		t.Errorf("expected a failure reply, got %q", got)
	}
}

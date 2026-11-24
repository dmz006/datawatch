package router

import (
	"net/http"
	"net/url"
	"testing"
)

func TestCancelCommTakesOutcomeAndDecision(t *testing.T) {
	for _, tc := range []struct{ text, query string }{
		{"autonomous cancel ab12 outcome=dropped drop no longer wanted", "decision=drop&outcome=dropped&reason=no+longer+wanted"},
		{"autonomous cancel ab12 outcome=superseded via=ff00 keep for the next release", "decision=keep&delivered_via=ff00&outcome=superseded&reason=for+the+next+release"},
		// A decision word inside a note belongs to the note.
		{"autonomous cancel ab12 outcome=dropped note=we drop this one", "note=we+drop+this+one&outcome=dropped"},
	} {
		log, port := laneFillCommDaemon(t, http.StatusOK, `{"status":"cancelled"}`)
		setupPeerRouter(t, port).HandleTestMessage(tc.text)
		got, err := url.ParseQuery(log.query)
		if err != nil {
			t.Fatalf("%s: %v", tc.text, err)
		}
		if log.method != http.MethodDelete || got.Encode() != tc.query {
			t.Errorf("%s: %s ?%s, want ?%s", tc.text, log.method, got.Encode(), tc.query)
		}
	}
	log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
	setupPeerRouter(t, port).HandleTestMessage("autonomous cancel ab12 outcome=dropped drop")
	if log.calls != 0 {
		t.Errorf("a decision with no reason reached the daemon (%d calls)", log.calls)
	}
}

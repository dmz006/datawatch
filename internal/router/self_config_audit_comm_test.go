// BL412 — comm `audit self-config`: the chat-channel read view.

package router_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/router"
)

func selfConfigCommRouter(t *testing.T) (*router.Router, *string, *string) {
	t.Helper()
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"count":1,"entries":[{"time":"t","surface":"mcp","tool":"config_set","allowed":true}],"path":"/x.jsonl"}`))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := router.NewRouter("h", "g", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, &gotPath, &gotQuery
}

func TestAuditSelfConfig_Comm(t *testing.T) {
	r, gotPath, gotQuery := selfConfigCommRouter(t)

	out := r.HandleTestMessage("audit self-config limit=5")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if !strings.Contains(out[0], "self-config audit") {
		t.Errorf("reply = %q, want the self-config audit view", out[0])
	}
	if !strings.Contains(out[0], "config_set") {
		t.Errorf("reply should carry the record content: %s", out[0])
	}
	if *gotPath != "/api/audit/self-config" {
		t.Errorf("path = %q", *gotPath)
	}
	if *gotQuery != "limit=5" {
		t.Errorf("query = %q, want limit=5", *gotQuery)
	}

	// No limit → no query (the daemon applies its default).
	*gotQuery = "unset"
	r.HandleTestMessage("audit self-config")
	if *gotQuery != "" {
		t.Errorf("bare call forwarded query %q, want none", *gotQuery)
	}

	// Stray words are dropped; the limit word wins.
	*gotQuery = "unset"
	r.HandleTestMessage("audit self-config bogus=1 limit=2")
	if *gotQuery != "limit=2" {
		t.Errorf("query = %q, want limit=2", *gotQuery)
	}
}

package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/router"
)

// identityCommRouter returns a router whose loopback is an httptest server
// that records the last request and answers with a fixed status and body.
func identityCommRouter(t *testing.T, status int, answer string) (*router.Router, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(b))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := router.NewRouter("h", "g", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, &seen
}

func joined(out []string) string { return strings.Join(out, "\n") }

func TestIdentityComm_Backups(t *testing.T) {
	t.Run("one line per backup, in the daemon's order", func(t *testing.T) {
		newer := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
		older := newer.Add(-24 * time.Hour)
		r, seen := identityCommRouter(t, 200, `{"keep":10,"backups":[`+
			`{"name":"identity-b.yaml","created_at":"`+newer.Format(time.RFC3339)+`","role":"engineer","field_count":5},`+
			`{"name":"identity-a.yaml","created_at":"`+older.Format(time.RFC3339)+`","role":"","field_count":2}]}`)
		got := joined(r.HandleTestMessage("identity backups"))
		if len(*seen) != 1 || (*seen)[0] != "GET /api/identity/backups " {
			t.Errorf("requests = %v", *seen)
		}
		ib, ia := strings.Index(got, "identity-b.yaml"), strings.Index(got, "identity-a.yaml")
		if ib < 0 || ia < 0 || ib > ia {
			t.Errorf("backups missing or out of order:\n%s", got)
		}
		for _, need := range []string{"engineer", "5 fields", "2 fields", newer.Local().Format("2006-01-02 15:04:05")} {
			if !strings.Contains(got, need) {
				t.Errorf("reply lacks %q:\n%s", need, got)
			}
		}
	})

	t.Run("none yet", func(t *testing.T) {
		r, _ := identityCommRouter(t, 200, `{"backups":[],"keep":10}`)
		if got := joined(r.HandleTestMessage("identity backups")); !strings.Contains(got, "no backups yet") {
			t.Errorf("reply = %q", got)
		}
	})
}

func TestIdentityComm_Restore(t *testing.T) {
	t.Run("latest", func(t *testing.T) {
		r, seen := identityCommRouter(t, 200, `{"role":"engineer"}`)
		got := joined(r.HandleTestMessage("identity restore latest"))
		if len(*seen) != 1 || !strings.HasPrefix((*seen)[0], "POST /api/identity/restore ") || !strings.Contains((*seen)[0], `"backup":"latest"`) {
			t.Errorf("requests = %v", *seen)
		}
		if !strings.Contains(got, "engineer") {
			t.Errorf("reply lacks the restored identity: %q", got)
		}
	})

	t.Run("no argument shows usage and sends nothing", func(t *testing.T) {
		r, seen := identityCommRouter(t, 200, `{}`)
		got := joined(r.HandleTestMessage("identity restore"))
		if len(*seen) != 0 {
			t.Errorf("requests = %v", *seen)
		}
		if !strings.Contains(got, "identity restore <name|latest>") {
			t.Errorf("reply = %q", got)
		}
	})

	t.Run("daemon error passes through", func(t *testing.T) {
		r, _ := identityCommRouter(t, 404, `{"error":"backup not found"}`)
		got := joined(r.HandleTestMessage("identity restore nope"))
		if !strings.Contains(got, "404") || !strings.Contains(got, "backup not found") {
			t.Errorf("reply = %q", got)
		}
	})
}

func TestIdentityComm_ResetIsNotAChatVerb(t *testing.T) {
	r, seen := identityCommRouter(t, 200, `{}`)
	got := joined(r.HandleTestMessage("identity reset"))
	if len(*seen) != 0 {
		t.Errorf("identity reset called the daemon: %v", *seen)
	}
	for _, need := range []string{"datawatch identity reset", "PWA", "reset_identity"} {
		if !strings.Contains(got, need) {
			t.Errorf("reply lacks %q: %q", need, got)
		}
	}
}

func TestIdentityComm_UsageListsNewVerbs(t *testing.T) {
	r, _ := identityCommRouter(t, 200, `{}`)
	got := joined(r.HandleTestMessage("identity bogus"))
	for _, need := range []string{"identity backups", "identity restore", "identity reset"} {
		if !strings.Contains(got, need) {
			t.Errorf("usage lacks %q: %q", need, got)
		}
	}
}

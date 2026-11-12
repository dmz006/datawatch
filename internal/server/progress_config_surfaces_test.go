// Every autonomous.progress key must be readable and writable through
// GET and PUT /api/config and the chat `configure` command, and a bad value
// must reject the whole PUT without writing anything.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/router"
)

func progressConfigField(t *testing.T, s *Server, field string) string {
	t.Helper()
	var progress map[string]json.RawMessage
	raw, ok := getAutonomousConfigMap(t, s)["progress"]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks progress")
	}
	if err := json.Unmarshal(raw, &progress); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	return strings.Trim(string(progress[field]), `"`)
}

func progressPutStatus(t *testing.T, s *Server, body map[string]any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr.Code
}

var progressConfigRows = []struct {
	field   string
	value   any
	want    string
	invalid []any
}{
	{"lanes", 7, "7", []any{-1, 2.5, "many", true}},
	{"inactive_gap_hours", 1.5, "1.5", []any{-1, "long", false}},
	{"backlog_path", "plans/backlog.md", "plans/backlog.md", []any{42, "a\u0000b"}},
	{"since", "2026-09-01", "2026-09-01", []any{"yesterday", "2026-13-40", 20260901}},
	{"snapshot_retention_days", 30, "30", []any{-5, 0.5, "forever"}},
}

func TestProgressConfigDefaultsOnGet(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	for field, want := range map[string]string{
		"lanes": "0", "inactive_gap_hours": "0", "backlog_path": "docs/plans/README.md",
		"since": "", "snapshot_retention_days": "0",
	} {
		if got := progressConfigField(t, s, field); got != want {
			t.Errorf("default %s = %q, want %q", field, got, want)
		}
	}
}

func TestProgressConfigRESTRoundTrip(t *testing.T) {
	for _, row := range progressConfigRows {
		key := "autonomous.progress." + row.field
		t.Run(row.field, func(t *testing.T) {
			s, _, cfgPath := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: row.value})
			if got := progressConfigField(t, s, row.field); got != row.want {
				t.Fatalf("GET after PUT = %q, want %q", got, row.want)
			}
			saved, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("config file not written: %v", err)
			}
			if !strings.Contains(string(saved), row.field+":") {
				t.Fatalf("config file lacks %s:\n%s", row.field, saved)
			}
			for _, bad := range row.invalid {
				if code := progressPutStatus(t, s, map[string]any{key: bad}); code != http.StatusBadRequest {
					t.Errorf("PUT %v: want 400, got %d", bad, code)
				}
				if got := progressConfigField(t, s, row.field); got != row.want {
					t.Errorf("value after rejected PUT %v = %q, want %q", bad, got, row.want)
				}
			}
		})
	}
}

func TestProgressConfigZeroValuesAccepted(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.progress.lanes": 9, "autonomous.progress.inactive_gap_hours": 2,
		"autonomous.progress.snapshot_retention_days": 10, "autonomous.progress.since": "2026-01-01",
	})
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.progress.lanes": 0, "autonomous.progress.inactive_gap_hours": 0,
		"autonomous.progress.snapshot_retention_days": 0, "autonomous.progress.since": "",
		"autonomous.progress.backlog_path": "",
	})
	for field, want := range map[string]string{
		"lanes": "0", "inactive_gap_hours": "0", "snapshot_retention_days": "0",
		"since": "", "backlog_path": "docs/plans/README.md",
	} {
		if got := progressConfigField(t, s, field); got != want {
			t.Errorf("%s after zeroing = %q, want %q", field, got, want)
		}
	}
}

func TestProgressConfigNumericStringsAccepted(t *testing.T) {
	// The CLI and the chat channel can send numbers as strings.
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.progress.lanes": "4", "autonomous.progress.inactive_gap_hours": "2.5",
		"autonomous.progress.snapshot_retention_days": "14",
	})
	for field, want := range map[string]string{"lanes": "4", "inactive_gap_hours": "2.5", "snapshot_retention_days": "14"} {
		if got := progressConfigField(t, s, field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	// A cleared number field in Settings arrives as an empty string: default.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.progress.lanes": ""})
	if got := progressConfigField(t, s, "lanes"); got != "0" {
		t.Errorf("lanes after an empty string = %q, want 0", got)
	}
}

func TestProgressConfigBadValueWritesNothing(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.progress.lanes": 3})
	before, _ := os.ReadFile(cfgPath)
	// One good key beside one bad key: the whole PUT is rejected.
	code := progressPutStatus(t, s, map[string]any{
		"autonomous.progress.snapshot_retention_days": 45,
		"autonomous.progress.since":                   "not-a-date",
		"autonomous.progress.lanes":                   12,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("mixed PUT: want 400, got %d", code)
	}
	for field, want := range map[string]string{"lanes": "3", "snapshot_retention_days": "0", "since": ""} {
		if got := progressConfigField(t, s, field); got != want {
			t.Errorf("%s after rejected PUT = %q, want %q", field, got, want)
		}
	}
	after, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(before, after) {
		t.Errorf("config file changed by a rejected PUT")
	}
}

func TestProgressConfigCommConfigure(t *testing.T) {
	for _, row := range progressConfigRows {
		key := "autonomous.progress." + row.field
		t.Run(row.field, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			r := router.NewRouter("h", "", nil, nil, 0, nil)
			r.SetConfigureFunc(func(k, v string) error {
				// Same typing rule as the daemon's configure hook: a complete
				// JSON number goes out as a number, anything else as a string.
				var val any = v
				var n json.Number
				if json.Unmarshal([]byte(v), &n) == nil {
					val = n
				}
				b, _ := json.Marshal(map[string]any{k: val})
				rr := httptest.NewRecorder()
				s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
				if rr.Code != http.StatusOK {
					return &configureErr{rr.Body.String()}
				}
				return nil
			})
			resp := r.HandleTestMessage("configure " + key + "=" + row.want)
			if len(resp) == 0 || !strings.Contains(resp[0], "Set "+key) {
				t.Fatalf("configure response = %v", resp)
			}
			if got := progressConfigField(t, s, row.field); got != row.want {
				t.Fatalf("GET after configure = %q, want %q", got, row.want)
			}
			bad := "-9"
			if row.field == "backlog_path" {
				return // any non-empty path is valid in chat
			}
			if row.field == "since" {
				bad = "someday"
			}
			resp = r.HandleTestMessage("configure " + key + "=" + bad)
			if len(resp) == 0 || !strings.Contains(resp[0], "Configure failed") {
				t.Fatalf("bad configure response = %v", resp)
			}
			if got := progressConfigField(t, s, row.field); got != row.want {
				t.Fatalf("value after rejected configure = %q, want %q", got, row.want)
			}
		})
	}
}

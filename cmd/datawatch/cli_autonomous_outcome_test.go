// Flag→request mapping tests for `autonomous prd-set-outcome`,
// `autonomous outcome-check`, `autonomous deploy-record` and the outcome flags
// of `autonomous prd-cancel`. They reuse the captureDaemon helpers.
package main

import (
	"net/http"
	"reflect"
	"testing"
)

func TestOutcomeCLIRegistered(t *testing.T) {
	auto := newAutonomousCmd()
	for _, name := range []string{"prd-set-outcome", "outcome-check", "deploy-record"} {
		found := false
		for _, sub := range auto.Commands() {
			if sub.Name() == name {
				found = true
			}
		}
		if !found {
			t.Errorf("autonomous %s is not registered", name)
		}
	}
}

func TestOutcomeCLIRequests(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		method     string
		path       string
		query      string
		wantBody   map[string]any
		wantNoBody bool
	}{
		{"set-outcome", []string{"prd-set-outcome", "ab12cd34", "--outcome", "delivered", "--via", "ff00ff00", "--note", "twin shipped it"},
			http.MethodPost, "/api/autonomous/prds/ab12cd34/outcome", "",
			map[string]any{"outcome": "delivered", "delivered_via": "ff00ff00", "note": "twin shipped it"}, false},
		{"set-outcome-archive", []string{"prd-set-outcome", "ab12cd34", "--outcome", "dropped", "--archive"},
			http.MethodPost, "/api/autonomous/prds/ab12cd34/archive", "",
			map[string]any{"outcome": "dropped"}, false},
		{"check-read", []string{"outcome-check"}, http.MethodGet, "/api/autonomous/outcome-check", "", nil, true},
		{"check-run", []string{"outcome-check", "--run"}, http.MethodPost, "/api/autonomous/outcome-check", "", nil, true},
		{"deploy-record", []string{"deploy-record", "--release", "v9.1.0", "--host", "h1", "--health", "passed", "--detail", "ok"},
			http.MethodPost, "/api/autonomous/deploy/record", "",
			map[string]any{"release": "v9.1.0", "host": "h1", "health": "passed", "detail": "ok"}, false},
		{"cancel-plain", []string{"prd-cancel", "ab12cd34"}, http.MethodDelete, "/api/autonomous/prds/ab12cd34", "", nil, true},
		{"cancel-outcome", []string{"prd-cancel", "ab12cd34", "--outcome", "superseded", "--via", "ff00ff00"},
			http.MethodDelete, "/api/autonomous/prds/ab12cd34", "delivered_via=ff00ff00&outcome=superseded", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{"mode":"shadow","findings":[],"counts":{}}`}
			withCaptureServer(t, c)
			if err := runSubCmd(t, newAutonomousCmd(), tc.args...); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if c.gotMethod != tc.method || c.gotPath != tc.path || c.gotQuery != tc.query {
				t.Fatalf("route = %s %s ?%s, want %s %s ?%s", c.gotMethod, c.gotPath, c.gotQuery, tc.method, tc.path, tc.query)
			}
			if tc.wantNoBody {
				if len(c.gotRaw) != 0 {
					t.Errorf("unexpected body %q", c.gotRaw)
				}
				return
			}
			if got := c.body(t); !reflect.DeepEqual(got, tc.wantBody) {
				t.Errorf("body = %v, want %v", got, tc.wantBody)
			}
		})
	}
}

func TestOutcomeCLIRefusesBadInput(t *testing.T) {
	for name, args := range map[string][]string{
		"no outcome":    {"prd-set-outcome", "ab12cd34"},
		"bad outcome":   {"prd-set-outcome", "ab12cd34", "--outcome", "gone"},
		"no release":    {"deploy-record"},
		"bad health":    {"deploy-record", "--release", "v1", "--health", "great"},
		"cancel via no": {"prd-cancel", "ab12cd34", "--outcome", "gone"},
	} {
		t.Run(name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{}`}
			withCaptureServer(t, c)
			if err := runSubCmd(t, newAutonomousCmd(), args...); err == nil {
				t.Fatal("must be refused before any request")
			}
			if c.gotMethod != "" {
				t.Fatalf("a request was sent: %s %s", c.gotMethod, c.gotPath)
			}
		})
	}
}

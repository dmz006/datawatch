package main

// `datawatch config set autonomous.automation.dependency_update.*`: each key
// stores through the daemon and reads back, and an invalid value is refused
// with the daemon's message (the allowed form) and a non-nil error. The fake
// daemon applies the same shared validator the real PUT /api/config handler
// runs.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const depUpdPrefix = "autonomous.automation.dependency_update."

type depUpdFakeDaemon struct {
	cur  config.DependencyUpdateConfig
	puts int
}

func newDepUpdFakeDaemon(t *testing.T) *depUpdFakeDaemon {
	t.Helper()
	d := &depUpdFakeDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		d.puts++
		var patch map[string]any
		if err := json.Unmarshal(raw, &patch); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		next := d.cur
		for k, v := range patch {
			switch strings.TrimPrefix(k, depUpdPrefix) {
			case "enabled":
				b, ok := v.(bool)
				if !ok {
					http.Error(w, depUpdPrefix+"enabled: must be true or false", http.StatusBadRequest)
					return
				}
				next.Enabled = &b
			case "schedule":
				s, ok := v.(string)
				if !ok {
					http.Error(w, depUpdPrefix+"schedule: must be a string 'weekday HH:MM'", http.StatusBadRequest)
					return
				}
				next.Schedule = s
			case "max_majors":
				f, ok := v.(float64)
				if !ok || f != float64(int(f)) {
					http.Error(w, depUpdPrefix+"max_majors: must be a whole number from 0 to 20", http.StatusBadRequest)
					return
				}
				n := int(f)
				next.MaxMajors = &n
			}
		}
		if err := next.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		d.cur = next
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d
}

func TestDependencyUpdateConfigSetRoundTrip(t *testing.T) {
	d := newDepUpdFakeDaemon(t)
	cfg := &config.Config{}
	if err := runConfigSet(cfg, depUpdPrefix+"enabled", "false"); err != nil {
		t.Fatalf("enabled set: %v", err)
	}
	if d.cur.EffectiveEnabled() {
		t.Errorf("enabled read back true, want false")
	}
	if err := runConfigSet(cfg, depUpdPrefix+"schedule", "friday 23:59"); err != nil {
		t.Fatalf("schedule set: %v", err)
	}
	if got := d.cur.EffectiveSchedule(); got != "friday 23:59" {
		t.Errorf("schedule read back %q, want friday 23:59", got)
	}
	if err := runConfigSet(cfg, depUpdPrefix+"max_majors", "0"); err != nil {
		t.Fatalf("max_majors set: %v", err)
	}
	if d.cur.MaxMajors == nil || d.cur.EffectiveMaxMajors() != 0 {
		t.Errorf("max_majors read back %v, want explicit 0", d.cur.MaxMajors)
	}
}

func TestDependencyUpdateConfigSetRefusesInvalid(t *testing.T) {
	cases := []struct{ name, key, value, want string }{
		{"bad schedule", "schedule", "someday 25:99", "weekday HH:MM"},
		{"max_majors above range", "max_majors", "25", "0 to 20"},
		{"max_majors negative", "max_majors", "-1", "0 to 20"},
		{"max_majors not a number", "max_majors", "many", "whole number"},
		{"enabled not a bool", "enabled", "maybe", "true or false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDepUpdFakeDaemon(t)
			err := runConfigSet(&config.Config{}, depUpdPrefix+tc.key, tc.value)
			if err == nil {
				t.Fatalf("runConfigSet(%s, %s) = nil, want refusal", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want HTTP 400 carrying %q", err, tc.want)
			}
			if d.cur.Enabled != nil || d.cur.Schedule != "" || d.cur.MaxMajors != nil {
				t.Errorf("refused write changed stored state: %+v", d.cur)
			}
		})
	}
}

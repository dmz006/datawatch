package server

// The autonomous.attempt_cap block on GET /api/config and PUT /api/config: the
// defaults, a round trip of every key through the live manager and the saved
// file, whole-PUT refusal of every bad value, the explicit empty list, and the
// actor a reset_task call is made as (only the operator's force reset may clear
// the attempt count).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
)

func attemptCapBlock(t *testing.T, s *Server) attemptCapSettings {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["attempt_cap"]
	if !ok {
		t.Fatal("GET /api/config autonomous lacks attempt_cap")
	}
	var got attemptCapSettings
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode attempt_cap: %v (%s)", err, raw)
	}
	return got
}

func TestAttemptCapConfigGetDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	want := attemptCapSettings{Enabled: true, MaxTotal: 6, FreeAttempts: 3, BackoffMinutes: []int{5, 15, 45}}
	if got := attemptCapBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
}

func TestAttemptCapConfigPutRoundTripEveryKey(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putInfraRetryRaw(s, map[string]any{
		"autonomous.attempt_cap.enabled":         false,
		"autonomous.attempt_cap.max_total":       9,
		"autonomous.attempt_cap.free_attempts":   2,
		"autonomous.attempt_cap.backoff_minutes": []any{1, 2, 3},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want := attemptCapSettings{Enabled: false, MaxTotal: 9, FreeAttempts: 2, BackoffMinutes: []int{1, 2, 3}}
	if got := attemptCapBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after PUT = %+v, want %+v", got, want)
	}
	// The live manager follows with no restart.
	if live := attemptCapConfigSurface(mgr.Config().AttemptCap); !reflect.DeepEqual(live, want) {
		t.Fatalf("live manager = %+v, want %+v", live, want)
	}
	// The saved file carries the same values.
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load the saved config: %v", err)
	}
	if got := attemptCapConfigSurface(saved.Autonomous.AttemptCap); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved file = %+v, want %+v", got, want)
	}

	// A comma list, and back to the other value of the flag.
	if rec = putInfraRetryRaw(s, map[string]any{
		"autonomous.attempt_cap.enabled":         true,
		"autonomous.attempt_cap.backoff_minutes": "10, 20",
	}); rec.Code != http.StatusOK {
		t.Fatalf("second PUT: %d %s", rec.Code, rec.Body.String())
	}
	got := attemptCapBlock(t, s)
	if !got.Enabled || got.MaxTotal != 9 || !reflect.DeepEqual(got.BackoffMinutes, []int{10, 20}) {
		t.Fatalf("after the second PUT = %+v (one key must leave the others)", got)
	}
}

func TestAttemptCapConfigExplicitEmptyListSurvives(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	for name, empty := range map[string]any{"empty array": []any{}, "empty text": ""} {
		t.Run(name, func(t *testing.T) {
			if rec := putInfraRetryRaw(s, map[string]any{"autonomous.attempt_cap.backoff_minutes": empty}); rec.Code != http.StatusOK {
				t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
			}
			if got := attemptCapBlock(t, s); got.BackoffMinutes == nil || len(got.BackoffMinutes) != 0 {
				t.Fatalf("an explicit empty list read back as %v, want []", got.BackoffMinutes)
			}
			if live := mgr.Config().AttemptCap; live.BackoffMinutes == nil || len(live.BackoffMinutes) != 0 {
				t.Fatalf("live manager list = %v, want an empty non-nil list", live.BackoffMinutes)
			}
			saved, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			if b := saved.Autonomous.AttemptCap.BackoffMinutes; b == nil || len(b) != 0 {
				t.Fatalf("saved list = %v, want an empty non-nil list", b)
			}
		})
	}
	// null clears it back to the default.
	if rec := putInfraRetryRaw(s, map[string]any{"autonomous.attempt_cap.backoff_minutes": nil}); rec.Code != http.StatusOK {
		t.Fatalf("PUT null: %d %s", rec.Code, rec.Body.String())
	}
	if got := attemptCapBlock(t, s); !reflect.DeepEqual(got.BackoffMinutes, []int{5, 15, 45}) {
		t.Fatalf("null left %v, want the default list", got.BackoffMinutes)
	}
}

func TestAttemptCapConfigInvalidValuesRefuseWholePut(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	bad := []struct {
		name  string
		patch map[string]any
		want  string
	}{
		{"zero cap", map[string]any{"autonomous.attempt_cap.max_total": 0}, "autonomous.attempt_cap.max_total"},
		{"fractional cap", map[string]any{"autonomous.attempt_cap.max_total": 2.5}, "autonomous.attempt_cap.max_total"},
		{"huge cap", map[string]any{"autonomous.attempt_cap.max_total": 100000}, "autonomous.attempt_cap.max_total"},
		{"zero free attempts", map[string]any{"autonomous.attempt_cap.free_attempts": 0}, "autonomous.attempt_cap.free_attempts"},
		{"negative delay", map[string]any{"autonomous.attempt_cap.backoff_minutes": []any{5, -1}}, "autonomous.attempt_cap.backoff_minutes[1]"},
		{"delay text", map[string]any{"autonomous.attempt_cap.backoff_minutes": "5,soon"}, "autonomous.attempt_cap.backoff_minutes[1]"},
		{"huge delay", map[string]any{"autonomous.attempt_cap.backoff_minutes": []any{99999999}}, "autonomous.attempt_cap.backoff_minutes[0]"},
		{"enabled text", map[string]any{"autonomous.attempt_cap.enabled": "maybe"}, "autonomous.attempt_cap.enabled"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			// A valid key beside the bad one must not be saved either.
			patch := map[string]any{"autonomous.attempt_cap.free_attempts": 5}
			for k, v := range tc.patch {
				patch[k] = v
			}
			rec := putInfraRetryRaw(s, patch)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("PUT: want 400 naming %s, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
			if got := attemptCapBlock(t, s); got.FreeAttempts != 3 {
				t.Fatalf("the valid key beside a bad one was saved: %+v", got)
			}
		})
	}
}

// resetActor: the credential decides who is calling, never the body.
func TestResetActor(t *testing.T) {
	store, err := auth.NewSessionTokenStore(filepath.Join(t.TempDir(), "session-tokens.json"))
	if err != nil {
		t.Fatalf("NewSessionTokenStore: %v", err)
	}
	tok, err := store.Mint("host-sess-7", []string{"autonomous:write"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	s := &Server{sessionTokens: store}
	req := func(ctx context.Context) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
	}

	// An admin-token caller is the operator unless its body names itself.
	if got := s.resetActor(req(context.Background()), ""); got != "operator" {
		t.Errorf("admin, no claim = %q, want operator", got)
	}
	if got := s.resetActor(req(context.Background()), "operator"); got != "operator" {
		t.Errorf("admin claiming operator = %q, want operator", got)
	}
	if got := s.resetActor(req(context.Background()), "doer"); got != "doer" {
		t.Errorf("admin naming itself doer = %q, want doer (not the operator)", got)
	}

	// A session's scoped credential names the session, whatever the body claims.
	ctx := context.WithValue(context.Background(), sessionCapsKey, []string{"autonomous:write"})
	ctx = context.WithValue(ctx, callerTokenKey, tok)
	for _, claim := range []string{"", "operator", "doer"} {
		if got := s.resetActor(req(ctx), claim); got != "session:host-sess-7" {
			t.Errorf("session credential claiming %q = %q, want session:host-sess-7", claim, got)
		}
	}
}

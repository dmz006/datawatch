package main

// `datawatch config set` for the review-reserve keys: a valid value is
// stored by the daemon, and a negative or non-integer one is refused with
// the daemon's message and a non-nil error (the root command exits 1 on any
// error). The fake daemon applies the same shared validator the real
// PUT /api/config handler runs, so the refusal text here is the text a user
// sees.

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// reserveFakeDaemon is a stateful stand-in for PUT /api/config that keeps
// the two reserve settings and refuses a write the shared validator rejects.
type reserveFakeDaemon struct {
	t        *testing.T
	slots    *int
	tokens   int64
	puts     int
	lastBody string
}

func newReserveFakeDaemon(t *testing.T) *reserveFakeDaemon {
	t.Helper()
	d := &reserveFakeDaemon{t: t, slots: intPtrOf(1), tokens: 0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		d.puts++
		d.lastBody = string(raw)
		var patch map[string]any
		if err := json.Unmarshal(raw, &patch); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		// The shared validator: whole non-negative numbers, in range.
		if v, ok := patch["autonomous.backoff_reserve_for_review"]; ok {
			f, isNum := v.(float64)
			if !isNum || f != math.Trunc(f) || f < 0 {
				http.Error(w, "autonomous.backoff_reserve_for_review: must be a whole number of slots >= 0 (0 = no slot reserve)", http.StatusBadRequest)
				return
			}
			if f > 10000 {
				http.Error(w, "autonomous.backoff_reserve_for_review: must be in 0..10000", http.StatusBadRequest)
				return
			}
			n := int(f)
			d.slots = &n
		}
		if v, ok := patch["autonomous.backoff_reserve_tokens_per_hour"]; ok {
			f, isNum := v.(float64)
			if !isNum || f != math.Trunc(f) || f < 0 {
				http.Error(w, "autonomous.backoff_reserve_tokens_per_hour: must be a whole number of tokens >= 0 (0 = no per-hour budget)", http.StatusBadRequest)
				return
			}
			if f > 1000000000 {
				http.Error(w, "autonomous.backoff_reserve_tokens_per_hour: must be in 0..1000000000", http.StatusBadRequest)
				return
			}
			d.tokens = int64(f)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d
}

func intPtrOf(n int) *int { return &n }

func TestBackoffReserveConfigSetAcceptsValidValues(t *testing.T) {
	d := newReserveFakeDaemon(t)
	cfg := &config.Config{}
	if err := runConfigSet(cfg, "autonomous.backoff_reserve_for_review", "3"); err != nil {
		t.Fatalf("slots set: %v", err)
	}
	if d.slots == nil || *d.slots != 3 {
		t.Errorf("stored slots = %v, want 3", d.slots)
	}
	if err := runConfigSet(cfg, "autonomous.backoff_reserve_tokens_per_hour", "250000"); err != nil {
		t.Fatalf("tokens set: %v", err)
	}
	if d.tokens != 250000 {
		t.Errorf("stored tokens = %d, want 250000", d.tokens)
	}

	// An explicit zero is valid (no slot reserve / no per-hour budget).
	if err := runConfigSet(cfg, "autonomous.backoff_reserve_for_review", "0"); err != nil {
		t.Fatalf("zero slots set: %v", err)
	}
	if d.slots == nil || *d.slots != 0 {
		t.Errorf("stored slots = %v, want explicit 0", d.slots)
	}
}

func TestBackoffReserveConfigSetRefusesInvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantMsg string
	}{
		{"slots negative", "autonomous.backoff_reserve_for_review", "-1", "must be a whole number of slots >= 0"},
		{"slots fractional", "autonomous.backoff_reserve_for_review", "1.5", "must be a whole number of slots >= 0"},
		{"slots not a number", "autonomous.backoff_reserve_for_review", "many", "must be a whole number of slots >= 0"},
		// Above range: the raw attempt is refused by the daemon, and the CLI's
		// quoted fallback carries the refusal a user sees, so it keeps the
		// whole-number text (the daemon treats a quoted number like any
		// non-number).
		{"slots above range", "autonomous.backoff_reserve_for_review", "10001", "must be a whole number of slots >= 0"},
		{"tokens negative", "autonomous.backoff_reserve_tokens_per_hour", "-5", "must be a whole number of tokens >= 0"},
		{"tokens fractional", "autonomous.backoff_reserve_tokens_per_hour", "2.5", "must be a whole number of tokens >= 0"},
		{"tokens not a number", "autonomous.backoff_reserve_tokens_per_hour", "soon", "must be a whole number of tokens >= 0"},
		{"tokens above range", "autonomous.backoff_reserve_tokens_per_hour", "2000000000", "must be a whole number of tokens >= 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newReserveFakeDaemon(t)
			before := d.tokens
			err := runConfigSet(&config.Config{}, tc.key, tc.value)
			if err == nil {
				t.Fatalf("runConfigSet(%s, %s) = nil, want refusal", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v, want HTTP 400 carrying %q", err, tc.wantMsg)
			}
			if d.tokens != before {
				t.Errorf("refused write changed stored state: tokens=%d", d.tokens)
			}
		})
	}
}

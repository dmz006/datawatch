// Review-reserve settings (autonomous.backoff_reserve_for_review /
// autonomous.backoff_reserve_tokens_per_hour): reachable and validated on
// both config surfaces, and the live per-hour reserve usage visible in the
// usage-backoff status.

package server

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	ubReserveSlotsKey  = "autonomous.backoff_reserve_for_review"
	ubReserveTokensKey = "autonomous.backoff_reserve_tokens_per_hour"
	ubReserveSlotsArg  = "backoff_reserve_for_review"
	ubReserveTokensArg = "backoff_reserve_tokens_per_hour"
)

func TestBackoffReserveConfig_Surfaces(t *testing.T) {
	s, mgr, cfgPath := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)

	// Unset config shows the effective defaults: 1 review slot, no
	// per-hour token budget.
	aut := getAutonomousConfigMap(t, s)
	if g := string(aut[ubReserveSlotsArg]); g != "1" {
		t.Fatalf("default %s = %s, want 1 (the default slot reserve)", ubReserveSlotsKey, g)
	}
	if g := string(aut[ubReserveTokensArg]); g != "0" {
		t.Fatalf("default %s = %s, want 0 (no per-hour budget)", ubReserveTokensKey, g)
	}

	// PUT /api/config in the comm `configure` shape: bare numbers.
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{ubReserveSlotsKey: 3, ubReserveTokensKey: 100000})
	aut = getAutonomousConfigMap(t, s)
	if g := string(aut[ubReserveSlotsArg]); g != "3" {
		t.Fatalf("after PUT: %s = %s, want 3", ubReserveSlotsKey, g)
	}
	if g := string(aut[ubReserveTokensArg]); g != "100000" {
		t.Fatalf("after PUT: %s = %s, want 100000", ubReserveTokensKey, g)
	}
	m := mgr.Config()
	if m.BackoffReserveForReview == nil || *m.BackoffReserveForReview != 3 || m.BackoffReserveTokensPerHour != 100000 {
		t.Fatalf("live manager after PUT = %v / %d, want 3 / 100000", m.BackoffReserveForReview, m.BackoffReserveTokensPerHour)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "backoff_reserve_for_review: 3") {
		t.Fatalf("config.yaml lacks backoff_reserve_for_review:\n%s", raw)
	}
	if !strings.Contains(string(raw), "backoff_reserve_tokens_per_hour: 100000") {
		t.Fatalf("config.yaml lacks backoff_reserve_tokens_per_hour:\n%s", raw)
	}

	// Bare-key form on /api/autonomous/config — the autonomous_config_set shape.
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{ubReserveSlotsArg: 2, ubReserveTokensArg: 500000})
	if m := mgr.Config(); m.BackoffReserveForReview == nil || *m.BackoffReserveForReview != 2 || m.BackoffReserveTokensPerHour != 500000 {
		t.Fatalf("live manager after autonomous-config PUT = %v / %d", m.BackoffReserveForReview, m.BackoffReserveTokensPerHour)
	}

	// An explicit zero means "no slot reserve" and must survive the
	// unset-defaults pass — only null is "absent".
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{ubReserveSlotsArg: 0})
	if m := mgr.Config(); m.BackoffReserveForReview == nil || *m.BackoffReserveForReview != 0 {
		t.Fatalf("explicit zero slot reserve collapsed to unset: %v", m.BackoffReserveForReview)
	}
	if m := mgr.Config(); m.BackoffReserveTokensPerHour != 500000 {
		t.Fatalf("slot-reserve PUT changed the token budget: %d", m.BackoffReserveTokensPerHour)
	}
	aut = getAutonomousConfigMap(t, s)
	if g := string(aut[ubReserveSlotsArg]); g != "0" {
		t.Fatalf("after explicit zero: %s = %s, want 0", ubReserveSlotsKey, g)
	}

	// null on the slot key is absence again (the unset mirror must not
	// overwrite the stored value), and the scalar token budget keeps the
	// stored value — like usage_backoff / backoff_max_wait_minutes.
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{ubReserveSlotsArg: nil, ubReserveTokensArg: nil})
	if m := mgr.Config(); m.BackoffReserveForReview != nil || m.BackoffReserveTokensPerHour != 500000 {
		t.Fatalf("null changed the reserve config: %v / %d, want nil / 500000", m.BackoffReserveForReview, m.BackoffReserveTokensPerHour)
	}

	// Negative, fractional, non-numeric and out-of-range values are
	// rejected on BOTH surfaces and change nothing.
	for _, v := range []any{-1, 1.5, "three", true, 10001} {
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{ubReserveSlotsKey: v})
		pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{ubReserveSlotsArg: v})
	}
	for _, v := range []any{-5, 2.5, "soon", true, 2000000000} {
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{ubReserveTokensKey: v})
		pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{ubReserveTokensArg: v})
	}
	if m := mgr.Config(); m.BackoffReserveForReview != nil || m.BackoffReserveTokensPerHour != 500000 {
		t.Fatalf("rejected PUTs changed the reserve config: %v / %d, want nil / 500000", m.BackoffReserveForReview, m.BackoffReserveTokensPerHour)
	}
}

func TestBackoffReserveStatus_ShowsLiveReserveUsage(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s := usageBackoffServer(t)
	gate := autonomous.NewBackoffReserve(func() time.Time { return now },
		func() int { return 1 }, func() int64 { return 1000000 })
	s.SetBackoffReserve(gate)
	gate.RecordTokens("claude-code", 12345)

	rr := ubDo(s, http.MethodPost, `{"backend":"claude-code","minutes":45,"reason":"weekly cap"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
	}
	st := ubStatus(t, rr)

	// The config half shows the effective reserve settings even though the
	// gate reads them through the daemon's wiring closures.
	if st.Config.ReserveForReview != 1 || st.Config.ReserveTokensPerHour != 0 {
		t.Errorf("config reserve = %d / %d, want 1 / 0", st.Config.ReserveForReview, st.Config.ReserveTokensPerHour)
	}

	// The reserve array names each live flag and its rolling one-hour
	// token usage.
	if len(st.Reserve) != 1 || st.Reserve[0].Backend != "claude-code" || st.Reserve[0].TokensUsed != 12345 {
		t.Fatalf("reserve = %+v, want one claude-code entry with 12345 tokens", st.Reserve)
	}
	if !strings.Contains(rr.Body.String(), `"reserve":`) {
		t.Errorf("status body lacks the reserve array: %s", rr.Body.String())
	}
}

func TestBackoffReserveStatus_OmitsReserveWithoutGate(t *testing.T) {
	s := usageBackoffServer(t)
	if rr := ubDo(s, http.MethodPost, `{"backend":"pi","minutes":10}`); rr.Code != http.StatusOK {
		t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
	}
	rr := ubDo(s, http.MethodGet, "")
	st := ubStatus(t, rr)
	if st.Reserve != nil {
		t.Fatalf("reserve = %+v, want omitted when the gate is not wired", st.Reserve)
	}
	if strings.Contains(rr.Body.String(), `"reserve"`) {
		t.Errorf("status body carries reserve although the gate is not wired: %s", rr.Body.String())
	}
}

func TestBackoffReserveStatus_OmitsReserveWithNoLiveFlags(t *testing.T) {
	s := usageBackoffServer(t)
	s.SetBackoffReserve(autonomous.NewBackoffReserve(nil, func() int { return 1 }, nil))
	rr := ubDo(s, http.MethodGet, "")
	st := ubStatus(t, rr)
	if st.Reserve != nil {
		t.Fatalf("reserve = %+v, want omitted with no live flags", st.Reserve)
	}
}

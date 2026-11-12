package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// lanePulseBody decodes the endpoint body into the fields these tests read.
type lanePulseBody struct {
	WindowSeconds int64 `json:"window_seconds"`
	Series        []struct {
		Lane           string  `json:"lane"`
		Node           string  `json:"node"`
		BusySeconds    float64 `json:"busy_seconds"`
		WaitingSeconds float64 `json:"waiting_seconds"`
		IdleSeconds    float64 `json:"idle_seconds"`
		Segments       []struct {
			State string    `json:"state"`
			Start time.Time `json:"start"`
			End   time.Time `json:"end"`
		} `json:"segments"`
	} `json:"series"`
	Burn struct {
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	} `json:"burn"`
}

func getLanePulse(t *testing.T, s *Server, query string) (*httptest.ResponseRecorder, lanePulseBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleDashboardLanePulse(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/lane-pulse"+query, nil))
	var body lanePulseBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v body %s", err, rec.Body.String())
		}
	}
	return rec, body
}

func TestLanePulse_MergeAndSubtractSpans(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	merged := mergeSpans([]span{{at(5), at(10)}, {at(0), at(3)}, {at(2), at(6)}, {at(8), at(8)}})
	if len(merged) != 1 || !merged[0].a.Equal(at(0)) || !merged[0].b.Equal(at(10)) {
		t.Fatalf("merge: got %v, want one span 0-10", merged)
	}
	cut := []span{{at(2), at(4)}, {at(6), at(7)}}
	left := subtractSpans([]span{{at(0), at(10)}}, cut)
	if len(left) != 3 || !left[0].b.Equal(at(2)) || !left[1].a.Equal(at(4)) || !left[1].b.Equal(at(6)) || !left[2].a.Equal(at(7)) {
		t.Fatalf("subtract: got %v", left)
	}
}

func TestLanePulse_SegmentsPartitionWindow(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	b := &lanePulseSeriesBuilder{lane: "claude-code", node: "n1",
		busy: []span{{at(10), at(20)}},
		wait: []span{{at(15), at(30)}}, // a request queues while the lane is busy: the two overlap
	}
	got := buildLanePulseSeries(b, at(0), at(60), at(0))
	if got.BusySeconds != 600 || got.WaitingSeconds != 900 || got.IdleSeconds != 2400 {
		t.Fatalf("totals busy=%v waiting=%v idle=%v", got.BusySeconds, got.WaitingSeconds, got.IdleSeconds)
	}
	states := []string{}
	for _, seg := range got.Segments {
		states = append(states, seg.State)
	}
	if strings.Join(states, ",") != "idle,busy,waiting,idle" {
		t.Fatalf("segment order: %v", states)
	}
}

func TestLanePulse_IdleStartsAtRecording(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	b := &lanePulseSeriesBuilder{lane: "x"}
	got := buildLanePulseSeries(b, t0, t0.Add(time.Hour), t0.Add(30*time.Minute))
	if got.IdleSeconds != 1800 {
		t.Fatalf("idle before recording began must not count: got %v", got.IdleSeconds)
	}
}

func TestLanePulse_RejectsBadWindowAndMethod(t *testing.T) {
	s := &Server{}
	for _, q := range []string{"?window=abc", "?window=30s", "?window=169h"} {
		if rec, _ := getLanePulse(t, s, q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	s.handleDashboardLanePulse(rec, httptest.NewRequest(http.MethodPost, "/api/dashboard/lane-pulse", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d, want 405", rec.Code)
	}
}

func TestLanePulse_NoLedgerIsEmptyNotError(t *testing.T) {
	s := &Server{}
	rec, body := getLanePulse(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if len(body.Series) != 0 || body.WindowSeconds != int64((6*time.Hour)/time.Second) {
		t.Fatalf("empty body: series=%d window=%d", len(body.Series), body.WindowSeconds)
	}
	if body.Burn.Available || body.Burn.Reason != lanePulseReasonNotWired {
		t.Fatalf("burn should carry the not-wired reason code: %+v", body.Burn)
	}
}

// TestLanePulse_BurnReasonIsCodeNotProse pins the burn reason to a stable code,
// so no English text reaches the clients.
func TestLanePulse_BurnReasonIsCodeNotProse(t *testing.T) {
	for _, code := range []string{lanePulseReasonNotWired, lanePulseReasonAdviceFailed, lanePulseReasonNeedsPrefix + "reading"} {
		if strings.ContainsAny(code, " :.,") || strings.ToLower(code) != code {
			t.Fatalf("reason %q is not a bare lowercase code", code)
		}
	}
}

func TestLanePulse_BusyFromReleasedLease(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	s.SetCapacity(l)
	if err := l.Acquire(context.Background(), capacity.Request{Holder: "task-a", PRDID: "prd-a", Pools: []string{"llm:claude-code"}, Node: "n1"}, 0, nil, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	l.Release("task-a")

	rec, body := getLanePulse(t, s, "?window=1h")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(body.Series) != 1 {
		t.Fatalf("series: %d, want 1", len(body.Series))
	}
	got := body.Series[0]
	if got.Lane != "claude-code" || got.Node != "n1" {
		t.Fatalf("series key %q/%q", got.Lane, got.Node)
	}
	if got.BusySeconds < 0.05 || got.BusySeconds > 5 {
		t.Fatalf("busy_seconds %v, want about 0.06", got.BusySeconds)
	}
	if len(got.Segments) == 0 || got.Segments[len(got.Segments)-1].State != "idle" && got.Segments[0].State != "busy" {
		t.Fatalf("segments: %+v", got.Segments)
	}
	if body.Burn.Available {
		t.Fatal("no usage calculator wired: burn must not claim availability")
	}
}

func TestLanePulse_OpenLeaseRunsToNow(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	s.SetCapacity(l)
	if err := l.Acquire(context.Background(), capacity.Request{Holder: "task-open", PRDID: "prd-o", Pools: []string{"llm:pi-prompt"}}, 0, nil, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	_, body := getLanePulse(t, s, "?window=1h")
	if len(body.Series) != 1 || body.Series[0].BusySeconds < 0.03 {
		t.Fatalf("open lease should count busy to now: %+v", body.Series)
	}
	l.Release("task-open")
}

func TestLanePulse_WithdrawnWaitIsWaitingTime(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	s.SetCapacity(l)
	l.SetLimit("llm:claude-code", 1)
	if err := l.Acquire(context.Background(), capacity.Request{Holder: "task-hold", PRDID: "prd-h", Pools: []string{"llm:claude-code"}}, 0, nil, nil); err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	// The second request queues behind the held slot and gives up after 60 ms.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(ctx, capacity.Request{Holder: "task-wait", PRDID: "prd-w", Pools: []string{"llm:claude-code"}}, 0, nil, nil)
	}()
	deadline := time.Now().Add(time.Second)
	for len(l.Snapshot().Waiting) == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if len(l.Snapshot().Waiting) != 1 {
		t.Fatal("second request never queued behind the held slot")
	}
	if err := <-done; err == nil {
		t.Fatal("queued request should have been withdrawn, not granted")
	}
	l.Release("task-hold")

	_, body := getLanePulse(t, s, "?window=1h")
	var waiting float64
	for _, sr := range body.Series {
		if sr.Lane == "claude-code" {
			waiting += sr.WaitingSeconds
		}
	}
	if waiting < 0.05 {
		t.Fatalf("waiting_seconds %v, want about 0.06 from the withdrawn request", waiting)
	}
}

func TestLanePulse_NoDollarValues(t *testing.T) {
	s := &Server{}
	rec, _ := getLanePulse(t, s, "")
	for _, word := range []string{"usd", "dollar", "spend", "cost"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), word) {
			t.Fatalf("response carries %q; the burn panel is in points only", word)
		}
	}
}

// The lane pulse card in the PWA reads these fields by name. A rename or a
// dropped key would blank the card silently, so the wire shape is pinned here:
// window bounds, the series array, and the burn block with its points, pace
// line, pace flag, readings and reason code.
func TestLanePulse_ResponseCarriesCardFields(t *testing.T) {
	s := &Server{}
	rec, _ := getLanePulse(t, s, "?window=6h")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"window_from", "window_to", "window_seconds", "series", "burn"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("response lacks %q", k)
		}
	}
	if _, ok := raw["series"].([]any); !ok {
		t.Fatalf("series is not an array: %T", raw["series"])
	}
	burn, ok := raw["burn"].(map[string]any)
	if !ok {
		t.Fatalf("burn is not an object: %T", raw["burn"])
	}
	for _, k := range []string{"available", "points", "pace_line", "ahead_of_pace", "burn_per_24h", "readings"} {
		if _, ok := burn[k]; !ok {
			t.Fatalf("burn lacks %q", k)
		}
	}
	// readings is null when no usage advice could be read; the card guards
	// with Array.isArray, so null and an array are both valid here.
	if _, isArr := burn["readings"].([]any); !isArr && burn["readings"] != nil {
		t.Fatalf("burn.readings is neither null nor an array: %T", burn["readings"])
	}
	if burn["reason"] == nil || burn["reason"] == "" {
		t.Fatalf("unavailable burn carries no reason code: %v", burn)
	}
}

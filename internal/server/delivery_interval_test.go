package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
)

// The outside timer reads a heartbeat with an interval over a day as
// unreadable, so the setting cannot go there.
func TestDeliveryIntervalIsCappedAtADay(t *testing.T) {
	for _, v := range []any{float64(86401), float64(90000), "90000"} {
		if err := firstDeliverySettingError(map[string]any{"delivery.interval_seconds": v}); err == nil {
			t.Errorf("%v accepted", v)
		}
		var cfg config.Config
		applyDeliveryConfigKey(&cfg, "delivery.interval_seconds", v)
		if cfg.Delivery.IntervalSeconds != 0 {
			t.Errorf("%v stored", v)
		}
	}
	for _, v := range []any{float64(0), float64(60), float64(86400)} {
		if err := firstDeliverySettingError(map[string]any{"delivery.interval_seconds": v}); err != nil {
			t.Errorf("%v refused: %v", v, err)
		}
	}
}

type countingSender struct{ n int }

func (c *countingSender) Channels() []string                         { return []string{"sms"} }
func (c *countingSender) Send(context.Context, string, string) error { c.n++; return nil }

// 40 calls on the route send one message.
func TestDeliveryFortyTestMessageCallsSendOne(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	sender := &countingSender{}
	e, err := delivery.New(delivery.Options{Dir: t.TempDir(), Sender: sender, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	mux := deliveryMux(&Server{delivery: e})
	codes := map[int]int{}
	for i := 0; i < 40; i++ {
		codes[deliveryCall(mux, http.MethodPost, "/api/delivery/test-message", "", "", nil).Code]++
	}
	if sender.n != 1 || codes[http.StatusOK] != 1 || codes[http.StatusTooManyRequests] != 39 {
		t.Fatalf("sent=%d codes=%v", sender.n, codes)
	}
}

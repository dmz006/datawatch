package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/delivery"
)

// The CLI, the assistant tools, the chat commands and the web page all act
// through these routes, so a real check behind them shows that no way of
// naming a group can accept or drop an alarm.
func TestDeliveryRoutesNeverAcceptOrDropAnAlarm(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	var found []delivery.Candidate
	e, err := delivery.New(delivery.Options{Dir: t.TempDir(), Host: "h", Now: func() time.Time { return now },
		Sources: []delivery.Source{{Name: "planted", Collect: func(context.Context, time.Time) ([]delivery.Candidate, error) {
			return found, nil
		}}}})
	if err != nil {
		t.Fatal(err)
	}
	pass := func() delivery.Report {
		now = now.Add(5 * time.Minute)
		r, err := e.Pass(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	pass()
	for i := 0; i < 60; i++ {
		found = append(found, delivery.Candidate{Kind: delivery.KindSchedule, ID: "s" + strconv.Itoa(i), State: "failed",
			Problem: "failed", Sticky: true, Since: now})
	}
	var alarm delivery.Entry
	for _, en := range pass().Items {
		if en.Kind == delivery.KindAlarm {
			alarm = en
		}
	}
	if alarm.Key == "" {
		t.Fatal("no alarm to try")
	}
	s := &Server{delivery: e}
	mux := deliveryMux(s)
	until := now.Add(48 * time.Hour).Format(time.RFC3339)
	for _, sel := range []string{`"kind":"alarm"`, `"keys":["` + alarm.Key + `"]`, `"state":"` + alarm.State + `"`,
		`"match_reason":"` + alarm.Reason + `"`, `"keys":["` + alarm.Key + `","schedule:s1:failed"]`} {
		for route, body := range map[string]string{
			"accept": `{` + sel + `,"reason":"parked","until":"` + until + `"}`,
			"drop":   `{` + sel + `,"reason":"parked"}`,
		} {
			rr := deliveryCall(mux, http.MethodPost, "/api/delivery/"+route, "", body, nil)
			if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "acknowledge") {
				t.Errorf("%s %s: %d %s", route, sel, rr.Code, rr.Body.String())
			}
		}
	}
	if r := e.Report(); len(r.Accepted) != 0 {
		t.Fatalf("something was accepted: %v", r.Accepted)
	}
	// Acknowledging it still works.
	if rr := deliveryCall(mux, http.MethodPost, "/api/delivery/ack", "", `{"keys":["`+alarm.Key+`"]}`, nil); rr.Code != http.StatusOK {
		t.Fatalf("ack: %d %s", rr.Code, rr.Body.String())
	}
}

func TestDeliveryTestMessageRefusals(t *testing.T) {
	for err, code := range map[error]int{delivery.ErrNotifyOff: http.StatusConflict, delivery.ErrTestTooSoon: http.StatusTooManyRequests} {
		f := &fakeDeliveryService{testErr: err}
		rr := deliveryCall(deliveryMux(&Server{delivery: f}), http.MethodPost, "/api/delivery/test-message", "", "", nil)
		if rr.Code != code || !strings.Contains(rr.Body.String(), err.Error()) {
			t.Errorf("%v: %d %s", err, rr.Code, rr.Body.String())
		}
	}
}

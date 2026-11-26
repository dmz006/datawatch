package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestScheduleAlertText_EndedUndeliveredAndSendFailed(t *testing.T) {
	sc := session.ScheduledCommand{ID: "ab12", ScheduleName: "watch", SessionID: "h-aa01", State: session.SchedFailed, Command: "SECRET-TEXT"}
	_, title, body := scheduleAlertText(session.ScheduleAlert{Schedule: sc, Settled: "its session ended (complete)"})
	if !strings.Contains(title, "did not run: watch") || !strings.Contains(body, "ab12") || !strings.Contains(body, "its session ended") {
		t.Errorf("%s / %s", title, body)
	}
	_, title2, body2 := scheduleAlertText(session.ScheduleAlert{Schedule: sc, Settled: "a: x\nb: y", Count: 12})
	if !strings.Contains(title2, "12 scheduled commands") || !strings.Contains(body2, "b: y") {
		t.Errorf("%s / %s", title2, body2)
	}
	sc.State, sc.ConsecutiveFailures, sc.RunAt = session.SchedPending, 1, time.Now()
	_, title3, body3 := scheduleAlertText(session.ScheduleAlert{Schedule: sc, SendFailed: true, Err: errors.New("exit status 1")})
	if !strings.Contains(title3, "send failed") || !strings.Contains(body3, "stays scheduled") {
		t.Errorf("%s / %s", title3, body3)
	}
	for _, text := range []string{title, body, title2, body2, title3, body3} {
		if strings.Contains(text, "SECRET-TEXT") {
			t.Errorf("command text in an alert: %s", text)
		}
	}
}

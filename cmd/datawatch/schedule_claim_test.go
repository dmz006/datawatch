package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// The session manager's 30 s pass lists the due commands, then types them with
// FireScheduled. The daemon's 10 s pass running between the two must leave one
// delivery for the fire.
func TestDaemonPassAndManagerPassTypeOneDueFireOnce(t *testing.T) {
	for label, cron := range map[string]string{"one-time": "", "cron": "*/5 * * * *"} {
		t.Run(label, func(t *testing.T) {
			e := newSchedTargetEnv(t)
			holder := e.session("aa01", "worker", session.StateRunning)
			if _, err := e.store.AddFull(session.AddOptions{
				SessionID: holder.FullID, Command: "run it", CronExpr: cron, RunAt: time.Now().Add(-time.Minute),
			}); err != nil {
				t.Fatal(err)
			}
			listed := e.store.DuePending(time.Now())
			fireDueCommands(e.store, e.mgr, time.Now())
			for _, sc := range listed {
				_, _ = e.mgr.FireScheduled(e.store, holder.FullID, sc)
			}
			if got := e.typed(holder); !slices.Equal(got, []string{"run it"}) {
				t.Fatalf("typed = %q, want it once", got)
			}
		})
	}
}

func TestScheduleAlertText_CutOffByARestart(t *testing.T) {
	for state, want := range map[string]string{
		session.SchedFailed:  "It is now failed",
		session.SchedPending: "stays scheduled for its next fire",
		session.SchedDone:    "no further fire",
	} {
		_, title, body := scheduleAlertText(session.ScheduleAlert{
			Schedule: session.ScheduledCommand{ID: "ab12cd34", State: state}, Interrupted: true,
		})
		if !strings.Contains(title, "cut off by a restart") || !strings.Contains(body, "may or may not have arrived") || !strings.Contains(body, want) {
			t.Errorf("%s: %s / %s", state, title, body)
		}
	}
}

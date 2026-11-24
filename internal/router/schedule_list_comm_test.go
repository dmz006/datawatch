package router

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// "schedule list" answers on the comm channel with every pending and failed
// schedule, each with its next fire, last result and re-arm state.
func TestScheduleListComm_ShowsNextLastAndRearmState(t *testing.T) {
	store, err := session.NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter("h", "g", nil, nil, 0, nil)
	r.SetScheduleStore(store)

	if got := r.HandleTestMessage("schedule list"); len(got) != 1 || !strings.Contains(got[0], "No pending scheduled items") {
		t.Fatalf("empty list reply %q", got)
	}

	waiting, _ := store.AddSpawn(session.AddSpawnOptions{Task: "scan", SessionName: "detector-pass", ScheduleName: "detector", CronExpr: "*/5 * * * *"})
	_, err = store.RecordStartFailure(waiting.ID, fmt.Errorf("%w (20) reached", session.ErrMaxSessions), time.Now(), session.DefaultRearmPolicy())
	if err != nil {
		t.Fatal(err)
	}
	dead, _ := store.AddSpawn(session.AddSpawnOptions{Task: "report", SessionName: "nightly-pass", ScheduleName: "nightly", CronExpr: "0 3 * * *"})
	_, _ = store.RecordStartFailure(dead.ID, errors.New("tmux exploded"), time.Now(), session.RearmPolicy{Mode: session.RearmStayFailed})

	got := r.HandleTestMessage("schedule list")
	if len(got) != 1 {
		t.Fatalf("one reply expected, got %q", got)
	}
	for _, want := range []string{waiting.ID, "SPAWN: detector-pass", "last deferred", "max sessions (20) reached", "waiting_capacity x1",
		dead.ID, "no next fire (failed)", "gave_up x1"} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("list reply missing %q:\n%s", want, got[0])
		}
	}
}

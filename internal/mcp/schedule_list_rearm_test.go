package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/session"
)

// schedule_list shows each schedule's next fire, last result and re-arm state,
// and still lists a schedule the re-arm policy gave up on.
func TestScheduleList_ShowsRearmState(t *testing.T) {
	store, err := session.NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{schedStore: store}
	waiting, _ := store.AddSpawn(session.AddSpawnOptions{Task: "scan", ScheduleName: "detector", CronExpr: "*/5 * * * *"})
	_, _ = store.RecordStartFailure(waiting.ID, fmt.Errorf("%w (20) reached", session.ErrMaxSessions), time.Now(), session.DefaultRearmPolicy())
	dead, _ := store.AddSpawn(session.AddSpawnOptions{Task: "report", ScheduleName: "nightly", CronExpr: "0 3 * * *"})
	_, _ = store.RecordStartFailure(dead.ID, fmt.Errorf("boom"), time.Now(), session.RearmPolicy{Mode: session.RearmStayFailed})

	res, err := s.handleScheduleList(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("schedule_list: %v %+v", err, res)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	for _, want := range []string{waiting.ID, "last deferred", "max sessions (20) reached", "waiting_capacity x1", dead.ID, "no next fire (failed)", "gave_up x1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("schedule_list missing %q:\n%s", want, text)
		}
	}
}

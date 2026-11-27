// "supervisor-loop wake" shows the pass count beside the count of typed event
// wakes.

package main

import (
	"strings"
	"testing"
)

func TestSupervisorWakeStatusTextShowsPassCount(t *testing.T) {
	body := strings.Replace(supervisorWakeBodyJSON, `"event_count":0`, `"event_count":2,"pass_count":9`, 1)
	supervisorLoopDaemon(t, 200, body)
	out, err := runSupervisorLoopCmd(t, "wake")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(2 events)", "event wake passes run: 9"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

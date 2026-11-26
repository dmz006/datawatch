package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// The daemon's supervisor loop retries a held task through the API, which
// starts the run; the manager alone only requeues it.
func TestSupervisorLoopIsWiredToRunItsRetry(t *testing.T) {
	src, err := os.ReadFile("supervisor_loop_wire.go")
	if err != nil {
		t.Fatal(err)
	}
	wire := strings.Index(string(src), "hooks.ResolveTaskReview = supervisorLoopTaskRetry(aAPI)")
	steps := strings.Index(string(src), "NewSupervisorLoopSteps(coord, engine, hooks)")
	if wire < 0 || steps < 0 || wire > steps {
		t.Errorf("hook set at %d, steps built at %d", wire, steps)
	}
	if supervisorLoopTaskRetry(nil) != nil {
		t.Error("a hook without an API")
	}
	m, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	retry := supervisorLoopTaskRetry(autonomouspkg.NewAPI(m))
	err = retry("0000", "t1", autonomouspkg.ReviewResolution{Action: "retry"}, "supervisor-loop")
	if err == nil || errors.Is(err, autonomouspkg.ErrTaskNotInReview) || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown Automaton: err = %v", err)
	}
}

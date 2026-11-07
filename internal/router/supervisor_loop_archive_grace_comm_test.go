package router

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// TestSupervisorLoopArchiveGraceComm: `configure <key>=<value>` hands the
// archive-grace key to the configure function untouched, in both key
// spellings, and reports success or the daemon's refusal. The configure
// function here applies the value with the supervisorloop setter, the same one
// PUT /api/config uses, so a negative value is refused as it is on REST.
func TestSupervisorLoopArchiveGraceComm(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	set := supervisorloop.DefaultSettings()
	var gotKey, gotValue string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotValue = key, value
		full := strings.TrimPrefix(key, "autonomous.")
		if !supervisorloop.IsKey(full) {
			return errors.New("unknown key " + key)
		}
		v, err := strconv.Atoi(value)
		if err != nil {
			return errors.New(full + ": not a number")
		}
		return set.Set(full, v)
	})

	run := func(text string) string {
		t.Helper()
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		return sent[len(sent)-1]
	}

	for _, key := range []string{
		"supervisor_loop.archive_cancelled_after_minutes",
		"autonomous.supervisor_loop.archive_cancelled_after_minutes",
	} {
		reply := run(key + "=15")
		if gotKey != key || gotValue != "15" {
			t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
		}
		if !strings.Contains(reply, "Set "+key+" = 15") {
			t.Errorf("reply %q lacks the Set confirmation", reply)
		}
		if set.ArchiveCancelledAfterMinutes != 15 {
			t.Errorf("%s: setter holds %d, want 15", key, set.ArchiveCancelledAfterMinutes)
		}
	}

	reply := run("supervisor_loop.archive_cancelled_after_minutes=0")
	if !strings.Contains(reply, "Set ") || set.ArchiveCancelledAfterMinutes != 0 {
		t.Errorf("0 must be accepted: reply %q, setter %d", reply, set.ArchiveCancelledAfterMinutes)
	}

	set.ArchiveCancelledAfterMinutes = 15
	reply = run("supervisor_loop.archive_cancelled_after_minutes=-5")
	if !strings.Contains(reply, "Configure failed") || !strings.Contains(reply, "archive_cancelled_after_minutes") {
		t.Errorf("negative: reply %q does not report the refusal", reply)
	}
	if strings.Contains(reply, "Set ") {
		t.Errorf("negative: reply %q claims success", reply)
	}
	if set.ArchiveCancelledAfterMinutes != 15 {
		t.Errorf("refused value changed the setter to %d", set.ArchiveCancelledAfterMinutes)
	}
}

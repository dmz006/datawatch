package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

const housekeepingRepairsCommSample = `{"grace_seconds":120,"groups":[
 {"group":"task_chain","mode":"report","would":[{"subject":"t1","kind":"would","found":"chain stalled","done":"would repair (report mode; nothing changed)","undo":"reset it"}],"did":[],"failed":[],"skipped_unsafe":[]},
 {"group":"leftovers","mode":"on","would":[],"did":[{"subject":"w1","kind":"did","found":"leftover","done":"cleaned","undo":"restore from backup"}],"failed":[],"skipped_unsafe":[]}
]}`

func TestHousekeepingRepairsComm_VerbOutput(t *testing.T) {
	if !slices.Contains(HousekeepingRepairsCommVerbs(), "housekeeping-repairs") {
		t.Fatalf("verbs = %v", HousekeepingRepairsCommVerbs())
	}
	for _, verb := range HousekeepingRepairsCommVerbs() {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf("Parse(%s).Type = %v", verb, c.Type)
		}
	}
	log, port := laneFillCommDaemon(t, http.StatusOK, housekeepingRepairsCommSample)
	r := setupPeerRouter(t, port)
	out := strings.Join(r.HandleTestMessage("autonomous housekeeping-repairs"), "\n")
	if log.path != "/api/autonomous/housekeeping-repairs" || log.calls != 1 {
		t.Errorf("path=%s calls=%d", log.path, log.calls)
	}
	for _, frag := range []string{"autonomous housekeeping-repairs", "task_chain: mode report", "would do 1", "leftovers: mode on", "did 1", "undo: reset it", "undo: restore from backup"} {
		if !strings.Contains(out, frag) {
			t.Errorf("reply lacks %q:\n%s", frag, out)
		}
	}
	_, port = laneFillCommDaemon(t, http.StatusOK, housekeepingRepairsCommSample)
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous housekeeping_repairs"), " "); !strings.Contains(got, "task_chain: mode report") {
		t.Errorf("underscore alias: %q", got)
	}
}

func TestHousekeepingRepairsComm_RefusesArgsAndErrors(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, housekeepingRepairsCommSample)
	r := setupPeerRouter(t, port)
	out := strings.Join(r.HandleTestMessage("autonomous housekeeping-repairs mode=on"), " ")
	if !strings.Contains(out, "unknown argument") || !strings.Contains(out, "usage: autonomous housekeeping-repairs") {
		t.Errorf("reply: %q", out)
	}
	if log.calls != 0 {
		t.Errorf("a refused verb reached the daemon: %d calls", log.calls)
	}
	_, port = laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	r = setupPeerRouter(t, port)
	if out := strings.Join(r.HandleTestMessage("autonomous housekeeping-repairs"), " "); !strings.Contains(out, "failed") {
		t.Errorf("error reply: %q", out)
	}
}

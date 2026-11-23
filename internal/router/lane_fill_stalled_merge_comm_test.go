package router

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// stalledMergeCommBody is a lane fill status with one idle Claude lane and two
// Automata waiting with no live work: one because its merge stalled, one
// because it waits for its closing review.
const stalledMergeCommBody = `{
  "mode": "on",
  "lanes": [
    {"id": "claude-1", "type": "claude", "state": "idle", "occupant": null,
     "blocker": "", "eligible_waiting": 1}
  ],
  "queued_eligible": {"gpu": 0, "claude": 1},
  "evidence": {"error": ""},
  "decisions": [],
  "alerts": [],
  "waiting": [
    {"prd_id": "w1", "title": "Add export", "backend": "claude-code",
     "wait_reason": "its merge has been pending for 41 minutes with no progress", "reason_code": "merge_stalled"},
    {"prd_id": "w2", "title": "Fix login", "backend": "pi-prompt",
     "wait_reason": "every task is completed", "reason_code": "waiting_review_merge"}
  ]
}`

// TestStalledMergeComm_StatusListsWaitingWithReason: the lane-fill reply lists
// each waiting Automaton with its title, its reason text and its reason code.
func TestStalledMergeComm_StatusListsWaitingWithReason(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, stalledMergeCommBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-fill"), " ")
	for _, want := range []string{
		"Waiting with no live work",
		`w1 "Add export" (claude-code): its merge has been pending for 41 minutes with no progress [merge_stalled]`,
		`w2 "Fix login" (pi-prompt): every task is completed [waiting_review_merge]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

// TestStalledMergeComm_NoWaitingListWhenNothingWaits: an empty list adds no
// heading to the reply.
func TestStalledMergeComm_NoWaitingListWhenNothingWaits(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, `{"mode":"on","lanes":[],"queued_eligible":{"gpu":0,"claude":0},"waiting":[],"decisions":[],"alerts":[]}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-fill"), " ")
	if strings.Contains(got, "Waiting with no live work") {
		t.Errorf("reply names a waiting list when nothing waits:\n%s", got)
	}
}

// stalledMergeConfigureRouter is a router whose configure writes are recorded
// and whose replies are captured, one reply per message.
type stalledMergeConfigureRouter struct {
	*attemptCostTest
	r *Router
}

func newStalledMergeConfigureRouter(t *testing.T) *stalledMergeConfigureRouter {
	t.Helper()
	at := &attemptCostTest{}
	r := &Router{hostname: "h", backend: at.backend()}
	r.SetConfigureFunc(at.configureFn())
	return &stalledMergeConfigureRouter{attemptCostTest: at, r: r}
}

func (c *stalledMergeConfigureRouter) configure(text string) (sent []string, calls [][2]string) {
	c.expectSends(1)
	c.r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	c.wg.Wait()
	return c.snapshot()
}

// TestStalledMergeComm_ConfigureAcceptsWholeNumbers: 0 (off), the default 30
// and any other whole number reach the write, which replies with the usual
// confirmation.
func TestStalledMergeComm_ConfigureAcceptsWholeNumbers(t *testing.T) {
	for _, val := range []string{"0", "30", "45", " 12 "} {
		c := newStalledMergeConfigureRouter(t)
		sent, calls := c.configure(config.LaneFillStalledMergeMinutesKey + "=" + val)
		want := strings.TrimSpace(val)
		if len(calls) != 1 || calls[0] != [2]string{config.LaneFillStalledMergeMinutesKey, want} {
			t.Fatalf("configure %q: configureFn calls = %v", val, calls)
		}
		wantReply := "[h] Set " + config.LaneFillStalledMergeMinutesKey + " = " + want + ". Restart may be required."
		if len(sent) != 1 || sent[0] != wantReply {
			t.Errorf("configure %q: reply = %q, want %q", val, sent, wantReply)
		}
	}
}

// TestStalledMergeComm_ConfigureRefusesTheRestTheSameWay: a negative, a
// fraction and text are refused before anything is written, with the message
// the REST endpoint, MCP and the CLI use.
func TestStalledMergeComm_ConfigureRefusesTheRestTheSameWay(t *testing.T) {
	want := config.ValidateLaneFillStalledMergeMinutes(-1).Error()
	for _, bad := range []string{"-1", "-30", "1.5", "soon", "true", "[30]"} {
		c := newStalledMergeConfigureRouter(t)
		sent, calls := c.configure(config.LaneFillStalledMergeMinutesKey + "=" + bad)
		if len(calls) != 0 {
			t.Errorf("configure %q reached configureFn: %v", bad, calls)
		}
		if len(sent) != 1 || sent[0] != "[h] Refused: "+want {
			t.Errorf("configure %q: reply = %q, want a refusal with %q", bad, sent, want)
		}
	}
}

// stalledMergeTrackerID matches the tracker ids operator-facing text must not carry.
var stalledMergeTrackerID = regexp.MustCompile(`\b(BL|GH#?|TS-?)\d+\b`)

// TestStalledMergeComm_UsageNamesTheSettingWithoutTrackerIDs: both usage
// replies name the key, its default and its off value, and carry no tracker id.
func TestStalledMergeComm_UsageNamesTheSettingWithoutTrackerIDs(t *testing.T) {
	for _, text := range []string{"help", "list"} {
		c := newStalledMergeConfigureRouter(t)
		sent, calls := c.configure(text)
		if len(calls) != 0 {
			t.Fatalf("configure %q wrote: %v", text, calls)
		}
		got := strings.Join(sent, "\n")
		for _, want := range []string{config.LaneFillStalledMergeMinutesKey, "default 30", "0 = rule off"} {
			if !strings.Contains(got, want) {
				t.Errorf("configure %q usage lacks %q", text, want)
			}
		}
		if stalledMergeTrackerID.MatchString(got) {
			t.Errorf("configure %q usage carries a tracker id", text)
		}
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// stalledMergeStatusBody is the lane fill body as the daemon sends it, with
// one Automaton waiting because its merge stalled and one waiting for its
// closing review.
const stalledMergeStatusBody = `{
	"mode":"on",
	"lanes":[{"id":"claude-1","type":"claude","state":"idle","occupant":null,"eligible_waiting":1}],
	"waiting":[
		{"prd_id":"w1","title":"Add export","backend":"claude-code",
		 "wait_reason":"its merge has been pending for 41 minutes with no progress","reason_code":"merge_stalled"},
		{"prd_id":"w2","title":"Fix login","wait_reason":"every task is completed","reason_code":"waiting_review_merge"}
	]
}`

// TestStalledMergeStatusCarriesWaitingList: autonomous_lane_fill_status passes
// the daemon's body through, so the waiting list arrives with each title,
// reason text and reason code.
func TestStalledMergeStatusCarriesWaitingList(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/lane-fill" {
			t.Errorf("proxied %s %s, want GET /api/autonomous/lane-fill", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(stalledMergeStatusBody))
	})
	res, err := s.handleAutonomousLaneFillStatus(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	for _, want := range []string{
		`"waiting":[`,
		`"reason_code":"merge_stalled"`,
		`"reason_code":"waiting_review_merge"`,
		`"title":"Add export"`,
		`"wait_reason":"its merge has been pending for 41 minutes with no progress"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("MCP output lacks %s: %s", want, text)
		}
	}
}

// tracker ids look like BL123, B87, GH#128 or an 8-hex Automaton id.
var stalledMergeTrackerID = regexp.MustCompile(`\b(BL|GH#?|TS-?)\d+\b`)

// TestStalledMergeDescriptionsLinkHowTo: the status tool, the config get tool
// and the config set argument all name the setting, the default and the off
// value, and link the how-to; none uses the tracker word or a tracker id.
func TestStalledMergeDescriptionsLinkHowTo(t *testing.T) {
	srv := &Server{}
	raw, _ := json.Marshal(srv.toolAutonomousConfigSet().InputSchema.Properties["lane_fill_stalled_merge_minutes"])
	var prop struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &prop); err != nil || prop.Description == "" {
		t.Fatalf("autonomous_config_set does not declare lane_fill_stalled_merge_minutes: %s", raw)
	}
	set := prop.Description
	// Only the lane fill sentence of the config get description is checked:
	// the rest of that text is older and shared with other settings.
	get := srv.toolAutonomousConfigGet().Description
	i := strings.Index(get, "lane_fill holds")
	if i < 0 {
		t.Fatal("config get description has no lane_fill sentence")
	}
	get = get[i:]
	if j := strings.Index(get, "docs/howto/automation-lane-fill.md"); j >= 0 {
		get = get[:j+len("docs/howto/automation-lane-fill.md")]
	}
	cases := map[string]string{
		"status":     srv.toolAutonomousLaneFillStatus().Description,
		"config_get": get,
		"config_set": set,
	}
	for name, desc := range cases {
		if !strings.Contains(desc, "stalled_merge_minutes") {
			t.Errorf("%s description does not name the setting", name)
		}
		if !strings.Contains(desc, "docs/howto/automation-lane-fill.md") {
			t.Errorf("%s description does not link the how-to", name)
		}
		if strings.Contains(desc, "PRD") || stalledMergeTrackerID.MatchString(desc) {
			t.Errorf("%s description uses a tracker word or id: %s", name, desc)
		}
	}
	for _, want := range []string{"waiting", "reason_code", "merge_stalled", "wait_reason"} {
		if !strings.Contains(cases["status"], want) {
			t.Errorf("status description does not name %q", want)
		}
	}
	for _, want := range []string{"default 30", "0 turns the rule off", "Whole number >= 0"} {
		if !strings.Contains(set, want) {
			t.Errorf("config set description lacks %q: %s", want, set)
		}
	}
}

// TestStalledMergeConfigSetSendsValueIncludingZero: the argument reaches the
// nested lane_fill object as stalled_merge_minutes, an explicit 0 included,
// and an absent argument sends nothing.
func TestStalledMergeConfigSetSendsValueIncludingZero(t *testing.T) {
	for _, n := range []float64{0, 45} {
		s, rec := activateServer(t, http.StatusOK, `{}`)
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
			"lane_fill_stalled_merge_minutes": n,
		})); err != nil {
			t.Fatalf("config set: %v", err)
		}
		lane, _ := rec.body["lane_fill"].(map[string]any)
		if len(lane) != 1 || lane["stalled_merge_minutes"] != n {
			t.Errorf("lane_fill = %v, want only stalled_merge_minutes=%v", lane, n)
		}
	}
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"lane_fill_idle_alert_minutes": float64(10),
	})); err != nil {
		t.Fatalf("config set: %v", err)
	}
	lane, _ := rec.body["lane_fill"].(map[string]any)
	if _, has := lane["stalled_merge_minutes"]; has {
		t.Errorf("an absent argument must send nothing: %v", lane)
	}
}

// TestStalledMergeConfigSetRefusalPassesThrough: a value the daemon refuses
// (negative, fractional) comes back as the daemon's message naming the key.
func TestStalledMergeConfigSetRefusalPassesThrough(t *testing.T) {
	msg := "autonomous.lane_fill.stalled_merge_minutes: must be a whole number >= 0 (0 turns the stalled-merge rule off, default 30)"
	for _, bad := range []float64{-1, 2.5} {
		s, _ := activateServer(t, http.StatusBadRequest, msg)
		res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
			"lane_fill_stalled_merge_minutes": bad,
		}))
		text := ""
		if err != nil {
			text = err.Error()
		} else if res != nil {
			text = automationText(t, res)
			if !res.IsError {
				t.Errorf("value %v: a refused change must be reported as an error: %s", bad, text)
			}
		}
		if !strings.Contains(text, "stalled_merge_minutes") {
			t.Errorf("value %v: refusal does not name the key: %q", bad, text)
		}
	}
}

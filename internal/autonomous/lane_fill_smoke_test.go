package autonomous

// Lane fill restart smoke. Two parts.
//
// Part 1 (TestLaneFillSmokeRestartShadowThenOn) always runs. It is in process,
// with no network and no daemon: a real temporary store, a fake starter, a
// fake clock and a fake pool evidence source. It runs shadow polls, checks the
// decisions reach the persisted file and that nothing is started, simulates a
// restart over the same directory, checks the decisions reload and nothing is
// decided before boot recovery is Ready, then switches the mode to on and
// checks each right Automaton is started exactly once and the held one never.
//
// Part 2 (TestLaneFillSmokeLive) is an opt-in live check against a SANDBOX
// daemon. It is skipped unless the environment names the daemon:
//
//	DW_LIVE_LANE_FILL_SMOKE=https://host:port  base URL of the sandbox daemon
//	                                           (required; no default, so the
//	                                           test can never reach a production
//	                                           daemon by accident)
//	DW_BASE_TOKEN=...                          bearer token, if the daemon has one
//	DW_BASE_INSECURE=0                         verify TLS (default: skip verify,
//	                                           as the other live scripts do)
//
// Run it against a sandbox daemon the operator started:
//
//	DW_LIVE_LANE_FILL_SMOKE=https://127.0.0.1:18443 \
//	  env -u TMUX -u TMUX_TMPDIR go test -count=1 -run TestLaneFillSmokeLive -v ./internal/autonomous/
//
// The live part only calls GET /api/autonomous/lane-fill and GET /api/config.
// It never writes, never starts an Automaton and never touches a process on
// the host. The verify script .datawatch/verify/lane-fill-restart-smoke.sh runs
// both parts; without the variable the live part is skipped.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const laneFillSmokeLiveEnv = "DW_LIVE_LANE_FILL_SMOKE"

// laneFillSmokeStart is the fake clock's first reading.
var laneFillSmokeStart = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

// laneFillSmokeClock is the fake clock the coordinator reads.
type laneFillSmokeClock struct{ t time.Time }

func (c *laneFillSmokeClock) Now() time.Time { return c.t }

func (c *laneFillSmokeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// laneFillSmokeConfig is one GPU lane and two Claude lanes, with no
// decompose-ahead, so the plan over the seeded Automata is exact.
func laneFillSmokeConfig(mode string) Config {
	cfg := DefaultConfig()
	cfg.LaneFill = LaneFillConfig{
		Mode: mode, GPULanes: 1, ClaudeLanes: 2, IdleAlertMinutes: 30,
		DecomposeAheadMin: 0, PollSeconds: 60, GPUBackend: "pi-prompt", ClaudeBackend: "claude-code",
	}
	return cfg
}

// laneFillSmokeRun is one daemon lifetime: a Manager and its Coordinator over
// one directory, with the fake starter.
type laneFillSmokeRun struct {
	t     *testing.T
	m     *Manager
	c     *Coordinator
	fake  *laneFillFake
	clock *laneFillSmokeClock
}

// startLaneFillSmoke builds the Manager and Coordinator the way the daemon
// does at boot. The coordinator is started but not Ready, and its own poll
// timer is pushed out of reach so only the test's polls run.
func startLaneFillSmoke(t *testing.T, dir string, mode string, clock *laneFillSmokeClock) *laneFillSmokeRun {
	t.Helper()
	m, err := NewManager(dir, laneFillSmokeConfig(mode), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &laneFillFake{m: m}
	c := NewCoordinator(m, fake)
	c.now = clock.Now
	c.laneFillTick = time.Hour
	c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: clock.Now()}})
	c.Start()
	t.Cleanup(func() {
		c.Stop()
		m.Stop()
	})
	return &laneFillSmokeRun{t: t, m: m, c: c, fake: fake, clock: clock}
}

// poll runs one lane fill pass and waits for any decompose it started.
func (r *laneFillSmokeRun) poll() {
	r.c.laneFillPoll(context.Background())
	r.c.laneFillWait()
}

// setMode switches the lane fill mode live, as a config change does.
func (r *laneFillSmokeRun) setMode(mode string) {
	cfg := r.m.Config()
	cfg.LaneFill.Mode = mode
	r.m.SetConfig(cfg)
}

// laneFillSmokeIDs names the seeded Automata by role.
type laneFillSmokeIDs struct {
	gpu, review, anyPref, held, draft string
}

// seedLaneFillSmoke adds the Automata the run plans over: an approved GPU-
// preferring one, a needs-review Claude-preferring one, an approved any-lane
// one, an approved GPU-preferring one held by a HOLD line in its spec, and a
// draft Claude-preferring one that no lane has room for.
func seedLaneFillSmoke(t *testing.T, m *Manager) laneFillSmokeIDs {
	t.Helper()
	add := func(title string, status PRDStatus, pref, backend, spec string, minute int) string {
		t.Helper()
		p, err := m.store.CreatePRD("spec "+title, "/p", backend, "", "")
		if err != nil {
			t.Fatal(err)
		}
		p.Title, p.Status, p.LanePreference, p.Spec = title, status, pref, spec
		p.CreatedAt = laneFillSmokeStart.Add(time.Duration(minute) * time.Minute)
		if err := m.store.SavePRD(p); err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	var ids laneFillSmokeIDs
	ids.gpu = add("gpu approved", PRDApproved, "gpu", "claude-code", "Runs on the local pool.", 1)
	ids.review = add("claude review", PRDNeedsReview, "claude", "claude-code", "Reviewed and ready.", 2)
	ids.anyPref = add("any approved", PRDApproved, "any", "claude-code", "Either lane will do.", 3)
	ids.held = add("held gpu", PRDApproved, "gpu", "pi-prompt", "HOLD: the pool driver is not ready yet.\nThe rest is unchanged.", 4)
	ids.draft = add("claude draft", PRDDraft, "claude", "claude-code", "Not decomposed yet.", 5)
	return ids
}

// readLaneFillSmokeFile reads the decisions the store wrote to disk, newest
// first, as the daemon persists them.
func readLaneFillSmokeFile(t *testing.T, m *Manager) []LaneFillDecision {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.store.dir, laneFillRecordsFile))
	if err != nil {
		t.Fatalf("read decisions file: %v", err)
	}
	var list []LaneFillDecision
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("decisions file does not parse: %v", err)
	}
	return list
}

// laneFillSmokeSameRecords reports whether two decision lists hold the same
// records, field by field and in the same order.
func laneFillSmokeSameRecords(a, b []LaneFillDecision) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Action != y.Action || x.LaneID != y.LaneID || x.PRDID != y.PRDID || x.Mode != y.Mode ||
			x.Executed != y.Executed || x.Error != y.Error || x.Blocker != y.Blocker ||
			x.RepeatCount != y.RepeatCount || !x.At.Equal(y.At) || !x.LastSeenAt.Equal(y.LastSeenAt) {
			return false
		}
	}
	return true
}

// laneFillSmokeStartDecisions indexes the start decisions by Automaton, newest
// first, so the first one found for an Automaton is its latest.
func laneFillSmokeStartDecisions(list []LaneFillDecision) map[string]LaneFillDecision {
	out := map[string]LaneFillDecision{}
	for _, d := range list {
		if d.Action != LaneFillActionStartPRD {
			continue
		}
		if _, seen := out[d.PRDID]; !seen {
			out[d.PRDID] = d
		}
	}
	return out
}

func TestLaneFillSmokeRestartShadowThenOn(t *testing.T) {
	dir := t.TempDir()
	clock := &laneFillSmokeClock{t: laneFillSmokeStart}

	// Part 1a: shadow polls over a fresh store.
	first := startLaneFillSmoke(t, dir, "shadow", clock)
	ids := seedLaneFillSmoke(t, first.m)
	first.c.SetReady()
	first.poll()
	clock.Advance(time.Minute)
	first.poll() // the same plan again: repeats, not new records

	if calls := first.fake.all(); len(calls) != 0 {
		t.Fatalf("shadow mode made starter calls: %v", calls)
	}
	persisted := readLaneFillSmokeFile(t, first.m)
	if len(persisted) != 3 {
		t.Errorf("persisted %d decisions, want 3 (one per lane); repeats must not add records: %+v", len(persisted), persisted)
	}
	starts := laneFillSmokeStartDecisions(persisted)
	for role, id := range map[string]string{"gpu": ids.gpu, "review": ids.review, "any": ids.anyPref} {
		d, ok := starts[id]
		switch {
		case !ok:
			t.Errorf("no persisted start decision for the %s Automaton %s", role, id)
		case d.Executed || d.Error != "" || d.Mode != LaneFillModeShadow:
			t.Errorf("%s start decision: executed=%v error=%q mode=%q, want shadow and not executed", role, d.Executed, d.Error, d.Mode)
		case d.RepeatCount != 2:
			t.Errorf("%s start decision repeat_count=%d, want 2 after two polls", role, d.RepeatCount)
		}
	}
	for _, d := range persisted {
		if d.PRDID == ids.held || d.PRDID == ids.draft {
			t.Errorf("shadow planned %s for %s, which must not be started or decomposed here", d.Action, d.PRDID)
		}
	}
	before := first.m.LaneFillStatus(0).Decisions
	if !laneFillSmokeSameRecords(before, persisted) {
		t.Fatalf("status decisions differ from the persisted file:\nstatus %+v\nfile   %+v", before, persisted)
	}

	// Part 1b: a simulated restart. The old daemon stops; a new Manager and
	// Coordinator open the same directory.
	first.c.Stop()
	first.m.Stop()
	clock.Advance(time.Minute)
	second := startLaneFillSmoke(t, dir, "shadow", clock)

	reloaded := second.m.LaneFillStatus(0).Decisions
	if !laneFillSmokeSameRecords(reloaded, persisted) {
		t.Fatalf("decisions after restart:\n%+v\nwant the persisted ones:\n%+v", reloaded, persisted)
	}

	// Nothing is decided before Ready: boot recovery has not finished, so a
	// poll must plan nothing, record nothing and call nothing.
	clock.Advance(time.Minute)
	second.poll()
	if st := second.m.LaneFillStatus(0); !st.LastPollAt.IsZero() || !laneFillSmokeSameRecords(st.Decisions, persisted) {
		t.Fatalf("a poll before Ready decided something: last poll %v, decisions %+v", st.LastPollAt, st.Decisions)
	}
	if calls := second.fake.all(); len(calls) != 0 {
		t.Fatalf("calls before Ready: %v", calls)
	}

	// Part 1c: switch to on. Each right Automaton starts exactly once, in lane
	// order: the GPU lane takes the gpu work (backend moved to the pool first),
	// the first Claude lane takes the review work (approved first), the second
	// takes the any-lane work. The held one and the draft never start.
	second.c.SetReady()
	second.setMode("on")
	second.poll()
	want := []string{
		fmt.Sprintf("setllm:%s:pi-prompt::%s", ids.gpu, laneFillActor),
		"run:" + ids.gpu,
		"approve:" + ids.review + ":" + laneFillActor,
		"run:" + ids.review,
		"run:" + ids.anyPref,
	}
	if got := second.fake.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls after switching on:\n%v\nwant\n%v", got, want)
	}

	// A second poll sees all three running and starts nothing more.
	clock.Advance(time.Minute)
	second.poll()
	if got := second.fake.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("second poll made calls: %v", got)
	}

	for _, id := range []string{ids.gpu, ids.review, ids.anyPref} {
		p, ok := second.m.store.GetPRD(id)
		if !ok || p.Status != PRDRunning {
			t.Errorf("Automaton %s status %v, want running", id, p)
		}
	}
	if p, ok := second.m.store.GetPRD(ids.held); !ok || p.Status != PRDApproved {
		t.Errorf("held Automaton changed: %+v", p)
	}
	if p, ok := second.m.store.GetPRD(ids.draft); !ok || p.Status != PRDDraft {
		t.Errorf("draft Automaton changed: %+v", p)
	}
	for _, d := range second.m.LaneFillStatus(0).Decisions {
		if d.Action == LaneFillActionStartPRD && d.Mode == LaneFillModeOn && !d.Executed {
			t.Errorf("start decision in on mode not executed: %+v", d)
		}
	}
}

// TestLaneFillSmokeLive reads a sandbox daemon's lane fill status and config.
// It is skipped unless DW_LIVE_LANE_FILL_SMOKE names the daemon.
func TestLaneFillSmokeLive(t *testing.T) {
	base := strings.TrimRight(os.Getenv(laneFillSmokeLiveEnv), "/")
	if base == "" {
		t.Skipf("set %s to the sandbox daemon base URL to run the live lane fill check", laneFillSmokeLiveEnv)
	}
	c := newLaneFillSmokeClient(base, os.Getenv("DW_BASE_TOKEN"))

	var cfg struct {
		Autonomous struct {
			LaneFill struct {
				Mode string `json:"mode"`
			} `json:"lane_fill"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(c.get(t, "/api/config"), &cfg); err != nil {
		t.Fatalf("GET /api/config does not parse: %v", err)
	}
	cfgMode := cfg.Autonomous.LaneFill.Mode
	if cfgMode == "" {
		t.Fatal("GET /api/config reports no autonomous.lane_fill.mode")
	}

	body := c.get(t, "/api/autonomous/lane-fill")
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(body, &keys); err != nil {
		t.Fatalf("lane fill status does not parse: %v", err)
	}
	var lanes []json.RawMessage
	if err := json.Unmarshal(keys["lanes"], &lanes); err != nil || lanes == nil {
		t.Fatalf("lane fill status has no lanes list (raw %q): %v", keys["lanes"], err)
	}
	var st struct {
		Mode      string `json:"mode"`
		Decisions []struct {
			Action   string `json:"action"`
			Executed bool   `json:"executed"`
			Mode     string `json:"mode"`
			PRDID    string `json:"prd_id"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("lane fill status does not parse: %v", err)
	}
	switch st.Mode {
	case "off", "shadow", "on":
	default:
		t.Fatalf("lane fill status mode %q is not off, shadow or on", st.Mode)
	}
	if st.Mode != cfgMode {
		t.Errorf("status mode %q, but GET /api/config says %q", st.Mode, cfgMode)
	}
	t.Logf("sandbox lane fill mode %q, %d lanes, %d decisions", st.Mode, len(lanes), len(st.Decisions))
	if st.Mode != "shadow" {
		t.Logf("the sandbox set mode %q rather than shadow; the shadow checks are skipped", st.Mode)
		return
	}
	for i, d := range st.Decisions {
		if d.Executed {
			t.Errorf("shadow mode decision %d (%s for %s) has executed=true", i, d.Action, d.PRDID)
		}
	}
}

// laneFillSmokeClient is a minimal GET client for the sandbox daemon.
type laneFillSmokeClient struct {
	base  string
	token string
	hc    *http.Client
}

func newLaneFillSmokeClient(base, token string) *laneFillSmokeClient {
	return &laneFillSmokeClient{
		base:  base,
		token: token,
		hc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: os.Getenv("DW_BASE_INSECURE") != "0", // #nosec G402 -- sandbox daemons use self-signed certs
				},
			},
		},
	}
}

// get fetches one path and returns the body. Any status other than 200 fails.
func (c *laneFillSmokeClient) get(t *testing.T, path string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", path, err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read GET %s body: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d body=%s", path, resp.StatusCode, out)
	}
	return out
}

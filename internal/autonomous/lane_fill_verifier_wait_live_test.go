package autonomous

// Verifier-wait lane occupancy smoke. Two parts.
//
// Part 1 (TestVerifierWaitSmokeInProcess) always runs. It is in process,
// with no network and no daemon: a real temporary store, a one-slot
// capacity pool, a fake starter, a fake clock and a fake pool evidence
// source. One running Automaton has its only running task queued on the
// verifier slot, so its Claude lane is occupied but not advancing. A poll
// in shadow mode must record a start decision for the ready Automaton that
// carries the lane's wait_reason and must do no starting; the switch to on
// mode must then start exactly that one Automaton and never a second.
//
// Part 2 (TestVerifierWaitSmokeLive) is an opt-in live check against a
// SANDBOX daemon. It is skipped unless the environment names the daemon:
//
//	DW_LIVE_VERIFIER_WAIT_SMOKE=https://host:port  base URL of the sandbox
//	                                                daemon (required; no
//	                                                default, so the test can
//	                                                never reach a production
//	                                                daemon by accident)
//	DW_BASE_TOKEN=...                               bearer token, if the
//	                                                daemon has one
//	DW_BASE_INSECURE=0                              verify TLS (default:
//	                                                skip verify, as the
//	                                                other live scripts do)
//
// Run it against a sandbox daemon the operator started:
//
//	DW_LIVE_VERIFIER_WAIT_SMOKE=https://127.0.0.1:18445 \
//	  env -u TMUX -u TMUX_TMPDIR go test -count=1 \
//	    -run TestVerifierWaitSmokeLive -v ./internal/autonomous/
//
// The live part only calls GET /api/autonomous/lane-fill and GET
// /api/config. It never writes, never starts an Automaton and never
// touches a process on the host.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const verifierWaitSmokeLiveEnv = "DW_LIVE_VERIFIER_WAIT_SMOKE"

// verifierWaitSmokeStart is Part 1's first clock reading.
var verifierWaitSmokeStart = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

// verifierWaitSmokeConfig is one Claude lane, no GPU lanes, with no
// decompose-ahead, so the plan over the seeded Automata is exact.
func verifierWaitSmokeConfig(mode string) Config {
	cfg := DefaultConfig()
	cfg.LaneFill = LaneFillConfig{
		Mode: mode, GPULanes: 0, ClaudeLanes: 1, IdleAlertMinutes: 30,
		DecomposeAheadMin: 0, PollSeconds: 60, GPUBackend: "pi-prompt", ClaudeBackend: "claude-code",
	}
	return cfg
}

// verifierWaitSmokeSeed plants the two Automata the run plans over: one
// running Automaton whose only running task is queued on the verifier slot
// (its lane is held), and one approved Automaton ready to start. It
// returns the ids, and the held Automaton's waiting task.
func verifierWaitSmokeSeed(t *testing.T, m *Manager) (heldID, readyID, heldTask string) {
	t.Helper()
	held := newPRDWithTasks(t, m, 1)
	p, ok := m.Store().GetPRD(held.ID)
	if !ok {
		t.Fatalf("GetPRD(held): not found")
	}
	p.Title = "held in verification"
	p.Status = PRDRunning
	p.LanePreference = "claude"
	p.Backend = "claude-code"
	p.CreatedAt = verifierWaitSmokeStart
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}

	r, err := m.store.CreatePRD("ready to start", "/p", "claude-code", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r.Title = "ready to start"
	r.Status = PRDApproved
	r.LanePreference = "claude"
	r.CreatedAt = verifierWaitSmokeStart.Add(time.Minute)
	if err := m.store.SavePRD(r); err != nil {
		t.Fatal(err)
	}

	ids := setTaskStatuses(t, m, held.ID, TaskVerifying)
	if len(ids) != 1 {
		t.Fatalf("held Automaton has %d tasks, want exactly 1", len(ids))
	}
	heldTask = ids[0]
	queueWaiter(t, m.Capacity(), VerifyHolder(heldTask), held.ID)
	return held.ID, r.ID, heldTask
}

func TestVerifierWaitSmokeInProcess(t *testing.T) {
	m, _ := poolFixture(t, false)
	heldID, readyID, heldTask := verifierWaitSmokeSeed(t, m)
	t.Logf("held=%s (task %s waiting as %s), ready=%s", heldID, heldTask, VerifyHolder(heldTask), readyID)

	m.SetConfig(verifierWaitSmokeConfig(string(LaneFillModeShadow)))

	fake := &laneFillFake{m: m}
	c := NewCoordinator(m, fake)
	clock := &laneFillSmokeClock{t: verifierWaitSmokeStart.Add(2 * time.Minute)}
	c.now = clock.Now
	c.laneFillTick = time.Hour // only the test's polls run
	c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: clock.Now()}})
	c.Start()
	t.Cleanup(func() {
		c.Stop()
		m.Stop()
	})
	c.SetReady()

	// Shadow: the held Claude lane counts as available (count_verifier_wait
	// defaults to true), so the one poll must record the ready Automaton for
	// the lane — with the lane's wait_reason on the record — and start
	// nothing.
	c.laneFillPoll(context.Background())
	c.laneFillWait()

	if calls := fake.all(); len(calls) != 0 {
		t.Fatalf("shadow mode made starter calls: %v", calls)
	}

	status := m.LaneFillStatus(0)
	var held *LaneFillLaneStatus
	var readyDecision *LaneFillDecision
	for i := range status.Lanes {
		l := &status.Lanes[i]
		if l.Occupant == nil || l.Occupant.PRDID != heldID {
			continue
		}
		held = l
		if l.State != "waiting_verifier" {
			t.Errorf("held lane state %q, want waiting_verifier: %+v", l.State, l)
		}
		if !l.Occupant.WaitingOnVerifier {
			t.Errorf("held lane occupant not flagged waiting on the verifier: %+v", l.Occupant)
		}
		if l.Occupant.WaitReason == "" {
			t.Errorf("held lane has no wait_reason: %+v", l)
		}
		if l.WaitingOnVerifier == false || l.WaitReason == "" {
			t.Errorf("held lane surface lacks wait fields: %+v", l)
		}
	}
	if held == nil {
		t.Fatalf("the held Claude lane is not in the status: %+v", status.Lanes)
	}
	for i := range status.Decisions {
		d := &status.Decisions[i]
		if d.Action == LaneFillActionStartPRD && d.PRDID == readyID {
			readyDecision = d
			if d.Executed {
				t.Errorf("shadow decision for %s is executed: %+v", readyID, d)
			}
			if d.Mode != LaneFillModeShadow {
				t.Errorf("shadow decision mode %q, want shadow: %+v", d.Mode, d)
			}
			if d.WaitReason != held.WaitReason {
				t.Errorf("decision wait_reason %q, want the lane's %q", d.WaitReason, held.WaitReason)
			}
			if d.WaitReason == "" {
				t.Errorf("decision has no wait_reason: %+v", d)
			}
			if d.LaneID != held.ID || d.LaneType != LaneTypeClaude {
				t.Errorf("decision planned %s for lane %s/%s, want the held lane %s: %+v",
					readyID, d.LaneType, d.LaneID, held.ID, d)
			}
		}
	}
	if readyDecision == nil {
		t.Fatalf("no shadow start decision for the ready Automaton %s: %+v", readyID, status.Decisions)
	}
	for _, d := range status.Decisions {
		if d.Action == LaneFillActionStartPRD && d.PRDID == heldID {
			t.Errorf("the held Automaton %s was itself planned to start: %+v", heldID, d)
		}
	}
	t.Logf("shadow: %+v", readyDecision)

	// On: the switch to on mode must start exactly the ready Automaton, on
	// the held lane.
	clock.Advance(time.Minute)
	cfg := m.Config()
	cfg.LaneFill.Mode = string(LaneFillModeOn)
	m.SetConfig(cfg)
	c.laneFillPoll(context.Background())
	c.laneFillWait()

	want := []string{"run:" + readyID}
	if got := fake.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls after switching on:\n%v\nwant\n%v", got, want)
	}
	p, ok := m.Store().GetPRD(readyID)
	if !ok || p.Status != PRDRunning {
		t.Errorf("ready Automaton %s status %v, want running", readyID, p)
	}
	hp, ok := m.Store().GetPRD(heldID)
	if !ok || hp.Status != PRDRunning {
		t.Errorf("held Automaton %s status %v, want still running", heldID, hp)
	}

	// A second poll starts nothing more: the ready Automaton is running now
	// and the held one is still blocked on its verifier.
	clock.Advance(time.Minute)
	c.laneFillPoll(context.Background())
	c.laneFillWait()
	if got := fake.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("second poll made extra calls: %v", got)
	}
	if calls := fake.all(); strings.Count(strings.Join(calls, ","), "run:"+readyID) != 1 {
		t.Fatalf("the ready Automaton started more than once: %v", calls)
	}

	// After the start the ready Automaton is the lane's occupant (the held
	// lane was counted available, which is exactly what placed it there), and
	// the held Automaton is still running in the store.
	status = m.LaneFillStatus(0)
	readyLane := false
	for i := range status.Lanes {
		l := &status.Lanes[i]
		if l.Occupant == nil {
			continue
		}
		if l.Occupant.PRDID == readyID {
			readyLane = true
		}
	}
	if !readyLane {
		t.Errorf("the ready Automaton is not placed on a lane: %+v", status.Lanes)
	}
}

// --- live part ---

// verifierWaitSmokeClient is a minimal GET client for the sandbox daemon.
type verifierWaitSmokeClient struct {
	base  string
	token string
	hc    *http.Client
}

func newVerifierWaitSmokeClient(base, token string) *verifierWaitSmokeClient {
	return &verifierWaitSmokeClient{
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

// get fetches one path and returns the body. Any status other than 200
// fails.
func (c *verifierWaitSmokeClient) get(t *testing.T, path string) []byte {
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

func TestVerifierWaitSmokeLive(t *testing.T) {
	base := strings.TrimRight(os.Getenv(verifierWaitSmokeLiveEnv), "/")
	if base == "" {
		t.Skipf("set %s to the sandbox daemon base URL to run the live verifier-wait lane check", verifierWaitSmokeLiveEnv)
	}
	c := newVerifierWaitSmokeClient(base, os.Getenv("DW_BASE_TOKEN"))

	// GET /api/autonomous/lane-fill: the status must parse and carry the
	// lane/decision field names the verifier-wait work added.
	body := c.get(t, "/api/autonomous/lane-fill")
	var st struct {
		Mode  string `json:"mode"`
		Lanes []struct {
			ID                string `json:"id"`
			Type              string `json:"type"`
			State             string `json:"state"`
			WaitingOnVerifier bool   `json:"waiting_on_verifier"`
			WaitReason        string `json:"wait_reason"`
			Occupant          *struct {
				WaitingOnVerifier bool   `json:"waiting_on_verifier"`
				WaitReason        string `json:"wait_reason"`
			} `json:"occupant"`
		} `json:"lanes"`
		Decisions []struct {
			Action     string `json:"action"`
			Executed   bool   `json:"executed"`
			Mode       string `json:"mode"`
			PRDID      string `json:"prd_id"`
			WaitReason string `json:"wait_reason"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("/api/autonomous/lane-fill does not parse: %v (raw %q)", err, body)
	}
	switch st.Mode {
	case "off", "shadow", "on":
	default:
		t.Fatalf("lane fill mode %q is not off, shadow or on", st.Mode)
	}
	waiting := 0
	for i := range st.Lanes {
		l := &st.Lanes[i]
		if l.State == "" {
			t.Fatalf("lane %d has no state: %+v", i, l)
		}
		if l.Occupant != nil && l.Occupant.WaitingOnVerifier && l.WaitReason == "" {
			t.Errorf("lane %s: occupant waiting on the verifier but the lane reports no wait_reason", l.ID)
		}
		if l.WaitingOnVerifier {
			waiting++
		}
	}
	for i, d := range st.Decisions {
		if st.Mode == "shadow" && d.Executed {
			t.Errorf("shadow mode decision %d (%s for %s) is executed", i, d.Action, d.PRDID)
		}
	}
	t.Logf("sandbox lane fill: mode=%s lanes=%d (waiting_on_verifier=%d) decisions=%d", st.Mode, len(st.Lanes), waiting, len(st.Decisions))

	// GET /api/config: autonomous.lane_fill must carry count_verifier_wait;
	// a daemon that has never set it reports the default, true.
	cfgBody := c.get(t, "/api/config")
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(cfgBody, &cfg); err != nil {
		t.Fatalf("/api/config does not parse: %v", err)
	}
	autoRaw, ok := cfg["autonomous"]
	if !ok {
		t.Fatal("/api/config has no autonomous block")
	}
	var auto map[string]json.RawMessage
	if err := json.Unmarshal(autoRaw, &auto); err != nil {
		t.Fatalf("/api/config autonomous does not parse: %v", err)
	}
	lfRaw, ok := auto["lane_fill"]
	if !ok {
		t.Fatal("/api/config autonomous has no lane_fill block")
	}
	var lf map[string]json.RawMessage
	if err := json.Unmarshal(lfRaw, &lf); err != nil {
		t.Fatalf("/api/config autonomous.lane_fill does not parse: %v", err)
	}
	cwRaw, ok := lf["count_verifier_wait"]
	if !ok {
		t.Fatal("/api/config autonomous.lane_fill has no count_verifier_wait key")
	}
	var cw bool
	if err := json.Unmarshal(cwRaw, &cw); err != nil {
		t.Fatalf("autonomous.lane_fill.count_verifier_wait = %s is not a boolean", cwRaw)
	}
	t.Logf("sandbox config: autonomous.lane_fill.count_verifier_wait=%v (unset defaults to true)", cw)
	if !cw {
		t.Log("note: the sandbox daemon has count_verifier_wait set to false; the default-true check passes only on a daemon that never set it")
	}
}

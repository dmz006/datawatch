// notify_events_end_to_end_test.go — end-to-end coverage of the
// notify-events feature: a guardrail profile with a notify_events list
// is created and assigned through the real REST surface, an Automaton
// inherits the profile tier, gets its own override and falls back
// again when it is cleared, invalid entries are rejected with 400 and
// a valid-kinds body, and a real terminal transition confirms that
// only the resolved kinds reach the fake alert/topic/comm sinks (a
// persisted subscription to a kind outside the set receives nothing
// and stays armed) while a Manager.Subscribe sees everything. Each
// real notify_events change lands exactly one audit entry; no-op and
// rejected writes land none.
//
// Real Manager, Store and REST handlers; no network and no session
// backend — sinks are fake in-memory callbacks.

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// sinkRecorder is the shared fake for the alert, topic and comm
// callback sinks used by the lifecycle sink installer and the
// subscription engine's comm fan-out.
type sinkRecorder struct {
	mu     sync.Mutex
	alerts []string
	topics []string
	comm   []string
}

func (r *sinkRecorder) recordAlert(level, title, body string) {
	r.mu.Lock()
	r.alerts = append(r.alerts, level+":"+title+" "+body)
	r.mu.Unlock()
}

func (r *sinkRecorder) publishTopics(topics []string, title, message string) {
	r.mu.Lock()
	r.topics = append(r.topics, strings.Join(topics, ",")+"|"+title)
	r.mu.Unlock()
}

func (r *sinkRecorder) commSend(text string) {
	r.mu.Lock()
	r.comm = append(r.comm, text)
	r.mu.Unlock()
}

func (r *sinkRecorder) alertCount() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.alerts) }
func (r *sinkRecorder) topicCount() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.topics) }
func (r *sinkRecorder) commCount() int  { r.mu.Lock(); defer r.mu.Unlock(); return len(r.comm) }

func (r *sinkRecorder) reset() {
	r.mu.Lock()
	r.alerts, r.topics, r.comm = nil, nil, nil
	r.mu.Unlock()
}

func waitForNotify(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func equalNotifyEvents(a, b []string) bool { return slices.Equal(a, b) }

// TestNotifyEventsEndToEnd drives the real Manager, Store and REST
// handlers (httptest + temp data dir) through the whole notify-events
// flow. Sinks are fake in-memory callbacks; no network, no tmux.
func TestNotifyEventsEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	m, err := autonomous.NewManager(dataDir, autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	aud := &auditRecorder{}
	m.SetAuditFn(aud.fn)

	s := &Server{autonomousMgr: autonomous.NewAPI(m)}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, rd)
		rc := httptest.NewRecorder()
		switch {
		case strings.HasPrefix(path, "/api/autonomous/prds"):
			s.handleAutonomousPRDs(rc, req)
		case strings.HasPrefix(path, "/api/autonomous/guardrail_profiles"):
			s.handleAutonomousGuardrailProfiles(rc, req)
		case strings.HasPrefix(path, "/api/autonomous/status"):
			s.handleAutonomousStatus(rc, req)
		default:
			t.Fatalf("no route for %s", path)
		}
		return rc
	}

	// Fake sinks: the lifecycle installer's alert + topic callbacks and
	// the subscription engine's comm fan-out. (The installer's comm
	// path is covered by the gate tests; here comm is asserted via the
	// engine so a blocked subscription is distinguishable from a
	// delivered one.)
	rec := &sinkRecorder{}
	cancelSinks := autonomous.InstallLifecycleSinks(m, autonomous.SinkConfig{
		RecordAlert:   rec.recordAlert,
		PublishTopics: rec.publishTopics,
	})
	t.Cleanup(cancelSinks)

	subStore, err := autonomous.NewSubscriptionStore(t.TempDir()+"/subs.json", nil)
	if err != nil {
		t.Fatalf("subscription store: %v", err)
	}
	eng := autonomous.NewLifecycleSubscriptionEngine(m, subStore, autonomous.SessionPushConfig{
		Comm: autonomous.CommSinkConfig{Send: rec.commSend, Window: 20 * time.Millisecond},
	})
	if err := eng.Start(); err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(eng.Close)

	// Manager.Subscribe: an ungated subscriber that must see every
	// lifecycle event for its Automaton, whatever the resolved set is.
	var subMu sync.Mutex
	subSeen := []string{}
	cancelSub := m.Subscribe(autonomous.LifecycleFilter{}, func(ev autonomous.LifecycleEvent) {
		subMu.Lock()
		subSeen = append(subSeen, ev.Kind)
		subMu.Unlock()
	})
	t.Cleanup(cancelSub)

	const notifyChanged = "autonomous_notify_events_changed"

	// (1) Create a guardrail profile with a notify_events list via
	// the REST surface.
	profBody := call(http.MethodPost, "/api/autonomous/guardrail_profiles",
		`{"name":"notify e2e profile","notify_events":["prd.completed","prd.cancelled"]}`)
	if profBody.Code != http.StatusCreated {
		t.Fatalf("create profile: got %d, want 201 (body %s)", profBody.Code, profBody.Body.String())
	}
	var prof struct {
		ID           string   `json:"id"`
		NotifyEvents []string `json:"notify_events"`
	}
	if err := json.Unmarshal(profBody.Body.Bytes(), &prof); err != nil {
		t.Fatalf("profile create body: %v", err)
	}
	if prof.ID == "" || !equalNotifyEvents(prof.NotifyEvents, []string{"prd.completed", "prd.cancelled"}) {
		t.Fatalf("profile: id=%q notify_events=%v", prof.ID, prof.NotifyEvents)
	}
	// Profile creation persists the list; creation itself is not a
	// notify-events *change*, so no autonomous_notify_events_changed
	// entry yet.
	if n := aud.count(notifyChanged); n != 0 {
		t.Fatalf("after profile create: %d audit notify-events entries, want 0 (details %v)", n, aud.entries(notifyChanged))
	}

	// (2) Create an Automaton and assign it the profile via REST.
	prd, err := m.CreatePRD("notify e2e automaton", t.TempDir(), "opencode", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("create automaton: %v", err)
	}
	assignBody, _ := json.Marshal(map[string]string{"guardrail_profile": prof.ID})
	if rc := call(http.MethodPut, "/api/autonomous/prds/"+prd.ID+"/guardrails", string(assignBody)); rc.Code != http.StatusOK {
		t.Fatalf("assign profile: got %d, want 200 (body %s)", rc.Code, rc.Body.String())
	}
	if n := aud.count(notifyChanged); n != 0 {
		t.Fatalf("profile assignment must not write a notify-events change entry: %d", n)
	}

	// (3) notify_events_effective resolves through the profile before
	// the Automaton has an override of its own.
	eff := fetchNotifyEffective(t, call(http.MethodGet, "/api/autonomous/prds/"+prd.ID, ""))
	if eff.Source != "profile" || !equalNotifyEvents(eff.Events, []string{"prd.completed", "prd.cancelled"}) {
		t.Fatalf("inherit-from-profile: source=%q events=%v", eff.Source, eff.Events)
	}

	// (4) Set the Automaton's own notify_events; the automaton tier
	// wins over the profile.
	setBody := `{"notify_events":["prd.failed","prd.cancelled"]}`
	if rc := call(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/set_notify_events", setBody); rc.Code != http.StatusOK {
		t.Fatalf("set notify_events: got %d, want 200 (body %s)", rc.Code, rc.Body.String())
	}
	eff = fetchNotifyEffective(t, call(http.MethodGet, "/api/autonomous/prds/"+prd.ID, ""))
	if eff.Source != "automaton" || !equalNotifyEvents(eff.Events, []string{"prd.failed", "prd.cancelled"}) {
		t.Fatalf("automaton tier: source=%q events=%v", eff.Source, eff.Events)
	}
	if n := aud.count(notifyChanged); n != 1 {
		t.Fatalf("after set: %d audit entries, want 1 (details %v)", n, aud.entries(notifyChanged))
	}
	got := aud.entries(notifyChanged)[0]
	if got["scope"] != "automaton" || got["prd_id"] != prd.ID || got["old"] != "" || got["new"] != "prd.failed,prd.cancelled" || got["source_after"] != "automaton" {
		t.Fatalf("automaton set audit entry wrong: %v", got)
	}

	// A byte-identical resubmit is a no-op: it succeeds but writes no
	// audit entry.
	if rc := call(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/set_notify_events", setBody); rc.Code != http.StatusOK {
		t.Fatalf("no-op set: got %d, want 200 (body %s)", rc.Code, rc.Body.String())
	}
	if n := aud.count(notifyChanged); n != 1 {
		t.Fatalf("no-op set wrote an audit entry: %d, want 1", n)
	}

	// Invalid lists are rejected with 400 and an operator-actionable
	// valid-kinds body, and never persist, never audit.
	bad := call(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/set_notify_events", `{"notify_events":["bogus.kind"]}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind: got %d, want 400 (body %s)", bad.Code, bad.Body.String())
	}
	if b := bad.Body.String(); !strings.Contains(b, "unknown notify event kind") || !strings.Contains(b, "valid kinds") || !strings.Contains(b, "prd.blocked") {
		t.Fatalf("unknown-kind 400 body missing valid-kinds guidance: %q", b)
	}
	empty := call(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/set_notify_events", `{"notify_events":[""]}`)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty-string kind: got %d, want 400 (body %s)", empty.Code, empty.Body.String())
	}
	if n := aud.count(notifyChanged); n != 1 {
		t.Fatalf("rejected sets wrote audit entries: %d, want 1", n)
	}
	eff = fetchNotifyEffective(t, call(http.MethodGet, "/api/autonomous/prds/"+prd.ID, ""))
	if eff.Source != "automaton" || !equalNotifyEvents(eff.Events, []string{"prd.failed", "prd.cancelled"}) {
		t.Fatalf("rejected sets must not change the stored list: source=%q events=%v", eff.Source, eff.Events)
	}

	// (5) Clear the Automaton's override: it falls back to the
	// assigned profile's tier.
	if rc := call(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/set_notify_events", `{"notify_events":[]}`); rc.Code != http.StatusOK {
		t.Fatalf("clear notify_events: got %d, want 200 (body %s)", rc.Code, rc.Body.String())
	}
	eff = fetchNotifyEffective(t, call(http.MethodGet, "/api/autonomous/prds/"+prd.ID, ""))
	if eff.Source != "profile" || !equalNotifyEvents(eff.Events, []string{"prd.completed", "prd.cancelled"}) {
		t.Fatalf("fall back to profile: source=%q events=%v", eff.Source, eff.Events)
	}
	if n := aud.count(notifyChanged); n != 2 {
		t.Fatalf("after clear: %d audit entries, want 2 (details %v)", n, aud.entries(notifyChanged))
	}
	got = aud.entries(notifyChanged)[1]
	if got["scope"] != "automaton" || got["prd_id"] != prd.ID || got["old"] != "prd.failed,prd.cancelled" || got["new"] != "" || got["source_after"] != "profile" {
		t.Fatalf("clear audit entry wrong: %v", got)
	}

	// (6) The status surface exposes the global default set and the
	// per-Automaton override resolved through the profile.
	stBody := call(http.MethodGet, "/api/autonomous/status", "")
	if stBody.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", stBody.Code)
	}
	type notifyOverride struct {
		ID     string   `json:"id"`
		Source string   `json:"source"`
		Events []string `json:"events"`
	}
	var st struct {
		Notify struct {
			DefaultEvents []string         `json:"default_events"`
			Overrides     []notifyOverride `json:"overrides"`
		} `json:"notify"`
	}
	if err := json.Unmarshal(stBody.Body.Bytes(), &st); err != nil {
		t.Fatalf("status body: %v", err)
	}
	wantDefaults := []string{"prd.needs_review", "prd.blocked", "prd.failed", "prd.completed", "prd.cancelled"}
	if !equalNotifyEvents(st.Notify.DefaultEvents, wantDefaults) {
		t.Fatalf("status default_events = %v, want %v", st.Notify.DefaultEvents, wantDefaults)
	}
	found := false
	for _, o := range st.Notify.Overrides {
		if o.ID == prd.ID {
			found = true
			if o.Source != "profile" || !equalNotifyEvents(o.Events, []string{"prd.completed", "prd.cancelled"}) {
				t.Fatalf("status override for %s = %+v (want profile tier)", prd.ID, o)
			}
		}
	}
	if !found {
		t.Fatalf("status overrides missing entry for %s: %v", prd.ID, st.Notify.Overrides)
	}

	// (7) Real status transitions fire only for kinds inside the
	// resolved set. Pin the resolved set to exactly prd.cancelled,
	// then drive a real cancellation through the REST handler.
	if rc := call(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/set_notify_events", `{"notify_events":["prd.cancelled"]}`); rc.Code != http.StatusOK {
		t.Fatalf("pin resolved set: got %d, want 200 (body %s)", rc.Code, rc.Body.String())
	}
	if n := aud.count(notifyChanged); n != 3 {
		t.Fatalf("after pin: %d audit entries, want 3 (details %v)", n, aud.entries(notifyChanged))
	}
	got = aud.entries(notifyChanged)[2]
	if got["scope"] != "automaton" || got["prd_id"] != prd.ID || got["old"] != "" || got["new"] != "prd.cancelled" {
		t.Fatalf("pin audit entry wrong: %v", got)
	}
	eff = fetchNotifyEffective(t, call(http.MethodGet, "/api/autonomous/prds/"+prd.ID, ""))
	if eff.Source != "automaton" || !equalNotifyEvents(eff.Events, []string{"prd.cancelled"}) {
		t.Fatalf("pinned resolved set: source=%q events=%v", eff.Source, eff.Events)
	}

	// Persisted subscriptions: one armed for a kind inside the set,
	// one (one-shot) for a kind outside it. prd.needs_review is a
	// valid, globally-defaulted kind, so outside-the-set here means
	// gated by this Automaton's resolved list, not an invalid entry.
	if _, err := subStore.Add(autonomous.Subscription{
		Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.cancelled"}},
		Sink:   autonomous.SubscriptionSink{Type: autonomous.SinkComm, Target: "e2e-inside"},
	}); err != nil {
		t.Fatalf("add subscription (inside set): %v", err)
	}
	oneShot := autonomous.Subscription{
		Filter:  autonomous.LifecycleFilter{Kinds: []string{"prd.needs_review"}},
		Sink:    autonomous.SubscriptionSink{Type: autonomous.SinkComm, Target: "e2e-outside"},
		OneShot: true,
	}
	if _, err := subStore.Add(oneShot); err != nil {
		t.Fatalf("add one-shot subscription (outside set): %v", err)
	}
	rec.reset()

	// Real transition via REST: the Automaton cancels, which is in
	// the resolved set; prd.needs_review is not.
	if rc := call(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/cancel", `{"actor":"e2e-test"}`); rc.Code != http.StatusOK {
		t.Fatalf("cancel: got %d, want 200 (body %s)", rc.Code, rc.Body.String())
	}
	waitForNotify(t, "alert sink delivery", func() bool { return rec.alertCount() == 1 })
	waitForNotify(t, "topic sink delivery", func() bool { return rec.topicCount() == 1 })
	waitForNotify(t, "comm sink delivery (inside-set subscription)", func() bool { return rec.commCount() == 1 })
	subMu.Lock()
	seen := append([]string{}, subSeen...)
	subMu.Unlock()
	// Manager.Subscribe is ungated and sees everything: the
	// cancellation AND the update signal.
	waitForNotify(t, "Manager.Subscribe to see prd.cancelled and prd.updated", func() bool {
		subMu.Lock()
		ok := slices.Contains(subSeen, "prd.cancelled") && slices.Contains(subSeen, "prd.updated")
		subMu.Unlock()
		return ok
	})
	if !slices.Contains(seen, "prd.cancelled") {
		t.Fatalf("Manager.Subscribe missed prd.cancelled: saw %v", seen)
	}
	if !slices.Contains(seen, "prd.updated") {
		t.Fatalf("Manager.Subscribe missed prd.updated (it must see everything): saw %v", seen)
	}

	// Grace period: the outside-set one-shot must NOT have fired.
	time.Sleep(300 * time.Millisecond)
	if n := rec.commCount(); n != 1 {
		t.Fatalf("comm sink fired %d times, want exactly 1 (outside-set subscription delivered: %q)",
			n, strings.Join(rec.comm, " | "))
	}
	if n := rec.alertCount(); n != 1 {
		t.Fatalf("alert sink fired %d times, want 1", n)
	}
	if n := rec.topicCount(); n != 1 {
		t.Fatalf("topic sink fired %d times, want 1", n)
	}

	// The one-shot was gated before delivery: it was not consumed and
	// remains armed in the persistent store for a future
	// allow/enable.
	live := subStore.List("")
	if len(live) != 2 {
		t.Fatalf("subscriptions after gated event: %+v (want both still present, one-shot still armed)", live)
	}
	var oneShotLive bool
	for _, sub := range live {
		if sub.OneShot && len(sub.Filter.Kinds) == 1 && sub.Filter.Kinds[0] == "prd.needs_review" {
			oneShotLive = true
		}
	}
	if !oneShotLive {
		t.Fatalf("one-shot subscription for the gated kind was consumed; it must stay armed: %+v", live)
	}

	// (8) The terminal transition writes no notify-events change
	// entry: still exactly one entry per REAL change, none for the
	// no-op or the rejected sets.
	if n := aud.count(notifyChanged); n != 3 {
		t.Fatalf("final notify-events audit entries: %d, want 3 (details %v)", n, aud.entries(notifyChanged))
	}
}

type notifyEffective struct {
	Source string   `json:"source"`
	Events []string `json:"events"`
}

// fetchNotifyEffective decodes the notify_events_effective block of an
// Automaton GET response.
func fetchNotifyEffective(t *testing.T, rc *httptest.ResponseRecorder) notifyEffective {
	t.Helper()
	if rc.Code != http.StatusOK {
		t.Fatalf("GET automaton: got %d, want 200 (body %s)", rc.Code, rc.Body.String())
	}
	var body struct {
		NotifyEventsEffective *notifyEffective `json:"notify_events_effective"`
	}
	if err := json.Unmarshal(rc.Body.Bytes(), &body); err != nil {
		t.Fatalf("automaton body: %v", err)
	}
	if body.NotifyEventsEffective == nil {
		t.Fatalf("notify_events_effective missing from: %s", rc.Body.String())
	}
	return *body.NotifyEventsEffective
}

// auditRecorder captures Manager audit callback entries so the test
// can assert exactly which notify-events changes were written.
type auditRecorder struct {
	mu      sync.Mutex
	actions []string
	details []map[string]any
}

func (a *auditRecorder) fn(action string, details map[string]any) {
	a.mu.Lock()
	a.actions = append(a.actions, action)
	a.details = append(a.details, details)
	a.mu.Unlock()
}

func (a *auditRecorder) count(action string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, act := range a.actions {
		if act == action {
			n++
		}
	}
	return n
}

func (a *auditRecorder) entries(action string) []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []map[string]any{}
	for i, act := range a.actions {
		if act == action {
			out = append(out, a.details[i])
		}
	}
	return out
}

package autonomous

import (
	"strings"
	"sync"
	"testing"
	"time"
)

type laneFillAlertRecord struct{ level, title, body string }

type laneFillAlertSink struct {
	mu   sync.Mutex
	list []laneFillAlertRecord
}

func (s *laneFillAlertSink) add(level, title, body string) {
	s.mu.Lock()
	s.list = append(s.list, laneFillAlertRecord{level, title, body})
	s.mu.Unlock()
}

func (s *laneFillAlertSink) all() []laneFillAlertRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]laneFillAlertRecord(nil), s.list...)
}

// newLaneFillAlertHarness is a shadow harness with one ready Automaton that
// fits only the Claude lane, so claude-1 is the lane that waits.
func newLaneFillAlertHarness(t *testing.T) (*laneFillHarness, *laneFillAlertSink, *PRD) {
	t.Helper()
	h := newLaneFillHarness(t, "shadow")
	sink := &laneFillAlertSink{}
	h.c.alertFn = sink.add
	h.c.SetReady()
	p := h.addPRD("review the docs", PRDApproved, "claude", "claude-code")
	return h, sink, p
}

func (h *laneFillHarness) advancePoll(d time.Duration) {
	h.now = h.now.Add(d)
	h.poll()
}

func TestLaneFillAlertNoneBeforeThreshold(t *testing.T) {
	h, sink, _ := newLaneFillAlertHarness(t)
	h.poll()
	h.advancePoll(10 * time.Minute)
	h.advancePoll(19*time.Minute + 59*time.Second)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("alerted before the threshold: %+v", got)
	}
}

func TestLaneFillAlertExactlyOneAtThreshold(t *testing.T) {
	h, sink, _ := newLaneFillAlertHarness(t)
	h.poll()
	h.advancePoll(29 * time.Minute)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("alerted at 29 minutes: %+v", got)
	}
	h.advancePoll(time.Minute)
	got := sink.all()
	if len(got) != 1 {
		t.Fatalf("alerts at the threshold = %d, want 1: %+v", len(got), got)
	}
	if got[0].level != "warn" {
		t.Errorf("level = %q, want warn", got[0].level)
	}
}

func TestLaneFillAlertNotRepeatedWithinEpisode(t *testing.T) {
	h, sink, _ := newLaneFillAlertHarness(t)
	h.poll()
	for i := 0; i < 6; i++ {
		h.advancePoll(10 * time.Minute)
	}
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("alerts over one long episode = %d, want 1: %+v", len(got), got)
	}
}

func TestLaneFillAlertNewEpisodeAlertsAgain(t *testing.T) {
	h, sink, p := newLaneFillAlertHarness(t)
	h.poll()
	h.advancePoll(30 * time.Minute)
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("first episode alerts = %d, want 1", len(got))
	}

	// No work is eligible: the episode ends.
	h.m.store.MutatePRD(p.ID, func(x *PRD) bool { x.Status = PRDCancelled; return true })
	h.advancePoll(time.Minute)

	// New work shows up: a new episode begins and alerts after its own wait.
	h.addPRD("second job", PRDApproved, "claude", "claude-code")
	h.advancePoll(time.Minute)
	h.advancePoll(29 * time.Minute)
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("alerted before the new episode's threshold: %+v", got)
	}
	h.advancePoll(time.Minute)
	got := sink.all()
	if len(got) != 2 {
		t.Fatalf("alerts after a new episode = %d, want 2: %+v", len(got), got)
	}
	if !strings.Contains(got[1].body, "second job") {
		t.Errorf("second alert does not name the new work: %q", got[1].body)
	}
}

func TestLaneFillAlertThresholdZeroDisables(t *testing.T) {
	h, sink, _ := newLaneFillAlertHarness(t)
	cfg := h.m.Config()
	cfg.LaneFill.IdleAlertMinutes = 0
	h.m.SetConfig(cfg)
	h.poll()
	h.advancePoll(10 * time.Hour)
	h.advancePoll(10 * time.Hour)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("threshold 0 still alerted: %+v", got)
	}
	if st := h.status(); len(st.Alerts) != 0 {
		t.Fatalf("threshold 0 recorded alerts: %+v", st.Alerts)
	}
}

func TestLaneFillAlertThresholdReadLive(t *testing.T) {
	h, sink, _ := newLaneFillAlertHarness(t)
	h.poll()
	h.advancePoll(5 * time.Minute)
	cfg := h.m.Config()
	cfg.LaneFill.IdleAlertMinutes = 5
	h.m.SetConfig(cfg)
	h.advancePoll(time.Minute)
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("a lowered threshold did not apply on the next poll: %+v", got)
	}
}

func TestLaneFillAlertNamesLaneAndWork(t *testing.T) {
	h, sink, p := newLaneFillAlertHarness(t)
	h.poll()
	h.advancePoll(45 * time.Minute)
	got := sink.all()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want 1", len(got))
	}
	if got[0].title != "Lane claude-1 idle with work waiting" {
		t.Errorf("title = %q", got[0].title)
	}
	for _, want := range []string{"claude-1", "45 minutes", "review the docs", p.ID} {
		if !strings.Contains(got[0].body, want) {
			t.Errorf("body %q lacks %q", got[0].body, want)
		}
	}

	// The status keeps the alert with its time, lane and work.
	st := h.status()
	if len(st.Alerts) != 1 {
		t.Fatalf("status alerts = %+v", st.Alerts)
	}
	a := st.Alerts[0]
	if !a.At.Equal(h.now) || a.LaneID != "claude-1" || !strings.Contains(a.Work, "review the docs") || !strings.Contains(a.Work, p.ID) {
		t.Errorf("status alert = %+v", a)
	}
}

func TestLaneFillAlertOccupiedLaneClearsEpisode(t *testing.T) {
	h, sink, p := newLaneFillAlertHarness(t)
	other := h.addPRD("other job", PRDApproved, "claude", "claude-code")
	h.poll()
	h.advancePoll(25 * time.Minute)

	// The lane gets an occupant: the episode ends.
	h.m.store.MutatePRD(p.ID, func(x *PRD) bool { x.Status = PRDRunning; return true })
	h.advancePoll(time.Minute)
	h.advancePoll(20 * time.Minute)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("an occupied lane alerted: %+v", got)
	}

	// The occupant leaves with work still waiting: the wait starts over.
	h.m.store.MutatePRD(p.ID, func(x *PRD) bool { x.Status = PRDCompleted; return true })
	h.advancePoll(time.Minute)
	h.advancePoll(29 * time.Minute)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("alerted before the new episode's threshold: %+v", got)
	}
	h.advancePoll(time.Minute)
	got := sink.all()
	if len(got) != 1 || !strings.Contains(got[0].body, other.ID) {
		t.Fatalf("alerts = %+v, want one naming %s", got, other.ID)
	}
}

func TestLaneFillAlertRecentAlertsBounded(t *testing.T) {
	h, _, _ := newLaneFillAlertHarness(t)
	for i := 0; i < laneFillAlertCap+10; i++ {
		h.c.lf.mu.Lock()
		h.c.lf.alerts = append([]LaneFillAlert{{At: h.now, LaneID: "claude-1"}}, h.c.lf.alerts...)
		h.c.lf.mu.Unlock()
	}
	h.poll()
	h.advancePoll(30 * time.Minute)
	st := h.status()
	if len(st.Alerts) != laneFillAlertCap {
		t.Fatalf("alerts kept = %d, want %d", len(st.Alerts), laneFillAlertCap)
	}
	if !st.Alerts[0].At.Equal(h.now) || st.Alerts[0].Work == "" {
		t.Errorf("newest alert is not first: %+v", st.Alerts[0])
	}
}

func TestLaneFillAlertOffModeForgetsEpisodes(t *testing.T) {
	h, sink, _ := newLaneFillAlertHarness(t)
	h.poll()
	h.advancePoll(20 * time.Minute)
	h.setMode("off")
	h.advancePoll(time.Hour)
	h.setMode("shadow")
	h.advancePoll(time.Minute)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("an episode survived off mode: %+v", got)
	}
}

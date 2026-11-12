package observer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
)

type silenceSink struct{ got []*alerts.Alert }

func (s *silenceSink) AddSystem(l alerts.Level, title, body string) *alerts.Alert {
	a := &alerts.Alert{Level: l, Title: title, Body: body}
	s.got = append(s.got, a)
	return a
}

var silenceT0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func silenceSetup(mins int, peers ...*PeerEntry) (*PeerSilenceWatcher, *PeerRegistry, *silenceSink) {
	reg := &PeerRegistry{peers: map[string]*PeerEntry{}}
	for _, p := range peers {
		reg.peers[p.Name] = p
	}
	sink := &silenceSink{}
	w := NewPeerSilenceWatcher(reg, sink, func() int { return mins }, func() time.Time { return silenceT0 })
	return w, reg, sink
}

func TestPeerSilenceBelowThresholdNoAlert(t *testing.T) {
	w, _, s := silenceSetup(10, &PeerEntry{Name: "a", RegisteredAt: silenceT0, LastPushAt: silenceT0})
	w.Check(silenceT0.Add(9 * time.Minute))
	if len(s.got) != 0 {
		t.Fatalf("alerts: %d", len(s.got))
	}
}

func TestPeerSilenceAboveThresholdOneAlert(t *testing.T) {
	w, _, s := silenceSetup(10, &PeerEntry{Name: "a", RegisteredAt: silenceT0, LastPushAt: silenceT0})
	w.Check(silenceT0.Add(25 * time.Minute))
	if len(s.got) != 1 {
		t.Fatalf("alerts: %d", len(s.got))
	}
	a := s.got[0]
	if !strings.Contains(a.Title, "a") || !strings.Contains(a.Body, "25m0s") || !strings.Contains(a.Body, "2026-01-01T12:00:00Z") {
		t.Fatalf("bad alert: %+v", a)
	}
}

func TestPeerSilenceNoRepeatAcrossPolls(t *testing.T) {
	w, _, s := silenceSetup(10, &PeerEntry{Name: "a", RegisteredAt: silenceT0, LastPushAt: silenceT0})
	for i := 11; i < 60; i += 5 {
		w.Check(silenceT0.Add(time.Duration(i) * time.Minute))
	}
	if len(s.got) != 1 {
		t.Fatalf("alerts: %d", len(s.got))
	}
}

func TestPeerSilenceRecovery(t *testing.T) {
	w, reg, s := silenceSetup(10, &PeerEntry{Name: "a", RegisteredAt: silenceT0, LastPushAt: silenceT0})
	w.Check(silenceT0.Add(20 * time.Minute))
	reg.peers["a"].LastPushAt = silenceT0.Add(21 * time.Minute)
	w.Check(silenceT0.Add(22 * time.Minute))
	w.Check(silenceT0.Add(23 * time.Minute))
	if len(s.got) != 2 || s.got[1].Level != alerts.LevelInfo || !strings.Contains(s.got[1].Title, "recovered") {
		t.Fatalf("alerts: %+v", s.got)
	}
}

func TestPeerSilenceNewOutageAfterRecovery(t *testing.T) {
	w, reg, s := silenceSetup(10, &PeerEntry{Name: "a", RegisteredAt: silenceT0, LastPushAt: silenceT0})
	w.Check(silenceT0.Add(20 * time.Minute))
	reg.peers["a"].LastPushAt = silenceT0.Add(21 * time.Minute)
	w.Check(silenceT0.Add(22 * time.Minute))
	w.Check(silenceT0.Add(40 * time.Minute))
	if len(s.got) != 3 || s.got[2].Level != alerts.LevelWarn {
		t.Fatalf("alerts: %d", len(s.got))
	}
}

func TestPeerSilenceThresholdZeroDisables(t *testing.T) {
	w, _, s := silenceSetup(0, &PeerEntry{Name: "a", RegisteredAt: silenceT0, LastPushAt: silenceT0})
	w.Check(silenceT0.Add(240 * time.Hour))
	if len(s.got) != 0 {
		t.Fatalf("alerts: %d", len(s.got))
	}
}

func TestPeerSilenceNeverPushedPeer(t *testing.T) {
	w, _, s := silenceSetup(10, &PeerEntry{Name: "a", RegisteredAt: silenceT0})
	w.Check(silenceT0.Add(5 * time.Minute))
	if len(s.got) != 0 {
		t.Fatal("alerted too early")
	}
	w.Check(silenceT0.Add(15 * time.Minute))
	if len(s.got) != 1 || !strings.Contains(s.got[0].Body, "never") {
		t.Fatalf("alerts: %+v", s.got)
	}
}

func TestPeerSilenceSelfPeerIgnored(t *testing.T) {
	w, _, s := silenceSetup(10,
		&PeerEntry{Name: "self", RegisteredAt: silenceT0},
		&PeerEntry{Name: "me", RegisteredAt: silenceT0})
	w.SetSelfName("me")
	w.Check(silenceT0.Add(time.Hour))
	if len(s.got) != 0 {
		t.Fatalf("alerts: %d", len(s.got))
	}
}

func TestPeerSilenceRunLoop(t *testing.T) {
	w, _, _ := silenceSetup(10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx, time.Millisecond); close(done) }()
	cancel()
	<-done
}

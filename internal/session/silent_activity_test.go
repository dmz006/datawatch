package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// A session that is printing, or sending channel events, is not silent, however
// long its record has gone unchanged.
func TestASessionWithOutputOrEventsIsNotCalledSilent(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	now := time.Now()
	old := now.Add(-time.Hour)
	dir := t.TempDir()
	add := func(id string, set func(s *Session)) *Session {
		s := addNamedHolder(t, mgr, f, id, id, StateRunning, idlePane)
		mgr.store.Mutate(func() {
			s.UpdatedAt = old
			if set != nil {
				set(s)
			}
		})
		return s
	}
	logFile := func(name string, at time.Time) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("step 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		return p
	}
	add("aa01", func(s *Session) { s.LogFile = logFile("printing.log", now.Add(-time.Second)) })
	add("aa02", func(s *Session) { s.LastChannelEventAt = now.Add(-time.Second) })
	add("aa03", func(s *Session) { // sealed output goes beside the log
		s.LogFile = logFile("sealed.log", old)
		logFile("sealed.log.enc", now.Add(-time.Second))
	})
	quiet := add("aa04", func(s *Session) {
		s.LogFile, s.LastChannelEventAt = logFile("quiet.log", old), old
	})
	n := watchNotices(mgr)
	mgr.CheckSilentSessions(now, 30*time.Minute)
	if len(n.got) != 1 || !strings.Contains(n.got[0], quiet.ID) {
		t.Fatalf("notices = %q, want one naming %s", n.got, quiet.ID)
	}
	for _, id := range []string{"aa01", "aa02", "aa03"} {
		if strings.Contains(n.got[0], id) {
			t.Errorf("%s is active and was called silent", id)
		}
	}
}

// The off switch is the existing setting: a negative value. 0 means the default.
func TestANegativeStaleTimeoutTurnsTheSilentNoticeOff(t *testing.T) {
	for yaml, want := range map[string]int{
		"session:\n  stale_timeout_seconds: -1\n": -1,
		"session:\n  stale_timeout_seconds: 0\n":  1800,
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Session.StaleTimeoutSeconds != want {
			t.Errorf("%q loaded as %d, want %d", yaml, cfg.Session.StaleTimeoutSeconds, want)
		}
	}
	mgr, f := newTestManagerWithFake(t)
	s := addNamedHolder(t, mgr, f, "aa01", "w", StateRunning, idlePane)
	mgr.store.Mutate(func() { s.UpdatedAt = time.Now().Add(-24 * time.Hour) })
	cfg := config.DefaultConfig()
	cfg.Session.StaleTimeoutSeconds = -1
	mgr.SetConfig(cfg)
	n := watchNotices(mgr)
	mgr.checkSilentSessions(time.Now())
	if len(n.got) != 0 {
		t.Fatalf("notices with the setting negative: %q", n.got)
	}
}

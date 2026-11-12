package server

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/secrets"
)

type vaultHealthHarness struct {
	m      *VaultHealthMonitor
	st     secrets.VaultStatus
	active bool
	thr    int
	now    time.Time
	store  *alerts.Store
}

func newVaultHealthHarness(t *testing.T) *vaultHealthHarness {
	t.Helper()
	store, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &vaultHealthHarness{active: true, thr: 60, now: time.Unix(1000, 0), store: store,
		st: secrets.VaultStatus{Reachable: false, Address: "https://vault.example:8200", LastError: "connection refused"}}
	h.m = &VaultHealthMonitor{
		Status:           func() (secrets.VaultStatus, bool) { return h.st, h.active },
		ThresholdSeconds: func() int { return h.thr },
		Alerts:           store,
		Now:              func() time.Time { return h.now },
	}
	return h
}

func (h *vaultHealthHarness) advance(d time.Duration) { h.now = h.now.Add(d); h.m.Check() }
func (h *vaultHealthHarness) count() int              { return len(h.store.List()) }

func TestVaultHealthNoAlertUnderThreshold(t *testing.T) {
	h := newVaultHealthHarness(t)
	h.m.Check()
	h.advance(59 * time.Second)
	if h.count() != 0 {
		t.Fatalf("alerts = %d, want 0", h.count())
	}
	h.st.Reachable = true
	h.advance(time.Second)
	if h.count() != 0 {
		t.Fatalf("short failure raised alerts: %d", h.count())
	}
}

func TestVaultHealthSingleAlertNoRepeatRecoveryAndNewOutage(t *testing.T) {
	h := newVaultHealthHarness(t)
	h.m.Check()
	h.advance(60 * time.Second)
	if h.count() != 1 {
		t.Fatalf("alerts at threshold = %d, want 1", h.count())
	}
	a := h.store.List()[0]
	if a.Level != alerts.LevelError || !strings.Contains(a.Body, "https://vault.example:8200") || !strings.Contains(a.Body, "connection refused") {
		t.Fatalf("bad alert: %+v", a)
	}
	h.advance(10 * time.Minute)
	h.advance(10 * time.Minute)
	if h.count() != 1 {
		t.Fatalf("repeat during outage: %d", h.count())
	}
	h.st.Reachable = true
	h.advance(time.Second)
	if h.count() != 2 {
		t.Fatalf("recovery alert missing: %d", h.count())
	}
	h.advance(time.Minute)
	if h.count() != 2 {
		t.Fatalf("extra alert while healthy: %d", h.count())
	}
	h.st.Reachable = false
	h.m.Check()
	h.advance(60 * time.Second)
	if h.count() != 3 {
		t.Fatalf("new outage did not alert: %d", h.count())
	}
}

func TestVaultHealthDisabledAndNotVault(t *testing.T) {
	h := newVaultHealthHarness(t)
	h.thr = 0
	h.m.Check()
	h.advance(time.Hour)
	if h.count() != 0 {
		t.Fatalf("threshold 0 alerted: %d", h.count())
	}
	h.thr = 60
	h.active = false
	h.m.Check()
	h.advance(time.Hour)
	if h.count() != 0 {
		t.Fatalf("non-Vault backend alerted: %d", h.count())
	}
}

func TestVaultHealthAlertRedactsTokens(t *testing.T) {
	h := newVaultHealthHarness(t)
	h.st.LastError = `Get failed: X-Vault-Token: hvs.CAESIFAKEFAKEFAKETOKEN123456 token=s.abcdefghijklmnopqrstuv000 secret="supersecretvalue99"`
	h.m.Check()
	h.advance(60 * time.Second)
	if h.count() != 1 {
		t.Fatalf("alerts = %d", h.count())
	}
	a := h.store.List()[0]
	txt := a.Title + a.Body
	for _, bad := range []string{"hvs.CAESIFAKE", "s.abcdefghijkl", "supersecretvalue99"} {
		if strings.Contains(txt, bad) {
			t.Fatalf("alert leaks %q: %s", bad, txt)
		}
	}
}

func TestVaultHealthStartStop(t *testing.T) {
	h := newVaultHealthHarness(t)
	h.m.Interval = time.Millisecond
	h.m.StartVaultHealthMonitor(t.Context())
	h.m.Stop()
	h.m.Stop()
}

// Vault health monitor — polls the active Vault secrets backend and raises a
// system alert when it has been unreachable continuously for the configured
// threshold (secrets.vault.unreachable_alert_after_seconds), plus one info
// alert on recovery. Uses the same alerts.Store.AddSystem path as the
// federation peer health monitor (peer_health.go).

package server

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/secrets"
)

const vaultHealthInterval = 15 * time.Second

// vaultTokenPattern catches Vault token shapes and token=/Bearer style
// assignments that the generic detectors may not flag.
var vaultTokenPattern = regexp.MustCompile(`(?i)\b(?:hv[sbr]\.[A-Za-z0-9_\-]{8,}|s\.[A-Za-z0-9]{16,})|(?:x-vault-token|token|secret|password|bearer)["']?\s*[:=]?\s*["']?[A-Za-z0-9._\-]{6,}`)

// scrubVaultText removes tokens and secret-looking values from error text.
func scrubVaultText(s string) string {
	s, _ = git.RedactSecrets(s)
	return vaultTokenPattern.ReplaceAllString(s, git.SecretRedactMarker)
}

// VaultHealthMonitor watches Vault reachability.
type VaultHealthMonitor struct {
	// Status returns the Vault status and whether Vault is the active backend.
	Status func() (secrets.VaultStatus, bool)
	// ThresholdSeconds returns the alert threshold; 0 disables.
	ThresholdSeconds func() int
	Alerts           *alerts.Store
	Now              func() time.Time // injectable clock
	Interval         time.Duration

	mu        sync.Mutex
	downSince time.Time
	alerted   bool
	cancel    context.CancelFunc
	done      chan struct{}
}

func (m *VaultHealthMonitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Check runs one poll cycle.
func (m *VaultHealthMonitor) Check() {
	if m.Alerts == nil || m.Status == nil {
		return
	}
	st, active := m.Status()
	threshold := 0
	if m.ThresholdSeconds != nil {
		threshold = m.ThresholdSeconds()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if !active || threshold <= 0 {
		m.downSince, m.alerted = time.Time{}, false
		return
	}
	if st.Reachable {
		if m.alerted {
			m.Alerts.AddSystem(alerts.LevelInfo, "Vault recovered",
				fmt.Sprintf("Vault at %s is reachable again.", st.Address))
		}
		m.downSince, m.alerted = time.Time{}, false
		return
	}
	now := m.now()
	if m.downSince.IsZero() {
		m.downSince = now
	}
	if m.alerted || now.Sub(m.downSince) < time.Duration(threshold)*time.Second {
		return
	}
	m.alerted = true
	m.Alerts.AddSystem(alerts.LevelError, "Vault unreachable",
		fmt.Sprintf("Vault at %s has been unreachable for %d seconds or more.\nLast error: %s",
			st.Address, threshold, scrubVaultText(st.LastError)))
}

// StartVaultHealthMonitor launches the polling loop; it exits on ctx cancel or Stop.
func (m *VaultHealthMonitor) StartVaultHealthMonitor(ctx context.Context) {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	done := m.done
	m.mu.Unlock()

	interval := m.Interval
	if interval <= 0 {
		interval = vaultHealthInterval
	}
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		m.Check()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.Check()
			}
		}
	}()
}

// Stop ends the loop and waits for it to exit.
func (m *VaultHealthMonitor) Stop() {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// StartVaultHealth builds a VaultHealthMonitor bound to this server's secrets
// store, alert store and live config (secrets.vault.unreachable_alert_after_seconds
// is re-read every poll, so config changes apply without a restart) and starts
// it. Each poll runs CheckHealth to refresh the cached status. Returns nil
// when no alert store is wired. Call after SetAlertStore and SetSecretsStore.
func (s *HTTPServer) StartVaultHealth(ctx context.Context) *VaultHealthMonitor {
	a := s.api
	if a == nil || a.alertStore == nil {
		return nil
	}
	m := &VaultHealthMonitor{
		Alerts: a.alertStore,
		Status: func() (secrets.VaultStatus, bool) {
			v, ok := a.secretsStore.(vaultBacked)
			if !ok {
				return secrets.VaultStatus{}, false
			}
			_ = v.CheckHealth()
			return v.Status(), true
		},
		ThresholdSeconds: func() int {
			if a.cfg == nil {
				return 0
			}
			return a.cfg.Secrets.Vault.EffectiveUnreachableAlertAfterSeconds()
		},
	}
	m.StartVaultHealthMonitor(ctx)
	return m
}

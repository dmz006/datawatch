// Package acme implements BL397 — the native ACME / Let's Encrypt
// subsystem. The daemon itself performs the ACME handshake and holds the
// cert lifecycle; the operator never leaves datawatch for certificate
// management. See
// docs/plans/2026-10-06-bl397-native-acme-letsencrypt.md for the full
// design and the operator-interview decision log this package implements.
//
// Scope (phase 1 only — see the plan doc's decision log): HTTP-01
// validation only (no DNS-01/dns_channel integration, that's phase 2);
// restart-based cert apply (writes the PEM, then calls the daemon's
// existing restart callback — no in-memory TLS hot-swap, that's phase 3).
package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
)

// Event is one entry in the Manager's issuer log — a bounded in-memory
// history of order/renewal state changes, distinct from Status() (current
// state only) and from the operator-facing alerts.Store (which holds
// every alert from every datawatch subsystem, not just ACME). Backs the
// acme_issuer_log MCP tool / CLI / comm verb.
type Event struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // "info" | "warn" | "error"
	Message string    `json:"message"`
	Detail  string    `json:"detail,omitempty"`
}

const maxIssuerLogEvents = 50

// DomainStatus is the renewal/issuance state of one domain's cert,
// returned by Manager.Status for the REST/MCP/CLI/comm/PWA surfaces.
type DomainStatus struct {
	Domain              string    `json:"domain"`
	Issued              bool      `json:"issued"`
	NotAfter            time.Time `json:"not_after,omitempty"`
	LastRenewal         time.Time `json:"last_renewal,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	InFlight            bool      `json:"in_flight"`
	Endpoint            string    `json:"endpoint"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
}

// Manager is the ACME subsystem. One Manager per datawatch instance,
// created at daemon startup when acme.enabled is true (or lazily on first
// enable via config save — callers decide; Manager itself just needs a
// valid AcmeConfig to construct).
type Manager struct {
	mu       sync.Mutex
	cfg      config.AcmeConfig
	dataDir  string
	account  *Account
	client   *lego.Client
	provider *httpProvider

	// restartFn is the daemon's existing self-restart callback (see
	// internal/server/api.go's handleRestart -> s.restartFn ->
	// syscall.Exec). Apply calls this directly after writing a new PEM
	// when cfg.Apply.HotSwap is false (phase 1's only mode) — there is no
	// separate "RESTART_FIELDS watcher" to wire into; see the BL397 plan
	// doc's "Implementation correction" note.
	restartFn func()

	status map[string]*DomainStatus // domain -> status
	events []Event                  // bounded ring buffer, see Event's doc comment
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// logEvent records an entry in the bounded issuer-log ring buffer and
// forwards it to the daemon's existing alert machinery (alerts.EmitSystem
// — no new transport, per the BL397 plan's design). The only severities
// are info/warn/error; "critical" from the PRD's alert table collapses to
// error, the highest level alerts.Level actually has.
func (m *Manager) logEvent(level alerts.Level, message, detail string) {
	m.mu.Lock()
	m.events = append(m.events, Event{Time: time.Now(), Level: string(level), Message: message, Detail: detail})
	if len(m.events) > maxIssuerLogEvents {
		m.events = m.events[len(m.events)-maxIssuerLogEvents:]
	}
	m.mu.Unlock()
	alerts.EmitSystem(level, message, detail)
}

// IssuerLog returns the bounded recent-event history (newest last).
// Backs the acme_issuer_log MCP tool / CLI / comm verb.
func (m *Manager) IssuerLog() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

// NewManager constructs a Manager. encKey is the same DWDAT2 derivation
// key every other encrypted store uses (nil for a non---secure daemon).
// restartFn may be nil (e.g. in tests); Apply then only writes the PEM
// and logs that a manual restart is needed.
func NewManager(cfg config.AcmeConfig, dataDir string, encKey []byte, restartFn func()) (*Manager, error) {
	if len(cfg.Domains) == 0 {
		return nil, errors.New("acme: at least one domain is required")
	}
	if cfg.Method != "http01" {
		return nil, fmt.Errorf("acme: method %q not supported in this build (phase 1 is http01-only)", cfg.Method)
	}

	account, err := LoadOrCreateAccount(dataDir, "", encKey)
	if err != nil {
		return nil, fmt.Errorf("acme account: %w", err)
	}

	m := &Manager{
		cfg:       cfg,
		dataDir:   dataDir,
		account:   account,
		provider:  newHTTPProvider(),
		restartFn: restartFn,
		status:    make(map[string]*DomainStatus),
		stopCh:    make(chan struct{}),
	}
	// Bug caught live 2026-10-06: status used to start blank on every
	// process restart (Issued: false), with nothing re-derived from the
	// cert actually on disk. Since Apply's own restart IS a process
	// restart, that produced an infinite loop: boot -> "not issued" ->
	// issue -> write PEM -> restart -> boot -> "not issued" (memory
	// lost) -> issue again -> ... Caught after 4 real orders against
	// staging before it was stopped. Fixed by checking the existing PEM
	// at startup and seeding Issued/NotAfter from it when present.
	existingIssued, existingNotAfter := loadExistingCertStatus(dataDir, cfg.Domains)
	for _, d := range cfg.Domains {
		m.status[d] = &DomainStatus{
			Domain:   d,
			Endpoint: cfg.Endpoint,
			Issued:   existingIssued,
			NotAfter: existingNotAfter,
		}
	}

	dirURL := directoryURL(cfg.Endpoint)
	needsRegister := !account.IsRegisteredFor(dirURL)
	if needsRegister {
		// Must clear BEFORE constructing the lego client below: lego
		// inspects GetRegistration() to decide whether to sign the
		// new-account request with an embedded JWK (correct here) or a
		// KeyID (only correct once already registered against THIS
		// directory). A stale cross-directory registration left in place
		// made lego pick the KeyID path and get a real 400 "No embedded
		// JWK in JWS header" from Let's Encrypt — found live during the
		// staging->production flip, one layer deeper than the
		// IsRegisteredFor bug this same flip first exposed.
		account.ClearRegistration()
	}

	legoCfg := lego.NewConfig(account)
	legoCfg.CADirURL = dirURL
	client, err := lego.NewClient(legoCfg)
	if err != nil {
		return nil, fmt.Errorf("acme client: %w", err)
	}
	if err := client.Challenge.SetHTTP01Provider(m.provider); err != nil {
		return nil, fmt.Errorf("acme http01 provider: %w", err)
	}
	m.client = client

	if needsRegister {
		// Bug found live 2026-10-06: staging and production are separate
		// ACME registries. A registration valid against one directory is
		// rejected by the other ("KeyID header contained an invalid
		// account URL"). Re-register (same account KEY, new registration)
		// whenever the configured endpoint doesn't match what this
		// account was last registered against — covers both the
		// first-ever-run case and a later staging<->production flip.
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("acme account registration: %w", err)
		}
		if err := account.SetRegistration(reg, dirURL); err != nil {
			return nil, fmt.Errorf("persist acme registration: %w", err)
		}
		m.logEvent(alerts.LevelInfo, "ACME account registered", dirURL)
	}

	return m, nil
}

func directoryURL(endpoint string) string {
	if endpoint == "production" {
		return lego.LEDirectoryProduction
	}
	return lego.LEDirectoryStaging
}

// loadExistingCertStatus checks for a cert already written at
// {dataDir}/tls/acme/<domains[0]>/fullchain.pem (applyCertificate's own
// write location, keyed off the first configured domain since all
// domains share one multi-SAN cert) and reports whether one exists and
// its expiry. Returns (false, zero) on any read/parse error — a missing
// or corrupt file is treated the same as "not issued yet", which is the
// safe default (triggers a fresh issue rather than silently trusting a
// cert this process can't verify).
func loadExistingCertStatus(dataDir string, domains []string) (bool, time.Time) {
	if len(domains) == 0 {
		return false, time.Time{}
	}
	path := filepath.Join(dataDir, "tls", "acme", domains[0], "fullchain.pem")
	data, err := os.ReadFile(path)
	if err != nil {
		return false, time.Time{}
	}
	leaf, err := parseLeafCert(data)
	if err != nil {
		return false, time.Time{}
	}
	return true, leaf.NotAfter
}

// ChallengeHandler returns the http.Handler the daemon registers on its
// existing mux at http01.PathPrefix ("/.well-known/acme-challenge/") —
// see provider.go's doc comment for why this isn't a second listener.
func (m *Manager) ChallengeHandler() http.Handler { return m.provider.Handler() }

// ChallengePathPrefix re-exports lego's well-known path prefix so callers
// wiring the mux don't need to import the lego package directly.
const ChallengePathPrefix = http01.PathPrefix

// Status returns a snapshot of every configured domain's cert state.
func (m *Manager) Status() []DomainStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DomainStatus, 0, len(m.status))
	for _, d := range m.cfg.Domains {
		if s, ok := m.status[d]; ok {
			out = append(out, *s)
		}
	}
	return out
}

// Start launches the background Renewer goroutine (checks every 6h,
// renewing any domain within RenewalDays of expiry). Call once at daemon
// startup after NewManager succeeds.
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.renewLoop()
}

// Stop signals the Renewer goroutine to exit and waits for it.
func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *Manager) renewLoop() {
	defer m.wg.Done()
	// Check once immediately on startup — without this, a freshly
	// enabled acme.enabled:true with no existing cert would sit idle for
	// up to 6h before ever issuing anything, which is wrong for both the
	// first-time-setup case and the "daemon restarted while a cert was
	// already close to expiry" case.
	m.checkAndRenewAll()

	const tick = 6 * time.Hour
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-t.C:
			m.checkAndRenewAll()
		}
	}
}

func (m *Manager) checkAndRenewAll() {
	for _, d := range m.cfg.Domains {
		m.mu.Lock()
		st := m.status[d]
		needsRenew := st != nil && (!st.Issued || time.Until(st.NotAfter) <= time.Duration(m.cfg.RenewalDays)*24*time.Hour)
		m.mu.Unlock()
		if needsRenew {
			if err := m.IssueNow(); err != nil {
				m.recordFailure(d, err)
			}
		}
	}
}

func (m *Manager) recordFailure(domain string, err error) {
	m.mu.Lock()
	st := m.status[domain]
	if st != nil {
		st.LastError = err.Error()
		st.ConsecutiveFailures++
		severity := alerts.LevelWarn
		if st.ConsecutiveFailures >= m.cfg.Retry.MaxConsecutiveFailures {
			severity = alerts.LevelError
		}
		m.mu.Unlock()
		m.logEvent(severity, "ACME order failed: "+domain, err.Error())
		return
	}
	m.mu.Unlock()
}

// IssueNow performs a single ACME order for every configured domain (one
// multi-SAN order, not one order per name — matches the PRD's "every
// name goes into ONE order" design). Used for both the initial issue and
// every renewal; a renewal with an unchanged identifier set is exempt
// from Let's Encrypt's new-order rate limit, so this is safe to call
// repeatedly.
func (m *Manager) IssueNow() error {
	m.mu.Lock()
	for _, d := range m.cfg.Domains {
		if st := m.status[d]; st != nil {
			st.InFlight = true
		}
	}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		for _, d := range m.cfg.Domains {
			if st := m.status[d]; st != nil {
				st.InFlight = false
			}
		}
		m.mu.Unlock()
	}()

	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate cert key: %w", err)
	}

	res, err := m.client.Certificate.Obtain(certificate.ObtainRequest{
		Domains:    m.cfg.Domains,
		PrivateKey: certKey,
		Bundle:     true,
	})
	if err != nil {
		return fmt.Errorf("obtain certificate: %w", err)
	}

	if err := m.applyCertificate(res); err != nil {
		return fmt.Errorf("apply certificate: %w", err)
	}
	return nil
}

// applyCertificate writes the issued PEM to {data_dir}/tls/acme/<name>/
// (reusing the existing auto-cert location every server.tls_cert/
// mcp.tls_cert path already points at), updates status, emits an alert,
// and triggers the daemon restart (phase 1's apply mechanism).
func (m *Manager) applyCertificate(res *certificate.Resource) error {
	name := m.cfg.Domains[0]
	dir := filepath.Join(m.dataDir, "tls", "acme", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := atomicWriteFile(filepath.Join(dir, "fullchain.pem"), res.Certificate, 0600); err != nil {
		return err
	}
	if err := atomicWriteFile(filepath.Join(dir, "privkey.pem"), res.PrivateKey, 0600); err != nil {
		return err
	}

	leaf, err := parseLeafCert(res.Certificate)
	if err != nil {
		return fmt.Errorf("parse issued cert: %w", err)
	}

	m.mu.Lock()
	now := time.Now()
	for _, d := range m.cfg.Domains {
		st := m.status[d]
		if st == nil {
			continue
		}
		st.Issued = true
		st.NotAfter = leaf.NotAfter
		st.LastRenewal = now
		st.LastError = ""
		st.ConsecutiveFailures = 0
	}
	m.mu.Unlock()

	m.logEvent(alerts.LevelInfo, "ACME certificate issued: "+name,
		fmt.Sprintf("expires %s", leaf.NotAfter.Format(time.RFC3339)))

	if m.cfg.Apply.HotSwap {
		// Phase 3, not built — see the BL397 plan doc. Falling through to
		// the restart path keeps phase-1 behavior correct even if a
		// config somehow sets hot_swap: true before phase 3 ships.
		m.logEvent(alerts.LevelWarn, "ACME hot_swap requested but not implemented",
			"falling back to restart-based apply (phase 3 is not built)")
	}
	if m.restartFn != nil {
		// Bug found live 2026-10-06: calling restartFn() synchronously
		// here raced the HTTP response when IssueNow was triggered via
		// POST /api/acme/renew — the process started exiting before the
		// response finished flushing, so the caller saw an empty/reset
		// response despite the renew succeeding. The existing
		// POST /api/restart handler already solved this exact race with
		// a short delay in a goroutine; mirrored here since applyCertificate
		// has the same callers-in-flight problem (REST, MCP, CLI, comm,
		// and the background Renewer itself).
		restartFn := m.restartFn
		go func() {
			time.Sleep(500 * time.Millisecond)
			restartFn()
		}()
	} else {
		m.logEvent(alerts.LevelWarn, "ACME certificate written, manual restart needed",
			"no restart callback was configured for this Manager")
	}
	return nil
}

func parseLeafCert(pemBytes []byte) (*x509.Certificate, error) {
	for {
		var block *pem.Block
		block, pemBytes = pem.Decode(pemBytes)
		if block == nil {
			return nil, errors.New("no CERTIFICATE block found")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
	}
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Peer certificate non-verification: operator-visible warning.
//
// A server / federation peer with tls_skip_verify set and no
// tls_cert_fingerprint pin is reachable without certificate verification.
// That state is accepted (self-signed peers are common), but every path
// that creates or updates such a peer must surface the one-line warning
// from multiserver.TLSWarning: in the response (a `warnings` array) and,
// once per peer per daemon run, as a warn-level system alert.

package server

import (
	"log"
	"sync"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// TLSPeerWarnTracker dedupes the alert side of the peer TLS warning: the
// one-line warning is logged on every Notify, but the emit hook (typically
// alerts.Store.AddSystem) fires at most once per peer name per daemon
// run. Shared by the REST create/update handlers and the daemon's
// startup sweep so a seeded peer warns exactly once either way it is
// seen.
type TLSPeerWarnTracker struct {
	mu   sync.Mutex
	seen map[string]struct{}
	// emit fires once per peer name, e.g.
	// alertStore.AddSystem(alerts.LevelWarn, "Peer certificate not verified", warning).
	// Nil means logging only.
	emit func(peerName, warning string)
}

// NewTLSPeerWarnTracker returns a tracker; emit may be nil.
func NewTLSPeerWarnTracker(emit func(peerName, warning string)) *TLSPeerWarnTracker {
	return &TLSPeerWarnTracker{seen: make(map[string]struct{}), emit: emit}
}

// Notify logs the warning for an entry whose certificate is not verified
// (tls_skip_verify set, no pin) and fires the emit hook for that peer on
// its first sighting only. No-op for nil entries or entries verified by
// CA chain or pinned.
func (t *TLSPeerWarnTracker) Notify(e *multiserver.Entry) {
	if t == nil || e == nil {
		return
	}
	warn := peerTLSWarning(e)
	if warn == "" {
		return
	}
	t.mu.Lock()
	if _, ok := t.seen[e.Name]; ok {
		t.mu.Unlock()
		return
	}
	t.seen[e.Name] = struct{}{}
	t.mu.Unlock()
	log.Printf("[servers] %s", warn)
	if t.emit != nil {
		t.emit(e.Name, warn)
	}
}

// peerTLSWarning is the one-line warning for a peer entry whose
// certificate is not verified; "" when verified by CA chain or pinned.
func peerTLSWarning(e *multiserver.Entry) string {
	if e == nil {
		return ""
	}
	return multiserver.TLSWarning(e.Name, e.TLSSkipVerify, e.TLSCertFingerprint)
}

// SetTLSPeerWarnTracker wires the peer certificate warning tracker into
// this server instance. Create/update handlers call it on every
// non-verified peer; nil (unwired) means warning text still appears in
// response bodies but no log/alert is produced.
func (s *Server) SetTLSPeerWarnTracker(t *TLSPeerWarnTracker) {
	s.tlsPeerWarn = t
}

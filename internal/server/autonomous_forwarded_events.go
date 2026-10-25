// BL427 (phase 5) — receiving endpoint for peer-forwarded lifecycle events.
// Design: docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (section 2(d) receiving daemon, section 3 loop prevention and audit).
//
//	POST /api/autonomous/events/forward   (autonomous.ForwardedEventsPath)
//
// The body is the envelope a peer's PeerForwarder sent (autonomous.ForwardedEnvelope).
// Answers:
//
//	202 {"accepted":true,"event_id":...}  the event was re-emitted to local subscriptions
//	400                                   malformed JSON, an unknown or oversized envelope,
//	                                      or a body that is not a forwarded envelope
//	401                                   a peer's request whose signature is missing or wrong
//	403                                   the caller lacks autonomous:write
//	405                                   any method but POST
//	409                                   a loop: this host originated the event, is already
//	                                      in its visited list, or the hop cap was reached
//	503                                   forwarding is disabled or the receiver is not wired
//
// Auth: the bearer token authenticates the peer (fedAuthMiddleware), and
// autonomous:write is required through fedCap. For a federated peer the
// X-Datawatch-Signature header must verify against the peer's own token
// (the HMAC key, as in the hop chain), so a captured body cannot be replayed
// outside the signature tolerance. An admin caller has no peer token and is
// not signature-checked: the operator is trusted here by the same rule as every
// other admin route.
//
// Every request this handler answers is audited (autonomous_lifecycle_forward_receive),
// accepted or refused. The response and the audit record never carry the bearer
// token or the signature.

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// forwardAuditFieldMax caps each peer-supplied string written to the audit log.
// Pending operator confirmation.
const forwardAuditFieldMax = 255

// ForwardedEventsAPI is the surface the receive endpoint needs. autonomous.Manager
// satisfies it; tests may substitute a fake.
type ForwardedEventsAPI interface {
	ForwardReceiveEnabled() bool
	ReceiveForwardedEvent(env autonomous.ForwardedEnvelope) error
}

// SetAutonomousForwardedEvents wires the receiving side of peer forwarding for
// ForwardedEventsPath. Nil (the default) makes the endpoint answer 503.
func (s *Server) SetAutonomousForwardedEvents(api ForwardedEventsAPI) {
	s.autonomousFwd = api
}

// forwardReceiveRecord is what one audit record of the endpoint carries. Fields
// the request did not reach stay empty.
type forwardReceiveRecord struct {
	outcome    string
	status     int
	reason     string
	peer       string
	eventID    string
	eventKind  string
	originHost string
	senderHost string
	hops       int
}

// auditForwardReceive writes the record of one answered request.
func (s *Server) auditForwardReceive(r *http.Request, rec forwardReceiveRecord) {
	s.audit(r.Context(), audit.ActionLifecycleForwardReceive, "lifecycle_forward", auditForwardText(rec.eventID), map[string]any{
		"outcome":     rec.outcome,
		"status_code": rec.status,
		"reason":      rec.reason,
		"peer":        auditForwardText(rec.peer),
		"event_id":    auditForwardText(rec.eventID),
		"event_kind":  auditForwardText(rec.eventKind),
		"origin_host": auditForwardText(rec.originHost),
		"sender_host": auditForwardText(rec.senderHost),
		"hops":        rec.hops,
	})
}

// auditForwardText caps a peer-supplied string before it reaches the audit log.
func auditForwardText(s string) string {
	if len(s) > forwardAuditFieldMax {
		return s[:forwardAuditFieldMax]
	}
	return s
}

// handleAutonomousForwardedEvents implements POST ForwardedEventsPath.
func (s *Server) handleAutonomousForwardedEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	peer := peerFromContext(r.Context())
	rec := forwardReceiveRecord{}
	if peer != nil {
		rec.peer = peer.Name
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		rec.outcome, rec.status, rec.reason = "refused", http.StatusForbidden, "the caller lacks autonomous:write"
		s.auditForwardReceive(r, rec)
		return
	}

	// refuse answers the request with code and text, and audits it.
	refuse := func(code int, reason string) {
		rec.outcome, rec.status, rec.reason = "refused", code, reason
		http.Error(w, reason, code)
		s.auditForwardReceive(r, rec)
	}

	if s.autonomousFwd == nil || !s.autonomousFwd.ForwardReceiveEnabled() {
		refuse(http.StatusServiceUnavailable, "forwarded events are not accepted by this daemon")
		return
	}

	// The body is read once, before any parse, so the signature covers the
	// exact bytes the peer sent.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, autonomous.WebhookMaxBodyBytes))
	if err != nil {
		refuse(http.StatusBadRequest, "the body is unreadable or exceeds the size cap")
		return
	}
	if peer != nil {
		sig := r.Header.Get(autonomous.WebhookSignatureHeader)
		if verr := autonomous.VerifyWebhookSignature([]byte(peer.Token), sig, body, time.Now(), autonomous.WebhookSignatureTolerance); verr != nil {
			refuse(http.StatusUnauthorized, "the request signature is missing or invalid")
			return
		}
	}

	var env autonomous.ForwardedEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		refuse(http.StatusBadRequest, "the body is not a valid forwarded envelope")
		return
	}
	rec.eventID = env.Event.ID
	rec.eventKind = env.Event.Kind
	rec.senderHost = env.Host
	if env.Forward != nil {
		rec.originHost = env.Forward.OriginHost
		rec.hops = env.Forward.Hops
	}

	if err := s.autonomousFwd.ReceiveForwardedEvent(env); err != nil {
		switch {
		case errors.Is(err, autonomous.ErrForwardReceiveDisabled):
			refuse(http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, autonomous.ErrForwardReceiveLoop):
			refuse(http.StatusConflict, err.Error())
		default:
			refuse(http.StatusBadRequest, err.Error())
		}
		return
	}

	rec.outcome, rec.status, rec.reason = "accepted", http.StatusAccepted, ""
	s.auditForwardReceive(r, rec)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "event_id": env.Event.ID})
}

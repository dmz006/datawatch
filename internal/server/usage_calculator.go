// usage_calculator.go — the shared usage-calculator REST helpers: the
// response shape of GET /api/usage/readings (account record plus
// decision-8's source info), the decision-9 source gate on appended
// readings, and the audit emitters for the record mutations (AGENT.md's
// Audit Logging Rule — one entry per request, emitted by the daemon that
// stores the write, so the actor is the real caller derived from ctx).

package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// usageCalcResourceType names the resource every usage-calculator audit
// entry attaches to.
const usageCalcResourceType = "usage_calculator"

// The federation capabilities the calculator's endpoints enforce, held
// here — the calculator's shared REST file — so the guards all read
// out of one place:
//
//	federation.CapUsageRead   (usage:read)   GET advice, GET readings
//	federation.CapUsageWrite  (usage:write)  POST/DELETE readings, the extra-
//	                                         credit and the remaining-work
//	                                         figure
var (
	usageCapabilityRead  = federation.CapUsageRead
	usageCapabilityWrite = federation.CapUsageWrite
)

// usageReadingsView is the body of a plain (non hop-marked)
// GET /api/usage/readings: the account record flattened, plus the source
// info — whether this daemon is the keeper, and on a peer whether the
// record was served from the last on-disk copy because the keeper could
// not be reached, and when that copy was taken. A peer's fetch
// (X-Usage-Hop: 1) still gets the bare AccountRecord, so the wire shape
// a peer decodes never gains fields it does not understand.
type usageReadingsView struct {
	usagecalc.AccountRecord
	IsKeeper    bool       `json:"is_keeper"`
	Stale       bool       `json:"stale,omitempty"`
	StaleReason string     `json:"stale_reason,omitempty"`
	CopiedAt    *time.Time `json:"copied_at,omitempty"`
}

// usageSourceGate applies decision 9's source kinds to an incoming read:
// the typed source is built (an empty source means typed); pasted and
// account are planned and answer 501; any other kind is a client error (400).
func usageSourceGate(source string) (int, string) {
	kind := strings.TrimSpace(source)
	switch kind {
	case "", usagecalc.SourceTyped:
		return http.StatusOK, ""
	case usagecalc.SourcePasted, usagecalc.SourceAccount:
		return http.StatusNotImplemented, "usage reading source " + kind + " is planned, not built yet"
	default:
		return http.StatusBadRequest, "unknown usage reading source: " + kind
	}
}

// auditUsageReadingAdded records a stored read (design 5, step 4): the
// read's kind, its figure(s), and its stamp.
func (s *Server) auditUsageReadingAdded(r *http.Request, rec usagecalc.Reading) {
	details := map[string]any{
		"source": rec.Source,
		"at":     rec.At.UTC().Format(time.RFC3339),
	}
	if rec.Percent != nil {
		details["percent"] = *rec.Percent
	}
	if rec.SpendUSD != nil {
		details["spend_usd"] = *rec.SpendUSD
	}
	if rec.SessionPercent != nil {
		details["session_percent"] = *rec.SessionPercent
	}
	// The stamp the keeper put on the reading, never a value the caller sent.
	if rec.Lanes != nil {
		details["lanes"] = *rec.Lanes
	}
	if rec.LanesSource != "" {
		details["lanes_source"] = rec.LanesSource
	}
	s.audit(r.Context(), audit.ActionUsageReadingAdded, usageCalcResourceType, rec.ID, details)
}

// auditUsageReadingDeleted records a read taken off the shared record —
// the operator correcting a read they had typed in.
func (s *Server) auditUsageReadingDeleted(r *http.Request, id string) {
	s.audit(r.Context(), audit.ActionUsageReadingDeleted, usageCalcResourceType, id, nil)
}

// auditUsageExtraCreditSet records the bought extra credit being set
// (points, 0 or more).
func (s *Server) auditUsageExtraCreditSet(r *http.Request, points float64) {
	s.audit(r.Context(), audit.ActionUsageExtraCreditSet, usageCalcResourceType, "extra_credit", map[string]any{"points": points})
}

// auditUsageRemainingWorkSet records the daemon-local typed remaining-work
// figure being set, or cleared (points == nil → cleared: true).
func (s *Server) auditUsageRemainingWorkSet(r *http.Request, points *float64) {
	if points == nil {
		s.audit(r.Context(), audit.ActionUsageRemainingWorkSet, usageCalcResourceType, "remaining_work", map[string]any{"cleared": true})
		return
	}
	s.audit(r.Context(), audit.ActionUsageRemainingWorkSet, usageCalcResourceType, "remaining_work", map[string]any{"points": *points})
}

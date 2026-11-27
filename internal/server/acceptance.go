// "Done when" lists (internal/acceptance): the REST surface.
//
//	GET  /api/acceptance               every list in short, or ?id=<request> for one
//	POST /api/acceptance/edit          create, change or fix a list
//	POST /api/acceptance/confirm       a person's answer on an item, or on "list"
//	POST /api/acceptance/accept        accept an item without proof
//	GET  /api/acceptance/report        the standing check (report only)
//	GET  /api/acceptance/catalog       methods, default items, project check names
//
// Four actions only: view, edit, confirm, accept. No route submits a verdict or
// evidence or sets an item met, and no route accepts command or script text.
// Each change records the name the request claims beside the caller datawatch
// observed from the credential. Who may change a list is recorded, not
// enforced beyond the route's capability.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/acceptance"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

func acceptanceMode(cfg *config.Config) string {
	if cfg != nil && cfg.Acceptance.Mode == acceptance.ModeReport {
		return acceptance.ModeReport
	}
	return acceptance.ModeOff
}

// SetAcceptance wires the "done when" service. secure marks an install whose
// stores are encrypted: chat then shows ids and results only.
func (s *Server) SetAcceptance(a *acceptance.Service, secure bool) {
	s.acceptance, s.acceptanceSecure = a, secure
}

// SetAcceptance wires the service on the HTTP server.
func (s *HTTPServer) SetAcceptance(a *acceptance.Service, secure bool) {
	if s.api != nil {
		s.api.SetAcceptance(a, secure)
	}
}

// AuditAcceptance writes one list change to the audit log: ids and callers
// only, never the ask, a statement or spoken words.
func (s *HTTPServer) AuditAcceptance(requestID string, e acceptance.Event) {
	if s.api != nil {
		s.api.auditAcceptance(requestID, e)
	}
}

func (s *Server) auditAcceptance(requestID string, e acceptance.Event) {
	if s.auditLog == nil {
		return
	}
	actor := e.By.Observed
	if actor == "" {
		actor = "datawatch"
	}
	_ = s.auditLog.Write(audit.Entry{
		Timestamp: e.At, Actor: actor, Action: audit.ActionAcceptanceChanged, Subject: requestID,
		Details: map[string]any{
			"change": e.Action, "item": e.Item, "claimed": e.By.Claimed, "observed": e.By.Observed,
			"after_freeze": e.AfterFreeze, "resource_type": "acceptance", "resource_id": requestID,
		},
	})
}

// acceptanceCallerFields is what a body may say about its caller. datawatch's
// model tools and command line send Via and ViaSession because they call with
// the daemon's own token. They are the request's own word and are not
// verified: who counts is recorded, not enforced, until the authentication
// work.
type acceptanceCallerFields struct {
	Actor      string `json:"actor,omitempty"`
	Via        string `json:"via,omitempty"`
	ViaSession string `json:"via_session,omitempty"`
}

// acceptanceCaller records the claimed name beside the observed caller.
func (s *Server) acceptanceCaller(ctx context.Context, f acceptanceCallerFields) acceptance.Caller {
	c := acceptance.Caller{Claimed: strings.TrimSpace(f.Actor)}
	if sessionCapsFromContext(ctx) != nil {
		id := ""
		if s.sessionTokens != nil {
			if tok := callerTokenFromContext(ctx); tok != "" {
				id = s.sessionTokens.SessionIDForToken(tok)
			}
		}
		if id == "" {
			// A session datawatch cannot name never counts as someone else.
			c.Observed, c.ViaTool = "session:unknown", true
			return c
		}
		c.Observed, c.SessionID = "session:"+id, id
		return c
	}
	if peer := peerFromContext(ctx); peer != nil {
		c.Observed, c.ViaTool = "peer:"+peer.Name, true
		return c
	}
	c.Observed = "admin-token"
	if via := strings.TrimSpace(f.Via); via != "" {
		c.Observed += " via " + via
		c.ViaTool = via == "mcp"
	}
	if id := strings.TrimSpace(f.ViaSession); id != "" {
		c.Observed += " from session:" + id
		c.SessionID = id
	}
	return c
}

// acceptanceItemInput is an item as a request may give it. It has no field for
// command or script text.
type acceptanceItemInput struct {
	ID             string                `json:"id,omitempty"`
	Statement      string                `json:"statement"`
	From           string                `json:"from,omitempty"`
	Source         string                `json:"source,omitempty"`
	Method         string                `json:"method"`
	ProjectCheck   string                `json:"project_check,omitempty"`
	Inputs         []string              `json:"inputs,omitempty"`
	Expect         []acceptance.Expect   `json:"expect,omitempty"`
	TimeoutSeconds int                   `json:"timeout_seconds,omitempty"`
	State          *acceptance.StateSpec `json:"state,omitempty"`
	Wait           *acceptance.WaitSpec  `json:"wait,omitempty"`
	Before         string                `json:"before,omitempty"`
	Change         bool                  `json:"change,omitempty"`
	Negative       bool                  `json:"negative,omitempty"`
	ReportOnly     bool                  `json:"report_only,omitempty"`
	Checker        string                `json:"checker,omitempty"`
	Effect         string                `json:"effect,omitempty"`
}

// item converts an input, keeping datawatch's own saved script of an existing
// command item: a request cannot send one.
func (in acceptanceItemInput) item(prev *acceptance.Item) acceptance.Item {
	it := acceptance.Item{
		ID: in.ID, Statement: in.Statement, From: in.From, Source: in.Source, Method: strings.TrimSpace(in.Method),
		State: in.State, Wait: in.Wait, Before: in.Before, Change: in.Change, Negative: in.Negative,
		ReportOnly: in.ReportOnly, Checker: in.Checker, Effect: in.Effect,
	}
	isCommand := it.Method == acceptance.MethodCommand || strings.HasPrefix(it.Method, "project:")
	if !isCommand {
		return it
	}
	check := acceptance.CheckSpec{ProjectCheck: in.ProjectCheck, Inputs: in.Inputs, Expect: in.Expect, TimeoutSec: in.TimeoutSeconds}
	if prev != nil && prev.Check != nil && prev.Check.Script != "" && in.ProjectCheck == "" && it.Method == acceptance.MethodCommand {
		check = *prev.Check
	}
	it.Check = &check
	return it
}

type acceptanceEditRequest struct {
	acceptanceCallerFields
	ID        string                `json:"id"`
	Source    string                `json:"source,omitempty"`
	Ask       string                `json:"ask,omitempty"`
	Scheduled bool                  `json:"scheduled,omitempty"`
	Packs     []string              `json:"packs,omitempty"`
	AddAsk    string                `json:"add_ask,omitempty"`
	Items     []acceptanceItemInput `json:"items,omitempty"`
	Remove    []string              `json:"remove,omitempty"`
	NeededBy  string                `json:"needed_by,omitempty"`
	Words     string                `json:"words,omitempty"`
	Hash      string                `json:"hash,omitempty"`
	// Freeze fixes the list named by Hash. It is sent alone, so what is fixed
	// is exactly what was read.
	Freeze bool `json:"freeze,omitempty"`
}

type acceptanceAnswerRequest struct {
	acceptanceCallerFields
	ID         string `json:"id"`
	Item       string `json:"item"`
	Answer     string `json:"answer,omitempty"` // yes or no (confirm)
	Reason     string `json:"reason,omitempty"` // accept
	Words      string `json:"words"`
	RelayedFor string `json:"relayed_for,omitempty"`
	Hash       string `json:"hash,omitempty"`
}

// acceptanceView is one list with how it reads now.
type acceptanceView struct {
	Mode    string             `json:"mode"`
	Secure  bool               `json:"secure"`
	List    *acceptance.List   `json:"list"`
	Rollup  acceptance.Rollup  `json:"rollup"`
	Flags   []acceptance.Flag  `json:"flags"`
	Changes acceptance.Changes `json:"changes"`
	Hash    string             `json:"hash"`
}

// hideCheckText replaces saved command and script text with their hashes, and
// drops the run cache: reading a list never hands out what datawatch runs.
func hideCheckText(items []acceptance.Item) {
	for i := range items {
		c := items[i].Check
		if c == nil {
			continue
		}
		cp := *c
		cp.Command, cp.Script = acceptance.TextDigest(cp.Command), acceptance.TextDigest(cp.Script)
		items[i].Check = &cp
	}
}

func (s *Server) acceptanceViewOf(l *acceptance.List) acceptanceView {
	checks := map[string]acceptance.ProjectCheck{}
	if s.cfg != nil {
		for name := range s.cfg.Acceptance.ProjectChecks {
			checks[name] = acceptance.ProjectCheck{}
		}
	}
	v := acceptanceView{
		Mode: acceptance.ModeReport, Secure: s.acceptanceSecure, Rollup: acceptance.Read(l),
		Flags: acceptance.Flags(l, checks, false), Changes: l.ChangesSinceFirst(), Hash: l.Hash(),
	}
	// The hash and the roll-up are computed before the text is hidden.
	hideCheckText(l.Items)
	for i := range l.Revisions {
		hideCheckText(l.Revisions[i].Items)
	}
	hideCheckText(v.Changes.Removed)
	for _, st := range l.State {
		st.Runs, st.InfraTries = nil, nil
	}
	v.List = l
	return v
}

func acceptanceWriteErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, acceptance.ErrOff), errors.Is(err, acceptance.ErrListChanged):
		code = http.StatusConflict
	case errors.Is(err, acceptance.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, acceptance.ErrInvalid):
		code = http.StatusBadRequest
	}
	http.Error(w, err.Error(), code)
}

// acceptanceReady checks method, capability and wiring.
func (s *Server) acceptanceReady(w http.ResponseWriter, r *http.Request, method, capability string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if !s.fedCap(w, r, capability) {
		return false
	}
	if s.acceptance == nil {
		http.Error(w, "done-when lists are not available (see the daemon log)", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func acceptanceDecode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	// An unknown field is refused, so a "command" or "script" is never
	// silently dropped and never accepted.
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) handleAcceptance(w http.ResponseWriter, r *http.Request) {
	if !s.acceptanceReady(w, r, http.MethodGet, federation.CapAutonomousRead) {
		return
	}
	if !s.acceptance.On() {
		// A plain answer, so a client can tell "off" from "broken".
		writeJSONOK(w, map[string]any{"mode": acceptance.ModeOff})
		return
	}
	if id := r.URL.Query().Get("id"); id != "" {
		l, err := s.acceptance.Get(id)
		if err != nil {
			acceptanceWriteErr(w, err)
			return
		}
		writeJSONOK(w, s.acceptanceViewOf(l))
		return
	}
	ov, err := s.acceptance.Overview()
	if err != nil {
		acceptanceWriteErr(w, err)
		return
	}
	writeJSONOK(w, map[string]any{"mode": acceptance.ModeReport, "secure": s.acceptanceSecure, "overview": ov})
}

func (s *Server) handleAcceptanceEdit(w http.ResponseWriter, r *http.Request) {
	if !s.acceptanceReady(w, r, http.MethodPost, federation.CapAutonomousWrite) {
		return
	}
	var req acceptanceEditRequest
	if !acceptanceDecode(w, r, &req) {
		return
	}
	by := s.acceptanceCaller(r.Context(), req.acceptanceCallerFields)
	if req.Freeze {
		if req.Ask != "" || req.AddAsk != "" || len(req.Items) > 0 || len(req.Remove) > 0 || req.NeededBy != "" {
			http.Error(w, "freeze is sent on its own, with the hash of the list that was read", http.StatusBadRequest)
			return
		}
		l, err := s.acceptance.Freeze(r.Context(), req.ID, req.Hash, by, req.Words)
		if err != nil {
			acceptanceWriteErr(w, err)
			return
		}
		writeJSONOK(w, s.acceptanceViewOf(l))
		return
	}
	in := acceptance.EditInput{
		RequestID: req.ID, Source: req.Source, Ask: req.Ask, Scheduled: req.Scheduled, Packs: req.Packs,
		AddAsk: req.AddAsk, Remove: req.Remove, Words: req.Words, Hash: req.Hash,
	}
	if req.NeededBy != "" {
		t, err := time.Parse(time.RFC3339, req.NeededBy)
		if err != nil {
			http.Error(w, "bad request: needed_by must be an RFC 3339 time", http.StatusBadRequest)
			return
		}
		in.NeededBy = &t
	}
	var prev *acceptance.List
	if l, err := s.acceptance.Get(req.ID); err == nil {
		prev = l
	}
	for _, item := range req.Items {
		var old *acceptance.Item
		if prev != nil {
			for i := range prev.Items {
				if prev.Items[i].ID == item.ID {
					old = &prev.Items[i]
				}
			}
		}
		in.Upsert = append(in.Upsert, item.item(old))
	}
	l, err := s.acceptance.Edit(r.Context(), in, by)
	if err != nil {
		acceptanceWriteErr(w, err)
		return
	}
	writeJSONOK(w, s.acceptanceViewOf(l))
}

func (s *Server) handleAcceptanceAnswer(w http.ResponseWriter, r *http.Request, accept bool) {
	if !s.acceptanceReady(w, r, http.MethodPost, federation.CapAutonomousWrite) {
		return
	}
	var req acceptanceAnswerRequest
	if !acceptanceDecode(w, r, &req) {
		return
	}
	by := s.acceptanceCaller(r.Context(), req.acceptanceCallerFields)
	var l *acceptance.List
	var err error
	if accept {
		l, err = s.acceptance.Accept(req.ID, req.Item, req.Reason, req.Words, req.Hash, by)
	} else {
		answer := strings.ToLower(strings.TrimSpace(req.Answer))
		if answer != "yes" && answer != "no" {
			http.Error(w, `bad request: answer must be "yes" or "no"`, http.StatusBadRequest)
			return
		}
		l, err = s.acceptance.Confirm(req.ID, req.Item, answer == "yes", req.Words, req.RelayedFor, req.Hash, by)
	}
	if err != nil {
		acceptanceWriteErr(w, err)
		return
	}
	writeJSONOK(w, s.acceptanceViewOf(l))
}

func (s *Server) handleAcceptanceConfirm(w http.ResponseWriter, r *http.Request) {
	s.handleAcceptanceAnswer(w, r, false)
}

func (s *Server) handleAcceptanceAccept(w http.ResponseWriter, r *http.Request) {
	s.handleAcceptanceAnswer(w, r, true)
}

func (s *Server) handleAcceptanceReport(w http.ResponseWriter, r *http.Request) {
	if !s.acceptanceReady(w, r, http.MethodGet, federation.CapAutonomousRead) {
		return
	}
	rep, err := s.acceptance.Report()
	if err != nil {
		acceptanceWriteErr(w, err)
		return
	}
	writeJSONOK(w, rep)
}

func (s *Server) handleAcceptanceCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.acceptanceReady(w, r, http.MethodGet, federation.CapAutonomousRead) {
		return
	}
	c, err := s.acceptance.Catalog()
	if err != nil {
		acceptanceWriteErr(w, err)
		return
	}
	writeJSONOK(w, c)
}

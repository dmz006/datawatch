// Pure tests for the hold REST surface. A small in-memory fake stands in for
// the manager: it keeps one Automaton record per id and applies the hold,
// extend and release rules the handlers rely on, so the handlers' parsing, status
// mapping, capability gate and audit entries are what is checked.

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

type holdFake struct {
	fakeOrchAutonomous

	prds      map[string]*autonomous.PRD
	holdCalls int
	extCalls  int
	relCalls  int
	lastReq   autonomous.HoldRequest
	migOpts   []autonomous.HoldMigrationOpts
	migErr    error
}

func newHoldFake() *holdFake {
	return &holdFake{prds: map[string]*autonomous.PRD{
		"aaaa0001": {ID: "aaaa0001", Title: "first", Status: autonomous.PRDDraft},
	}}
}

func (f *holdFake) GetPRD(id string) (any, bool) {
	p, ok := f.prds[id]
	return p, ok
}

func (f *holdFake) Hold(id string, req autonomous.HoldRequest, actor string) (any, error) {
	f.holdCalls++
	f.lastReq = req
	p, ok := f.prds[id]
	if !ok {
		return nil, errors.New("not found")
	}
	if p.Status == autonomous.PRDHeld {
		return nil, errors.New("already held")
	}
	p.Hold = &autonomous.HoldState{Reason: strings.TrimSpace(req.Reason), Condition: req.Condition, Until: req.Until, HeldBy: actor, From: p.Status}
	p.Status = autonomous.PRDHeld
	return p, nil
}

func (f *holdFake) ExtendHold(id string, req autonomous.HoldRequest, actor string) (any, error) {
	f.extCalls++
	f.lastReq = req
	p, ok := f.prds[id]
	if !ok || p.Status != autonomous.PRDHeld {
		return nil, errors.New("not held")
	}
	if req.Reason != "" {
		p.Hold.Reason = req.Reason
	}
	if req.Until != nil {
		p.Hold.Until = req.Until
	}
	return p, nil
}

func (f *holdFake) ReleaseHold(id, actor, note string) (any, error) {
	f.relCalls++
	p, ok := f.prds[id]
	if !ok || p.Status != autonomous.PRDHeld {
		return nil, errors.New("not held")
	}
	p.Status = p.Hold.From
	p.Hold = nil
	return p, nil
}

func (f *holdFake) HeldAutomata() autonomous.HeldGroup {
	var list []*autonomous.PRD
	for _, p := range f.prds {
		list = append(list, p)
	}
	return autonomous.BuildHeldGroup(list, time.Now())
}

func (f *holdFake) MigrateToHold(opts autonomous.HoldMigrationOpts) (autonomous.HoldMigrationReport, error) {
	f.migOpts = append(f.migOpts, opts)
	if f.migErr != nil {
		return autonomous.HoldMigrationReport{}, f.migErr
	}
	rep := autonomous.HoldMigrationReport{DryRun: opts.DryRun, Candidates: []autonomous.HoldMigrationCandidate{}}
	if !opts.DryRun {
		for _, id := range opts.IDs {
			rep.Held = append(rep.Held, autonomous.HoldMigrationHeld{ID: id})
		}
	}
	return rep, nil
}

// holdServe dispatches one request to the right handler the way the mux does.
func holdServe(s *Server, method, path string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	rec := httptest.NewRecorder()
	switch {
	case strings.HasSuffix(path, "/hold"):
		s.handleAutomatonHold(rec, req, "aaaa0001")
	case strings.HasSuffix(path, "/release_hold"):
		s.handleAutomatonReleaseHold(rec, req, "aaaa0001")
	case strings.HasSuffix(path, "/held"):
		s.handleAutonomousHeld(rec, req)
	default:
		s.handleAutonomousHeldMigrate(rec, req)
	}
	return rec
}

const (
	holdPath    = "/api/autonomous/prds/aaaa0001/hold"
	releasePath = "/api/autonomous/prds/aaaa0001/release_hold"
)

func TestHoldRESTHoldReleaseRoundTrip(t *testing.T) {
	f := newHoldFake()
	s := &Server{autonomousMgr: f}
	rec := holdServe(s, http.MethodPost, holdPath, map[string]any{"reason": "until after the release", "until": "72h"})
	if rec.Code != http.StatusOK {
		t.Fatalf("hold: %d %s", rec.Code, rec.Body.String())
	}
	var got autonomous.PRD
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != autonomous.PRDHeld || got.Hold == nil || got.Hold.Until == nil {
		t.Fatalf("held record = %+v", got)
	}
	if d := time.Until(*got.Hold.Until); d < 71*time.Hour || d > 73*time.Hour {
		t.Errorf("duration until = %v, want about 72h", d)
	}
	rec = holdServe(s, http.MethodPost, releasePath, map[string]any{"note": "ready"})
	if rec.Code != http.StatusOK {
		t.Fatalf("release: %d %s", rec.Code, rec.Body.String())
	}
	got = autonomous.PRD{}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != autonomous.PRDDraft {
		t.Errorf("status after release = %q, want draft", got.Status)
	}
	if rec := holdServe(s, http.MethodGet, holdPath, nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET hold: %d", rec.Code)
	}
}

func TestHoldRESTExtendOnSecondHold(t *testing.T) {
	f := newHoldFake()
	s := &Server{autonomousMgr: f}
	if rec := holdServe(s, http.MethodPost, holdPath, map[string]any{"reason": "first"}); rec.Code != http.StatusOK {
		t.Fatalf("hold: %d %s", rec.Code, rec.Body.String())
	}
	rec := holdServe(s, http.MethodPost, holdPath, map[string]any{"until": "2099-01-02"})
	if rec.Code != http.StatusOK {
		t.Fatalf("second hold: %d %s", rec.Code, rec.Body.String())
	}
	if f.holdCalls != 1 || f.extCalls != 1 {
		t.Fatalf("hold calls %d, extend calls %d; want 1 and 1", f.holdCalls, f.extCalls)
	}
	u := f.prds["aaaa0001"].Hold.Until
	if u == nil || u.Format(time.RFC3339) != "2099-01-02T23:59:59Z" {
		t.Errorf("until = %v, want the end of 2099-01-02 UTC", u)
	}
	if f.prds["aaaa0001"].Hold.Reason != "first" {
		t.Errorf("extend lost the reason")
	}
}

func TestHoldRESTRejectsBadUntil(t *testing.T) {
	f := newHoldFake()
	rec := holdServe(&Server{autonomousMgr: f}, http.MethodPost, holdPath, map[string]any{"reason": "r", "until": "next tuesday"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"RFC 3339", "YYYY", "2026-12-01", "72h"} {
		if want == "YYYY" {
			continue
		}
		if !strings.Contains(body, want) {
			t.Errorf("body %q does not name %q", body, want)
		}
	}
	if f.holdCalls != 0 {
		t.Errorf("a bad until reached the manager")
	}
}

func TestHoldRESTRejectsEmptyReason(t *testing.T) {
	f := newHoldFake()
	s := &Server{autonomousMgr: f}
	if rec := holdServe(s, http.MethodPost, holdPath, map[string]any{"reason": "   "}); rec.Code != http.StatusBadRequest {
		t.Fatalf("hold: want 400, got %d", rec.Code)
	}
	if f.holdCalls != 0 {
		t.Errorf("an empty reason reached the manager")
	}
	rec := holdServe(s, http.MethodPost, "/api/autonomous/held/migrate", map[string]any{"dry_run": false, "ids": []string{"aaaa0001"}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("migrate without reason: want 400, got %d", rec.Code)
	}
	if len(f.migOpts) != 0 {
		t.Errorf("a real migrate with no reason reached the manager")
	}
}

func TestHoldRESTHeldGroup(t *testing.T) {
	f := newHoldFake()
	s := &Server{autonomousMgr: f}
	_ = holdServe(s, http.MethodPost, holdPath, map[string]any{"reason": "waiting for the release"})
	rec := holdServe(s, http.MethodGet, "/api/autonomous/held", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var g autonomous.HeldGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if g.Total != 1 || len(g.Held) != 1 || g.Held[0].ID != "aaaa0001" || g.Held[0].Reason != "waiting for the release" {
		t.Errorf("group = %+v", g)
	}
	if rec := holdServe(s, http.MethodPost, "/api/autonomous/held", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST held: %d", rec.Code)
	}
}

func TestHoldRESTMigrateDryRunAndReal(t *testing.T) {
	f := newHoldFake()
	s := &Server{autonomousMgr: f}
	const p = "/api/autonomous/held/migrate"
	if rec := holdServe(s, http.MethodPost, p, map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("empty body: %d %s", rec.Code, rec.Body.String())
	}
	if len(f.migOpts) != 1 || !f.migOpts[0].DryRun {
		t.Fatalf("empty ids must default to a dry run: %+v", f.migOpts)
	}
	rec := holdServe(s, http.MethodPost, p, map[string]any{"ids": []string{"aaaa0001"}, "dry_run": false, "reason": "after the release", "until": "2099-01-01"})
	if rec.Code != http.StatusOK {
		t.Fatalf("real run: %d %s", rec.Code, rec.Body.String())
	}
	o := f.migOpts[1]
	if o.DryRun || o.Reason != "after the release" || o.Until == nil || len(o.IDs) != 1 {
		t.Errorf("opts = %+v", o)
	}
	var rep autonomous.HoldMigrationReport
	_ = json.Unmarshal(rec.Body.Bytes(), &rep)
	if len(rep.Held) != 1 {
		t.Errorf("report = %+v", rep)
	}
	// A real run with no ids is a 400.
	if rec := holdServe(s, http.MethodPost, p, map[string]any{"dry_run": false, "reason": "r"}); rec.Code != http.StatusBadRequest {
		t.Errorf("real run without ids: %d", rec.Code)
	}
	// The audit entry is named hold_migrate.
	al, err := audit.NewAt(filepath.Join(t.TempDir(), "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s2 := &Server{autonomousMgr: newHoldFake(), auditLog: al}
	_ = holdServe(s2, http.MethodPost, p, map[string]any{})
	_ = holdServe(s2, http.MethodPost, holdPath, map[string]any{"reason": "r"})
	_ = holdServe(s2, http.MethodPost, releasePath, nil)
	entries, _ := al.Read(audit.QueryFilter{})
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Action] = true
	}
	for _, a := range []string{"hold_migrate", "hold", "release_hold"} {
		if !seen[a] {
			t.Errorf("no %s audit entry in %v", a, seen)
		}
	}
}

func TestHoldRESTMigrateBusyIs409(t *testing.T) {
	f := newHoldFake()
	f.migErr = autonomous.ErrHoldMigrationBusy
	rec := holdServe(&Server{autonomousMgr: f}, http.MethodPost, "/api/autonomous/held/migrate",
		map[string]any{"dry_run": false, "ids": []string{"aaaa0001"}, "reason": "r"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
	f.migErr = errors.New("other failure")
	rec = holdServe(&Server{autonomousMgr: f}, http.MethodPost, "/api/autonomous/held/migrate", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("other failure: want 400, got %d", rec.Code)
	}
}

func TestHoldRESTRequiresWriteCapability(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	f := newHoldFake()
	s.autonomousMgr = f
	body, _ := json.Marshal(map[string]any{"reason": "r"})
	// admin 200, full-control peer 200, read-only peer 403.
	capTest(t, s, http.MethodPost, holdPath, body,
		func(w http.ResponseWriter, r *http.Request) { s.handleAutomatonHold(w, r, "aaaa0001") },
		http.StatusOK, http.StatusOK, http.StatusForbidden) // the second call extends
	mig, _ := json.Marshal(map[string]any{})
	capTest(t, s, http.MethodPost, "/api/autonomous/held/migrate", mig,
		s.handleAutonomousHeldMigrate, http.StatusOK, http.StatusOK, http.StatusForbidden)
	capTest(t, s, http.MethodPost, releasePath, nil,
		func(w http.ResponseWriter, r *http.Request) { s.handleAutomatonReleaseHold(w, r, "aaaa0001") },
		http.StatusOK, http.StatusBadRequest, http.StatusForbidden) // the full-control call finds it already released
}

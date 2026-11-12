// TestUsageCalc*: the audit trail of the usage-calculator record endpoints
// (AGENT.md's Audit Logging Rule, design 5 step 4, decision 9) — one entry
// per record mutation, emitted by the daemon that stores the write, with the
// real caller as actor; plus the decision-9 source gate and the
// decision-8 source info on the record view.

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// newUsageAuditServer wires a file-backed audit log onto a fresh
// keeper-only readings server (the same pattern the autonomy and HLLM-003
// audit tests use) and returns both; the log is closed at cleanup.
func newUsageAuditServer(t *testing.T) (*Server, *audit.Log) {
	t.Helper()
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	s.auditLog = al
	return s, al
}

// doUsage runs one request against the given handler and returns the
// recorder; used where the target handler is not the mux's single
// handleUsageReadings.
func doUsage(t *testing.T, s *Server, method, path string, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := withUsageCaps(httptest.NewRequest(method, path, r), federation.CapUsageRead, federation.CapUsageWrite)
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestUsageCalcAuditRecordMutations(t *testing.T) {
	s, al := newUsageAuditServer(t)

	// The remaining-work figure needs the advice service wired; the fresh
	// test server has no series peers, so the local store stands in.
	if err := s.AttachUsageCalculator(nil); err != nil {
		t.Fatalf("AttachUsageCalculator: %v", err)
	}

	// 1. POST a typed reading (the built source).
	rr := doUsage(t, s, http.MethodPost, "/api/usage/readings", jsonMarshalUsage(t, usagecalc.Reading{
		At:      time.Now().UTC().Add(-time.Hour),
		Percent: fptrUsage(61.5),
		Source:  usagecalc.SourceTyped,
	}), s.handleUsageReadings)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rr.Code, rr.Body.String())
	}

	// 2. PUT extra credit.
	if rr := doUsage(t, s, http.MethodPut, "/api/usage/extra-credit", `{"points": 25}`, s.handleUsageExtraCredit); rr.Code != http.StatusOK {
		t.Fatalf("PUT extra-credit = %d: %s", rr.Code, rr.Body.String())
	}

	// Find the reading's id for the delete.
	var rec usagecalc.AccountRecord
	if err := json.NewDecoder(doUsage(t, s, http.MethodGet, "/api/usage/readings", "", s.handleUsageReadings).Body).Decode(&rec); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if len(rec.Readings) != 1 {
		t.Fatalf("record = %+v, want one reading", rec)
	}
	readingID := rec.Readings[0].ID

	// 3. DELETE it through the operator form (?id=; the wire form is the
	// same operation in the peer's shape, both end in the same audit entry).
	if rr := doUsage(t, s, http.MethodDelete, "/api/usage/readings?id="+readingID, "", s.handleUsageReadings); rr.Code != http.StatusOK {
		t.Fatalf("DELETE ?id= = %d: %s", rr.Code, rr.Body.String())
	}

	// 4. PUT the remaining-work figure, then DELETE (clear) it.
	if rr := doUsage(t, s, http.MethodPut, "/api/usage/remaining-work", `{"points": 72.25}`, s.handleUsageRemainingWork); rr.Code != http.StatusOK {
		t.Fatalf("PUT remaining-work = %d: %s", rr.Code, rr.Body.String())
	}
	if rr := doUsage(t, s, http.MethodDelete, "/api/usage/remaining-work", "", s.handleUsageRemainingWork); rr.Code != http.StatusOK {
		t.Fatalf("DELETE remaining-work = %d: %s", rr.Code, rr.Body.String())
	}

	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	byAction := map[string][]audit.Entry{}
	for _, e := range entries {
		byAction[e.Action] = append(byAction[e.Action], e)
	}

	added := byAction[audit.ActionUsageReadingAdded]
	if len(added) != 1 {
		t.Fatalf("%s entries = %d, want exactly 1: %+v", audit.ActionUsageReadingAdded, len(added), entries)
	}
	if got := added[0].Details["resource_id"]; got != readingID {
		t.Errorf("added resource_id = %v, want the reading id %q", got, readingID)
	}
	if got := added[0].Details["source"]; got != usagecalc.SourceTyped {
		t.Errorf("added source = %v, want typed", got)
	}
	if got := added[0].Details["percent"]; got != float64(61.5) {
		t.Errorf("added percent = %v, want 61.5", got)
	}
	if got := added[0].Details["resource_type"]; got != "usage_calculator" {
		t.Errorf("added resource_type = %v, want usage_calculator", got)
	}

	deleted := byAction[audit.ActionUsageReadingDeleted]
	if len(deleted) != 1 {
		t.Fatalf("%s entries = %d, want exactly 1: %+v", audit.ActionUsageReadingDeleted, len(deleted), entries)
	}
	if got := deleted[0].Details["resource_id"]; got != readingID {
		t.Errorf("deleted resource_id = %v, want %q", got, readingID)
	}

	credit := byAction[audit.ActionUsageExtraCreditSet]
	if len(credit) != 1 {
		t.Fatalf("%s entries = %d, want exactly 1: %+v", audit.ActionUsageExtraCreditSet, len(credit), entries)
	}
	if got := credit[0].Details["points"]; got != float64(25) {
		t.Errorf("credit points = %v, want 25", got)
	}

	rew := byAction[audit.ActionUsageRemainingWorkSet]
	// Read is newest-first, so the clear comes before the set; assert
	// order-independently that one is the set and the other the clear.
	var sawSet, sawClear bool
	for _, e := range rew {
		if p, ok := e.Details["points"]; ok {
			if p != float64(72.25) {
				t.Errorf("remaining-work points = %v, want 72.25", p)
			}
			sawSet = true
		}
		if cleared, ok := e.Details["cleared"].(bool); ok && cleared {
			sawClear = true
		}
	}
	if !sawSet || !sawClear {
		t.Errorf("remaining-work entries = %+v, want a set (72.25) and a clear (cleared:true)", rew)
	}

	// No session in the test context: the actor falls back to "operator".
	for _, e := range entries {
		if e.Actor != "operator" {
			t.Errorf("%s actor = %q, want operator (no session in ctx)", e.Action, e.Actor)
		}
	}
}

func TestUsageCalcReadingsSourceGate(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")

	post := func(source string) *httptest.ResponseRecorder {
		body := jsonMarshalUsage(t, usagecalc.Reading{At: time.Now().UTC(), Percent: fptrUsage(50), Source: source})
		return doUsage(t, s, http.MethodPost, "/api/usage/readings", body, s.handleUsageReadings)
	}

	if rr := post(usagecalc.SourceTyped); rr.Code != http.StatusOK {
		t.Errorf("source typed = %d, want 200", rr.Code)
	}
	if rr := post(""); rr.Code != http.StatusOK {
		t.Errorf("empty source = %d, want 200 (typed is the default)", rr.Code)
	}
	if rr := post(usagecalc.SourcePasted); rr.Code != http.StatusNotImplemented {
		t.Errorf("source pasted = %d, want 501", rr.Code)
	}
	if rr := post(usagecalc.SourceAccount); rr.Code != http.StatusNotImplemented {
		t.Errorf("source account = %d, want 501", rr.Code)
	}
	if rr := post("crawled"); rr.Code != http.StatusBadRequest {
		t.Errorf("source crawled = %d, want 400", rr.Code)
	}
}

func TestUsageCalcReadingsViewSourceInfo(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")

	// Keeper view: is_keeper=true, no stale fields.
	rr := doUsage(t, s, http.MethodGet, "/api/usage/readings", "", s.handleUsageReadings)
	var view map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if got, _ := view["is_keeper"].(bool); !got {
		t.Errorf("is_keeper = %v, want true (this daemon is the keeper)", view["is_keeper"])
	}
	if _, present := view["stale"]; present {
		t.Errorf("fresh keeper record carries stale: %v", view)
	}

	// The hop-marked (peer fetch) body stays the bare record: no is_keeper.
	req := httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil)
	req.Header.Set(usagecalc.HeaderUsageHop, "1")
	req = withUsageCaps(req, federation.CapUsageRead)
	hopRr := httptest.NewRecorder()
	s.handleUsageReadings(hopRr, req)
	var hopView map[string]any
	if err := json.Unmarshal(hopRr.Body.Bytes(), &hopView); err != nil {
		t.Fatalf("decode hop view: %v", err)
	}
	if _, present := hopView["is_keeper"]; present {
		t.Errorf("hop-marked view gained is_keeper: %v", hopView)
	}
}

func jsonMarshalUsage(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

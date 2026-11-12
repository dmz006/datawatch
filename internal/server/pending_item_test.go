// Tests for the pending item REST surface: the read shape, create, attach,
// refresh, the method, disabled and capability guards, the config round trip
// of the nine pending item keys through PUT and GET /api/config (and the
// autonomous config route), the strict refusal of a bad value, and the
// insert_member body that names a pending item. All with fakes; no daemon.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// pendingItemFake serves canned pending items and Automations and records
// what the routes asked of it. It follows the manager's own rules for a blank
// title, a duplicate ref, an unknown item and a wrong state, so the tests see
// the status words the real manager writes.
type pendingItemFake struct {
	fakeOrchAutonomous
	autoCfg any
	items   []*autonomous.PendingItem
	autos   []any
	last    autonomous.PendingItemRefreshResult
	hasLast bool

	filter    autonomous.PendingItemFilter
	listCalls int
	created   []string // title|ref|actor
	attached  []string // item|automaton|actor
	refreshes int

	attachErr  error
	attachRes  *autonomous.PendingItemAttachResult
	refreshRes autonomous.PendingItemRefreshResult
}

func (f *pendingItemFake) Config() any { return f.autoCfg }

func (f *pendingItemFake) ListAutomations() []any { return f.autos }

func (f *pendingItemFake) ListPendingItems(flt autonomous.PendingItemFilter) []*autonomous.PendingItem {
	f.listCalls++
	f.filter = flt
	var out []*autonomous.PendingItem
	for _, it := range f.items {
		if flt.State == "" || it.State == flt.State {
			out = append(out, it)
		}
	}
	return out
}

func (f *pendingItemFake) CreatePendingItem(title, ref, actor string) (*autonomous.PendingItem, error) {
	f.created = append(f.created, title+"|"+ref+"|"+actor)
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("400: pending item title is required")
	}
	for _, it := range f.items {
		if ref != "" && it.Ref == ref {
			return nil, fmt.Errorf("409: an internal pending item with ref %q already exists (%s)", ref, it.ID)
		}
	}
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	it := &autonomous.PendingItem{
		ID: "c0ffee01", Ref: ref, Title: strings.TrimSpace(title), State: autonomous.PendingItemWaiting,
		Source: autonomous.PendingItemSourceInternal, CreatedAt: now, UpdatedAt: now,
	}
	f.items = append(f.items, it)
	return it, nil
}

func (f *pendingItemFake) AttachPendingItem(itemID, automatonID, actor string) (*autonomous.PendingItemAttachResult, error) {
	f.attached = append(f.attached, itemID+"|"+automatonID+"|"+actor)
	if f.attachErr != nil {
		return nil, f.attachErr
	}
	return f.attachRes, nil
}

func (f *pendingItemFake) RefreshPendingItems() autonomous.PendingItemRefreshResult {
	f.refreshes++
	return f.refreshRes
}

func (f *pendingItemFake) LastPendingItemRefresh() (autonomous.PendingItemRefreshResult, bool) {
	return f.last, f.hasLast
}

func pendingItemFixture() *pendingItemFake {
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	attachedAt := t0.Add(time.Hour)
	f := &pendingItemFake{
		items: []*autonomous.PendingItem{
			{ID: "aaaa1111", Ref: "REG-7", Title: "Wire the registry", State: autonomous.PendingItemWaiting,
				Source: autonomous.PendingItemSourceFile, CreatedAt: t0, UpdatedAt: t0},
			{ID: "bbbb2222", Title: "Tidy the docs", State: autonomous.PendingItemAttached, Source: autonomous.PendingItemSourceInternal,
				AutomatonID: "feed0001", CreatedAt: t0, UpdatedAt: attachedAt, AttachedAt: &attachedAt},
		},
	}
	blocked := map[string]any{
		"id": "a0000001", "name": "Release train", "status": "blocked", "current_member_id": "m2",
		"idle_since": t0.Add(2 * time.Hour), "updated_at": t0.Add(3 * time.Hour),
		"members": []map[string]any{
			{"id": "m1", "kind": "prd", "prd_id": "feed0002"},
			{"id": "m2", "kind": "pending_item", "pending_item_id": "aaaa1111"},
		},
		"decisions": []map[string]any{
			{"at": t0, "kind": "advance", "note": "moved on"},
			{"at": t0.Add(2 * time.Hour), "kind": "blocked", "note": "member \"Wire the registry\" is a pending item waiting for an Automaton to be attached (item aaaa1111); attach one to continue"},
		},
	}
	converted := map[string]any{
		"id": "a0000002", "name": "Docs pass", "status": "active", "current_member_id": "m9",
		"updated_at": t0,
		"members": []map[string]any{
			{"id": "m9", "kind": "prd", "prd_id": "feed0001", "pending_item_id": "bbbb2222"},
		},
	}
	f.autos = []any{blocked, converted}
	return f
}

func pendingItemDo(s *Server, method, path, body string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	var req *http.Request
	if rd != nil {
		req = httptest.NewRequest(method, path, rd)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	if req.URL.Path == "/api/autonomous/pending-items" {
		s.handlePendingItems(rec, req)
	} else {
		s.handlePendingItemsSub(rec, req)
	}
	return rec
}

func TestPendingItemReadShape(t *testing.T) {
	f := pendingItemFixture()
	f.autoCfg = map[string]any{"automation": map[string]any{
		"pending_item_policy":      "skip",
		"pending_item_auto_resume": false,
		"pending_item_source": map[string]any{
			"path": "docs/registry.md", "id_column": "ID", "done_values": []string{"shipped"},
		},
	}}
	f.last, f.hasLast = autonomous.PendingItemRefreshResult{
		Configured: true, Path: "docs/registry.md", At: time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC),
		Rows: 5, Added: 2, Updated: 1, Done: 1, Dropped: 0, Kept: 1, Skipped: 3, Error: "",
	}, true
	s := &Server{autonomousMgr: f}

	rec := pendingItemDo(s, http.MethodGet, "/api/autonomous/pending-items", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Policy     string `json:"policy"`
		AutoResume bool   `json:"auto_resume"`
		Source     struct {
			Configured   bool     `json:"configured"`
			Path         string   `json:"path"`
			IDPattern    string   `json:"id_pattern"`
			IDColumn     string   `json:"id_column"`
			TitleColumn  string   `json:"title_column"`
			StateColumn  string   `json:"state_column"`
			DoneValues   []string `json:"done_values"`
			OnMissingRow string   `json:"on_missing_row"`
		} `json:"source"`
		LastRefresh struct {
			Configured bool       `json:"configured"`
			At         *time.Time `json:"at"`
			Rows       int        `json:"rows"`
			Added      int        `json:"added"`
			Updated    int        `json:"updated"`
			Done       int        `json:"done"`
			Dropped    int        `json:"dropped"`
			Kept       int        `json:"kept"`
			Skipped    int        `json:"skipped"`
			Error      string     `json:"error"`
		} `json:"last_refresh"`
		Items []struct {
			ID          string `json:"id"`
			Ref         string `json:"ref"`
			Title       string `json:"title"`
			State       string `json:"state"`
			Source      string `json:"source"`
			AutomatonID string `json:"automaton_id"`
			Automations []struct {
				AutomationID string `json:"automation_id"`
				Name         string `json:"name"`
				MemberID     string `json:"member_id"`
				Held         bool   `json:"held"`
			} `json:"automations"`
		} `json:"items"`
		Held []struct {
			AutomationID string    `json:"automation_id"`
			Name         string    `json:"name"`
			MemberID     string    `json:"member_id"`
			ItemID       string    `json:"item_id"`
			Title        string    `json:"title"`
			Ref          string    `json:"ref"`
			Since        time.Time `json:"since"`
			Reason       string    `json:"reason"`
		} `json:"held"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if got.Policy != "skip" || got.AutoResume {
		t.Errorf("policy/auto_resume = %q/%v, want skip/false", got.Policy, got.AutoResume)
	}
	src := got.Source
	if !src.Configured || src.Path != "docs/registry.md" || src.IDColumn != "ID" ||
		src.IDPattern != `^\S+$` || src.TitleColumn != "2" || src.StateColumn != "" ||
		src.OnMissingRow != "keep" || len(src.DoneValues) != 1 || src.DoneValues[0] != "shipped" {
		t.Errorf("source = %+v", src)
	}
	lr := got.LastRefresh
	if !lr.Configured || lr.At == nil || lr.Rows != 5 || lr.Added != 2 || lr.Updated != 1 || lr.Done != 1 || lr.Kept != 1 || lr.Skipped != 3 {
		t.Errorf("last_refresh = %+v", lr)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2: %s", len(got.Items), rec.Body)
	}
	first := got.Items[0]
	if first.ID != "aaaa1111" || first.Ref != "REG-7" || first.State != "waiting" || first.Source != "file" ||
		len(first.Automations) != 1 || first.Automations[0].AutomationID != "a0000001" ||
		first.Automations[0].Name != "Release train" || first.Automations[0].MemberID != "m2" || !first.Automations[0].Held {
		t.Errorf("first item = %+v", first)
	}
	second := got.Items[1]
	if second.State != "attached" || second.AutomatonID != "feed0001" || second.Source != "internal" ||
		len(second.Automations) != 1 || second.Automations[0].Held {
		t.Errorf("second item = %+v", second)
	}
	if len(got.Held) != 1 {
		t.Fatalf("held = %d, want 1: %s", len(got.Held), rec.Body)
	}
	h := got.Held[0]
	if h.AutomationID != "a0000001" || h.Name != "Release train" || h.MemberID != "m2" || h.ItemID != "aaaa1111" ||
		h.Title != "Wire the registry" || h.Ref != "REG-7" || h.Since.IsZero() || !strings.Contains(h.Reason, "waiting for an Automaton") {
		t.Errorf("held = %+v", h)
	}

	// The state filter reaches the manager, and a bad word is a 400 naming the words.
	rec = pendingItemDo(s, http.MethodGet, "/api/autonomous/pending-items?state=waiting", "")
	if rec.Code != http.StatusOK || f.filter.State != autonomous.PendingItemWaiting {
		t.Errorf("state=waiting: status %d, filter %q", rec.Code, f.filter.State)
	}
	calls := f.listCalls
	rec = pendingItemDo(s, http.MethodGet, "/api/autonomous/pending-items?state=nope", "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "waiting") || !strings.HasPrefix(rec.Body.String(), "400:") {
		t.Errorf("state=nope: want 400 naming the states, got %d %q", rec.Code, rec.Body)
	}
	if f.listCalls != calls {
		t.Error("a refused state reached the manager")
	}

	// The automation_id filter keeps only that Automation's items and holds.
	rec = pendingItemDo(s, http.MethodGet, "/api/autonomous/pending-items?automation_id=a0000002", "")
	var narrowed struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Held []any `json:"held"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &narrowed); err != nil {
		t.Fatalf("decode narrowed: %v", err)
	}
	if len(narrowed.Items) != 1 || narrowed.Items[0].ID != "bbbb2222" || len(narrowed.Held) != 0 {
		t.Errorf("automation_id filter = %s", rec.Body)
	}

	// Lists are always arrays, never null.
	empty := &pendingItemFake{}
	rec = pendingItemDo(&Server{autonomousMgr: empty}, http.MethodGet, "/api/autonomous/pending-items", "")
	for _, want := range []string{`"items":[]`, `"held":[]`, `"policy":"hold"`, `"auto_resume":true`, `"done_values":["done","closed","shipped"]`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("empty read lacks %s: %s", want, rec.Body)
		}
	}
}

func TestPendingItemCreate(t *testing.T) {
	f := &pendingItemFake{}
	s := &Server{autonomousMgr: f}

	rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items", `{"title":"  Cache the lookups  ","ref":"X-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	var item struct {
		ID          string `json:"id"`
		Ref         string `json:"ref"`
		Title       string `json:"title"`
		State       string `json:"state"`
		Source      string `json:"source"`
		Automations []any  `json:"automations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if item.ID != "c0ffee01" || item.Ref != "X-1" || item.Title != "Cache the lookups" ||
		item.State != "waiting" || item.Source != "internal" || item.Automations == nil {
		t.Errorf("item = %+v", item)
	}
	if len(f.created) != 1 || f.created[0] != "  Cache the lookups  |X-1|" {
		t.Errorf("manager saw %q", f.created)
	}

	// A blank title is a 400, a body that is not JSON is a 400, a ref is optional.
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items", `{"title":"   "}`)
	if rec.Code != http.StatusBadRequest || !strings.HasPrefix(rec.Body.String(), "400:") {
		t.Errorf("blank title = %d %q, want 400", rec.Code, rec.Body)
	}
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items", `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400", rec.Code)
	}
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items", `{"title":"No ref"}`)
	if rec.Code != http.StatusCreated {
		t.Errorf("no ref = %d: %s", rec.Code, rec.Body)
	}
}

func TestPendingItemCreateConflict(t *testing.T) {
	f := pendingItemFixture()
	s := &Server{autonomousMgr: f}
	rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items", `{"title":"Another","ref":"REG-7"}`)
	if rec.Code != http.StatusConflict || !strings.HasPrefix(rec.Body.String(), "409:") {
		t.Fatalf("duplicate ref = %d %q, want 409 starting with 409:", rec.Code, rec.Body)
	}
	if len(f.items) != 2 {
		t.Errorf("a refused create changed the registry: %d items", len(f.items))
	}
}

func TestPendingItemAttach(t *testing.T) {
	f := pendingItemFixture()
	attachedAt := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	f.attachRes = &autonomous.PendingItemAttachResult{
		Item: &autonomous.PendingItem{
			ID: "aaaa1111", Title: "Wire the registry", State: autonomous.PendingItemAttached,
			Source: autonomous.PendingItemSourceFile, AutomatonID: "feed0003", AttachedAt: &attachedAt,
		},
		Converted: []autonomous.PendingItemAttachConversion{{AutomationID: "a0000001", MemberIDs: []string{"m2"}}},
		Resumed:   []string{"a0000001"},
	}
	s := &Server{autonomousMgr: f}

	rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/aaaa1111/attach", `{"automaton_id":"feed0003","actor":"operator"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach = %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Item struct {
			ID          string `json:"id"`
			State       string `json:"state"`
			AutomatonID string `json:"automaton_id"`
		} `json:"item"`
		Converted []struct {
			AutomationID string   `json:"automation_id"`
			MemberIDs    []string `json:"member_ids"`
		} `json:"converted_automations"`
		Resumed []string `json:"resumed_automations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if got.Item.ID != "aaaa1111" || got.Item.State != "attached" || got.Item.AutomatonID != "feed0003" ||
		len(got.Converted) != 1 || got.Converted[0].AutomationID != "a0000001" || got.Converted[0].MemberIDs[0] != "m2" ||
		len(got.Resumed) != 1 || got.Resumed[0] != "a0000001" {
		t.Errorf("body = %s", rec.Body)
	}
	if len(f.attached) != 1 || f.attached[0] != "aaaa1111|feed0003|operator" {
		t.Errorf("manager saw %q", f.attached)
	}

	// An attach that converted nothing still answers arrays, never null.
	f.attachRes = &autonomous.PendingItemAttachResult{Item: f.items[0]}
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/aaaa1111/attach", `{"automaton_id":"feed0003"}`)
	if !strings.Contains(rec.Body.String(), `"converted_automations":[]`) || !strings.Contains(rec.Body.String(), `"resumed_automations":[]`) {
		t.Errorf("empty attach body = %s", rec.Body)
	}
}

func TestPendingItemAttachNotFound(t *testing.T) {
	f := pendingItemFixture()
	s := &Server{autonomousMgr: f}

	f.attachErr = fmt.Errorf("404: pending item zzzz9999 not found")
	rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/zzzz9999/attach", `{"automaton_id":"feed0003"}`)
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Body.String(), "404:") {
		t.Errorf("unknown item = %d %q, want 404", rec.Code, rec.Body)
	}
	f.attachErr = fmt.Errorf("404: Automaton nope0000 not found")
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/aaaa1111/attach", `{"automaton_id":"nope0000"}`)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Automaton nope0000") {
		t.Errorf("unknown Automaton = %d %q, want 404", rec.Code, rec.Body)
	}
	f.attachErr = fmt.Errorf("400: an attach needs the id of the Automaton to attach")
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/aaaa1111/attach", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no automaton_id = %d, want 400", rec.Code)
	}
	// A path that is not one of the routes is a 404.
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/aaaa1111/detach", `{}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown sub-route = %d, want 404", rec.Code)
	}
}

func TestPendingItemAttachConflict(t *testing.T) {
	f := pendingItemFixture()
	s := &Server{autonomousMgr: f}
	for name, msg := range map[string]string{
		"wrong state":   "409: pending item bbbb2222 already has Automaton feed0001 attached",
		"owned already": "409: Automaton feed0002 already belongs to automation a0000001",
	} {
		f.attachErr = fmt.Errorf("%s", msg)
		rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/bbbb2222/attach", `{"automaton_id":"feed0002"}`)
		if rec.Code != http.StatusConflict || rec.Body.String() != msg+"\n" {
			t.Errorf("%s: = %d %q, want 409 with the manager's text", name, rec.Code, rec.Body)
		}
	}
	// An error without a status word is the daemon's fault: 500.
	f.attachErr = fmt.Errorf("disk full")
	rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/bbbb2222/attach", `{"automaton_id":"feed0002"}`)
	if rec.Code != http.StatusInternalServerError || !strings.HasPrefix(rec.Body.String(), "500:") {
		t.Errorf("plain error = %d %q, want 500", rec.Code, rec.Body)
	}
}

func TestPendingItemRefresh(t *testing.T) {
	f := &pendingItemFake{refreshRes: autonomous.PendingItemRefreshResult{
		Configured: true, Path: "docs/registry.md", At: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC),
		Rows: 4, Added: 2, Updated: 1, Done: 1, Dropped: 0, Kept: 0, Skipped: 1,
	}}
	s := &Server{autonomousMgr: f}
	rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d: %s", rec.Code, rec.Body)
	}
	var got autonomous.PendingItemRefreshResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if !got.Configured || got.Rows != 4 || got.Added != 2 || got.Updated != 1 || got.Done != 1 || got.Skipped != 1 || got.Error != "" {
		t.Errorf("refresh result = %+v", got)
	}
	if f.refreshes != 1 {
		t.Errorf("manager refreshed %d times, want 1", f.refreshes)
	}

	// A refresh that could not read the file still answers 200 with the error.
	f.refreshRes = autonomous.PendingItemRefreshResult{Configured: true, Path: "docs/registry.md", Error: "the registry file has no table"}
	rec = pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/refresh", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "the registry file has no table") {
		t.Errorf("failed refresh = %d %q, want 200 carrying the error", rec.Code, rec.Body)
	}
}

func TestPendingItemNoFileRegistry(t *testing.T) {
	f := &pendingItemFake{refreshRes: autonomous.PendingItemRefreshResult{At: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}}
	s := &Server{autonomousMgr: f}

	rec := pendingItemDo(s, http.MethodPost, "/api/autonomous/pending-items/refresh", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Fatalf("refresh with no registry = %d %q, want 200 with configured false", rec.Code, rec.Body)
	}

	rec = pendingItemDo(s, http.MethodGet, "/api/autonomous/pending-items", "")
	var got struct {
		Source struct {
			Configured bool   `json:"configured"`
			Path       string `json:"path"`
		} `json:"source"`
		LastRefresh struct {
			Configured bool       `json:"configured"`
			At         *time.Time `json:"at"`
		} `json:"last_refresh"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if got.Source.Configured || got.Source.Path != "" || got.LastRefresh.Configured || got.LastRefresh.At != nil {
		t.Errorf("with no registry: source %+v, last_refresh %+v", got.Source, got.LastRefresh)
	}
}

func TestPendingItemMethodNotAllowed(t *testing.T) {
	f := pendingItemFixture()
	s := &Server{autonomousMgr: f}
	cases := []struct {
		path    string
		methods []string
		allow   string
	}{
		{"/api/autonomous/pending-items", []string{http.MethodPut, http.MethodDelete, http.MethodPatch}, "GET, POST"},
		{"/api/autonomous/pending-items/refresh", []string{http.MethodGet, http.MethodPut, http.MethodDelete}, "POST"},
		{"/api/autonomous/pending-items/aaaa1111/attach", []string{http.MethodGet, http.MethodPut, http.MethodDelete}, "POST"},
	}
	for _, c := range cases {
		for _, m := range c.methods {
			rec := pendingItemDo(s, m, c.path, "")
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", m, c.path, rec.Code)
			}
			if rec.Header().Get("Allow") != c.allow {
				t.Errorf("%s %s: Allow = %q, want %q", m, c.path, rec.Header().Get("Allow"), c.allow)
			}
		}
	}
	if f.listCalls != 0 || f.refreshes != 0 || len(f.attached) != 0 || len(f.created) != 0 {
		t.Error("a refused method reached the manager")
	}
}

func TestPendingItemDisabled(t *testing.T) {
	s := &Server{}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/autonomous/pending-items", ""},
		{http.MethodPost, "/api/autonomous/pending-items", `{"title":"x"}`},
		{http.MethodPost, "/api/autonomous/pending-items/refresh", ""},
		{http.MethodPost, "/api/autonomous/pending-items/aaaa1111/attach", `{"automaton_id":"feed0003"}`},
	} {
		rec := pendingItemDo(s, c.method, c.path, c.body)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "autonomous disabled") {
			t.Errorf("%s %s with the Automata off = %d %q, want 503 autonomous disabled", c.method, c.path, rec.Code, rec.Body)
		}
	}
	// A manager that carries no pending item registry is a 503 too, not a panic.
	rec := pendingItemDo(&Server{autonomousMgr: &fakeOrchAutonomous{}}, http.MethodGet, "/api/autonomous/pending-items", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("manager without the registry = %d, want 503", rec.Code)
	}

	// The capability gate: read for GET, write for the rest, and a denied call never reaches the manager.
	f := pendingItemFixture()
	srv := &Server{autonomousMgr: f}
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/pending-items", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec = httptest.NewRecorder()
	srv.handlePendingItems(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("GET without autonomous:read = %d %q, want 403 naming it", rec.Code, rec.Body)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/autonomous/pending-items", strings.NewReader(`{"title":"x"}`))
	req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	srv.handlePendingItems(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousWrite) {
		t.Errorf("POST with read only = %d %q, want 403 naming autonomous:write", rec.Code, rec.Body)
	}
	if f.listCalls != 0 || len(f.created) != 0 {
		t.Error("a denied call reached the manager")
	}
}

// ---- config ----

func pendingItemConfigServer(t *testing.T) (*Server, *autonomousConfigPropagationSpy) {
	t.Helper()
	spy := &autonomousConfigPropagationSpy{}
	return &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}, spy
}

func pendingItemPutConfig(s *Server, patch map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(patch)
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rec
}

// pendingItemGetConfig reads the nine keys back from GET /api/config as the
// dotted-name values the PUT used.
func pendingItemGetConfig(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Autonomous struct {
			Automation struct {
				Policy     string         `json:"pending_item_policy"`
				AutoResume bool           `json:"pending_item_auto_resume"`
				Source     map[string]any `json:"pending_item_source"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	a := got.Autonomous.Automation
	out := map[string]any{
		config.PendingItemKeyPolicy:     a.Policy,
		config.PendingItemKeyAutoResume: a.AutoResume,
	}
	for field, key := range map[string]string{
		"path": config.PendingItemKeyPath, "id_pattern": config.PendingItemKeyIDPattern,
		"id_column": config.PendingItemKeyIDColumn, "title_column": config.PendingItemKeyTitleColumn,
		"state_column": config.PendingItemKeyStateColumn, "done_values": config.PendingItemKeyDoneValues,
		"on_missing_row": config.PendingItemKeyOnMissingRow,
	} {
		v, ok := a.Source[field]
		if !ok {
			t.Fatalf("GET /api/config pending_item_source lacks %q: %v", field, a.Source)
		}
		out[key] = v
	}
	return out
}

func TestPendingItemConfigRoundTrip(t *testing.T) {
	s, spy := pendingItemConfigServer(t)

	// Unset reads the defaults.
	d := pendingItemGetConfig(t, s)
	wantDefaults := map[string]any{
		config.PendingItemKeyPolicy: "hold", config.PendingItemKeyAutoResume: true,
		config.PendingItemKeyPath: "", config.PendingItemKeyIDPattern: `^\S+$`,
		config.PendingItemKeyIDColumn: "1", config.PendingItemKeyTitleColumn: "2",
		config.PendingItemKeyStateColumn: "", config.PendingItemKeyOnMissingRow: "keep",
	}
	for k, want := range wantDefaults {
		if d[k] != want {
			t.Errorf("default %s = %v, want %v", k, d[k], want)
		}
	}
	if dv, _ := d[config.PendingItemKeyDoneValues].([]any); len(dv) != 3 || dv[0] != "done" || dv[2] != "shipped" {
		t.Errorf("default done_values = %v", d[config.PendingItemKeyDoneValues])
	}

	// Every key, PUT then GET. The id pattern is not this repository's.
	patch := map[string]any{
		config.PendingItemKeyPolicy:       "skip",
		config.PendingItemKeyAutoResume:   false,
		config.PendingItemKeyPath:         "planning/backlog.md",
		config.PendingItemKeyIDPattern:    `^[A-Z]{2,}-\d{3}$`,
		config.PendingItemKeyIDColumn:     "Ticket",
		config.PendingItemKeyTitleColumn:  "Summary",
		config.PendingItemKeyStateColumn:  "4",
		config.PendingItemKeyDoneValues:   []any{"complete", "Shipped"},
		config.PendingItemKeyOnMissingRow: "drop",
	}
	if rec := pendingItemPutConfig(s, patch); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rec.Code, rec.Body)
	}
	got := pendingItemGetConfig(t, s)
	for k, want := range patch {
		if k == config.PendingItemKeyDoneValues {
			continue
		}
		if got[k] != want {
			t.Errorf("after PUT %s = %v, want %v", k, got[k], want)
		}
	}
	if dv, _ := got[config.PendingItemKeyDoneValues].([]any); len(dv) != 2 || dv[0] != "complete" || dv[1] != "Shipped" {
		t.Errorf("after PUT done_values = %v", got[config.PendingItemKeyDoneValues])
	}

	// The change reaches the live manager without a restart.
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode forwarded config: %v (%s)", err, spy.lastSetConfig)
	}
	var auto struct {
		Policy     string `json:"pending_item_policy"`
		AutoResume *bool  `json:"pending_item_auto_resume"`
		Source     struct {
			Path         string   `json:"path"`
			IDPattern    string   `json:"id_pattern"`
			DoneValues   []string `json:"done_values"`
			OnMissingRow string   `json:"on_missing_row"`
		} `json:"pending_item_source"`
	}
	if err := json.Unmarshal(fwd["automation"], &auto); err != nil {
		t.Fatalf("decode forwarded automation: %v (%s)", err, fwd["automation"])
	}
	if auto.Policy != "skip" || auto.AutoResume == nil || *auto.AutoResume || auto.Source.Path != "planning/backlog.md" ||
		auto.Source.IDPattern != `^[A-Z]{2,}-\d{3}$` || len(auto.Source.DoneValues) != 2 || auto.Source.OnMissingRow != "drop" {
		t.Errorf("forwarded automation = %s", fwd["automation"])
	}

	// Text forms (what the CLI and the comm channel send) are accepted, and an absent key keeps its value.
	if rec := pendingItemPutConfig(s, map[string]any{
		config.PendingItemKeyAutoResume: "true",
		config.PendingItemKeyDoneValues: "closed, finished",
	}); rec.Code != http.StatusOK {
		t.Fatalf("PUT text forms = %d: %s", rec.Code, rec.Body)
	}
	got = pendingItemGetConfig(t, s)
	if got[config.PendingItemKeyAutoResume] != true || got[config.PendingItemKeyPolicy] != "skip" ||
		got[config.PendingItemKeyIDColumn] != "Ticket" {
		t.Errorf("after text PUT: %v", got)
	}
	if dv, _ := got[config.PendingItemKeyDoneValues].([]any); len(dv) != 2 || dv[0] != "closed" || dv[1] != "finished" {
		t.Errorf("after text PUT done_values = %v", got[config.PendingItemKeyDoneValues])
	}

	// A key cleared back to empty reads its default and the clear reaches the live manager.
	if rec := pendingItemPutConfig(s, map[string]any{
		config.PendingItemKeyPath:      "",
		config.PendingItemKeyIDPattern: "",
		config.PendingItemKeyPolicy:    "",
	}); rec.Code != http.StatusOK {
		t.Fatalf("PUT clears = %d: %s", rec.Code, rec.Body)
	}
	got = pendingItemGetConfig(t, s)
	if got[config.PendingItemKeyPath] != "" || got[config.PendingItemKeyIDPattern] != `^\S+$` || got[config.PendingItemKeyPolicy] != "hold" {
		t.Errorf("after clears: %v", got)
	}
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode forwarded config: %v", err)
	}
	var cleared map[string]json.RawMessage
	_ = json.Unmarshal(fwd["automation"], &cleared)
	var srcRaw map[string]json.RawMessage
	_ = json.Unmarshal(cleared["pending_item_source"], &srcRaw)
	if string(srcRaw["path"]) != `""` || string(cleared["pending_item_policy"]) != `""` {
		t.Errorf("a clear was not forwarded explicitly: %s", fwd["automation"])
	}

	// The same keys through PUT /api/autonomous/config.
	body := `{"automation":{"pending_item_policy":"skip","pending_item_auto_resume":false,"pending_item_source":{"id_column":"3","done_values":["x"]}}}`
	rec := httptest.NewRecorder()
	s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/autonomous/config = %d: %s", rec.Code, rec.Body)
	}
}

func TestPendingItemConfigInvalid(t *testing.T) {
	s, spy := pendingItemConfigServer(t)
	if rec := pendingItemPutConfig(s, map[string]any{
		config.PendingItemKeyPolicy:    "skip",
		config.PendingItemKeyIDPattern: `^\d+$`,
	}); rec.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d: %s", rec.Code, rec.Body)
	}
	before := pendingItemGetConfig(t, s)
	calls := spy.calls

	long := strings.Repeat("a", config.PendingItemIDPatternMaxLen+1)
	cases := []struct {
		name  string
		patch map[string]any
		want  []string
	}{
		{"policy unknown word", map[string]any{config.PendingItemKeyPolicy: "maybe"}, []string{"hold", "skip"}},
		{"policy wrong case", map[string]any{config.PendingItemKeyPolicy: "HOLD"}, []string{"hold", "skip"}},
		{"policy not a string", map[string]any{config.PendingItemKeyPolicy: 3}, []string{"hold", "skip"}},
		{"auto resume unknown word", map[string]any{config.PendingItemKeyAutoResume: "yes"}, []string{"true", "false"}},
		{"auto resume number", map[string]any{config.PendingItemKeyAutoResume: 1}, []string{"true", "false"}},
		{"auto resume empty", map[string]any{config.PendingItemKeyAutoResume: ""}, []string{"true", "false"}},
		{"on missing row unknown word", map[string]any{config.PendingItemKeyOnMissingRow: "delete"}, []string{"keep", "drop"}},
		{"id pattern not a regexp", map[string]any{config.PendingItemKeyIDPattern: "("}, []string{"regular expression"}},
		{"id pattern too long", map[string]any{config.PendingItemKeyIDPattern: long}, []string{"at most"}},
		{"id column blank", map[string]any{config.PendingItemKeyIDColumn: "   "}, []string{"column"}},
		{"title column not a string", map[string]any{config.PendingItemKeyTitleColumn: []any{"a"}}, []string{"header name"}},
		{"done values entry blank", map[string]any{config.PendingItemKeyDoneValues: "done,,closed"}, []string{"non-blank"}},
		{"done values not strings", map[string]any{config.PendingItemKeyDoneValues: []any{1, 2}}, []string{"string"}},
		{"path not a string", map[string]any{config.PendingItemKeyPath: 7}, []string{"file path"}},
		{"one bad key rejects the whole PUT", map[string]any{
			config.PendingItemKeyPolicy:       "hold",
			config.PendingItemKeyOnMissingRow: "nope",
		}, []string{"keep", "drop"}},
	}
	for _, c := range cases {
		rec := pendingItemPutConfig(s, c.patch)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", c.name, rec.Code, rec.Body)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(rec.Body.String(), w) {
				t.Errorf("%s: message %q does not name %q", c.name, rec.Body.String(), w)
			}
		}
	}
	after := pendingItemGetConfig(t, s)
	for k, v := range before {
		if fmt.Sprint(after[k]) != fmt.Sprint(v) {
			t.Errorf("a refused PUT changed %s from %v to %v", k, v, after[k])
		}
	}
	if spy.calls != calls {
		t.Errorf("a refused PUT reached the manager (%d forwards)", spy.calls-calls)
	}

	// The autonomous config route refuses the same values.
	for _, body := range []string{
		`{"automation":{"pending_item_policy":"maybe"}}`,
		`{"automation":{"pending_item_auto_resume":"yes"}}`,
		`{"automation":{"pending_item_source":{"id_pattern":"("}}}`,
		`{"automation":{"pending_item_source":{"on_missing_row":"delete"}}}`,
		`{"automation":{"pending_item_source":{"done_values":["done",""]}}}`,
	} {
		rec := httptest.NewRecorder()
		s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT /api/autonomous/config %s = %d, want 400: %s", body, rec.Code, rec.Body)
		}
	}
	if spy.calls != calls {
		t.Errorf("a refused autonomous config PUT reached the manager")
	}
}

// ---- insert_member ----

// insertSpy is a minimal Automation controller that records what insert_member
// handed to the manager.
type insertSpy struct {
	fakeOrchAutonomous
	got   []autonomous.InsertMemberInput
	calls int
}

func (i *insertSpy) InsertMember(id string, in autonomous.InsertMemberInput) (any, error) {
	i.calls++
	i.got = append(i.got, in)
	return map[string]any{"id": id, "version": 2}, nil
}
func (i *insertSpy) PauseAutomation(string, string, int64) (any, error)  { return nil, nil }
func (i *insertSpy) ResumeAutomation(string, string, int64) (any, error) { return nil, nil }
func (i *insertSpy) CancelAutomation(string, string, bool, bool, int64) (any, error) {
	return nil, nil
}
func (i *insertSpy) SkipMember(string, string, string, string, int64) (any, error) { return nil, nil }
func (i *insertSpy) RemoveMember(string, string, string, int64) (any, error)       { return nil, nil }
func (i *insertSpy) ReorderEdges(string, string, string, string, string, string, int64) (any, error) {
	return nil, nil
}
func (i *insertSpy) ApproveInsertedMember(string, string, string, int64) (any, error) {
	return nil, nil
}
func (i *insertSpy) SetAutomationLLM(string, *string, *string, *string, *string, *string, string, int64) (any, error) {
	return nil, nil
}
func (i *insertSpy) SetAutomationVerificationLLM(string, *string, *string, *string, string, int64) (any, error) {
	return nil, nil
}
func (i *insertSpy) SetAutomationBudgetPolicy(string, autonomous.BudgetSetRequest, string, int64) (any, error) {
	return nil, nil
}
func (i *insertSpy) SetAutomationDecisionAssistant(string, string, string, *autonomous.DecisionAssistant, int64) (any, error) {
	return nil, nil
}

func TestPendingItemInsertMemberAcceptsItemID(t *testing.T) {
	spy := &insertSpy{}
	s := &Server{autonomousMgr: spy}
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleAutomations(rec, httptest.NewRequest(http.MethodPost, "/api/automations/a0000001/insert_member", strings.NewReader(body)))
		return rec
	}

	// pending_item_id inside member, like prd_id.
	rec := post(`{"reason":"work that has no Automaton yet","member":{"pending_item_id":" aaaa1111 "},"after":"m1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert with member.pending_item_id = %d: %s", rec.Code, rec.Body)
	}
	if len(spy.got) != 1 || spy.got[0].PendingItemID != "aaaa1111" || spy.got[0].PRDID != "" || spy.got[0].AfterMemberID != "m1" {
		t.Errorf("manager input = %+v", spy.got)
	}

	// pending_item_id at the top of the body.
	rec = post(`{"reason":"again","pending_item_id":"bbbb2222","urgent":true}`)
	if rec.Code != http.StatusOK || len(spy.got) != 2 || spy.got[1].PendingItemID != "bbbb2222" || !spy.got[1].Urgent {
		t.Errorf("top-level pending_item_id = %d %+v", rec.Code, spy.got)
	}

	// prd_id alone is unchanged.
	rec = post(`{"reason":"an Automaton","member":{"prd_id":"feed0001","name":"Docs"}}`)
	if rec.Code != http.StatusOK || len(spy.got) != 3 || spy.got[2].PRDID != "feed0001" || spy.got[2].PendingItemID != "" || spy.got[2].Name != "Docs" {
		t.Errorf("prd_id insert = %d %+v", rec.Code, spy.got)
	}

	// The two are exclusive: a body that carries both is a 400 and never reaches the manager.
	calls := spy.calls
	rec = post(`{"reason":"both","member":{"prd_id":"feed0001","pending_item_id":"aaaa1111"}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "prd_id") || !strings.Contains(rec.Body.String(), "pending_item_id") {
		t.Errorf("both = %d %q, want 400 naming both", rec.Code, rec.Body)
	}
	if spy.calls != calls {
		t.Error("an insert naming both reached the manager")
	}
}

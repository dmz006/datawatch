// Automation MCP tools (automation_list/get/create/update/delete) proxied
// against an in-memory httptest backend.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// automationsBackend is a minimal in-memory /api/automations backend:
// POST → 201 with id/version/status=draft, GET list/get, PUT bumps version
// (rejecting a stale expected_version with 409), DELETE → 204.
type automationsBackend struct {
	mu    sync.Mutex
	seq   int
	store map[string]map[string]any
}

func newAutomationsBackend() *automationsBackend {
	return &automationsBackend{store: map[string]map[string]any{}}
}

func (b *automationsBackend) serve(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/automations"), "/")

	switch r.Method {
	case http.MethodPost:
		if id != "" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		var entry map[string]any
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		b.seq++
		entry["id"] = fmt.Sprintf("auto-%d", b.seq)
		entry["version"] = b.seq
		entry["status"] = "draft"
		b.store[entry["id"].(string)] = entry
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(jsonMarshal(entry))

	case http.MethodGet:
		if id == "" {
			list := make([]map[string]any, 0, len(b.store))
			for _, e := range b.store {
				list = append(list, e)
			}
			_, _ = w.Write(jsonMarshal(list))
			return
		}
		e, ok := b.store[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write(jsonMarshal(e))

	case http.MethodPut:
		if id == "" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		e, ok := b.store[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if ev, ok := body["expected_version"]; ok && ev != nil {
			if fmt.Sprint(ev) != fmt.Sprint(e["version"]) {
				http.Error(w, `{"error":"version conflict"}`, http.StatusConflict)
				return
			}
		}
		for k, v := range body {
			if k == "id" || k == "version" || k == "expected_version" {
				continue
			}
			e[k] = v
		}
		switch cur := e["version"].(type) {
		case float64:
			e["version"] = int(cur) + 1
		case int:
			e["version"] = cur + 1
		default:
			e["version"] = 2
		}
		_, _ = w.Write(jsonMarshal(e))

	case http.MethodDelete:
		if id == "" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if _, ok := b.store[id]; !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		delete(b.store, id)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func jsonMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// automationRecord captures the last request the handlers proxied.
type automationRecord struct {
	method string
	path   string
	body   map[string]any
}

// automationHarness is the in-memory backend plus a Server pointed at it
// (webPort set like capacityTestServer), with a record of the last proxied
// request.
type automationHarness struct {
	s   *Server
	rec *automationRecord
}

func newAutomationHarness(t *testing.T) *automationHarness {
	t.Helper()
	be := newAutomationsBackend()
	rec := &automationRecord{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.method, rec.path = r.Method, r.URL.Path
		_ = json.Unmarshal(raw, &rec.body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		be.serve(w, r)
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &automationHarness{s: &Server{webPort: port}, rec: rec}
}

func automationText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	if tc, ok := res.Content[0].(mcpsdk.TextContent); ok {
		return tc.Text
	}
	t.Fatalf("unexpected content type %T", res.Content[0])
	return ""
}

func TestAutomationTools_Names(t *testing.T) {
	s := &Server{}
	want := map[string]string{
		"automation_list":   s.toolAutomationList().Name,
		"automation_get":    s.toolAutomationGet().Name,
		"automation_create": s.toolAutomationCreate().Name,
		"automation_update": s.toolAutomationUpdate().Name,
		"automation_delete": s.toolAutomationDelete().Name,
	}
	for name, got := range want {
		if name != got {
			t.Errorf("tool name = %q, want %q", got, name)
		}
	}
}

func TestAutomationCreate_ForwardsPOSTBody(t *testing.T) {
	h := newAutomationHarness(t)
	res, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{
		"name":            "chain-1",
		"entry_member_id": "m1",
		"members_json":    `[{"id":"m1","prd_id":"prd-9","on_complete":"m2","on_blocked":"","on_failed":"","added_reason":"entry"}]`,
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if strings.HasPrefix(text, "error:") {
		t.Fatalf("unexpected error: %s", text)
	}
	if h.rec.method != http.MethodPost || h.rec.path != "/api/automations" {
		t.Errorf("proxied %s %s, want POST /api/automations", h.rec.method, h.rec.path)
	}
	if h.rec.body["name"] != "chain-1" {
		t.Errorf("name = %v, want chain-1", h.rec.body["name"])
	}
	members, ok := h.rec.body["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members not forwarded: %v", h.rec.body["members"])
	}
	m0, _ := members[0].(map[string]any)
	if m0["id"] != "m1" || m0["prd_id"] != "prd-9" || m0["on_complete"] != "m2" {
		t.Errorf("member[0] = %v", m0)
	}
	if !strings.Contains(text, "auto-1") || !strings.Contains(text, "draft") {
		t.Errorf("result missing backend id/status: %s", text)
	}
}

func TestAutomationList_GetsAllRecords(t *testing.T) {
	h := newAutomationHarness(t)
	// Seed through the create handler so the record list has an entry.
	if _, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "seed"})); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	h.rec.method, h.rec.path = "", ""
	res, err := h.s.handleAutomationList(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if h.rec.method != http.MethodGet || h.rec.path != "/api/automations" {
		t.Errorf("proxied %s %s, want GET /api/automations", h.rec.method, h.rec.path)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "seed") {
		t.Errorf("list text missing backend record: %s", text)
	}
}

func TestAutomationGet_FetchesByID(t *testing.T) {
	h := newAutomationHarness(t)
	res, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "chain-9"}))
	if err != nil || res == nil {
		t.Fatalf("create: %v", err)
	}
	text := automationText(t, res)
	// Backend assigns sequential ids; first creation is auto-1.
	if !strings.Contains(text, "auto-1") {
		t.Fatalf("create result missing id: %s", text)
	}
	res, err = h.s.handleAutomationGet(context.Background(), callReq(map[string]any{"id": "auto-1"}))
	if err != nil || res == nil {
		t.Fatalf("get: %v", err)
	}
	if h.rec.method != http.MethodGet || h.rec.path != "/api/automations/auto-1" {
		t.Errorf("proxied %s %s, want GET /api/automations/auto-1", h.rec.method, h.rec.path)
	}
	text = automationText(t, res)
	if !strings.Contains(text, "chain-9") {
		t.Errorf("get text missing record: %s", text)
	}
}

func TestAutomationUpdate_ForwardsExpectedVersion(t *testing.T) {
	h := newAutomationHarness(t)
	if _, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "chain-1"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := h.s.handleAutomationUpdate(context.Background(), callReq(map[string]any{
		"id":               "auto-1",
		"expected_version": float64(1),
		"name":             "chain-1-renamed",
		"reason":           "rename for test",
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if strings.HasPrefix(text, "error:") {
		t.Fatalf("unexpected error: %s", text)
	}
	if h.rec.method != http.MethodPut || h.rec.path != "/api/automations/auto-1" {
		t.Errorf("proxied %s %s, want PUT /api/automations/auto-1", h.rec.method, h.rec.path)
	}
	if h.rec.body["expected_version"] != float64(1) {
		t.Errorf("expected_version = %v, want 1", h.rec.body["expected_version"])
	}
	if h.rec.body["name"] != "chain-1-renamed" {
		t.Errorf("name = %v, want chain-1-renamed", h.rec.body["name"])
	}
	if !strings.Contains(text, "chain-1-renamed") || !strings.Contains(text, `"version":2`) {
		t.Errorf("update result unexpected: %s", text)
	}
}

func TestAutomationUpdate_StaleVersionRejected(t *testing.T) {
	h := newAutomationHarness(t)
	if _, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "chain-1"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := h.s.handleAutomationUpdate(context.Background(), callReq(map[string]any{
		"id":               "auto-1",
		"expected_version": float64(99),
		"name":             "should-not-apply",
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if !strings.HasPrefix(text, "error:") || !strings.Contains(text, "409") {
		t.Errorf("stale version not rejected: %s", text)
	}
}

func TestAutomationDelete_ReturnsDeleted(t *testing.T) {
	h := newAutomationHarness(t)
	if _, err := h.s.handleAutomationCreate(context.Background(), callReq(map[string]any{"name": "chain-1"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := h.s.handleAutomationDelete(context.Background(), callReq(map[string]any{"id": "auto-1"}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if h.rec.method != http.MethodDelete || h.rec.path != "/api/automations/auto-1" {
		t.Errorf("proxied %s %s, want DELETE /api/automations/auto-1", h.rec.method, h.rec.path)
	}
	if text := automationText(t, res); text != `{"status":"deleted"}` {
		t.Errorf("text = %q, want {\"status\":\"deleted\"}", text)
	}
}

func TestAutomation_BackendErrorSurfacesAsText(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	res, err := s.handleAutomationList(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if !strings.HasPrefix(text, "error:") || !strings.Contains(text, "HTTP 500") {
		t.Errorf("error text = %q", text)
	}
}

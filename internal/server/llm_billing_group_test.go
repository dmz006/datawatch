// REST round-trip coverage for the LLM registry entry's billing_group
// label (shared cloud/billing account across federated peers).

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLLMBillingGroupRESTRoundTrip verifies that billing_group
// round-trips through PUT/GET on /api/llms: creation carries it, an
// update that omits the field keeps the stored value, and an update
// that sets it changes the stored value.
func TestLLMBillingGroupRESTRoundTrip(t *testing.T) {
	srv := newLLMServer(t)

	// POST — create with a billing group.
	body, _ := json.Marshal(map[string]any{
		"name":          "billing-cc",
		"kind":          "claude-code",
		"billing_group": "shared-plan-a",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/llms", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create: status %d body: %s", w.Code, w.Body.String())
	}

	// GET — read it back.
	var got billingGroupEntry
	get := func() {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/llms/billing-cc", nil)
		req.URL.Path = "/api/llms/billing-cc"
		w := httptest.NewRecorder()
		srv.handleLLMs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get: status %d body: %s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode get response: %v", err)
		}
	}
	get()
	if got.Name != "billing-cc" {
		t.Fatalf("get: name got %q, want %q", got.Name, "billing-cc")
	}
	if got.BillingGroup != "shared-plan-a" {
		t.Fatalf("create/get: billing_group got %q, want %q", got.BillingGroup, "shared-plan-a")
	}

	// PUT — update without the field; the stored value must survive.
	body, _ = json.Marshal(map[string]any{"name": "billing-cc", "kind": "claude-code", "model": "claude-sonnet"})
	req = httptest.NewRequest(http.MethodPut, "/api/llms/billing-cc", bytes.NewReader(body))
	req.URL.Path = "/api/llms/billing-cc"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: status %d body: %s", w.Code, w.Body.String())
	}
	get()
	if got.Model != "claude-sonnet" {
		t.Errorf("update: model got %q, want %q", got.Model, "claude-sonnet")
	}
	if got.BillingGroup != "shared-plan-a" {
		t.Errorf("update without billing_group must keep the stored value, got %q", got.BillingGroup)
	}

	// PUT — set a new group; the stored value must change.
	body, _ = json.Marshal(map[string]any{"name": "billing-cc", "kind": "claude-code", "billing_group": "shared-plan-b"})
	req = httptest.NewRequest(http.MethodPut, "/api/llms/billing-cc", bytes.NewReader(body))
	req.URL.Path = "/api/llms/billing-cc"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update group: status %d body: %s", w.Code, w.Body.String())
	}
	get()
	if got.BillingGroup != "shared-plan-b" {
		t.Errorf("update group: billing_group got %q, want %q", got.BillingGroup, "shared-plan-b")
	}

	// GET list — the field must also appear in the batch view.
	req = httptest.NewRequest(http.MethodGet, "/api/llms", nil)
	req.URL.Path = "/api/llms"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d", w.Code)
	}
	var list struct {
		LLMs []billingGroupEntry `json:"llms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, l := range list.LLMs {
		if l.Name == "billing-cc" {
			found = true
			if l.BillingGroup != "shared-plan-b" {
				t.Errorf("list: billing_group got %q, want %q", l.BillingGroup, "shared-plan-b")
			}
			break
		}
	}
	if !found {
		t.Error("list: billing-cc not found in /api/llms response")
	}

	// PUT — a malformed group must be rejected and leave the stored value alone.
	body, _ = json.Marshal(map[string]any{"name": "billing-cc", "kind": "claude-code", "billing_group": "shared\x00plan"})
	req = httptest.NewRequest(http.MethodPut, "/api/llms/billing-cc", bytes.NewReader(body))
	req.URL.Path = "/api/llms/billing-cc"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update with control char: expected 400, got %d body: %s", w.Code, w.Body.String())
	}
	get()
	if got.BillingGroup != "shared-plan-b" {
		t.Errorf("rejected update must leave stored billing_group alone, got %q", got.BillingGroup)
	}
}

// billingGroupEntry decodes the subset of the LLM view this test checks.
type billingGroupEntry struct {
	Name         string `json:"name"`
	Model        string `json:"model"`
	BillingGroup string `json:"billing_group"`
}

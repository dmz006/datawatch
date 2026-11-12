// CLI surfaces: `federation peer add|update --fingerprint-sync` (tri-state:
// absent = unchanged on update) and `llm add|update --billing-group`
// (empty = clears on update). Flags forward verbatim to the daemon's REST
// bodies and must be omitted when the operator did not pass them.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type fingerprintCall struct {
	method string
	path   string
	body   map[string]any
}

func fingerprintDaemon(t *testing.T) func() []fingerprintCall {
	t.Helper()
	var mu sync.Mutex
	var calls []fingerprintCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		calls = append(calls, fingerprintCall{method: r.Method, path: r.URL.Path, body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return func() []fingerprintCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]fingerprintCall(nil), calls...)
	}
}

func lastCall(t *testing.T, seen func() []fingerprintCall) *fingerprintCall {
	t.Helper()
	calls := seen()
	if len(calls) == 0 {
		t.Fatal("daemon saw no request")
	}
	c := calls[len(calls)-1]
	return &c
}

func TestCLIFederationPeerFingerprintSyncFlag(t *testing.T) {
	t.Run("add: explicit true is forwarded", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newFederationCmd()
		c.SetArgs([]string{"peer", "add", "--name", "peer-a", "--url", "http://peer-a:8080", "--fingerprint-sync", "true"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		call := lastCall(t, seen)
		if call.method != http.MethodPost || call.path != "/api/federation/peers" {
			t.Fatalf("routed %s %s, want POST /api/federation/peers", call.method, call.path)
		}
		if call.body["fingerprint_sync"] != true {
			t.Errorf("add: fingerprint_sync = %v, want true", call.body["fingerprint_sync"])
		}
	})

	t.Run("add: flag absent is omitted from the body", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newFederationCmd()
		c.SetArgs([]string{"peer", "add", "--name", "peer-b", "--url", "http://peer-b:8081"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if _, ok := lastCall(t, seen).body["fingerprint_sync"]; ok {
			t.Errorf("add: fingerprint_sync should be omitted when the flag is absent")
		}
	})

	t.Run("update: explicit false is forwarded (turns the exchange off)", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newFederationCmd()
		c.SetArgs([]string{"peer", "update", "peer-a", "--url", "http://peer-a:8080", "--fingerprint-sync=false"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		call := lastCall(t, seen)
		if call.method != http.MethodPut || call.path != "/api/federation/peers/peer-a" {
			t.Fatalf("routed %s %s, want PUT /api/federation/peers/peer-a", call.method, call.path)
		}
		if v, ok := call.body["fingerprint_sync"]; !ok || v != false {
			t.Errorf("update: fingerprint_sync = %v (present=%v), want explicit false", v, ok)
		}
	})

	t.Run("update: flag absent is omitted (stored value survives)", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newFederationCmd()
		c.SetArgs([]string{"peer", "update", "peer-a", "--url", "http://peer-a:8080"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if _, ok := lastCall(t, seen).body["fingerprint_sync"]; ok {
			t.Errorf("update: fingerprint_sync should be omitted when the flag is absent")
		}
	})
}

func TestCLILLMBillingGroupFlag(t *testing.T) {
	t.Run("add: forwarded to the daemon", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newLLMAddCmd(false)
		c.SetArgs([]string{"shared", "--kind", "claude-code", "--billing-group", "shared-plan-a"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		call := lastCall(t, seen)
		if call.method != http.MethodPost || call.path != "/api/llms" {
			t.Fatalf("routed %s %s, want POST /api/llms", call.method, call.path)
		}
		if call.body["billing_group"] != "shared-plan-a" {
			t.Errorf("add: billing_group = %v, want shared-plan-a", call.body["billing_group"])
		}
	})

	t.Run("add: flag absent is omitted from the body", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newLLMAddCmd(false)
		c.SetArgs([]string{"shared", "--kind", "claude-code"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if _, ok := lastCall(t, seen).body["billing_group"]; ok {
			t.Errorf("add: billing_group should be omitted when the flag is absent")
		}
	})

	t.Run("update: forwarded to the daemon", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newLLMAddCmd(true)
		c.SetArgs([]string{"shared", "--kind", "claude-code", "--billing-group", "shared-plan-b"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		call := lastCall(t, seen)
		if call.method != http.MethodPut || call.path != "/api/llms/shared" {
			t.Fatalf("routed %s %s, want PUT /api/llms/shared", call.method, call.path)
		}
		if call.body["billing_group"] != "shared-plan-b" {
			t.Errorf("update: billing_group = %v, want shared-plan-b", call.body["billing_group"])
		}
	})

	t.Run("update: explicit empty clears (sent as empty string)", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newLLMAddCmd(true)
		c.SetArgs([]string{"shared", "--kind", "claude-code", "--billing-group", ""})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		call := lastCall(t, seen)
		if v, ok := call.body["billing_group"]; !ok || v != "" {
			t.Errorf("update: billing_group = %v (present=%v), want explicit empty", v, ok)
		}
	})

	t.Run("update: flag absent is omitted (stored label survives)", func(t *testing.T) {
		seen := fingerprintDaemon(t)
		c := newLLMAddCmd(true)
		c.SetArgs([]string{"shared", "--kind", "claude-code"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if _, ok := lastCall(t, seen).body["billing_group"]; ok {
			t.Errorf("update: billing_group should be omitted when the flag is absent")
		}
	})
}

package main

// `datawatch config set mcp.allow_sensitive_writes <bool>` is accepted the same
// way `mcp.allow_self_config` is: the value goes to the daemon's PUT /api/config
// as a JSON boolean, and a refusal from the daemon comes back as an error. The
// fake daemon mirrors the real handler, which stores the value as an explicit
// bool pointer so the operator's choice sticks.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	sensitiveWritesKey = "mcp.allow_sensitive_writes"
	selfConfigKey      = "mcp.allow_self_config"
)

// boolConfigFakeDaemon records the typed value PUT /api/config receives for the
// two MCP permission keys.
type boolConfigFakeDaemon struct {
	mcp      config.MCPConfig
	bodies   []string
	refusing string // when set, every PUT is refused with this text
}

func newBoolConfigFakeDaemon(t *testing.T) (*boolConfigFakeDaemon, *httptest.Server) {
	t.Helper()
	d := &boolConfigFakeDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		d.bodies = append(d.bodies, string(raw))
		if d.refusing != "" {
			http.Error(w, d.refusing, http.StatusBadRequest)
			return
		}
		var patch map[string]any
		if err := json.Unmarshal(raw, &patch); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		for k, v := range patch {
			b, isBool := v.(bool)
			switch k {
			case sensitiveWritesKey:
				if !isBool {
					http.Error(w, k+": expected true or false", http.StatusBadRequest)
					return
				}
				d.mcp.AllowSensitiveWrites = &b
			case selfConfigKey:
				if !isBool {
					http.Error(w, k+": expected true or false", http.StatusBadRequest)
					return
				}
				d.mcp.AllowSelfConfig = &b
			default:
				http.Error(w, "unknown key "+k, http.StatusBadRequest)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d, srv
}

func TestSensitiveWritesConfigSet(t *testing.T) {
	t.Run("false turns the class off and is sent as a boolean", func(t *testing.T) {
		d, _ := newBoolConfigFakeDaemon(t)
		if err := runConfigSet(&config.Config{}, sensitiveWritesKey, "false"); err != nil {
			t.Fatalf("runConfigSet: %v", err)
		}
		if len(d.bodies) != 1 {
			t.Fatalf("PUT count = %d, want 1 (bodies %v)", len(d.bodies), d.bodies)
		}
		if got, want := d.bodies[0], `{"mcp.allow_sensitive_writes": false}`; got != want {
			t.Errorf("body = %s, want %s", got, want)
		}
		if d.mcp.AllowSensitiveWrites == nil || *d.mcp.AllowSensitiveWrites {
			t.Errorf("AllowSensitiveWrites = %v, want explicit false", d.mcp.AllowSensitiveWrites)
		}
		if d.mcp.SensitiveWritesEnabled() {
			t.Error("SensitiveWritesEnabled() = true after setting false")
		}
	})

	t.Run("true turns the class back on", func(t *testing.T) {
		d, _ := newBoolConfigFakeDaemon(t)
		off := false
		d.mcp.AllowSensitiveWrites = &off
		if err := runConfigSet(&config.Config{}, sensitiveWritesKey, "true"); err != nil {
			t.Fatalf("runConfigSet: %v", err)
		}
		if got, want := d.bodies[0], `{"mcp.allow_sensitive_writes": true}`; got != want {
			t.Errorf("body = %s, want %s", got, want)
		}
		if d.mcp.AllowSensitiveWrites == nil || !*d.mcp.AllowSensitiveWrites {
			t.Errorf("AllowSensitiveWrites = %v, want explicit true", d.mcp.AllowSensitiveWrites)
		}
	})

	t.Run("accepted the same way as allow_self_config", func(t *testing.T) {
		d, _ := newBoolConfigFakeDaemon(t)
		for _, key := range []string{selfConfigKey, sensitiveWritesKey} {
			if err := runConfigSet(&config.Config{}, key, "false"); err != nil {
				t.Fatalf("runConfigSet(%s): %v", key, err)
			}
		}
		if len(d.bodies) != 2 {
			t.Fatalf("PUT count = %d, want 2 (bodies %v)", len(d.bodies), d.bodies)
		}
		// Same shape for both keys: one JSON boolean field, no quoting retry.
		for i, key := range []string{selfConfigKey, sensitiveWritesKey} {
			want := `{"` + key + `": false}`
			if d.bodies[i] != want {
				t.Errorf("body[%d] = %s, want %s", i, d.bodies[i], want)
			}
		}
		if d.mcp.AllowSelfConfig == nil || *d.mcp.AllowSelfConfig {
			t.Errorf("AllowSelfConfig = %v, want explicit false", d.mcp.AllowSelfConfig)
		}
		if d.mcp.AllowSensitiveWrites == nil || *d.mcp.AllowSensitiveWrites {
			t.Errorf("AllowSensitiveWrites = %v, want explicit false", d.mcp.AllowSensitiveWrites)
		}
	})

	t.Run("a daemon refusal comes back as an error", func(t *testing.T) {
		d, _ := newBoolConfigFakeDaemon(t)
		d.refusing = "mcp.allow_sensitive_writes: refused"
		err := runConfigSet(&config.Config{}, sensitiveWritesKey, "false")
		if err == nil {
			t.Fatal("runConfigSet returned nil for a refused write")
		}
		if !strings.Contains(err.Error(), "mcp.allow_sensitive_writes: refused") {
			t.Errorf("error %q does not carry the daemon's message", err)
		}
		if d.mcp.AllowSensitiveWrites != nil {
			t.Errorf("AllowSensitiveWrites = %v after a refused write, want unset", d.mcp.AllowSensitiveWrites)
		}
	})

	t.Run("an unreachable daemon is an error", func(t *testing.T) {
		_, srv := newBoolConfigFakeDaemon(t)
		srv.Close()
		if err := runConfigSet(&config.Config{}, sensitiveWritesKey, "true"); err == nil {
			t.Fatal("runConfigSet returned nil with the daemon down")
		}
	})

	t.Run("help names the key", func(t *testing.T) {
		long := newConfigSetCmd().Long
		for _, want := range []string{sensitiveWritesKey, selfConfigKey} {
			if !strings.Contains(long, want) {
				t.Errorf("config set help does not mention %s", want)
			}
		}
	})
}

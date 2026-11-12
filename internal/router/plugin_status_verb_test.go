package router

import (
	"net/http"
	"strings"
	"testing"
)

// pluginStatusManagedView is a full /api/plugins/<name> body for a managed
// service-kind plugin. It deliberately carries secret values in the fields
// the verb must not render (a literal API token in env, a ${secret:...} env
// ref, and the connection auth token) so the tests can prove a secret value
// never reaches the reply.
const pluginStatusManagedView = `{
  "name": "PluginStatusDemo",
  "description": "demo service plugin",
  "version": "1.4.2",
  "entry": "bin/svc",
  "kind": "service",
  "env": {
    "API_TOKEN": "sk-live-9f8a7b6c5d4e3f2a1b0c",
    "SSH_KEY": "${secret:ssh-private-key}"
  },
  "connection": {
    "base_url": "http://127.0.0.1:8317",
    "auth": {"type": "bearer", "token": "tok-SECRETVALUE-42"},
    "health_path": "/healthz"
  },
  "lifecycle": {"supported_modes": ["managed"], "entry": "bin/svc", "restart_policy": "always"},
  "dir": "/opt/datawatch/plugins/demo",
  "resolved_entry": "/opt/datawatch/plugins/demo/bin/svc",
  "enabled": true,
  "invoke_count": 0,
  "error_count": 0,
  "service": {
    "mode": "managed",
    "state": "starting",
    "address": "http://127.0.0.1:8317",
    "version": "1.4.2",
    "uptime_seconds": 7203,
    "last_health_check": "2026-11-01T09:00:00Z",
    "last_health_status": "up",
    "pid": 4321,
    "restarts": 2,
    "last_error": "crashed: exit 137 (OOM killed)"
  }
}`

// pluginStatusQuietView is a healthy managed plugin with no open event.
const pluginStatusQuietView = `{
  "name": "PluginStatusQuiet",
  "version": "1.4.2",
  "kind": "service",
  "env": {"API_TOKEN": "sk-live-9f8a7b6c5d4e3f2a1b0c"},
  "connection": {"base_url": "http://127.0.0.1:8317", "auth": {"type": "bearer", "token": "tok-SECRETVALUE-42"}},
  "lifecycle": {"supported_modes": ["managed"], "entry": "bin/svc"},
  "dir": "/opt/datawatch/plugins/quiet",
  "enabled": true,
  "invoke_count": 0,
  "error_count": 0,
  "service": {
    "mode": "managed",
    "state": "healthy",
    "address": "http://127.0.0.1:8317",
    "version": "1.4.2",
    "uptime_seconds": 7203,
    "last_health_check": "2026-11-01T09:00:00Z",
    "last_health_status": "up",
    "pid": 4321,
    "restarts": 2
  }
}`

// pluginStatusAttachView is the /api/plugins/<name> body for an
// attach-only service-kind plugin: no lifecycle state exists.
const pluginStatusAttachView = `{
  "name": "PluginStatusExternal",
  "version": "2.0.1",
  "kind": "service",
  "connection": {"base_url": "http://192.0.2.5:9000", "auth": {"type": "bearer", "token": "${secret:ext-service-token}"}},
  "dir": "/opt/datawatch/plugins/external",
  "enabled": true,
  "invoke_count": 0,
  "error_count": 0,
  "service": {
    "mode": "attach_only",
    "address": "http://192.0.2.5:9000",
    "version": "2.0.1",
    "uptime_seconds": 3600,
    "last_health_check": "2026-11-01T09:10:00Z",
    "last_health_status": "down",
    "last_health_error": "connection refused"
  }
}`

// pluginStatusAutomationView is a non-service plugin (no service block).
const pluginStatusAutomationView = `{
  "name": "PluginStatusHooks",
  "version": "0.3.0",
  "kind": "automation",
  "hooks": ["on_session_start"],
  "dir": "/opt/datawatch/plugins/hooks",
  "resolved_entry": "/opt/datawatch/plugins/hooks/bins/hook",
  "enabled": true,
  "invoke_count": 4,
  "error_count": 0
}`

// pluginStatusDaemon stands in for the loopback: it serves the named views
// on /api/plugins/<name> and 404s anything else, like the real handler.
func pluginStatusDaemon(t *testing.T, views map[string]string) int {
	t.Helper()
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Errorf("plugin status must GET, got %s", req.Method)
		}
		name := strings.TrimPrefix(req.URL.Path, "/api/plugins/")
		body, ok := views[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	return port
}

func pluginStatusViews() map[string]string {
	return map[string]string{
		"PluginStatusDemo":     pluginStatusManagedView,
		"PluginStatusQuiet":    pluginStatusQuietView,
		"PluginStatusExternal": pluginStatusAttachView,
		"PluginStatusHooks":    pluginStatusAutomationView,
	}
}

func TestPluginStatus_ManagedReply(t *testing.T) {
	r := setupPeerRouter(t, pluginStatusDaemon(t, pluginStatusViews()))
	got := strings.Join(r.HandleTestMessage("plugin status PluginStatusDemo"), " ")

	for _, want := range []string{
		"plugin PluginStatusDemo",
		"managed mode",
		"state:      starting",
		"address:    http://127.0.0.1:8317",
		"version:    1.4.2",
		"uptime:     2h0m3s",
		"last check: 2026-11-01T09:00:00Z (probe up)",
		"open event: crashed: exit 137 (OOM killed)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply missing %q\nreply: %s", want, got)
		}
	}
}

func TestPluginStatus_UnknownPlugin(t *testing.T) {
	r := setupPeerRouter(t, pluginStatusDaemon(t, pluginStatusViews()))
	got := strings.Join(r.HandleTestMessage("plugin status PluginStatusGhost"), " ")
	if !strings.Contains(got, "unknown plugin") {
		t.Errorf("want an unknown-plugin reply, got: %s", got)
	}
	if !strings.Contains(got, "usage: plugin status <name>") {
		t.Errorf("want usage hint, got: %s", got)
	}
}

func TestPluginStatus_AttachOnlyReply(t *testing.T) {
	r := setupPeerRouter(t, pluginStatusDaemon(t, pluginStatusViews()))
	got := strings.Join(r.HandleTestMessage("plugin status PluginStatusExternal"), " ")

	for _, want := range []string{
		"plugin PluginStatusExternal",
		"attach_only mode",
		"address:    http://192.0.2.5:9000",
		"version:    2.0.1",
		"uptime:     1h0m0s",
		"last check: 2026-11-01T09:10:00Z (probe down)",
		"open event: connection refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply missing %q\nreply: %s", want, got)
		}
	}
	if strings.Contains(got, "state:") {
		t.Errorf("attach-only plugin has no lifecycle state; reply must not fake one: %s", got)
	}
}

func TestPluginStatus_NoOpenEvent(t *testing.T) {
	r := setupPeerRouter(t, pluginStatusDaemon(t, pluginStatusViews()))
	got := strings.Join(r.HandleTestMessage("plugin status PluginStatusQuiet"), " ")
	if !strings.Contains(got, "open event: none") {
		t.Errorf("want 'open event: none', got: %s", got)
	}
}

func TestPluginStatus_NonServicePlugin(t *testing.T) {
	r := setupPeerRouter(t, pluginStatusDaemon(t, pluginStatusViews()))
	got := strings.Join(r.HandleTestMessage("plugin status PluginStatusHooks"), " ")
	if !strings.Contains(got, "service-kind") {
		t.Errorf("want a not-a-service-plugin reply, got: %s", got)
	}
}

func TestPluginStatus_NeverIncludesSecretValue(t *testing.T) {
	r := setupPeerRouter(t, pluginStatusDaemon(t, pluginStatusViews()))
	// Every reply from the verb, across plugin kinds and event states.
	replies := []string{}
	for _, name := range []string{
		"PluginStatusDemo",
		"PluginStatusQuiet",
		"PluginStatusExternal",
		"PluginStatusHooks",
		"PluginStatusGhost",
	} {
		replies = append(replies, strings.Join(r.HandleTestMessage("plugin status "+name), " "))
	}
	all := strings.Join(replies, "\n")
	for _, secret := range []string{
		"sk-live-9f8a7b6c5d4e3f2a1b0c", // literal API token in manifest env
		"tok-SECRETVALUE-42",           // connection auth token
		"${secret:ssh-private-key}",    // env secret reference
		"${secret:ext-service-token}",  // attach auth secret reference
	} {
		if strings.Contains(all, secret) {
			t.Errorf("reply leaked a secret value %q", secret)
		}
	}
}

func TestPluginStatus_BareShowsUsage(t *testing.T) {
	r := setupPeerRouter(t, pluginStatusDaemon(t, pluginStatusViews()))
	got := strings.Join(r.HandleTestMessage("plugin"), " ")
	if !strings.Contains(got, "usage: plugin status <name>") {
		t.Errorf("bare 'plugin' should show usage, got: %s", got)
	}
	got = strings.Join(r.HandleTestMessage("plugin status"), " ")
	if !strings.Contains(got, "usage: plugin status <name>") {
		t.Errorf("'plugin status' without a name should show usage, got: %s", got)
	}
}

func TestPluginStatus_Parse(t *testing.T) {
	c := Parse("plugin status PluginStatusDemo")
	if c.Type != CmdPluginStatus || c.Text != "status PluginStatusDemo" {
		t.Errorf("got %+v", c)
	}
	if c := Parse("plugin"); c.Type != CmdPluginStatus || c.Text != "" {
		t.Errorf("bare plugin: got %+v", c)
	}
	// The existing plural verb must be untouched.
	if c := Parse("plugins get PluginStatusDemo"); c.Type != CmdPlugins {
		t.Errorf("plugins verb regressed: got %+v", c)
	}
	if c := Parse("plugins"); c.Type != CmdPlugins {
		t.Errorf("bare plugins regressed: got %+v", c)
	}
}

// BL33 — plugins package unit tests.

package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/secrets"
)

// makePlugin writes a shell-script plugin manifest+entry into dir and
// returns the plugin directory. On Windows it skips.
func makePlugin(t *testing.T, root, name, entryScript string, hooks []string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entry := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(entry, []byte(entryScript), 0755); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	hooksStr := ""
	for _, h := range hooks {
		hooksStr += fmt.Sprintf("  - %s\n", h)
	}
	man := fmt.Sprintf(`name: %s
entry: ./plugin.sh
hooks:
%stimeout_ms: 1500
`, name, hooksStr)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(man), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func TestRegistry_Discover_FindsManifests(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "filter",
		"#!/bin/sh\nread line\necho '{\"action\":\"pass\"}'\n",
		[]string{"post_session_output"})
	r, err := NewRegistry(Config{Enabled: true, Dir: root})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got := r.List()
	if len(got) != 1 || got[0].Name != "filter" {
		t.Fatalf("expected one plugin 'filter', got %+v", got)
	}
}

// SetChangeFn fires after a rediscover and an enable toggle, and the hook
// may call back into the registry (the lock is not held).
func TestRegistry_SetChangeFn(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "filter",
		"#!/bin/sh\nread line\necho '{\"action\":\"pass\"}'\n",
		[]string{"post_session_output"})
	r, err := NewRegistry(Config{Enabled: true, Dir: root})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	calls := 0
	r.SetChangeFn(func() {
		calls++
		_ = r.List()
	})
	if err := r.Discover(); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !r.SetEnabled("filter", false) {
		t.Fatal("SetEnabled: plugin not found")
	}
	if r.SetEnabled("missing", true) {
		t.Fatal("SetEnabled on unknown plugin reported success")
	}
	if calls != 2 {
		t.Fatalf("change hook calls = %d, want 2", calls)
	}
}

func TestRegistry_Discover_SkipsNonPluginDirs(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "not-a-plugin"), 0755)
	_ = os.WriteFile(filepath.Join(root, "not-a-plugin", "README.md"),
		[]byte("hi"), 0644)
	r, _ := NewRegistry(Config{Enabled: true, Dir: root})
	if len(r.List()) != 0 {
		t.Fatalf("expected empty registry: %+v", r.List())
	}
}

func TestRegistry_Invoke_ReplacesLine(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "redact",
		`#!/bin/sh
# Read one line; respond with a redacted replacement.
read line
echo '{"action":"replace","line":"[redacted]"}'
`,
		[]string{"post_session_output"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	resp, err := r.Invoke(context.Background(), "redact",
		HookPostSessionOutput, Request{Line: "sk-abcd1234"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Action != "replace" || resp.Line != "[redacted]" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRegistry_Invoke_TimeoutReturnsPass(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "hang",
		"#!/bin/sh\nsleep 5\necho '{\"action\":\"pass\"}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 200})
	resp, err := r.Invoke(context.Background(), "hang",
		HookOnAlert, Request{Text: "anything"})
	if err != nil {
		t.Fatalf("Invoke returned hard error: %v", err)
	}
	if resp.Action != "pass" || resp.Error == "" {
		t.Fatalf("expected pass+error, got %+v", resp)
	}
}

func TestRegistry_Fanout_ChainReplacements(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "aaa-upper",
		`#!/bin/sh
read j
# crude: echo a fixed replacement
echo '{"action":"replace","line":"STAGE1"}'
`,
		[]string{"post_session_output"})
	makePlugin(t, root, "bbb-suffix",
		`#!/bin/sh
read j
echo '{"action":"replace","line":"STAGE1/STAGE2"}'
`,
		[]string{"post_session_output"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	resp, err := r.Fanout(context.Background(), HookPostSessionOutput,
		Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if resp.Action != "replace" || resp.Line != "STAGE1/STAGE2" {
		t.Fatalf("chain broken: %+v", resp)
	}
}

func TestRegistry_DisabledList_HonoredAtDiscovery(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "keep",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	makePlugin(t, root, "skip-me",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{
		Enabled: true, Dir: root, Disabled: []string{"skip-me"},
	})
	plugins := r.List()
	if len(plugins) != 2 {
		t.Fatalf("expected 2 discovered: %+v", plugins)
	}
	for _, p := range plugins {
		if p.Name == "skip-me" && p.Enabled {
			t.Fatalf("skip-me should have Enabled=false: %+v", p)
		}
		if p.Name == "keep" && !p.Enabled {
			t.Fatalf("keep should have Enabled=true: %+v", p)
		}
	}
}

func TestRegistry_SetEnabled(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "toggle",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root})
	if !r.SetEnabled("toggle", false) {
		t.Fatalf("SetEnabled: plugin not found")
	}
	p, _ := r.Get("toggle")
	if p.Enabled {
		t.Fatalf("toggle should be disabled")
	}
}

func TestRegistry_DisabledGlobal_NoDiscovery(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "x",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{Enabled: false, Dir: root})
	if got := r.List(); len(got) != 0 {
		t.Fatalf("disabled registry should skip discovery: %+v", got)
	}
}

// mockPluginStore implements secrets.Store for plugin env injection tests.
type mockPluginStore struct {
	data   map[string]string
	scopes map[string][]string
}

func (m *mockPluginStore) List() ([]secrets.Secret, error) { return nil, nil }
func (m *mockPluginStore) Get(name string) (secrets.Secret, error) {
	v, ok := m.data[name]
	if !ok {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	sec := secrets.Secret{Name: name, Value: v, Backend: "mock"}
	if m.scopes != nil {
		sec.Scopes = m.scopes[name]
	}
	return sec, nil
}
func (m *mockPluginStore) Set(n, v string, tags []string, desc string, scopes []string) error {
	return nil
}
func (m *mockPluginStore) Delete(name string) error { return nil }
func (m *mockPluginStore) Exists(name string) (bool, error) {
	_, ok := m.data[name]
	return ok, nil
}

// writeManifest writes a manifest.yaml into dir, overwriting any existing one.
func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestRegistry_Invoke_EnvInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	// Plugin echoes the MY_SECRET env var back as the replacement line.
	makePlugin(t, root, "env-test",
		"#!/bin/sh\nread j\nprintf '{\"action\":\"replace\",\"line\":\"%s\"}\\n' \"$MY_SECRET\"\n",
		[]string{"post_session_output"})
	// Overwrite the manifest to include an env block.
	writeManifest(t, filepath.Join(root, "env-test"), `name: env-test
entry: ./plugin.sh
hooks:
  - post_session_output
timeout_ms: 2000
env:
  MY_SECRET: "${secret:plugin-key}"
`)
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	r.SetSecretsStore(&mockPluginStore{data: map[string]string{"plugin-key": "injected-value"}})

	resp, err := r.Invoke(context.Background(), "env-test", HookPostSessionOutput, Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Line != "injected-value" {
		t.Fatalf("expected injected env var, got %q", resp.Line)
	}
}

func TestRegistry_Invoke_EnvInjection_ScopeDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	// Plugin echoes MY_SECRET; if scope is denied, the var should be absent.
	makePlugin(t, root, "env-scoped",
		"#!/bin/sh\nread j\nprintf '{\"action\":\"replace\",\"line\":\"%s\"}\\n' \"${MY_SECRET:-ABSENT}\"\n",
		[]string{"post_session_output"})
	writeManifest(t, filepath.Join(root, "env-scoped"), `name: env-scoped
entry: ./plugin.sh
hooks:
  - post_session_output
timeout_ms: 2000
env:
  MY_SECRET: "${secret:locked-key}"
`)
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	r.SetSecretsStore(&mockPluginStore{
		data:   map[string]string{"locked-key": "secret-value"},
		scopes: map[string][]string{"locked-key": {"agent:only"}}, // not "plugin:*"
	})

	resp, err := r.Invoke(context.Background(), "env-scoped", HookPostSessionOutput, Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	// Scope denied → env var not injected → plugin sees ABSENT
	if resp.Line != "ABSENT" {
		t.Fatalf("expected ABSENT (scope denied), got %q", resp.Line)
	}
}

func TestRegistry_Invoke_PlainEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	makePlugin(t, root, "plain-env",
		"#!/bin/sh\nread j\nprintf '{\"action\":\"replace\",\"line\":\"%s\"}\\n' \"$PLAIN_VAR\"\n",
		[]string{"post_session_output"})
	writeManifest(t, filepath.Join(root, "plain-env"), `name: plain-env
entry: ./plugin.sh
hooks:
  - post_session_output
timeout_ms: 2000
env:
  PLAIN_VAR: "hello-world"
`)
	// No secrets store wired — plain values still injected.
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})

	resp, err := r.Invoke(context.Background(), "plain-env", HookPostSessionOutput, Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Line != "hello-world" {
		t.Fatalf("expected plain env var injected, got %q", resp.Line)
	}
}

// ── Manifest v3 managed services: operator opt-in + Registry supervision ──
// These run the real testdata/svchelper child (built by buildSvcHelper in
// supervisor_test.go) — no mocked exec.

// writeServicePlugin writes a kind: service plugin whose lifecycle declares
// modes and, when modes includes managed, runs svchelper on port.
func writeServicePlugin(t *testing.T, root, name string, port int, modes ...string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	man := fmt.Sprintf(`name: %s
kind: service
connection:
  base_url: http://127.0.0.1:%d
  health_path: /healthz
lifecycle:
  supported_modes: [%s]
`, name, port, strings.Join(modes, ", "))
	for _, m := range modes {
		if m == LifecycleManaged {
			man += fmt.Sprintf("  entry: %s\n  args: [\"-port\", \"%d\"]\n  stop_timeout_ms: 2000\n", buildSvcHelper(t), port)
		}
	}
	writeManifest(t, dir, man)
}

// newManagedRegistry builds a registry over root and stops every managed
// service at test end.
func newManagedRegistry(t *testing.T, root string, managed ...string) *Registry {
	t.Helper()
	r, err := NewRegistry(Config{Enabled: true, Dir: root, Managed: managed})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = r.StopAll(ctx)
	})
	return r
}

// serviceAnswers reports whether svchelper answers its health endpoint.
func serviceAnswers(port int) bool {
	c := http.Client{Timeout: 300 * time.Millisecond}
	resp, err := c.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitRunning waits until name's service has a PID and answers on port.
func waitRunning(t *testing.T, r *Registry, name string, port int) int {
	t.Helper()
	var pid int
	waitFor(t, 10*time.Second, name+" running", func() bool {
		st, ok := r.ServiceStatus(name)
		pid = st.PID
		return ok && st.PID > 0 && serviceAnswers(port)
	})
	return pid
}

func TestRegistry_Managed_IneligibleKindsNeverStarted(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "auto", "#!/bin/sh\nread l\necho '{}'\n", []string{"post_session_output"})
	attachPort := freePort(t)
	writeServicePlugin(t, root, "attach", attachPort, LifecycleAttachOnly)
	r := newManagedRegistry(t, root, "auto", "attach")

	r.StartManaged(context.Background())
	for _, name := range []string{"auto", "attach"} {
		if _, ok := r.ServiceStatus(name); ok {
			t.Errorf("%s: supervisor created for a plugin that cannot be managed", name)
		}
		if r.SetManaged(name, true) {
			t.Errorf("SetManaged(%s, true) = true, want rejection", name)
		}
	}
	if serviceAnswers(attachPort) {
		t.Error("attach_only plugin was started")
	}
}

func TestRegistry_Managed_NotOptedInNeverStarted(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	writeServicePlugin(t, root, "svc", port, LifecycleAttachOnly, LifecycleManaged)
	r := newManagedRegistry(t, root) // no operator opt-in

	r.StartManaged(context.Background())
	if _, ok := r.ServiceStatus("svc"); ok {
		t.Fatal("supervisor created without operator opt-in")
	}
	time.Sleep(200 * time.Millisecond)
	if serviceAnswers(port) {
		t.Fatal("service started without operator opt-in")
	}
}

func TestRegistry_Managed_NothingSpawnedBeforeStartManaged(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	writeServicePlugin(t, root, "svc", port, LifecycleManaged)
	r := newManagedRegistry(t, root, "svc")

	// Discover, SetEnabled and SetManaged must not spawn on the no-daemon path.
	if err := r.Discover(); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	r.SetEnabled("svc", false)
	r.SetEnabled("svc", true)
	if !r.SetManaged("svc", true) {
		t.Fatal("SetManaged rejected a managed-declared plugin")
	}
	if _, ok := r.ServiceStatus("svc"); ok {
		t.Fatal("supervisor created before StartManaged")
	}
	if serviceAnswers(port) {
		t.Fatal("service started before StartManaged")
	}
}

func TestRegistry_Managed_StartAndStopOnDisable(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	writeServicePlugin(t, root, "svc", port, LifecycleManaged)
	r := newManagedRegistry(t, root, "svc")
	log := &eventLog{}
	r.SetServiceEventFn(log.add)

	r.StartManaged(context.Background())
	pid := waitRunning(t, r, "svc", port)
	if log.count(EventServiceStart) != 1 {
		t.Errorf("start events = %d, want 1", log.count(EventServiceStart))
	}

	// The API surfaces the service status object for a supervised plugin…
	got, ok := NewAPI(r).Get("svc")
	if !ok {
		t.Fatal("API.Get(svc) not found")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Name    string `json:"name"`
		Service *struct {
			State    string `json:"state"`
			PID      int    `json:"pid"`
			Restarts int    `json:"restarts"`
		} `json:"service"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Name != "svc" || decoded.Service == nil || decoded.Service.PID != pid || decoded.Service.State == "" {
		t.Fatalf("plugin JSON missing service status: %s", b)
	}

	if !r.SetEnabled("svc", false) {
		t.Fatal("SetEnabled(false) = false")
	}
	if _, ok := r.ServiceStatus("svc"); ok {
		t.Error("supervisor still registered after disable")
	}
	assertGone(t, pid)
	if log.count(EventServiceStop) != 1 {
		t.Errorf("stop events = %d, want 1", log.count(EventServiceStop))
	}

	// …and omits it once there is no supervisor.
	got, _ = NewAPI(r).Get("svc")
	b, _ = json.Marshal(got)
	var plain map[string]any
	if err := json.Unmarshal(b, &plain); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, has := plain["service"]; has {
		t.Errorf("unsupervised plugin JSON has a service object: %s", b)
	}

	// Re-enabling an eligible plugin after StartManaged starts it again.
	r.SetEnabled("svc", true)
	if pid2 := waitRunning(t, r, "svc", port); pid2 == pid {
		t.Errorf("re-enabled service reused pid %d", pid)
	}
}

func TestRegistry_SetManaged(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	writeServicePlugin(t, root, "svc", port, LifecycleManaged)
	writeServicePlugin(t, root, "attach", freePort(t), LifecycleAttachOnly)
	r := newManagedRegistry(t, root)
	r.StartManaged(context.Background())

	if r.SetManaged("attach", true) {
		t.Error("SetManaged accepted a plugin lacking managed in supported_modes")
	}
	if r.SetManaged("missing", true) {
		t.Error("SetManaged accepted an unknown plugin")
	}
	if !r.SetManaged("svc", true) {
		t.Fatal("SetManaged(svc, true) rejected")
	}
	pid := waitRunning(t, r, "svc", port)
	if !r.SetManaged("svc", false) {
		t.Fatal("SetManaged(svc, false) rejected")
	}
	if _, ok := r.ServiceStatus("svc"); ok {
		t.Error("supervisor still registered after opt-out")
	}
	assertGone(t, pid)

	// The runtime opt-in survives a rediscover (hot-reload).
	r.SetManaged("svc", true)
	waitRunning(t, r, "svc", port)
	if err := r.Discover(); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if _, ok := r.ServiceStatus("svc"); !ok {
		t.Error("rediscover dropped the runtime opt-in")
	}
}

func TestRegistry_Managed_RediscoverKeepsStopsAndStarts(t *testing.T) {
	root := t.TempDir()
	keepPort, gonePort, newPort := freePort(t), freePort(t), freePort(t)
	writeServicePlugin(t, root, "keep", keepPort, LifecycleManaged)
	writeServicePlugin(t, root, "gone", gonePort, LifecycleManaged)
	r := newManagedRegistry(t, root, "keep", "gone", "new")
	r.StartManaged(context.Background())
	keepPID := waitRunning(t, r, "keep", keepPort)
	gonePID := waitRunning(t, r, "gone", gonePort)

	if err := os.RemoveAll(filepath.Join(root, "gone")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeServicePlugin(t, root, "new", newPort, LifecycleManaged)
	if err := r.Discover(); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if st, ok := r.ServiceStatus("keep"); !ok || st.PID != keepPID {
		t.Errorf("keep: status=%+v ok=%v, want the original pid %d still supervised", st, ok, keepPID)
	}
	if _, ok := r.ServiceStatus("gone"); ok {
		t.Error("gone: supervisor survived plugin removal")
	}
	assertGone(t, gonePID)
	waitRunning(t, r, "new", newPort)
}

func TestRegistry_StopAll(t *testing.T) {
	root := t.TempDir()
	p1, p2 := freePort(t), freePort(t)
	writeServicePlugin(t, root, "one", p1, LifecycleManaged)
	writeServicePlugin(t, root, "two", p2, LifecycleManaged)
	r := newManagedRegistry(t, root, "one", "two")
	r.StartManaged(context.Background())
	pid1 := waitRunning(t, r, "one", p1)
	pid2 := waitRunning(t, r, "two", p2)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := r.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	for _, name := range []string{"one", "two"} {
		st, ok := r.ServiceStatus(name)
		if !ok || st.State != StateStopped || st.PID != 0 {
			t.Errorf("%s after StopAll: status=%+v ok=%v, want stopped", name, st, ok)
		}
	}
	assertGone(t, pid1)
	assertGone(t, pid2)
	for _, v := range NewAPI(r).List() {
		b, _ := json.Marshal(v)
		var pv struct {
			Service *struct {
				State string `json:"state"`
			} `json:"service"`
		}
		if err := json.Unmarshal(b, &pv); err != nil || pv.Service == nil || pv.Service.State != string(StateStopped) {
			t.Errorf("API.List entry after StopAll lacks a stopped service status: %s", b)
		}
	}

	// StopAll clears the started flag: a later rediscover spawns nothing.
	if err := r.Discover(); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if st, _ := r.ServiceStatus("one"); st.State != StateStopped {
		t.Errorf("rediscover after StopAll restarted a service: %+v", st)
	}

	// StartManaged starts the stopped supervisors again.
	r.StartManaged(context.Background())
	waitRunning(t, r, "one", p1)
	waitRunning(t, r, "two", p2)
}

func TestRegistry_StartManaged_FailureDoesNotBlockOthers(t *testing.T) {
	root := t.TempDir()
	goodPort, badPort := freePort(t), freePort(t)
	writeServicePlugin(t, root, "good", goodPort, LifecycleManaged)
	writeServicePlugin(t, root, "bad", badPort, LifecycleManaged)
	// Point bad's lifecycle entry at a binary that does not exist.
	writeManifest(t, filepath.Join(root, "bad"), fmt.Sprintf(`name: bad
kind: service
connection:
  base_url: http://127.0.0.1:%d
lifecycle:
  supported_modes: [managed]
  entry: ./does-not-exist
`, badPort))
	r := newManagedRegistry(t, root, "good", "bad")

	r.StartManaged(context.Background())
	waitRunning(t, r, "good", goodPort)
	st, ok := r.ServiceStatus("bad")
	if !ok || st.State != StateFailed || st.LastError == "" {
		t.Fatalf("bad: status=%+v ok=%v, want a registered failed supervisor with its error", st, ok)
	}
}

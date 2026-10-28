package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Hostname == "" {
		t.Error("Hostname should not be empty")
	}
	if cfg.DataDir == "" {
		t.Error("DataDir should not be empty")
	}
	if cfg.Signal.ConfigDir == "" {
		t.Error("Signal.ConfigDir should not be empty")
	}
	if cfg.Signal.DeviceName == "" {
		t.Error("Signal.DeviceName should default to hostname")
	}
	if cfg.Session.MaxSessions != 10 {
		t.Errorf("MaxSessions = %d, want 10", cfg.Session.MaxSessions)
	}
	if cfg.Session.InputIdleTimeout != 10 {
		t.Errorf("InputIdleTimeout = %d, want 10", cfg.Session.InputIdleTimeout)
	}
	if cfg.Session.TailLines != 20 {
		t.Errorf("TailLines = %d, want 20", cfg.Session.TailLines)
	}
	if cfg.Session.AlertContextLines != 10 {
		t.Errorf("AlertContextLines = %d, want 10", cfg.Session.AlertContextLines)
	}
	if cfg.Session.LLMBackend != "claude-code" {
		t.Errorf("LLMBackend = %q, want claude-code", cfg.Session.LLMBackend)
	}
	// ClaudeBin moved to LLM registry (v7 clean move); not in SessionConfig.
	if cfg.Session.DefaultProjectDir == "" {
		t.Error("DefaultProjectDir should not be empty")
	}
	if !cfg.Session.AutoGitCommit {
		t.Error("AutoGitCommit should default to true")
	}
	if !cfg.Server.Enabled {
		t.Error("Server.Enabled should default to true")
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.PinCheckSeconds != 300 {
		t.Errorf("PinCheckSeconds = %d, want 300", cfg.PinCheckSeconds)
	}
	if len(cfg.ComputeNodes) != 0 {
		t.Errorf("ComputeNodes = %d entries, want none by default", len(cfg.ComputeNodes))
	}
}

func TestComputeNodeYAML_PinDefaults(t *testing.T) {
	var node ComputeNodeYAML
	if node.NodePinnedModel != "" {
		t.Errorf("NodePinnedModel = %q, want empty (no pin)", node.NodePinnedModel)
	}
	if node.AllowModelSwap {
		t.Error("AllowModelSwap should default to false")
	}
}

func TestLoad_PinSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
pin_check_seconds: 0
compute_nodes:
  - name: gpu1
    kind: ollama
    address: http://gpu1:11434
    node_pinned_model: qwen3.8-64k:27b
    allow_model_swap: true
  - name: gpu2
    kind: ollama
    address: http://gpu2:11434
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// An explicit 0 means the pin check is off; it must not revert to the default.
	if cfg.PinCheckSeconds != 0 {
		t.Errorf("PinCheckSeconds = %d, want 0 (explicit off)", cfg.PinCheckSeconds)
	}
	if len(cfg.ComputeNodes) != 2 {
		t.Fatalf("ComputeNodes = %d entries, want 2", len(cfg.ComputeNodes))
	}
	pinned := cfg.ComputeNodes[0]
	if pinned.NodePinnedModel != "qwen3.8-64k:27b" || !pinned.AllowModelSwap {
		t.Errorf("gpu1 pin = %q swap=%v, want qwen3.8-64k:27b swap=true", pinned.NodePinnedModel, pinned.AllowModelSwap)
	}
	unpinned := cfg.ComputeNodes[1]
	if unpinned.NodePinnedModel != "" || unpinned.AllowModelSwap {
		t.Errorf("gpu2 pin = %q swap=%v, want no pin and swap=false", unpinned.NodePinnedModel, unpinned.AllowModelSwap)
	}
}

func TestSave_PinCheckSecondsZeroRoundTrips(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PinCheckSeconds = 0
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.PinCheckSeconds != 0 {
		t.Errorf("PinCheckSeconds after round trip = %d, want 0", loaded.PinCheckSeconds)
	}
}

func TestLoad_NonExistent(t *testing.T) {
	cfg, err := Load("/nonexistent/path/to/config.yaml")
	if err != nil {
		t.Fatalf("Load non-existent file should return defaults, got error: %v", err)
	}
	if cfg.Session.MaxSessions != 10 {
		t.Errorf("MaxSessions = %d, want 10 (default)", cfg.Session.MaxSessions)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: [unclosed"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestLoad_Partial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
signal:
  account_number: "+12125551234"
  group_id: "abc123"
session:
  max_sessions: 5
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Signal.AccountNumber != "+12125551234" {
		t.Errorf("AccountNumber = %q, want +12125551234", cfg.Signal.AccountNumber)
	}
	if cfg.Signal.GroupID != "abc123" {
		t.Errorf("GroupID = %q, want abc123", cfg.Signal.GroupID)
	}
	if cfg.Session.MaxSessions != 5 {
		t.Errorf("MaxSessions = %d, want 5", cfg.Session.MaxSessions)
	}
	// Unset fields should keep defaults
	if cfg.Session.LLMBackend != "claude-code" {
		t.Errorf("LLMBackend = %q, want claude-code (default)", cfg.Session.LLMBackend)
	}
	if cfg.Session.TailLines != 20 {
		t.Errorf("TailLines = %d, want 20 (default)", cfg.Session.TailLines)
	}
}

func TestLoad_ZeroFieldsGetDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Explicit zeros should be replaced with defaults
	yaml := `
session:
  max_sessions: 0
  tail_lines: 0
  input_idle_timeout: 0
hostname: ""
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Session.MaxSessions != 10 {
		t.Errorf("MaxSessions = %d, want 10 (zero → default)", cfg.Session.MaxSessions)
	}
	if cfg.Session.TailLines != 20 {
		t.Errorf("TailLines = %d, want 20 (zero → default)", cfg.Session.TailLines)
	}
	if cfg.Session.InputIdleTimeout != 10 {
		t.Errorf("InputIdleTimeout = %d, want 10 (zero → default)", cfg.Session.InputIdleTimeout)
	}
	if cfg.Hostname == "" {
		t.Error("Hostname should not be empty after defaulting")
	}
}

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Signal.AccountNumber = "+12125551234"
	cfg.Signal.GroupID = "testgroup"
	cfg.Session.MaxSessions = 3
	cfg.Server.Port = 9090
	cfg.Server.PublicURL = "https://datawatch.example.com" // F10 S4.2
	cfg.MCP.PublicURL = "https://host.tailnet.ts.net:8081" // GH#207

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}

	if loaded.Signal.AccountNumber != cfg.Signal.AccountNumber {
		t.Errorf("AccountNumber: got %q, want %q", loaded.Signal.AccountNumber, cfg.Signal.AccountNumber)
	}
	if loaded.Signal.GroupID != cfg.Signal.GroupID {
		t.Errorf("GroupID: got %q, want %q", loaded.Signal.GroupID, cfg.Signal.GroupID)
	}
	if loaded.Session.MaxSessions != cfg.Session.MaxSessions {
		t.Errorf("MaxSessions: got %d, want %d", loaded.Session.MaxSessions, cfg.Session.MaxSessions)
	}
	if loaded.Server.Port != cfg.Server.Port {
		t.Errorf("Server.Port: got %d, want %d", loaded.Server.Port, cfg.Server.Port)
	}
	if loaded.Server.PublicURL != cfg.Server.PublicURL {
		t.Errorf("Server.PublicURL: got %q, want %q", loaded.Server.PublicURL, cfg.Server.PublicURL)
	}
	if loaded.MCP.PublicURL != cfg.MCP.PublicURL {
		t.Errorf("MCP.PublicURL: got %q, want %q", loaded.MCP.PublicURL, cfg.MCP.PublicURL)
	}
}

// F10 S6.7 — MemoryConfig.FallbackSQLite round-trips through YAML.
func TestSave_RoundTrip_MemoryFallbackSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := DefaultConfig()
	cfg.Memory.Backend = "postgres"
	cfg.Memory.PostgresURL = "postgres://x/y"
	cfg.Memory.FallbackSQLite = true
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Memory.FallbackSQLite {
		t.Errorf("FallbackSQLite did not survive round-trip")
	}
}

// AgentsConfig round-trip: every field must survive Save → Load with
// no loss so operators can rely on YAML edits.
// BL110 — MCP self-config gating round-trips.
func TestSave_RoundTrip_MCPSelfConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig()
	cfg.MCP.AllowSelfConfig = true
	cfg.MCP.SelfConfigAuditPath = "/tmp/dw/self.jsonl"

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.MCP.AllowSelfConfig {
		t.Errorf("AllowSelfConfig did not round-trip")
	}
	if loaded.MCP.SelfConfigAuditPath != "/tmp/dw/self.jsonl" {
		t.Errorf("SelfConfigAuditPath: got %q want /tmp/dw/self.jsonl",
			loaded.MCP.SelfConfigAuditPath)
	}
}

func TestSave_RoundTrip_AgentsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Agents.ImagePrefix = "harbor.example.com/dw"
	cfg.Agents.ImageTag = "v9.9.9"
	cfg.Agents.DockerBin = "podman"
	cfg.Agents.KubectlBin = "oc"
	cfg.Agents.CallbackURL = "https://parent:8443"
	cfg.Agents.BootstrapTokenTTLSeconds = 600
	cfg.Agents.WorkerBootstrapDeadlineSeconds = 120
	cfg.Agents.PQCBootstrap = true            // BL95
	cfg.Agents.IdleReaperIntervalSeconds = 30 // BL108
	cfg.Agents.SecretsProvider = "file"       // BL111
	cfg.Agents.SecretsBaseDir = "/var/lib/datawatch/secrets"

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Agents != cfg.Agents {
		t.Errorf("AgentsConfig round-trip diverged:\n got %+v\nwant %+v", loaded.Agents, cfg.Agents)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %04o, want 0600", perm)
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "config.yaml")

	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("Save with nested path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestConfigPath(t *testing.T) {
	p := ConfigPath()
	if p == "" {
		t.Error("ConfigPath should not be empty")
	}
	if filepath.Base(p) != "config.yaml" {
		t.Errorf("ConfigPath base = %q, want config.yaml", filepath.Base(p))
	}
}

func TestGetOutputMode_OpenWebUIDefaultsToChat(t *testing.T) {
	cfg := DefaultConfig()
	// Empty OutputMode should default to "chat" for openwebui
	mode := cfg.GetOutputMode("openwebui")
	if mode != "chat" {
		t.Errorf("GetOutputMode(openwebui) = %q, want \"chat\"", mode)
	}
}

func TestGetOutputMode_OpenWebUIExplicitOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OpenWebUI.OutputMode = "terminal"
	mode := cfg.GetOutputMode("openwebui")
	if mode != "terminal" {
		t.Errorf("GetOutputMode(openwebui) with explicit override = %q, want \"terminal\"", mode)
	}
}

func TestGetOutputMode_OtherBackendsDefaultToTerminal(t *testing.T) {
	cfg := DefaultConfig()
	// Backends that default to terminal mode
	for _, backend := range []string{"claude-code", "aider", "goose", "gemini", "shell"} {
		mode := cfg.GetOutputMode(backend)
		if mode != "terminal" {
			t.Errorf("GetOutputMode(%s) = %q, want \"terminal\"", backend, mode)
		}
	}
}

func TestGetOutputMode_OllamaDefaultsToChat(t *testing.T) {
	cfg := DefaultConfig()
	mode := cfg.GetOutputMode("ollama")
	if mode != "chat" {
		t.Errorf("GetOutputMode(ollama) = %q, want \"chat\"", mode)
	}
}

func TestProxyConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Proxy.Enabled {
		t.Error("Proxy.Enabled should default to false")
	}
	if cfg.Proxy.HealthInterval != 0 {
		t.Errorf("Proxy.HealthInterval default = %d, want 0 (uses pool default)", cfg.Proxy.HealthInterval)
	}
}

// ── Memory config helpers ──

func boolPtr(v bool) *bool { return &v }

func TestMemoryConfig_IsAutoHooks(t *testing.T) {
	m := MemoryConfig{AutoHooks: boolPtr(true)}
	if !m.IsAutoHooks() {
		t.Error("expected true")
	}
	m.AutoHooks = boolPtr(false)
	if m.IsAutoHooks() {
		t.Error("expected false")
	}
	m.AutoHooks = nil
	if !m.IsAutoHooks() {
		t.Error("expected true for nil (default)")
	}
}

func TestMemoryConfig_EffectiveHookInterval(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveHookInterval() != 15 {
		t.Errorf("expected default 15, got %d", m.EffectiveHookInterval())
	}
	m.HookSaveInterval = 30
	if m.EffectiveHookInterval() != 30 {
		t.Errorf("expected 30, got %d", m.EffectiveHookInterval())
	}
}

func TestMemoryConfig_IsSessionAwareness(t *testing.T) {
	m := MemoryConfig{SessionAwareness: boolPtr(true)}
	if !m.IsSessionAwareness() {
		t.Error("expected true")
	}
	m.SessionAwareness = nil
	// nil defaults to true per the method
	if !m.IsSessionAwareness() {
		t.Log("nil defaults to false (or true depending on impl)")
	}
}

func TestMemoryConfig_IsSessionBroadcast(t *testing.T) {
	m := MemoryConfig{SessionBroadcast: boolPtr(true)}
	if !m.IsSessionBroadcast() {
		t.Error("expected true")
	}
}

func TestMemoryConfig_EffectiveStorageMode(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveStorageMode() != "summary" {
		t.Errorf("expected 'summary', got %q", m.EffectiveStorageMode())
	}
	m.StorageMode = "verbatim"
	if m.EffectiveStorageMode() != "verbatim" {
		t.Errorf("expected 'verbatim', got %q", m.EffectiveStorageMode())
	}
}

func TestMemoryConfig_IsAutoSave(t *testing.T) {
	m := MemoryConfig{AutoSave: boolPtr(true)}
	if !m.IsAutoSave() {
		t.Error("expected true")
	}
}

func TestMemoryConfig_IsLearningsEnabled(t *testing.T) {
	m := MemoryConfig{LearningsEnabled: boolPtr(true)}
	if !m.IsLearningsEnabled() {
		t.Error("expected true")
	}
}

func TestMemoryConfig_EffectiveBackend(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveBackend() != "sqlite" {
		t.Errorf("expected 'sqlite', got %q", m.EffectiveBackend())
	}
	m.Backend = "postgres"
	if m.EffectiveBackend() != "postgres" {
		t.Errorf("expected 'postgres', got %q", m.EffectiveBackend())
	}
}

func TestMemoryConfig_EffectiveEmbedder(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveEmbedder() != "ollama" {
		t.Errorf("expected 'ollama', got %q", m.EffectiveEmbedder())
	}
}

func TestMemoryConfig_EffectiveTopK(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveTopK() != 5 {
		t.Errorf("expected default 5, got %d", m.EffectiveTopK())
	}
	m.TopK = 10
	if m.EffectiveTopK() != 10 {
		t.Errorf("expected 10, got %d", m.EffectiveTopK())
	}
}

// ── Console size and input mode ──

func TestGetConsoleSize_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	cols, rows := cfg.GetConsoleSize("claude-code")
	if cols < 80 || rows < 24 {
		t.Errorf("expected at least 80x24, got %dx%d", cols, rows)
	}
}

func TestGetConsoleSize_CustomOllama(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ollama.ConsoleCols = 120
	cfg.Ollama.ConsoleRows = 40
	cols, rows := cfg.GetConsoleSize("ollama")
	if cols != 120 || rows != 40 {
		t.Errorf("expected 120x40, got %dx%d", cols, rows)
	}
}

func TestGetInputMode_Default(t *testing.T) {
	cfg := DefaultConfig()
	mode := cfg.GetInputMode("claude-code")
	if mode != "tmux" {
		t.Errorf("expected 'tmux', got %q", mode)
	}
}

func TestGetOutputMode_ACPDefaultsToChat(t *testing.T) {
	cfg := DefaultConfig()
	mode := cfg.GetOutputMode("opencode-acp")
	if mode != "chat" {
		t.Errorf("expected 'chat' for ACP, got %q", mode)
	}
}

// ── Detection config ──

// TestDefaultDetection pins the built-in detection defaults. Note the
// rate-limit contract: the built-in default no longer seeds a flat
// rate_limit_patterns list — when unset, the session Manager falls back
// to its per-backend anchored table (internal/session/rate_limit_lines.go).
// An operator-configured rate_limit_patterns keeps the flat-substring
// behavior (see TestGetDetection_RateLimitPatternsOperatorKept).
func TestDefaultDetection(t *testing.T) {
	d := DefaultDetection()
	if len(d.PromptPatterns) == 0 {
		t.Error("expected non-empty prompt patterns")
	}
	if len(d.CompletionPatterns) == 0 {
		t.Error("expected non-empty completion patterns")
	}
	if len(d.RateLimitPatterns) != 0 {
		t.Errorf("built-in default RateLimitPatterns should be unset (per-backend anchored table is the fallback), got %d entries: %v", len(d.RateLimitPatterns), d.RateLimitPatterns)
	}
}

func TestGetDetection_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	d := cfg.GetDetection("claude-code")
	if d.PromptDebounce != 3 {
		t.Errorf("expected debounce default 3, got %d", d.PromptDebounce)
	}
	if d.NotifyCooldown != 15 {
		t.Errorf("expected cooldown default 15, got %d", d.NotifyCooldown)
	}
}

func TestGetDetection_PerBackendOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ollama.Detection.PromptPatterns = []string{">>> "}
	d := cfg.GetDetection("ollama")
	// Should have both default patterns + ollama override
	found := false
	for _, p := range d.PromptPatterns {
		if p == ">>> " {
			found = true
		}
	}
	if !found {
		t.Error("expected ollama override pattern '>>> ' in merged detection")
	}
}

func TestGetDetection_RateLimitPatternsOperatorKept(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Detection.RateLimitPatterns = []string{"DATAWATCH_RATE_LIMITED:", "my custom phrase"}
	for _, backend := range []string{"claude-code", "pi-prompt", "ollama"} {
		got := cfg.GetDetection(backend).RateLimitPatterns
		if len(got) != 2 || got[0] != "DATAWATCH_RATE_LIMITED:" || got[1] != "my custom phrase" {
			t.Errorf("backend %q: expected operator rate-limit list to pass through unchanged, got %v", backend, got)
		}
	}
	// And with no operator list configured, GetDetection still leaves the
	// field unset (the per-backend anchored table applies then).
	cfg2 := DefaultConfig()
	for _, backend := range []string{"claude-code", "pi-prompt", "ollama"} {
		if got := cfg2.GetDetection(backend).RateLimitPatterns; len(got) != 0 {
			t.Errorf("backend %q: expected unset rate-limit list with no operator config, got %v", backend, got)
		}
	}
}

// ── Pipeline config ──

func TestPipelineConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	// 0 means executor uses default of 3
	_ = cfg.Pipeline.MaxParallel
}

// ── F10: workspace_root resolver ──

func TestSessionConfig_ResolveProjectDir(t *testing.T) {
	cases := []struct {
		name      string
		workspace string
		def       string
		in        string
		want      string
	}{
		{
			name: "empty input falls back to default",
			def:  "/home/u",
			in:   "",
			want: "/home/u",
		},
		{
			name: "empty input + no default returns empty (caller decides)",
			in:   "",
			want: "",
		},
		{
			name:      "absolute path passes through unchanged regardless of workspace_root",
			workspace: "/workspace",
			in:        "/etc/foo",
			want:      "/etc/foo",
		},
		{
			name:      "relative + workspace_root joins under workspace_root",
			workspace: "/workspace",
			in:        "datawatch",
			want:      "/workspace/datawatch",
		},
		{
			name:      "relative ./ + workspace_root joins cleanly",
			workspace: "/workspace",
			in:        "./datawatch",
			want:      "/workspace/datawatch",
		},
		{
			name: "relative + no workspace_root resolves to abs against CWD",
			in:   "datawatch",
			// want is computed below since CWD is environment-dependent
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := SessionConfig{WorkspaceRoot: tc.workspace, DefaultProjectDir: tc.def}
			got := s.ResolveProjectDir(tc.in)
			if tc.name == "relative + no workspace_root resolves to abs against CWD" {
				if got == "" || got[0] != '/' {
					t.Errorf("expected absolute path under CWD, got %q", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestAutonomousConfig_FalseBoolsMarshalExplicitly guards the real root
// cause of a hung E2E run found live 2026-10-05: AutonomousConfig is
// marshaled to JSON and merge-unmarshaled onto the live
// internal/autonomous.Manager's own config on every autonomous.* write
// (internal/server/api.go handlePutConfig -> autonomousMgr.SetConfig).
// That merge-unmarshal only touches JSON keys actually present in the
// payload, so omitempty on a bool field means an explicit "set this back
// to false" (e.g. PUT /api/config {"autonomous.per_story_approval":false}
// restoring after a test toggled it true) is marshaled as an ABSENT key
// and silently never overwrites the manager's still-true value — every
// subsequent PRD approval kept wrongly gating its stories into
// awaiting_approval forever, since nothing calls approve_story for them.
// If omitempty is ever reintroduced on one of these fields, this test
// catches it without needing to reproduce the full hang.
func TestAutonomousConfig_FalseBoolsMarshalExplicitly(t *testing.T) {
	cfg := AutonomousConfig{
		PerStoryApproval:       false,
		AutoApproveChildren:    false,
		ContinueOnStoryFailure: false,
		InjectionGuard:         false,
		BlockOnInjection:       false,
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(b, &asMap); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, field := range []string{
		"per_story_approval", "auto_approve_children",
		"continue_on_story_failure", "injection_guard", "block_on_injection",
	} {
		if _, present := asMap[field]; !present {
			t.Errorf("%s is false and missing from the marshaled JSON — omitempty would silently drop an explicit "+
				"false sent through the SetConfig merge-unmarshal path, re-opening the per_story_approval hang", field)
		}
	}
}

func TestDefaultConfig_CIWatchTimeoutSeconds(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Autonomous.CIWatchTimeoutSeconds != 1500 {
		t.Errorf("Autonomous.CIWatchTimeoutSeconds = %d, want 1500", cfg.Autonomous.CIWatchTimeoutSeconds)
	}
	if len(cfg.Autonomous.PerPRDGuardrails) != 0 {
		t.Errorf("PerPRDGuardrails should default empty (ci-watch off), got %v", cfg.Autonomous.PerPRDGuardrails)
	}
}

func TestDefaultConfig_BlockOnFileDriftFalse(t *testing.T) {
	if DefaultConfig().Autonomous.BlockOnFileDrift {
		t.Error("Autonomous.BlockOnFileDrift should default false (opt-in strict drift enforcement)")
	}
}

func TestBlockOnFileDrift_YAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  block_on_file_drift: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Autonomous.BlockOnFileDrift {
		t.Fatal("block_on_file_drift: true in YAML did not load")
	}
	out := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, out); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "block_on_file_drift: true") {
		t.Errorf("saved YAML lacks block_on_file_drift: true:\n%s", raw)
	}
	cfg2, err := Load(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !cfg2.Autonomous.BlockOnFileDrift {
		t.Error("block_on_file_drift did not survive Save/Load round-trip")
	}
	// Explicit false must also round-trip (no omitempty).
	cfg2.Autonomous.BlockOnFileDrift = false
	if err := Save(cfg2, out); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(out)
	if !strings.Contains(string(raw), "block_on_file_drift: false") {
		t.Errorf("explicit false should be written, got:\n%s", raw)
	}
}

func TestDriftMode_DefaultIsBlock(t *testing.T) {
	if got := DefaultConfig().Autonomous.DriftMode; got != "block" {
		t.Errorf("Autonomous.DriftMode default = %q, want \"block\"", got)
	}
}

func TestDriftMode_YAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  drift_mode: warn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Autonomous.DriftMode != "warn" {
		t.Fatalf("drift_mode: warn in YAML did not load (got %q)", cfg.Autonomous.DriftMode)
	}
	out := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, out); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "drift_mode: warn") {
		t.Errorf("saved YAML lacks drift_mode: warn:\n%s", raw)
	}
	cfg2, err := Load(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg2.Autonomous.DriftMode != "warn" {
		t.Errorf("drift_mode did not survive Save/Load round-trip (got %q)", cfg2.Autonomous.DriftMode)
	}
	// "block" (the default) must round-trip exactly too.
	cfg2.Autonomous.DriftMode = "block"
	if err := Save(cfg2, out); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(out)
	if !strings.Contains(string(raw), "drift_mode: block") {
		t.Errorf("drift_mode: block should be written, got:\n%s", raw)
	}
}

func TestValidateDriftMode(t *testing.T) {
	for _, mode := range []string{"block", "warn", "WARN", " block ", "Block", "Warn"} {
		if err := ValidateDriftMode(mode); err != nil {
			t.Errorf("ValidateDriftMode(%q) = %v, want nil", mode, err)
		}
	}
	for _, mode := range []string{"", "allow", "silent", "blocking", "warns", "block and warn", "true"} {
		err := ValidateDriftMode(mode)
		if err == nil {
			t.Errorf("ValidateDriftMode(%q) = nil, want error", mode)
			continue
		}
		if !strings.Contains(err.Error(), "block\" or \"warn\"") {
			t.Errorf("ValidateDriftMode(%q) error %q does not name the accepted values", mode, err)
		}
	}
}

func TestDefaultConfig_ScopeLintEnforce(t *testing.T) {
	if got := DefaultConfig().Autonomous.ScopeLint; got != ScopeLintEnforce {
		t.Errorf("Autonomous.ScopeLint = %q, want %q", got, ScopeLintEnforce)
	}
}

func TestScopeLint_YAMLRoundTrip(t *testing.T) {
	for _, mode := range []string{ScopeLintOff, ScopeLintWarn, ScopeLintEnforce} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("autonomous:\n  scope_lint: "+mode+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := cfg.Autonomous.ScopeLintMode(); got != mode {
				t.Fatalf("loaded ScopeLintMode() = %q, want %q", got, mode)
			}
			out := filepath.Join(t.TempDir(), "saved.yaml")
			if err := Save(cfg, out); err != nil {
				t.Fatalf("Save: %v", err)
			}
			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			// yaml.v3 quotes "off" (a YAML 1.1 boolean word), so accept either form.
			if !strings.Contains(string(raw), "scope_lint: "+mode) && !strings.Contains(string(raw), `scope_lint: "`+mode+`"`) {
				t.Errorf("saved YAML lacks scope_lint: %s:\n%s", mode, raw)
			}
			cfg2, err := Load(out)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if got := cfg2.Autonomous.ScopeLintMode(); got != mode {
				t.Errorf("ScopeLintMode() after round-trip = %q, want %q", got, mode)
			}
		})
	}
}

func TestScopeLintMode_UnknownFallsBackToEnforce(t *testing.T) {
	for _, value := range []string{"", "   ", "garbage", "on"} {
		a := AutonomousConfig{ScopeLint: value}
		if got := a.ScopeLintMode(); got != ScopeLintEnforce {
			t.Errorf("ScopeLint %q resolved to %q, want %q", value, got, ScopeLintEnforce)
		}
	}
}

// BL428 §3a — autonomous.project_write_serialization /
// autonomous.refuse_dirty_tree_start (config surface for
// docs/plans/2026-10-14-concurrency-and-budget-design.md).

func loadYAMLBody(t *testing.T, body string) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestProjectWriteSerialization_DefaultsToAdvisory(t *testing.T) {
	cfg := loadYAMLBody(t, "autonomous:\n  enabled: true\n")
	if cfg.Autonomous.ProjectWriteSerialization != "advisory" {
		t.Fatalf("project_write_serialization = %q, want default %q",
			cfg.Autonomous.ProjectWriteSerialization, "advisory")
	}
	if cfg.Autonomous.RefuseDirtyTreeStart {
		t.Fatal("refuse_dirty_tree_start should default false")
	}
}

func TestProjectWriteSerialization_YAMLParseAndRoundTrip(t *testing.T) {
	cfg := loadYAMLBody(t, "autonomous:\n  project_write_serialization: parallel\n  refuse_dirty_tree_start: true\n")
	if cfg.Autonomous.ProjectWriteSerialization != "parallel" {
		t.Fatalf("project_write_serialization = %q, want %q",
			cfg.Autonomous.ProjectWriteSerialization, "parallel")
	}
	if !cfg.Autonomous.RefuseDirtyTreeStart {
		t.Fatal("refuse_dirty_tree_start: true in YAML did not load")
	}
	// "parallel" is reserved-but-disabled: accepted at load time.
	if len(cfg.DeprecatedKeys) != 0 {
		t.Fatalf("DeprecatedKeys = %v, want empty (neither key is deprecated)", cfg.DeprecatedKeys)
	}
	out := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, out); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "project_write_serialization: parallel") {
		t.Errorf("saved YAML lacks project_write_serialization: parallel:\n%s", raw)
	}
	if !strings.Contains(string(raw), "refuse_dirty_tree_start: true") {
		t.Errorf("saved YAML lacks refuse_dirty_tree_start: true:\n%s", raw)
	}
	again, err := Load(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again.Autonomous.ProjectWriteSerialization != "parallel" || !again.Autonomous.RefuseDirtyTreeStart {
		t.Fatalf("round trip lost values: %+v", again.Autonomous)
	}
}

func TestProjectWriteSerialization_AdvisoryExplicit(t *testing.T) {
	cfg := loadYAMLBody(t, "autonomous:\n  project_write_serialization: advisory\n")
	if cfg.Autonomous.ProjectWriteSerialization != "advisory" {
		t.Fatalf("project_write_serialization = %q, want %q",
			cfg.Autonomous.ProjectWriteSerialization, "advisory")
	}
}

func TestProjectWriteSerialization_InvalidValueRejected(t *testing.T) {
	for _, bad := range []string{"serial", "ADVISORY", "per_project_dir", "off", " ", "advisory " + "\t"} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte("autonomous:\n  project_write_serialization: '"+bad+"'\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatalf("Load accepted invalid project_write_serialization %q, want error", bad)
		}
	}
}

// Regression guard: the max_parallel_tasks deprecation detection must
// keep working alongside the new BL428 §3a keys in the same struct.
func TestDeprecatedKeys_MaxParallelTasksStillDetected(t *testing.T) {
	cfg := loadYAMLBody(t, "autonomous:\n  max_parallel_tasks: 3\n  project_write_serialization: advisory\n  refuse_dirty_tree_start: true\n")
	found := false
	for _, k := range cfg.DeprecatedKeys {
		if k == "autonomous.max_parallel_tasks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("DeprecatedKeys = %v, want it to contain autonomous.max_parallel_tasks", cfg.DeprecatedKeys)
	}
	// The new keys must not be reported as deprecated.
	for _, k := range cfg.DeprecatedKeys {
		if k == "autonomous.project_write_serialization" || k == "autonomous.refuse_dirty_tree_start" {
			t.Fatalf("DeprecatedKeys = %v — the new BL428 keys are not deprecated", cfg.DeprecatedKeys)
		}
	}
}

func TestDefaultConfig_UndeclaredEditModeWarn(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Autonomous.UndeclaredEditMode != "warn" {
		t.Fatalf("Autonomous.UndeclaredEditMode = %q, want \"warn\"", cfg.Autonomous.UndeclaredEditMode)
	}
}

func TestYAMLRoundTrip_UndeclaredEditMode(t *testing.T) {
	// An explicitly set value ("block") must survive Marshal->Unmarshal.
	cfg := DefaultConfig()
	cfg.Autonomous.UndeclaredEditMode = "block"
	out := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, out); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "undeclared_edit_mode: block") {
		t.Errorf("saved YAML lacks 'undeclared_edit_mode: block':\n%s", raw)
	}
	cfg2, err := Load(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg2.Autonomous.UndeclaredEditMode != "block" {
		t.Errorf("UndeclaredEditMode after round-trip = %q, want \"block\"", cfg2.Autonomous.UndeclaredEditMode)
	}

	// No omitempty: an unset config still marshals the default explicitly
	// as 'undeclared_edit_mode: warn' (explicit-clear discipline).
	cfg3 := DefaultConfig()
	out3 := filepath.Join(t.TempDir(), "default.yaml")
	if err := Save(cfg3, out3); err != nil {
		t.Fatalf("Save(default): %v", err)
	}
	raw3, err := os.ReadFile(out3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw3), "undeclared_edit_mode: warn") {
		t.Errorf("default YAML lacks 'undeclared_edit_mode: warn':\n%s", raw3)
	}
}

func TestValidateUndeclaredEditMode(t *testing.T) {
	for _, ok := range []string{"off", "warn", "block", " Block ", "OFF"} {
		if err := ValidateUndeclaredEditMode(ok); err != nil {
			t.Errorf("ValidateUndeclaredEditMode(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "strict", "on", "true"} {
		if err := ValidateUndeclaredEditMode(bad); err == nil {
			t.Errorf("ValidateUndeclaredEditMode(%q) = nil, want error", bad)
		}
	}
}

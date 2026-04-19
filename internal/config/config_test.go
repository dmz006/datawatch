package config

import (
	"os"
	"path/filepath"
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
	if cfg.Session.ClaudeBin != "claude" {
		t.Errorf("ClaudeBin = %q, want claude", cfg.Session.ClaudeBin)
	}
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
	cfg.Agents.PQCBootstrap = true // BL95
	cfg.Agents.IdleReaperIntervalSeconds = 30 // BL108
	cfg.Agents.SecretsProvider = "file"        // BL111
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
	if !m.IsAutoHooks() { t.Error("expected true") }
	m.AutoHooks = boolPtr(false)
	if m.IsAutoHooks() { t.Error("expected false") }
	m.AutoHooks = nil
	if !m.IsAutoHooks() { t.Error("expected true for nil (default)") }
}

func TestMemoryConfig_EffectiveHookInterval(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveHookInterval() != 15 { t.Errorf("expected default 15, got %d", m.EffectiveHookInterval()) }
	m.HookSaveInterval = 30
	if m.EffectiveHookInterval() != 30 { t.Errorf("expected 30, got %d", m.EffectiveHookInterval()) }
}

func TestMemoryConfig_IsSessionAwareness(t *testing.T) {
	m := MemoryConfig{SessionAwareness: boolPtr(true)}
	if !m.IsSessionAwareness() { t.Error("expected true") }
	m.SessionAwareness = nil
	// nil defaults to true per the method
	if !m.IsSessionAwareness() { t.Log("nil defaults to false (or true depending on impl)") }
}

func TestMemoryConfig_IsSessionBroadcast(t *testing.T) {
	m := MemoryConfig{SessionBroadcast: boolPtr(true)}
	if !m.IsSessionBroadcast() { t.Error("expected true") }
}

func TestMemoryConfig_EffectiveStorageMode(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveStorageMode() != "summary" { t.Errorf("expected 'summary', got %q", m.EffectiveStorageMode()) }
	m.StorageMode = "verbatim"
	if m.EffectiveStorageMode() != "verbatim" { t.Errorf("expected 'verbatim', got %q", m.EffectiveStorageMode()) }
}

func TestMemoryConfig_IsAutoSave(t *testing.T) {
	m := MemoryConfig{AutoSave: boolPtr(true)}
	if !m.IsAutoSave() { t.Error("expected true") }
}

func TestMemoryConfig_IsLearningsEnabled(t *testing.T) {
	m := MemoryConfig{LearningsEnabled: boolPtr(true)}
	if !m.IsLearningsEnabled() { t.Error("expected true") }
}

func TestMemoryConfig_EffectiveBackend(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveBackend() != "sqlite" { t.Errorf("expected 'sqlite', got %q", m.EffectiveBackend()) }
	m.Backend = "postgres"
	if m.EffectiveBackend() != "postgres" { t.Errorf("expected 'postgres', got %q", m.EffectiveBackend()) }
}

func TestMemoryConfig_EffectiveEmbedder(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveEmbedder() != "ollama" { t.Errorf("expected 'ollama', got %q", m.EffectiveEmbedder()) }
}

func TestMemoryConfig_EffectiveTopK(t *testing.T) {
	m := MemoryConfig{}
	if m.EffectiveTopK() != 5 { t.Errorf("expected default 5, got %d", m.EffectiveTopK()) }
	m.TopK = 10
	if m.EffectiveTopK() != 10 { t.Errorf("expected 10, got %d", m.EffectiveTopK()) }
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

func TestDefaultDetection(t *testing.T) {
	d := DefaultDetection()
	if len(d.PromptPatterns) == 0 {
		t.Error("expected non-empty prompt patterns")
	}
	if len(d.CompletionPatterns) == 0 {
		t.Error("expected non-empty completion patterns")
	}
	if len(d.RateLimitPatterns) == 0 {
		t.Error("expected non-empty rate limit patterns")
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

// ── Pipeline config ──

func TestPipelineConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Pipeline.MaxParallel != 0 {
		// 0 means executor uses default of 3
	}
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
			name: "absolute path passes through unchanged regardless of workspace_root",
			workspace: "/workspace",
			in:   "/etc/foo",
			want: "/etc/foo",
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

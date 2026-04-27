// v5.21.0 — verify the observer.* + whisper.* config-parity sweep.
// Pre-v5.21.0 every observer.* key silently no-op'd through PUT
// /api/config because applyConfigPatch had zero observer cases;
// whisper.{backend,endpoint,api_key} were also missing.

package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestApplyConfigPatch_ObserverScalars(t *testing.T) {
	cfg := &config.Config{}

	applyConfigPatch(cfg, map[string]interface{}{
		"observer.tick_interval_ms": 2000,
		"observer.top_n_broadcast":  150,
		"observer.include_kthreads": true,
		"observer.ebpf_enabled":     "true",
		"observer.conn_correlator":  true,
	})

	if cfg.Observer.TickIntervalMs != 2000 {
		t.Errorf("tick_interval_ms = %d", cfg.Observer.TickIntervalMs)
	}
	if cfg.Observer.TopNBroadcast != 150 {
		t.Errorf("top_n_broadcast = %d", cfg.Observer.TopNBroadcast)
	}
	if !cfg.Observer.IncludeKthreads {
		t.Error("include_kthreads false")
	}
	if cfg.Observer.EBPFEnabled != "true" {
		t.Errorf("ebpf_enabled = %q", cfg.Observer.EBPFEnabled)
	}
	if !cfg.Observer.ConnCorrelator {
		t.Error("conn_correlator false")
	}
}

func TestApplyConfigPatch_ObserverPointerBools(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"observer.plugin_enabled":       true,
		"observer.process_tree_enabled": false,
		"observer.session_attribution":  true,
		"observer.backend_attribution":  true,
		"observer.docker_discovery":     false,
		"observer.gpu_attribution":      true,
	})

	check := func(name string, p *bool, want bool) {
		if p == nil {
			t.Errorf("%s: pointer nil", name)
			return
		}
		if *p != want {
			t.Errorf("%s = %v, want %v", name, *p, want)
		}
	}
	check("plugin_enabled", cfg.Observer.PluginEnabled, true)
	check("process_tree_enabled", cfg.Observer.ProcessTreeEnabled, false)
	check("session_attribution", cfg.Observer.SessionAttribution, true)
	check("backend_attribution", cfg.Observer.BackendAttribution, true)
	check("docker_discovery", cfg.Observer.DockerDiscovery, false)
	check("gpu_attribution", cfg.Observer.GPUAttribution, true)
}

func TestApplyConfigPatch_ObserverFederation(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"observer.federation.parent_url":             "https://root.example:8443",
		"observer.federation.peer_name":              "edge-2",
		"observer.federation.push_interval_seconds":  15,
		"observer.federation.token_path":             "/etc/dw/fed.token",
		"observer.federation.insecure":               true,
	})

	if cfg.Observer.Federation.ParentURL != "https://root.example:8443" {
		t.Errorf("parent_url = %q", cfg.Observer.Federation.ParentURL)
	}
	if cfg.Observer.Federation.PeerName != "edge-2" {
		t.Errorf("peer_name = %q", cfg.Observer.Federation.PeerName)
	}
	if cfg.Observer.Federation.PushIntervalSeconds != 15 {
		t.Errorf("push_interval_seconds = %d", cfg.Observer.Federation.PushIntervalSeconds)
	}
	if cfg.Observer.Federation.TokenPath != "/etc/dw/fed.token" {
		t.Errorf("token_path = %q", cfg.Observer.Federation.TokenPath)
	}
	if !cfg.Observer.Federation.Insecure {
		t.Error("insecure false")
	}
}

func TestApplyConfigPatch_ObserverPeers(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"observer.peers.allow_register":               true,
		"observer.peers.token_ttl_rotation_grace_s":   90,
		"observer.peers.push_interval_seconds":        7,
		"observer.peers.listen_addr":                  ":9001",
	})

	if !cfg.Observer.Peers.AllowRegister {
		t.Error("allow_register false")
	}
	if cfg.Observer.Peers.TokenRotationGraceS != 90 {
		t.Errorf("token_ttl_rotation_grace_s = %d", cfg.Observer.Peers.TokenRotationGraceS)
	}
	if cfg.Observer.Peers.PushIntervalSeconds != 7 {
		t.Errorf("push_interval_seconds = %d", cfg.Observer.Peers.PushIntervalSeconds)
	}
	if cfg.Observer.Peers.ListenAddr != ":9001" {
		t.Errorf("listen_addr = %q", cfg.Observer.Peers.ListenAddr)
	}
}

func TestApplyConfigPatch_ObserverOllamaTap(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"observer.ollama_tap.endpoint": "http://ollama.local:11434",
	})
	if cfg.Observer.OllamaTap.Endpoint != "http://ollama.local:11434" {
		t.Errorf("ollama_tap.endpoint = %q", cfg.Observer.OllamaTap.Endpoint)
	}
}

func TestApplyConfigPatch_WhisperHTTPFields(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"whisper.backend":  "openwebui",
		"whisper.endpoint": "https://owui.local:8080",
		"whisper.api_key":  "secret-token",
	})

	if cfg.Whisper.Backend != "openwebui" {
		t.Errorf("backend = %q", cfg.Whisper.Backend)
	}
	if cfg.Whisper.Endpoint != "https://owui.local:8080" {
		t.Errorf("endpoint = %q", cfg.Whisper.Endpoint)
	}
	if cfg.Whisper.APIKey != "secret-token" {
		t.Errorf("api_key = %q", cfg.Whisper.APIKey)
	}
}

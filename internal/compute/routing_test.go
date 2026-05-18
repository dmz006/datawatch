// BL318 unit tests — routing field validation and defaults.

package compute

import (
	"testing"
)

// minimally valid node for routing tests (reuse, then override fields).
func baseNode() *Node {
	return &Node{
		Name: "test-node",
		Kind: KindOllama,
	}
}

func TestValidate_RoutingDirect_Pass(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDirect
	if err := n.Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_RoutingEmpty_Pass(t *testing.T) {
	n := baseNode()
	// empty Routing == direct, must pass
	if err := n.Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_InvalidRoutingMode(t *testing.T) {
	n := baseNode()
	n.Routing = "banana"
	if err := n.Validate(); err == nil {
		t.Fatal("expected error for unknown routing mode")
	}
}

func TestValidate_DockerNetwork_NoConfig(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDockerNetwork
	// no RoutingDockerNetwork sub-config
	if err := n.Validate(); err == nil {
		t.Fatal("expected error: docker-network without sub-config")
	}
}

func TestValidate_DockerNetwork_NoImage(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDockerNetwork
	n.RoutingDockerNetwork = &RoutingDockerNetworkConfig{
		// Image intentionally empty
	}
	if err := n.Validate(); err == nil {
		t.Fatal("expected error: docker-network without image")
	}
}

func TestValidate_DockerNetwork_Valid(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDockerNetwork
	n.RoutingDockerNetwork = &RoutingDockerNetworkConfig{
		Image: "ollama/ollama:latest",
	}
	if err := n.Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_DatawatchProxy_NoConfig(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDatawatchProxy
	if err := n.Validate(); err == nil {
		t.Fatal("expected error: datawatch-proxy without sub-config")
	}
}

func TestValidate_DatawatchProxy_NoPeer(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDatawatchProxy
	n.RoutingDatawatchProxy = &RoutingDatawatchProxyConfig{
		RemoteLLMName: "my-llm",
		// Peer intentionally empty
	}
	if err := n.Validate(); err == nil {
		t.Fatal("expected error: datawatch-proxy without peer")
	}
}

func TestValidate_DatawatchProxy_NoRemoteLLMName(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDatawatchProxy
	n.RoutingDatawatchProxy = &RoutingDatawatchProxyConfig{
		Peer: "peer-1",
		// RemoteLLMName intentionally empty
	}
	if err := n.Validate(); err == nil {
		t.Fatal("expected error: datawatch-proxy without remote_llm_name")
	}
}

func TestValidate_DatawatchProxy_Valid(t *testing.T) {
	n := baseNode()
	n.Routing = RoutingDatawatchProxy
	n.RoutingDatawatchProxy = &RoutingDatawatchProxyConfig{
		Peer:          "peer-1",
		RemoteLLMName: "my-llm",
	}
	if err := n.Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRoutingDockerNetworkConfig_effective_Defaults(t *testing.T) {
	cfg := &RoutingDockerNetworkConfig{
		Image: "ollama/ollama",
	}
	eff := cfg.effective()

	if eff.NetworkName != "datawatch-llm" {
		t.Errorf("expected NetworkName=datawatch-llm, got %q", eff.NetworkName)
	}
	if eff.Port != 11434 {
		t.Errorf("expected Port=11434, got %d", eff.Port)
	}
}

func TestRoutingDockerNetworkConfig_effective_NoOverwrite(t *testing.T) {
	cfg := &RoutingDockerNetworkConfig{
		Image:       "custom-image",
		NetworkName: "my-net",
		Port:        8080,
	}
	eff := cfg.effective()

	if eff.NetworkName != "my-net" {
		t.Errorf("expected NetworkName=my-net, got %q", eff.NetworkName)
	}
	if eff.Port != 8080 {
		t.Errorf("expected Port=8080, got %d", eff.Port)
	}
}

func TestRoutingDockerNetworkConfig_effective_DoesNotMutate(t *testing.T) {
	cfg := &RoutingDockerNetworkConfig{
		Image: "ollama/ollama",
	}
	_ = cfg.effective()
	// Original struct must be unchanged.
	if cfg.NetworkName != "" {
		t.Errorf("effective() must not mutate original: NetworkName=%q", cfg.NetworkName)
	}
	if cfg.Port != 0 {
		t.Errorf("effective() must not mutate original: Port=%d", cfg.Port)
	}
}

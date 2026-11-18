package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResourceSubscriptionDefaultsOffAndTemplateNamesIt(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MCP.ResourceSubscriptions {
		t.Fatal("mcp.resource_subscriptions must default to false")
	}
	var parsed struct {
		MCP struct {
			ResourceSubscriptions bool `yaml:"resource_subscriptions"`
		} `yaml:"mcp"`
	}
	cfg.MCP.ResourceSubscriptions = true
	out := GenerateAnnotatedConfig(cfg)
	if !strings.Contains(out, "resource_subscriptions: true") {
		t.Fatalf("template lacks the key:\n%s", out)
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil || !parsed.MCP.ResourceSubscriptions {
		t.Fatalf("template does not round-trip (err=%v, value=%v)", err, parsed.MCP.ResourceSubscriptions)
	}
}

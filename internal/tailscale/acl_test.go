// BL243 Phase 3 — ACL generator unit tests.

package tailscale

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newTestClientACL(cfg *Config) *Client {
	return &Client{cfg: cfg, httpClient: nil}
}

func TestGenerateACLPolicy_DefaultTags(t *testing.T) {
	c := newTestClientACL(&Config{Enabled: true})
	policy, err := c.GenerateACLPolicy(context.Background())
	if err != nil {
		t.Fatalf("GenerateACLPolicy: %v", err)
	}

	var p ACLPolicy
	if err := json.Unmarshal([]byte(policy), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Default managed tags should be declared as owners
	for _, tag := range DefaultManagedTags() {
		if _, ok := p.TagOwners[tag]; !ok {
			t.Errorf("missing tagOwner for %q", tag)
		}
	}

	// group:dw-agents must exist
	if _, ok := p.Groups["group:dw-agents"]; !ok {
		t.Error("missing group:dw-agents")
	}

	// At least 2 ACL rules (mesh + catch-all)
	if len(p.ACLs) < 2 {
		t.Errorf("expected ≥2 ACL rules, got %d", len(p.ACLs))
	}

	// Catch-all must be last
	last := p.ACLs[len(p.ACLs)-1]
	if last.Src[0] != "*" || last.Dst[0] != "*:*" {
		t.Errorf("last rule is not catch-all: %+v", last)
	}
}

func TestGenerateACLPolicy_AllowedPeers(t *testing.T) {
	c := newTestClientACL(&Config{
		Enabled: true,
		ACL: ACLConfig{
			AllowedPeers: []string{"laptop", "tag:ops"},
		},
	})
	policy, err := c.GenerateACLPolicy(context.Background())
	if err != nil {
		t.Fatalf("GenerateACLPolicy: %v", err)
	}

	// AllowedPeers rule must appear
	if !strings.Contains(policy, `"laptop"`) || !strings.Contains(policy, `"tag:ops"`) {
		t.Errorf("allowed peers missing from policy:\n%s", policy)
	}

	var p ACLPolicy
	if err := json.Unmarshal([]byte(policy), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Should have: mesh rule + allowed-peers rule + catch-all = 3 rules
	if len(p.ACLs) != 3 {
		t.Errorf("expected 3 ACL rules, got %d", len(p.ACLs))
	}
}

func TestGenerateACLPolicy_CustomManagedTags(t *testing.T) {
	c := newTestClientACL(&Config{
		Enabled: true,
		ACL: ACLConfig{
			ManagedTags: []string{"tag:custom-agent"},
		},
	})
	policy, err := c.GenerateACLPolicy(context.Background())
	if err != nil {
		t.Fatalf("GenerateACLPolicy: %v", err)
	}

	var p ACLPolicy
	if err := json.Unmarshal([]byte(policy), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := p.TagOwners["tag:custom-agent"]; !ok {
		t.Error("custom tag missing from tagOwners")
	}
	// Default tags should NOT appear
	for _, dt := range DefaultManagedTags() {
		if _, ok := p.TagOwners[dt]; ok {
			t.Errorf("default tag %q should not appear when custom tags set", dt)
		}
	}
}

func TestGenerateACLPolicy_ValidJSON(t *testing.T) {
	c := newTestClientACL(&Config{
		Enabled: true,
		ACL: ACLConfig{
			AllowedPeers: []string{"peer-a"},
			ManagedTags:  []string{"tag:dw-agent", "tag:dw-research"},
		},
	})
	policy, err := c.GenerateACLPolicy(context.Background())
	if err != nil {
		t.Fatalf("GenerateACLPolicy: %v", err)
	}
	var out interface{}
	if err := json.Unmarshal([]byte(policy), &out); err != nil {
		t.Errorf("generated policy is not valid JSON: %v\n%s", err, policy)
	}
}

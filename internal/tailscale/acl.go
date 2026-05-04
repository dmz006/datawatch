// BL243 Phase 3 — ACL policy generator for headscale.
//
// GenerateACLPolicy builds a headscale-compatible JSON ACL policy from the
// daemon config + a live node query.  The policy is incremental and safe:
//
//  1. Allows all managed-tag agent pods to reach each other (full mesh).
//  2. Allows the operator-specified AllowedPeers to reach agent pods.
//  3. Declares tag owners so headscale can validate tag assignments.
//  4. Appends a catch-all accept rule to preserve existing connectivity.
//
// The generated JSON can be reviewed in the PWA and then pushed with
// POST /api/tailscale/acl/push (or auto-push when no body is provided).

package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
)

// ACLPolicy is the headscale JSON policy document.
type ACLPolicy struct {
	TagOwners map[string][]string `json:"tagOwners,omitempty"`
	Groups    map[string][]string `json:"groups,omitempty"`
	ACLs      []ACLRule           `json:"acls"`
}

// ACLRule is one entry in the headscale ACL list.
type ACLRule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
}

// GenerateACLPolicy builds a JSON ACL policy from the daemon config and the
// live node list. Nodes() is called for existing-node awareness (allowed to
// fail — a non-nil error just means the node list is omitted from the log but
// the policy is still generated).
func (c *Client) GenerateACLPolicy(ctx context.Context) (string, error) {
	managedTags := c.cfg.ACL.ManagedTags
	if len(managedTags) == 0 {
		managedTags = DefaultManagedTags()
	}

	// Tag owner declarations — all managed tags owned by autogroup:admin
	tagOwners := make(map[string][]string, len(managedTags))
	for _, tag := range managedTags {
		tagOwners[tag] = []string{"autogroup:admin"}
	}

	// Group collecting all managed agent tags for compact rule writing
	groups := map[string][]string{
		"group:dw-agents": managedTags,
	}

	acls := make([]ACLRule, 0, 4)

	// Rule 1 — agent pods can reach each other (full mesh)
	acls = append(acls, ACLRule{
		Action: "accept",
		Src:    []string{"group:dw-agents"},
		Dst:    []string{"group:dw-agents:*"},
	})

	// Rule 2 — AllowedPeers can reach agent pods (one-way ingress)
	if len(c.cfg.ACL.AllowedPeers) > 0 {
		acls = append(acls, ACLRule{
			Action: "accept",
			Src:    c.cfg.ACL.AllowedPeers,
			Dst:    []string{"group:dw-agents:*"},
		})
	}

	// Rule 3 — catch-all: preserve connectivity for all other nodes.
	// This ensures existing services continue to work after ACL push.
	acls = append(acls, ACLRule{
		Action: "accept",
		Src:    []string{"*"},
		Dst:    []string{"*:*"},
	})

	policy := ACLPolicy{
		TagOwners: tagOwners,
		Groups:    groups,
		ACLs:      acls,
	}

	b, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal acl policy: %w", err)
	}
	return string(b), nil
}

// GenerateAndPushACL generates the ACL policy and immediately pushes it to
// headscale. This is the one-shot "generate + push" used at daemon startup
// and via the comm/REST "acl auto-push" surface.
func (c *Client) GenerateAndPushACL(ctx context.Context) (string, error) {
	policy, err := c.GenerateACLPolicy(ctx)
	if err != nil {
		return "", fmt.Errorf("generate acl: %w", err)
	}
	if err := c.PushACL(ctx, policy); err != nil {
		return "", fmt.Errorf("push acl: %w", err)
	}
	return policy, nil
}

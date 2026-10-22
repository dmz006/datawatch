package fingerprint

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}
	return key
}

// TestExportSkipsNodesWithoutIdentity — a node with no node-side
// identity source is skipped (not fingerprinted, not an error); a
// resolvable node and a grouped LLM both produce one Item each.
func TestExportSkipsNodesWithoutIdentity(t *testing.T) {
	exp := Exporter{
		Nodes: &IdentityResolver{
			Nodes: func() []compute.Node {
				return []compute.Node{
					{Name: "bound", Kind: compute.KindLocal},
					{Name: "orphan", Kind: compute.KindOllama}, // no observer peer, not local
				}
			},
			Local: func() map[string]any {
				return map[string]any{"gpu_uuids": []string{"uuid-1"}}
			},
		},
		LLMs: func() []inference.LLM {
			return []inference.LLM{{Name: "llm-a", BillingGroup: "shared-plan-a"}}
		},
	}
	got := exp.Export(testKey(t))
	if len(got) != 2 {
		t.Fatalf("expected 2 items (1 node + 1 LLM), got %d: %+v", len(got), got)
	}
	if got[0].Name != "bound" || got[0].Kind != KindNode {
		t.Errorf("first item = %+v, want node 'bound'", got[0])
	}
	if got[1].Name != "llm-a" || got[1].Kind != KindBilling {
		t.Errorf("second item = %+v, want billing LLM 'llm-a'", got[1])
	}
	for _, it := range got {
		if it.Hash == "" {
			t.Errorf("item %q has empty hash", it.Name)
		}
	}
}

// TestExportBillingGroupHashed — the hash for a billing-group Item
// is Hash(key, KindBilling, the TRIMMED group), LLMs with an empty
// group produce no Item, and the group itself never appears.
func TestExportBillingGroupHashed(t *testing.T) {
	key := testKey(t)
	exp := Exporter{
		LLMs: func() []inference.LLM {
			return []inference.LLM{
				{Name: "llm-a", BillingGroup: "  shared-plan-a  "},
				{Name: "llm-b"}, // no group
			}
		},
	}
	got := exp.Export(key)
	if len(got) != 1 || got[0].Name != "llm-a" {
		t.Fatalf("expected exactly one item for llm-a, got %+v", got)
	}
	want, err := Hash(key, KindBilling, "shared-plan-a")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if got[0].Hash != want {
		t.Errorf("hash = %s, want %s (trimmed group)", got[0].Hash, want)
	}
	nodeKindHash, err := Hash(key, KindNode, "shared-plan-a")
	if err != nil {
		t.Fatalf("Hash(node-kind): %v", err)
	}
	if got[0].Hash == nodeKindHash {
		t.Error("billing hash must not equal a node-kind hash of the same value")
	}
}

// TestExportItemsCarryNoRawValues — the JSON of the exported items
// must not contain the identity value, the node address, or the
// billing label: Name (the operator's own label) is the only
// plain-text field.
func TestExportItemsCarryNoRawValues(t *testing.T) {
	exp := Exporter{
		Nodes: &IdentityResolver{
			Nodes: func() []compute.Node {
				return []compute.Node{{Name: "alpha", Kind: compute.KindLocal, Address: "http://10.0.0.5:11434"}}
			},
			Local: func() map[string]any {
				return map[string]any{"gpu_uuids": []string{"uuid-1", "uuid-2"}}
			},
		},
		LLMs: func() []inference.LLM {
			return []inference.LLM{{Name: "claude-a", BillingGroup: "shared-plan-a", APIKeyRef: "sk-raw-credential"}}
		},
	}
	body, err := json.Marshal(exp.Export(testKey(t)))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	bodyStr := string(body)
	for _, raw := range []string{"uuid-1,uuid-2", "uuid-1", "10.0.0.5", "shared-plan-a", "sk-raw-credential"} {
		if strings.Contains(bodyStr, raw) {
			t.Errorf("exported JSON must not contain the raw value %q: %s", raw, bodyStr)
		}
	}
	// The operator's own labels ARE (allowed to be) present.
	for _, label := range []string{"alpha", "claude-a"} {
		if !strings.Contains(bodyStr, label) {
			t.Errorf("exported JSON should carry the operator label %q", label)
		}
	}
}

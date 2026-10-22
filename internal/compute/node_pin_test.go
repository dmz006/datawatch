package compute

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidate_NodePinnedModel_WhitespaceRejected(t *testing.T) {
	n := baseNode()
	n.NodePinnedModel = "   "
	err := n.Validate()
	if err == nil {
		t.Fatal("whitespace-only node_pinned_model passed validation")
	}
	if !strings.Contains(err.Error(), "node_pinned_model") {
		t.Fatalf("error %q does not name node_pinned_model", err)
	}
}

func TestValidate_NodePinnedModel_TrimmedAndKept(t *testing.T) {
	n := baseNode()
	n.NodePinnedModel = "  qwen3.8-64k:27b  "
	if err := n.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if n.NodePinnedModel != "qwen3.8-64k:27b" {
		t.Fatalf("NodePinnedModel = %q, want trimmed value", n.NodePinnedModel)
	}
}

func TestValidate_NodePinnedModel_EmptyMeansNoPin(t *testing.T) {
	n := baseNode()
	if err := n.Validate(); err != nil {
		t.Fatalf("Validate with no pin: %v", err)
	}
}

// The pin fields are on the Node JSON that /api/compute/nodes reads and
// writes, so they must survive a marshal/unmarshal round trip.
func TestNode_PinFieldsJSONRoundTrip(t *testing.T) {
	in := baseNode()
	in.NodePinnedModel = "qwen3.8-64k:27b"
	in.AllowModelSwap = true
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Node
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.NodePinnedModel != "qwen3.8-64k:27b" || !out.AllowModelSwap {
		t.Fatalf("round trip lost pin fields: %+v", out)
	}
	for _, key := range []string{`"node_pinned_model":"qwen3.8-64k:27b"`, `"allow_model_swap":true`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("JSON %s lacks %s", b, key)
		}
	}
}

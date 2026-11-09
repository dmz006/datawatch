package server

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestExpandAttemptCostDottedKeysFoldsIntoWholeTable(t *testing.T) {
	stored := map[string]config.FirstAttemptChoice{"note": {Backend: "pi-prompt", Model: "qwen3.8:27b"}}
	patch := map[string]interface{}{
		"autonomous.first_attempt.docs.backend": "pi-prompt",
		"autonomous.first_attempt.docs.model":   "qwen3.8:27b",
		"other.key":                             true,
	}
	if err := expandAttemptCostDottedKeys(patch, stored); err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, left := patch["autonomous.first_attempt.docs.backend"]; left {
		t.Error("dotted key was not removed from the patch")
	}
	table, ok := patch["autonomous.first_attempt"].(map[string]interface{})
	if !ok {
		t.Fatalf("whole table missing: %#v", patch)
	}
	docs := table["docs"].(map[string]interface{})
	if docs["backend"] != "pi-prompt" || docs["model"] != "qwen3.8:27b" {
		t.Errorf("docs = %#v", docs)
	}
	if note := table["note"].(map[string]interface{}); note["backend"] != "pi-prompt" {
		t.Errorf("stored note entry lost: %#v", note)
	}
	if patch["other.key"] != true {
		t.Error("unrelated key was touched")
	}
}

func TestExpandAttemptCostDottedKeysRefusesBadShapes(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"unknown field":     {"autonomous.first_attempt.docs.color": "red"},
		"missing field":     {"autonomous.first_attempt.docs": "pi"},
		"non-string value":  {"autonomous.first_attempt.docs.model": 3.0},
		"whole table shape": {"autonomous.first_attempt": "not-an-object", "autonomous.first_attempt.docs.model": "m"},
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			err := expandAttemptCostDottedKeys(patch, nil)
			if err == nil {
				t.Fatalf("expand accepted %v", patch)
			}
			if !strings.Contains(err.Error(), "autonomous.first_attempt") {
				t.Errorf("err %q does not name the key", err)
			}
		})
	}
}

func TestExpandAttemptCostDottedKeysNoOpWithoutDottedKeys(t *testing.T) {
	patch := map[string]interface{}{"autonomous.first_attempt_escalate_after_failures": 2.0}
	if err := expandAttemptCostDottedKeys(patch, nil); err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, added := patch["autonomous.first_attempt"]; added {
		t.Error("whole table added with no dotted key present")
	}
}

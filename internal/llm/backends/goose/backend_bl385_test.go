// BL385 Phase 4 — executor injection tests for Goose backend MCP args.
//
// TC-1: gooseEnvPrefix includes --caller-prd-id when PRDID set
// TC-2: gooseEnvPrefix includes --caller-story-id when StoryID set

package goose

import (
	"strings"
	"testing"
)

// TestBL385_GooseBackend_InjectsPRDIDInMCPArgs verifies that when the backend
// has callerPRDID set, GOOSE_MCP__DATAWATCH__ARGS includes --caller-prd-id.
func TestBL385_GooseBackend_InjectsPRDIDInMCPArgs(t *testing.T) {
	b := &Backend{
		binary:         "goose",
		channelEnabled: true,
		sessionFullID:  "sess-abc",
		callerPRDID:    "prd-xyz",
	}
	env := b.gooseEnvPrefix()
	if !strings.Contains(env, "--caller-prd-id") {
		t.Errorf("expected --caller-prd-id in gooseEnvPrefix, got: %q", env)
	}
	if !strings.Contains(env, "prd-xyz") {
		t.Errorf("expected prd-xyz in gooseEnvPrefix, got: %q", env)
	}
}

// TestBL385_GooseBackend_InjectsStoryIDInMCPArgs verifies that when the backend
// has callerStoryID set, GOOSE_MCP__DATAWATCH__ARGS includes --caller-story-id.
func TestBL385_GooseBackend_InjectsStoryIDInMCPArgs(t *testing.T) {
	b := &Backend{
		binary:         "goose",
		channelEnabled: true,
		sessionFullID:  "sess-abc",
		callerStoryID:  "story-789",
	}
	env := b.gooseEnvPrefix()
	if !strings.Contains(env, "--caller-story-id") {
		t.Errorf("expected --caller-story-id in gooseEnvPrefix, got: %q", env)
	}
	if !strings.Contains(env, "story-789") {
		t.Errorf("expected story-789 in gooseEnvPrefix, got: %q", env)
	}
}

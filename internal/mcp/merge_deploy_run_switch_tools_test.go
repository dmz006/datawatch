package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDeployMCP_RunRefusalNamesTheSetting(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, config.MergeDeployRunRefusal, http.StatusForbidden)
	})
	res, err := s.handleAutonomousDeployRun(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want an MCP error result, got %+v err=%v", res, err)
	}
	if text := automationText(t, res); !strings.Contains(text, config.MergeDeployRunRefusal) {
		t.Errorf("error text %q, want the whole refusal", text)
	}
}

func TestDeployMCP_ToolTextNamesTheRunSwitch(t *testing.T) {
	s := &Server{}
	for name, desc := range map[string]string{
		"autonomous_deploy_run": s.toolAutonomousDeployRun().Description,
		"config_set":            s.toolConfigSet().Description,
	} {
		if !strings.Contains(desc, config.MergeDeployRunKey) {
			t.Errorf("%s does not name %s", name, config.MergeDeployRunKey)
		}
	}
}

package mcp

// gate_repair_mode argument for autonomous_config_set. The engine mode
// (autonomous.gate_repair.mode) lives in the daemon config, so the tool sends
// it on PUT /api/config. It is validated here, before any request is made,
// with the same validator every surface uses; anything other than off, report
// or on is refused with the valid values named, so nothing is sent.

import (
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
)

// gateRepairModePatch returns the daemon-config patch for the gate_repair_mode
// argument, or nil when the caller did not pass it. A present argument must be
// one of off, report or on; anything else is refused here, before anything is
// sent.
func gateRepairModePatch(req mcpsdk.CallToolRequest) (map[string]any, error) {
	raw, ok := req.GetArguments()["gate_repair_mode"]
	if !ok {
		return nil, nil
	}
	str, isString := raw.(string)
	if !isString {
		return nil, fmt.Errorf(`gate_repair_mode must be a string: "off", "report" or "on" (got %T)`, raw)
	}
	mode, err := config.ParseGateRepairMode(str)
	if err != nil {
		return nil, fmt.Errorf(`gate_repair_mode %v`, err)
	}
	return map[string]any{"autonomous.gate_repair.mode": mode}, nil
}

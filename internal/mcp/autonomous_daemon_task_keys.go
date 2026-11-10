// autonomous_daemon_task_keys.go — the autonomous config MCP tools' surface
// for three daemon-config settings: autonomous.task_templates.mode,
// autonomous.script_gate_timeout_seconds and autonomous.project_gate_command.
//
// Get merges the stored values (read from GET /api/config, the same source
// the REST surface reports) into the autonomous_config_get response. Set
// validates every argument locally with the same rules the REST PUT
// /api/config applies, and only then sends one config PUT, so a refused value
// stores nothing.

package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/dmz006/datawatch/internal/config"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// daemonTaskModeArg is the argument name of autonomous.task_templates.mode.
const daemonTaskModeArg = "task_templates_mode"

// daemonTaskKeysSetOptions are the autonomous_config_set parameters for the
// three settings.
func daemonTaskKeysSetOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString(daemonTaskModeArg, mcpsdk.Description(
			"autonomous.task_templates.mode — how each decomposition standard task is handled, per template id. "+
				"Each mode is llm, daemon or off; daemon is allowed only for the daemon-capable template ids and never for final_gate. "+
				"Give a JSON object ({\"changelog\":\"daemon\"}) or id=mode pairs (\"changelog=daemon,final_gate=llm\"). "+
				"The entries are merged onto the stored map; an id that is not given keeps its stored mode.")),
		mcpsdk.WithNumber("script_gate_timeout_seconds", mcpsdk.Description(
			"autonomous.script_gate_timeout_seconds — the time cap, in whole seconds, on one script gate run (default 900; 0 = no cap). Negative or fractional values are refused.")),
		mcpsdk.WithString("project_gate_command", mcpsdk.Description(
			"autonomous.project_gate_command — the project gate command run as one line of shell text (empty = unset). Multi-line values and values over 1024 bytes are refused.")),
	}
}

// daemonTaskKeysGetDescription is appended to the autonomous_config_get
// description: it names the three keys and the allowed mode values.
const daemonTaskKeysGetDescription = "autonomous.task_templates.mode (a map of template id to mode: llm, daemon or off), " +
	"autonomous.script_gate_timeout_seconds (whole seconds, 0 = no cap, default 900) and autonomous.project_gate_command (one line of shell text, empty = unset) " +
	"are included in this response. Set them with the task_templates_mode, script_gate_timeout_seconds and project_gate_command arguments of autonomous_config_set. "

// daemonTaskKeysSetDescription is appended to the autonomous_config_set
// description: it names the three keys and the allowed mode values.
const daemonTaskKeysSetDescription = "task_templates_mode sets autonomous.task_templates.mode per template id (llm, daemon or off; daemon only for daemon-capable ids, never final_gate). " +
	"script_gate_timeout_seconds sets autonomous.script_gate_timeout_seconds (whole seconds >= 0, 0 = no cap). " +
	"project_gate_command sets autonomous.project_gate_command (one line, empty = unset). " +
	"A refused value is returned as a tool error and nothing is stored. "

// mergeDaemonTaskSettings adds the three daemon-config settings, read from
// GET /api/config's autonomous object, onto the autonomous config JSON the
// get tool returns. The mode map is merged into the task_templates object
// the manager reports, so its other fields (enabled, templates) stay.
func mergeDaemonTaskSettings(autoCfg []byte, daemon map[string]json.RawMessage) ([]byte, error) {
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(autoCfg, &merged); err != nil {
		return nil, fmt.Errorf("decode /api/autonomous/config: %w", err)
	}
	for _, k := range []string{"script_gate_timeout_seconds", "project_gate_command"} {
		if v, ok := daemon[k]; ok {
			merged[k] = v
		}
	}
	if raw, ok := daemon["task_templates"]; ok {
		var surface struct {
			Mode json.RawMessage `json:"mode"`
		}
		if err := json.Unmarshal(raw, &surface); err == nil && len(surface.Mode) > 0 {
			tt := map[string]json.RawMessage{}
			if cur, ok := merged["task_templates"]; ok {
				var existing map[string]json.RawMessage
				if json.Unmarshal(cur, &existing) == nil && existing != nil {
					tt = existing
				}
			}
			tt["mode"] = surface.Mode
			b, err := json.Marshal(tt)
			if err != nil {
				return nil, err
			}
			merged["task_templates"] = b
		}
	}
	return json.Marshal(merged)
}

// daemonTaskKeysPatch reads the three arguments, validates every one that is
// present, and returns the /api/config patch keyed by the dotted setting
// names. Any refusal is returned before anything is written.
func daemonTaskKeysPatch(req mcpsdk.CallToolRequest) (map[string]any, error) {
	patch := map[string]any{}
	args := req.GetArguments()
	if raw, ok := args[daemonTaskModeArg]; ok {
		modes, err := parseTaskTemplateModeArg(raw)
		if err != nil {
			return nil, err
		}
		for id, mode := range modes {
			patch["autonomous.task_templates.mode."+id] = mode
		}
	}
	if raw, ok := args["script_gate_timeout_seconds"]; ok {
		f, isNum := raw.(float64)
		if !isNum || f != math.Trunc(f) || f < 0 {
			return nil, fmt.Errorf("autonomous.script_gate_timeout_seconds: must be a whole number of seconds >= 0 (0 = no cap)")
		}
		patch["autonomous.script_gate_timeout_seconds"] = int(f)
	}
	if raw, ok := args["project_gate_command"]; ok {
		cmd, isStr := raw.(string)
		if !isStr {
			return nil, fmt.Errorf("autonomous.project_gate_command: must be a string (empty = unset)")
		}
		if err := config.ValidateProjectGateCommand("autonomous", cmd); err != nil {
			return nil, err
		}
		patch["autonomous.project_gate_command"] = cmd
	}
	return patch, nil
}

// parseTaskTemplateModeArg turns the task_templates_mode argument into a
// template id → mode map. It accepts a JSON object (a map argument or a JSON
// string) or id=mode pairs. Every entry is checked with the config rule the
// load path and the REST PUT use (TaskTemplatesConfig.Validate).
func parseTaskTemplateModeArg(raw any) (map[string]string, error) {
	out := map[string]string{}
	switch v := raw.(type) {
	case map[string]any:
		for id, mv := range v {
			mode, isStr := mv.(string)
			if !isStr {
				return nil, fmt.Errorf("autonomous.task_templates.mode[%q]: must be a string (\"llm\", \"daemon\" or \"off\")", id)
			}
			out[id] = mode
		}
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, fmt.Errorf("task_templates_mode: empty value; give a JSON object or id=mode pairs (\"llm\", \"daemon\" or \"off\")")
		}
		if strings.HasPrefix(s, "{") {
			var obj map[string]string
			if err := json.Unmarshal([]byte(s), &obj); err != nil {
				return nil, fmt.Errorf("task_templates_mode: JSON object parse: %v", err)
			}
			out = obj
			break
		}
		for _, pair := range strings.Split(s, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			id, mode, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("task_templates_mode: %q is not id=mode nor a JSON object", pair)
			}
			out[strings.TrimSpace(id)] = strings.TrimSpace(mode)
		}
	default:
		return nil, fmt.Errorf("task_templates_mode must be a string (a JSON object or id=mode pairs) or an object")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("task_templates_mode: no entries; give a JSON object or id=mode pairs")
	}
	for id, mode := range out {
		tc := config.TaskTemplatesConfig{Mode: map[string]string{id: mode}}
		if err := tc.Validate(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

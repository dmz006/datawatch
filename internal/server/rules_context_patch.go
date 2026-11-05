// The autonomous.rules_context.* settings on the config PUT surface:
// the per-project rules-file/context-file names and the digest
// sub-configuration, validated whole (file names, digest mode, size
// bounds, project overrides) before any of it is applied.

package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

func rulesContextPatchTouched(patch map[string]interface{}) bool {
	for k := range patch {
		if k == "autonomous.rules_context" || strings.HasPrefix(k, "autonomous.rules_context.") {
			return true
		}
	}
	return false
}

// parseRulesContextProjectList turns the JSON value of
// autonomous.rules_context.projects into override entries (a string
// context_files value is accepted comma-separated). Nil when v is not a
// list of objects.
func parseRulesContextProjectList(v interface{}) []config.AutonomousRulesContextProject {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]config.AutonomousRulesContextProject, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil
		}
		var cf []string
		if arr, ok := toStringArray(m["context_files"]); ok {
			cf = arr
		} else if s, ok := m["context_files"].(string); ok {
			cf = splitCSV(s)
		}
		b, _ := toInt(m["digest_max_bytes"])
		out = append(out, config.AutonomousRulesContextProject{
			ProjectDir:     strings.TrimSpace(toString(m["project_dir"])),
			RulesFile:      strings.TrimSpace(toString(m["rules_file"])),
			ContextFiles:   cf,
			DigestMode:     strings.TrimSpace(toString(m["digest_mode"])),
			DigestMaxBytes: b,
		})
	}
	return out
}

// contextFilesFromValue accepts a list of file names or a
// comma-separated string, the same leniency as the other list-valued
// keys.
func contextFilesFromValue(v interface{}) ([]string, bool) {
	if arr, ok := toStringArray(v); ok {
		return arr, true
	}
	if s, ok := v.(string); ok {
		return splitCSV(s), true
	}
	return nil, false
}

// validateRulesContextPatch refuses the whole config PUT when any
// autonomous.rules_context.* value is invalid (absolute or
// parent-escaping file names, empty entries, more than 8 context files,
// an unknown digest mode, an out-of-range digest_max_bytes, or a project
// entry without a project_dir). It merges the patch over the current
// base so the whole candidate config is validated, then leaves the
// config untouched.
func validateRulesContextPatch(base config.AutonomousRulesContextConfig, patch map[string]interface{}) error {
	if !rulesContextPatchTouched(patch) {
		return nil
	}
	c := base
	if v, ok := patch["autonomous.rules_context"]; ok {
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("autonomous.rules_context: must be an object, got %T", v)
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("autonomous.rules_context: %v", err)
		}
	}
	if v, ok := patch["autonomous.rules_context.rules_file"]; ok {
		if _, isStr := v.(string); !isStr {
			return fmt.Errorf("autonomous.rules_context.rules_file: must be a string file name")
		}
		c.RulesFile = strings.TrimSpace(toString(v))
	}
	if v, ok := patch["autonomous.rules_context.context_files"]; ok {
		files, ok := contextFilesFromValue(v)
		if !ok {
			return fmt.Errorf("autonomous.rules_context.context_files: must be a list of file names or a comma-separated string")
		}
		c.ContextFiles = files
	}
	if v, ok := patch["autonomous.rules_context.digest_mode"]; ok {
		if _, isStr := v.(string); !isStr {
			return fmt.Errorf("autonomous.rules_context.digest_mode: must be a string")
		}
		c.DigestMode = strings.TrimSpace(toString(v))
	}
	if v, ok := patch["autonomous.rules_context.digest_max_bytes"]; ok {
		n, ok := toInt(v)
		if !ok {
			return fmt.Errorf("autonomous.rules_context.digest_max_bytes: must be an integer")
		}
		c.DigestMaxBytes = n
	}
	if v, ok := patch["autonomous.rules_context.projects"]; ok {
		prjs := parseRulesContextProjectList(v)
		if prjs == nil {
			return fmt.Errorf("autonomous.rules_context.projects: must be a list of project entries (project_dir plus optional rules_file, context_files, digest_mode, digest_max_bytes)")
		}
		c.Projects = prjs
	}
	if err := c.Validate(); err != nil {
		return err
	}
	return nil
}

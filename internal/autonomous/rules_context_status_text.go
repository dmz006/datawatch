// Text for the rules and context settings read, shared by the command line
// and the chat channel. Counts, names and sizes only — never file content.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// RulesContextStatusView is the GET /api/autonomous/rules-context body.
type RulesContextStatusView struct {
	Config config.AutonomousRulesContextConfig `json:"config"`
	Policy struct {
		ProjectDir     string   `json:"project_dir"`
		RulesFile      string   `json:"rules_file"`
		ContextFiles   []string `json:"context_files"`
		DigestMode     string   `json:"digest_mode"`
		DigestMaxBytes int      `json:"digest_max_bytes"`
	} `json:"policy"`
	Record *struct {
		PRDID          string   `json:"prd_id"`
		Recorded       bool     `json:"recorded"`
		FileCount      int      `json:"file_count"`
		PresentCount   int      `json:"present_count"`
		MissingCount   int      `json:"missing_count"`
		Missing        []string `json:"missing"`
		DigestMode     string   `json:"digest_mode"`
		DigestInjected bool     `json:"digest_injected"`
		DigestBytes    int      `json:"digest_bytes"`
	} `json:"record"`
}

// RenderRulesContextStatusText parses the response body and renders it.
func RenderRulesContextStatusText(body []byte) (string, error) {
	var v RulesContextStatusView
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("rules and context response: %w", err)
	}
	var b strings.Builder
	p := v.Policy
	fmt.Fprintf(&b, "rules_file=%s\n", p.RulesFile)
	fmt.Fprintf(&b, "context_files=%s\n", strings.Join(p.ContextFiles, ","))
	fmt.Fprintf(&b, "digest_mode=%s\n", p.DigestMode)
	fmt.Fprintf(&b, "digest_max_bytes=%d\n", p.DigestMaxBytes)
	if p.ProjectDir != "" {
		fmt.Fprintf(&b, "project_dir=%s\n", p.ProjectDir)
	}
	if n := len(v.Config.Projects); n > 0 {
		dirs := make([]string, 0, n)
		for _, pr := range v.Config.Projects {
			dirs = append(dirs, pr.ProjectDir)
		}
		fmt.Fprintf(&b, "project overrides (%d): %s\n", n, strings.Join(dirs, ", "))
	}
	if r := v.Record; r != nil {
		if !r.Recorded {
			fmt.Fprintf(&b, "automaton %s: no planning record yet\n", r.PRDID)
		} else {
			fmt.Fprintf(&b, "automaton %s: %d files (%d present, %d missing), digest %s injected=%t bytes=%d\n",
				r.PRDID, r.FileCount, r.PresentCount, r.MissingCount, r.DigestMode, r.DigestInjected, r.DigestBytes)
			if len(r.Missing) > 0 {
				fmt.Fprintf(&b, "missing: %s\n", strings.Join(r.Missing, ", "))
			}
		}
	}
	return b.String(), nil
}

// ParseRulesContextConfig reads the config object out of a GET or PUT body.
func ParseRulesContextConfig(body []byte) (config.AutonomousRulesContextConfig, error) {
	var v RulesContextStatusView
	if err := json.Unmarshal(body, &v); err != nil {
		return config.AutonomousRulesContextConfig{}, fmt.Errorf("rules and context response: %w", err)
	}
	return v.Config, nil
}

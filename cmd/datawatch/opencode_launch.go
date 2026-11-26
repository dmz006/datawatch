// The opencode.json datawatch writes before it launches opencode. The model
// reaches opencode only through this file.

package main

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
	"github.com/dmz006/datawatch/internal/session"
)

// opencodeConfigRefusalFailsLaunch decides what happens when datawatch cannot
// safely add its settings to a project's opencode.json (it cannot read it in
// full, a comment included, or cannot write it). true: the launch fails and
// says why, so no session runs on a model nobody chose. false: a warning is
// logged and the session launches on whatever the file and opencode's own
// defaults say. Either way the file is left as it is.
const opencodeConfigRefusalFailsLaunch = true

// opencodeLaunchCheck is the daemon's launch check (Manager.SetLaunchCheck).
func opencodeLaunchCheck(cfg *config.Config, failLaunch bool) func(*session.Session) error {
	return func(sess *session.Session) error {
		// Always written (not conditional) so the default model is always set.
		// Idempotent; operator-added entries are preserved.
		if sess.BackendFamily != "opencode" && sess.BackendFamily != "opencode-acp" {
			return nil
		}
		model := sess.Model
		if model == "" && cfg.OpenCode.DefaultModel != "" {
			model = cfg.OpenCode.DefaultModel
		}
		ocOpts := opencode.ProjectConfigOpts{
			Model:            model,
			OllamaURL:        sess.OllamaURL,
			ChunkTimeoutSec:  cfg.OpenCode.OllamaChunkTimeoutSec,
			HeaderTimeoutSec: cfg.OpenCode.OllamaHeaderTimeoutSec,
			BuildTemperature: cfg.OpenCode.BuildTemperature,
			// 2026-10-15, live-validated — deny only the tools that are safe
			// to deny for ANY one-shot autonomous task regardless of
			// content; see DenyTools' doc comment for why this is not a
			// broader allow-list.
			DenyTools: []string{"task", "external_directory"},
		}
		if sess.LSPLanguage != "" {
			if srv, ok := cfg.LSP.Servers[sess.LSPLanguage]; ok {
				ocOpts.LSPServers = map[string]opencode.LSPServer{
					sess.LSPLanguage: {
						Command:    srv.Command,
						Extensions: srv.Extensions,
						Env:        srv.Env,
					},
				}
			}
		}
		if err := opencode.WriteProjectConfig(sess.ProjectDir, ocOpts); err != nil {
			if failLaunch {
				refusal := fmt.Errorf("datawatch cannot safely add its settings to opencode.json, so this session would run on a model nobody chose (make the file plain JSON with no comments, or move it away): %w", err)
				fmt.Printf("[opencode] %s not launched: %v\n", sess.ID, refusal)
				return refusal
			}
			fmt.Printf("[warn] %s: launching without datawatch's opencode settings, so the model is whatever the project's file and opencode's defaults say: %v\n", sess.ID, err)
			return nil
		}
		fmt.Printf("[opencode] wrote project config (model=%s lsp=%s) for %s\n",
			model, sess.LSPLanguage, sess.ID)
		return nil
	}
}

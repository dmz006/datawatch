// cli_autonomous_lint.go — the `datawatch autonomous lint ...` CLI verbs:
// the decompose-lint config (read + scalar set), the read-only dry run for
// one Automaton, and the lint record the executor stored for it. Thin REST
// proxies over the daemon endpoints (GET/PUT
// /api/autonomous/decompose-lint/config, per-PRD
// /api/autonomous/prds/{id}/decompose-lint); findings render through
// internal/autonomous/decompose_lint_text.go, the same wording the comm
// verbs (autonomous lint ...) and the web UI show.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// decomposeLintConfigPath is the daemon's decompose-lint config endpoint
// (GET snapshot, PUT validated replace of the named block(s)).
const decomposeLintConfigPath = "/api/autonomous/decompose-lint/config"

// decomposeLintPRDPath is the per-Automaton decompose-lint endpoint
// (GET the stored record, POST the read-only dry run).
func decomposeLintPRDPath(prd string) string {
	return "/api/autonomous/prds/" + prd + "/decompose-lint"
}

// lintDo issues a decompose-lint request and returns the raw response body
// WITHOUT printing it — the verb decides how the body renders. Non-2xx
// replies are an error carrying the daemon's message (same convention as
// daemonJSON).
func lintDo(method, path string, body []byte) ([]byte, error) {
	u := daemonURL() + path
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, reader)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "BEARER "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("%s %s: %s", method, path, string(out))
	}
	return out, nil
}

func newAutonomousLintCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "lint",
		Short: "Decompose lint: config, dry-run, findings",
		Long: `The decompose-lint surface: the rule pack's config, a read-only
dry run over one Automaton's stored plan, and the lint record the
executor stored for it.

  lint config                 Show the decompose-lint config (mode, retry budget, rules, disabled rules)
  lint config-set key=value ...   Set mode / max_retries / disabled_rules, preserving the rest of the block
  lint dry-run <id>           Lint one Automaton's stored plan read-only (nothing is retried, nothing is stored)
  lint findings <id>          Show the decompose-lint record the executor last stored for one Automaton

Findings print one line each: [severity] rule_id task: message.`,
	}
	c.AddCommand(
		newAutonomousLintConfigCmd(),
		newAutonomousLintConfigSetCmd(),
		newAutonomousLintDryRunCmd(),
		newAutonomousLintFindingsCmd(),
	)
	return c
}

func newAutonomousLintConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the decompose-lint config",
		Long: "Show the live decompose-lint config: mode (off|shadow|enforce), " +
			"the enforce retry budget (max_retries), the active rules (built-in " +
			"pack merged with operator rules, disabled rules removed) and the " +
			"disabled rule ids. A read: nothing is configured or run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := lintDo(http.MethodGet, decomposeLintConfigPath, nil)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(append(out, '\n'))
			return err
		},
	}
}

func newAutonomousLintConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-set key=value ...",
		Short: "Set decompose-lint mode / max_retries / disabled_rules",
		Long: `Set scalar fields of the decompose_lint config block, preserving
everything else (notably the active rule pack).

  lint config-set mode=enforce
  lint config-set max_retries=2 disabled_rules=bare_verbs,no_test_mention
  lint config-set disabled_rules=          # clear the disabled rule list

Values: mode is off | shadow | enforce; max_retries is a whole number >= 0;
disabled_rules is a comma-separated list of rule ids (empty = nothing
disabled). The daemon re-validates the merged block and replaces it
atomically; the reply is the new config.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			overrides, err := autonomous.ParseDecomposeLintOverrides(args)
			if err != nil {
				return err
			}
			cur, err := lintDo(http.MethodGet, decomposeLintConfigPath, nil)
			if err != nil {
				return err
			}
			var cfg map[string]json.RawMessage
			if err := json.Unmarshal(cur, &cfg); err != nil {
				return fmt.Errorf("decompose-lint config is not a JSON object: %v", err)
			}
			block, err := autonomous.MergeDecomposeLintBlock(cfg["decompose_lint"], overrides)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]json.RawMessage{"decompose_lint": block})
			if err != nil {
				return err
			}
			var body []byte
			body, err = lintDo(http.MethodPut, decomposeLintConfigPath, payload)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(append(body, '\n'))
			return err
		},
	}
}

func newAutonomousLintDryRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dry-run <id>",
		Short: "Lint one Automaton's stored plan read-only",
		Long: "Run the effective decompose-lint rules over one Automaton's STORED " +
			"stories and tasks and print the findings: the mode in effect, how " +
			"many active rules ran, and one line per finding ([severity] rule_id " +
			"task: message). Read-only — no planner call, no retry, nothing is " +
			"stored or held.\n\n" +
			"  lint dry-run <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := lintDo(http.MethodPost, decomposeLintPRDPath(args[0]), []byte(`null`))
			if err != nil {
				return err
			}
			var rep autonomous.DecomposeLintReport
			if err := json.Unmarshal(out, &rep); err != nil {
				return fmt.Errorf("dry-run reply is not a decompose-lint report: %v", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), autonomous.RenderDecomposeLintReport(rep))
			return err
		},
	}
}

func newAutonomousLintFindingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "findings <id>",
		Short: "Show the decompose-lint record stored for one Automaton",
		Long: "Show the decompose-lint record the executor stored for one " +
			"Automaton: the mode it ran in, the attempts (1 plus any enforce " +
			"retries consumed), and the final findings one line each " +
			"([severity] rule_id task: message). A read; it is a 'no record' " +
			"answer when mode off ran the last decompose, before the first " +
			"decompose, or after deletion.\n\n" +
			"  lint findings <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := lintDo(http.MethodGet, decomposeLintPRDPath(args[0]), nil)
			if err != nil {
				return err
			}
			var rec struct {
				PRDID         string                          `json:"prd_id"`
				DecomposeLint *autonomous.DecomposeLintRecord `json:"decompose_lint"`
			}
			if err := json.Unmarshal(out, &rec); err != nil {
				return fmt.Errorf("findings reply is not a stored-record body: %v", err)
			}
			prd := rec.PRDID
			if prd == "" {
				prd = args[0]
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), autonomous.RenderDecomposeLintRecord(prd, rec.DecomposeLint))
			return err
		},
	}
}

// decompose_lint.go — the `autonomous lint ...` comm verbs: the
// decompose-lint config (read + scalar set), the read-only dry run for one
// Automaton, and the lint record the executor stored for it. These proxy
// the same daemon REST endpoints the `datawatch autonomous lint ...` CLI
// uses — the daemon owns the config and the engine; the router is a thin
// comm surface. Findings render through internal/autonomous
// (decompose_lint_text.go), the operator's one wording for the finding.

package router

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const decomposeLintUsage = "usage: autonomous lint config | config-set k=v ... | dry-run <prd-id> | findings <prd-id>"

// decomposeLintConfigPath is the daemon's decompose-lint config endpoint
// (GET snapshot, PUT validated replace of the named block(s)).
const decomposeLintConfigPath = "/api/autonomous/decompose-lint/config"

// decomposeLintPRDPath is the per-Automaton decompose-lint endpoint
// (GET the stored record, POST the read-only dry run).
func decomposeLintPRDPath(prd string) string {
	return "/api/autonomous/prds/" + prd + "/decompose-lint"
}

// DecomposeLintCommVerbs lists the decompose-lint comm verb forms for the
// parity checks. The chat command is the single verb "lint"; the args
// after it pick the form.
func DecomposeLintCommVerbs() []string {
	return []string{"lint config", "lint config-set k=v ...", "lint dry-run <prd-id>", "lint findings <prd-id>"}
}

// handleDecomposeLintVerb answers the `autonomous lint ...` comm verb.
// args is the full argument list ("lint", subverb, ...) as the autonomous
// handler passes it.
func (r *Router) handleDecomposeLintVerb(verb string, args []string) {
	sub := ""
	if len(args) > 1 {
		sub = args[1]
	}
	switch sub {
	case "config":
		r.replyDecomposeLintConfig()
	case "config-set", "config_set":
		r.replyDecomposeLintConfigSet(args[2:])
	case "dry-run", "dry_run":
		r.replyDecomposeLintDryRun(args)
	case "findings":
		r.replyDecomposeLintRecord(args)
	default:
		r.reply("autonomous lint", decomposeLintUsage)
	}
}

// replyDecomposeLintConfig shows the live decompose-lint config (a read).
func (r *Router) replyDecomposeLintConfig() {
	out, err := r.commGet(decomposeLintConfigPath, nil)
	if err != nil {
		r.reply("autonomous lint config failed", err.Error())
		return
	}
	r.reply("autonomous lint config", prettyJSON(out))
}

// replyDecomposeLintConfigSet sets scalar fields of the decompose_lint
// block (mode, max_retries, disabled_rules), preserving the rest —
// notably the rule pack — by reading the live block and PUTting the
// merged one back, the same merge the CLI does with one wording.
func (r *Router) replyDecomposeLintConfigSet(kvs []string) {
	if len(kvs) == 0 {
		r.reply("autonomous lint config-set failed", "expected mode, max_retries or disabled_rules as key=value\n"+decomposeLintUsage)
		return
	}
	overrides, err := autonomous.ParseDecomposeLintOverrides(kvs)
	if err != nil {
		r.reply("autonomous lint config-set failed", err.Error())
		return
	}
	cur, err := r.commGet(decomposeLintConfigPath, nil)
	if err != nil {
		r.reply("autonomous lint config-set failed", err.Error())
		return
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cur), &cfg); err != nil {
		r.reply("autonomous lint config-set failed", "decompose-lint config is not a JSON object")
		return
	}
	block, err := autonomous.MergeDecomposeLintBlock(cfg["decompose_lint"], overrides)
	if err != nil {
		r.reply("autonomous lint config-set failed", err.Error())
		return
	}
	payload, _ := json.Marshal(map[string]json.RawMessage{"decompose_lint": block})
	out, err := r.commJSON(http.MethodPut, decomposeLintConfigPath, string(payload))
	if err != nil {
		r.reply("autonomous lint config-set failed", err.Error())
		return
	}
	r.reply("autonomous lint config-set", prettyJSON(out))
}

// replyDecomposeLintDryRun lints one Automaton's stored plan READ-ONLY and
// replies with the rendered report — no planner call, no retry, nothing
// stored.
func (r *Router) replyDecomposeLintDryRun(args []string) {
	if len(args) < 3 {
		r.reply("autonomous lint dry-run failed", "expected a prd-id\n"+decomposeLintUsage)
		return
	}
	id := args[2]
	out, err := r.commJSON(http.MethodPost, decomposeLintPRDPath(id), "null")
	if err != nil {
		r.reply("autonomous lint dry-run failed", err.Error())
		return
	}
	var rep autonomous.DecomposeLintReport
	if err := json.Unmarshal([]byte(out), &rep); err == nil {
		r.reply("autonomous lint dry-run", autonomous.RenderDecomposeLintReport(rep))
		return
	}
	r.reply("autonomous lint dry-run", prettyJSON(out))
}

// replyDecomposeLintRecord shows the decompose-lint record the executor
// stored for one Automaton (or the "no record" wording).
func (r *Router) replyDecomposeLintRecord(args []string) {
	if len(args) < 3 {
		r.reply("autonomous lint findings failed", "expected a prd-id\n"+decomposeLintUsage)
		return
	}
	id := args[2]
	out, err := r.commGet(decomposeLintPRDPath(id), nil)
	if err != nil {
		r.reply("autonomous lint findings failed", err.Error())
		return
	}
	var rec struct {
		PRDID         string                          `json:"prd_id"`
		DecomposeLint *autonomous.DecomposeLintRecord `json:"decompose_lint"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err == nil {
		prd := rec.PRDID
		if prd == "" {
			prd = id
		}
		r.reply("autonomous lint findings", autonomous.RenderDecomposeLintRecord(prd, rec.DecomposeLint))
		return
	}
	r.reply("autonomous lint findings", prettyJSON(out))
}

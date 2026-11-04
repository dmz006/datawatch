// decompose_lint.go — the comm channel surface for the decompose-lint
// config, the dry-run and the stored findings: the parity for
// `datawatch autonomous lint ...` in cmd/datawatch/cli_autonomous_lint.go
// (registered in sx2_parity.go's handleAutonomous switch as
// "lint"/"decompose-lint"/"decompose_lint").
//
// Every sub-verb proxies the daemon's REST endpoints for decompose-lint
// (GET /api/autonomous/decompose-lint/config, PUT
// /api/autonomous/decompose-lint/config, POST (dry-run) and GET
// /api/autonomous/prds/{id}/decompose-lint) over the same commGet/
// commJSON loopback lane-fill uses, so chat and CLI agree. And the
// finding lines themselves come from the shared wording in
// internal/autonomous/decompose_lint_text.go — the same
// `[severity] rule_id task: message` the CLI prints — so an operator
// reads one wording in both surfaces.
package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// decomposeLintUsage is the `usage:` a bad shape gets back; the tokens are
// the same the dispatch below accepts.
const decomposeLintUsage = "usage: autonomous lint config | lint config-set mode=... max_retries=N disabled_rules=a,b | lint dry-run <prd-id> | lint findings <prd-id>"

// DecomposeLintCommVerbs lists the decompose-lint comm verbs, the parity
// for LaneFillCommVerbs.
func DecomposeLintCommVerbs() []string {
	return []string{"lint config", "lint config-set", "lint dry-run", "lint findings"}
}

// handleDecomposeLintVerb dispatches the four lint sub-verbs. args[0] is
// the verb token as the autonomous handler passed it ("lint" or an alias);
// the sub-verb is the token after it.
func (r *Router) handleDecomposeLintVerb(verb string, args []string) {
	failLabel := "autonomous " + verb
	if len(args) < 2 {
		r.reply(failLabel+" failed", decomposeLintUsage)
		return
	}
	sub := strings.ToLower(strings.Trim(args[1], "-_"))
	switch sub {
	case "config":
		r.replyDecomposeLintConfig(failLabel)
	case "config-set", "config_set":
		r.replyDecomposeLintConfigSet(failLabel, args[2:])
	case "dry-run", "dry_run":
		r.replyDecomposeLintDryRun(failLabel, args)
	case "findings":
		r.replyDecomposeLintFindings(failLabel, args)
	default:
		r.reply(failLabel+" failed", decomposeLintUsage)
	}
}

// replyDecomposeLintConfig — `lint config`: the live decompose-lint config.
func (r *Router) replyDecomposeLintConfig(failLabel string) {
	out, err := r.commGet("/api/autonomous/decompose-lint/config", nil)
	if err != nil {
		r.reply(failLabel+" failed", err.Error())
		return
	}
	r.reply("autonomous lint config", prettyJSON(out))
}

// replyDecomposeLintConfigSet — `lint config-set key=value ...`: set mode /
// max_retries / disabled_rules on top of the live block, preserving
// everything untouched (notably the active rule pack), and reply with the
// daemon's "here is the new config" body.
func (r *Router) replyDecomposeLintConfigSet(failLabel string, pairs []string) {
	if len(pairs) == 0 {
		r.reply(failLabel+" failed", "expected at least one of mode, max_retries, disabled_rules as key=value; "+decomposeLintUsage)
		return
	}
	overrides, err := autonomous.ParseDecomposeLintOverrides(pairs)
	if err != nil {
		r.reply(failLabel+" failed", err.Error())
		return
	}
	live, err := r.commGet("/api/autonomous/decompose-lint/config", nil)
	if err != nil {
		r.reply(failLabel+" failed", err.Error())
		return
	}
	block, err := autonomous.MergeDecomposeLintBlock([]byte(live), overrides)
	if err != nil {
		r.reply(failLabel+" failed", "merging the overrides: "+err.Error())
		return
	}
	body, err := json.Marshal(map[string]any{"decompose_lint": json.RawMessage(block)})
	if err != nil {
		r.reply(failLabel+" failed", "encoding the merged block: "+err.Error())
		return
	}
	out, err := r.commJSON(http.MethodPut, "/api/autonomous/decompose-lint/config", string(body))
	if err != nil {
		r.reply(failLabel+" failed", err.Error())
		return
	}
	r.reply("autonomous lint config-set", prettyJSON(out))
}

// replyDecomposeLintDryRun — `lint dry-run <prd-id>`: lint one stored plan
// read-only and reply with the daemon's report, one line per finding in
// the shared wording.
func (r *Router) replyDecomposeLintDryRun(failLabel string, args []string) {
	if len(args) < 3 || args[2] == "" {
		r.reply(failLabel+" failed", "expected: autonomous lint dry-run <prd-id>")
		return
	}
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[2]+"/decompose-lint", "")
	if err != nil {
		r.reply(failLabel+" failed", err.Error())
		return
	}
	var rep autonomous.DecomposeLintReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		r.reply(failLabel+" failed", "the reply is not a decompose-lint report: "+err.Error())
		return
	}
	r.reply("autonomous lint dry-run", strings.TrimRight(autonomous.RenderDecomposeLintReport(rep), "\n"))
}

// replyDecomposeLintFindings — `lint findings <prd-id>`: the
// decompose-lint record the executor stored for one PRD.
func (r *Router) replyDecomposeLintFindings(failLabel string, args []string) {
	if len(args) < 3 || args[2] == "" {
		r.reply(failLabel+" failed", "expected: autonomous lint findings <prd-id>")
		return
	}
	out, err := r.commGet("/api/autonomous/prds/"+args[2]+"/decompose-lint", nil)
	if err != nil {
		r.reply(failLabel+" failed", err.Error())
		return
	}
	var stored struct {
		DecomposeLint *autonomous.DecomposeLintRecord `json:"decompose_lint"`
	}
	if err := json.Unmarshal([]byte(out), &stored); err != nil {
		r.reply(failLabel+" failed", "the reply is not a stored decompose-lint record: "+err.Error())
		return
	}
	r.reply("autonomous lint findings", strings.TrimRight(autonomous.RenderDecomposeLintRecord(args[2], stored.DecomposeLint), "\n"))
}

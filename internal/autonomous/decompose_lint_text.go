// decompose_lint_text.go — the operator-facing text for decompose lint,
// shared by the `datawatch autonomous lint ...` CLI verbs and the `autonomous
// lint ...` comm verbs so the operator sees one wording wherever they look,
// plus the plain key=value override parsing both surfaces accept.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LintFindingLine renders one decompose-lint finding:
// `[severity] rule_id task: message`. When a finding is scoped to a story
// (no single offending task) the story id stands in for the task, and
// neither is known (a whole-plan finding) the id field is "-".
func LintFindingLine(f LintFinding) string {
	id := strings.TrimSpace(f.Task)
	if id == "" {
		id = strings.TrimSpace(f.Story)
	}
	if id == "" {
		id = "-"
	}
	return fmt.Sprintf("[%s] %s %s: %s", f.Severity, f.RuleID, id, f.Message)
}

// RenderLintFindings renders a finding set one line per finding; an empty
// set is a clean run and says so.
func RenderLintFindings(findings []LintFinding) string {
	if len(findings) == 0 {
		return "clean: 0 findings"
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, LintFindingLine(f))
	}
	return strings.Join(lines, "\n")
}

// RenderDecomposeLintReport renders a dry-run report (what a plan would
// record) in the operator-facing form: one header line, one line per
// finding.
func RenderDecomposeLintReport(rep DecomposeLintReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "decompose lint of %s (mode %s, %d active rules): %d block, %d warn\n",
		rep.PRD, rep.Mode, rep.RuleCount, rep.Summary.Block, rep.Summary.Warn)
	b.WriteString(RenderLintFindings(rep.Findings))
	return b.String()
}

// RenderDecomposeLintRecord renders the decompose-lint record the executor
// stored for one PRD, or the "no record" wording when none is stored.
func RenderDecomposeLintRecord(prd string, rec *DecomposeLintRecord) string {
	if rec == nil {
		return "no decompose-lint record stored for " + prd +
			" — mode off records nothing, and there is none before the first decompose or after deletion"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "decompose lint of %s (mode %s, %d attempts, %d findings)\n",
		prd, rec.Mode, rec.Attempts, len(rec.Findings))
	b.WriteString(RenderLintFindings(rec.Findings))
	return b.String()
}

// ParseDecomposeLintOverrides parses `key=value` pairs (the CLI's
// `lint config-set` args and the comm verb's) into overrides for the
// decompose_lint config block. Allowed keys and values:
//
//	mode           "off" | "shadow" | "enforce"
//	max_retries    whole number >= 0
//	disabled_rules comma list of rule ids (empty value = nothing disabled)
//
// The daemon's PUT is the final gate (it validates the full block); this
// parsing exists so both surfaces reject bad input before any request is
// sent, with one wording.
func ParseDecomposeLintOverrides(pairs []string) (map[string]any, error) {
	out := map[string]any{}
	for _, kv := range pairs {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			return nil, fmt.Errorf("expected key=value, got %q", kv)
		}
		key := strings.TrimSpace(kv[:i])
		val := strings.TrimSpace(kv[i+1:])
		switch strings.ToLower(key) {
		case "mode":
			mode := strings.ToLower(val)
			switch mode {
			case "off", "shadow", "enforce":
			default:
				return nil, fmt.Errorf("mode must be off, shadow or enforce (got %q)", val)
			}
			out["mode"] = mode
		case "max_retries":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("max_retries must be a whole number >= 0 (got %q)", val)
			}
			out["max_retries"] = n
		case "disabled_rules":
			rules := []string{}
			for _, p := range strings.Split(val, ",") {
				if p = strings.TrimSpace(p); p != "" {
					rules = append(rules, p)
				}
			}
			out["disabled_rules"] = rules
		default:
			return nil, fmt.Errorf("unknown key %q (allowed: mode, max_retries, disabled_rules)", key)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("expected at least one of mode, max_retries, disabled_rules as key=value")
	}
	return out, nil
}

// MergeDecomposeLintBlock applies parsed overrides (from
// ParseDecomposeLintOverrides) onto the current decompose_lint config
// block — the live JSON object a GET returns — and returns the merged
// block as JSON. Everything the block already carries that the override
// does not touch (notably the active rules) passes through unchanged;
// that is what keeps `config-set mode=enforce` from wiping the pack.
func MergeDecomposeLintBlock(current []byte, overrides map[string]any) ([]byte, error) {
	block := map[string]json.RawMessage{}
	if trimmed := strings.TrimSpace(string(current)); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &block); err != nil {
			return nil, fmt.Errorf("the current decompose_lint block is not a JSON object: %v", err)
		}
	}
	for key, val := range overrides {
		var raw []byte
		var err error
		if raw, err = json.Marshal(val); err != nil {
			return nil, fmt.Errorf("cannot encode %s: %v", key, err)
		}
		block[key] = raw
	}
	return json.Marshal(block)
}

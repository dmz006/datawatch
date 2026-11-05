// autonomous rules over the chat channel: the same read and write as the
// REST route /api/autonomous/rules-context.

package router

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

const (
	rulesUsage    = "usage: autonomous rules [automaton-id]"
	rulesSetUsage = "usage: autonomous rules set <rules_file|context_files|digest_mode|digest_max_bytes> <value> [project-dir]   (context_files takes a comma-separated list)"
	rulesPath     = "/api/autonomous/rules-context"
)

// RulesContextCommVerbs lists the rules and context comm verbs for the parity test.
func RulesContextCommVerbs() []string { return []string{"rules", "rules set"} }

// handleRulesContextVerb answers `autonomous rules ...`; args[0] is "rules".
func (r *Router) handleRulesContextVerb(args []string) {
	rest := args[1:]
	if len(rest) > 0 && strings.EqualFold(rest[0], "set") {
		r.handleRulesContextSet(rest[1:])
		return
	}
	if len(rest) > 1 {
		r.reply("autonomous rules failed", rulesUsage)
		return
	}
	q := url.Values{}
	if len(rest) == 1 && strings.TrimSpace(rest[0]) != "" {
		q.Set("prd", strings.TrimSpace(rest[0]))
	}
	out, err := r.commGet(rulesPath, q)
	if err != nil {
		r.reply("autonomous rules failed", err.Error())
		return
	}
	text, err := autonomous.RenderRulesContextStatusText([]byte(out))
	if err != nil {
		r.reply("autonomous rules failed", err.Error())
		return
	}
	r.reply("autonomous rules", strings.TrimRight(text, "\n"))
}

func (r *Router) handleRulesContextSet(args []string) {
	if len(args) < 2 || len(args) > 3 {
		r.reply("autonomous rules set failed", rulesSetUsage)
		return
	}
	u := config.RulesContextUpdate{}
	if len(args) == 3 {
		u.ProjectDir = strings.TrimSpace(args[2])
	}
	if err := u.SetRulesContextField(args[0], args[1]); err != nil {
		r.reply("autonomous rules set failed", err.Error()+"\n"+rulesSetUsage)
		return
	}
	cur, err := r.commGet(rulesPath, nil)
	if err != nil {
		r.reply("autonomous rules set failed", err.Error())
		return
	}
	curCfg, err := autonomous.ParseRulesContextConfig([]byte(cur))
	if err != nil {
		r.reply("autonomous rules set failed", err.Error())
		return
	}
	next, err := config.ApplyRulesContextUpdate(curCfg, u)
	if err != nil {
		r.reply("autonomous rules set failed", err.Error())
		return
	}
	body, _ := json.Marshal(next)
	path := rulesPath
	if u.ProjectDir != "" {
		path += "?" + url.Values{"project_dir": {u.ProjectDir}}.Encode()
	}
	out, err := r.commJSON("PUT", path, string(body))
	if err != nil {
		r.reply("autonomous rules set failed", err.Error())
		return
	}
	text, err := autonomous.RenderRulesContextStatusText([]byte(out))
	if err != nil {
		r.reply("autonomous rules set failed", err.Error())
		return
	}
	r.reply("autonomous rules set", strings.TrimRight(text, "\n"))
}

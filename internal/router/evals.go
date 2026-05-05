// BL259 Phase 1 v6.10.0 — comm-channel verbs for the Evals framework.
//
//	evals                    → list suites
//	evals list               → list suites (alias)
//	evals run <suite>        → execute suite
//	evals runs [<suite>]     → list past runs
//	evals get-run <id>       → fetch one run

package router

import (
	"net/url"
	"strings"
)

const evalsUsage = `Usage:
  evals                            list suites
  evals list                       list suites
  evals run <suite>                execute a suite
  evals runs [<suite>]             list past runs (most recent first)
  evals get-run <id>               fetch one run`

func (r *Router) handleEvalsCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if text == "" || lower == "list" {
		out, err := r.commGet("/api/evals/suites", nil)
		if err != nil {
			r.reply("evals", err.Error())
			return
		}
		r.reply("evals", prettyJSON(out))
		return
	}

	parts := strings.Fields(text)
	if len(parts) == 0 {
		r.reply("evals", evalsUsage)
		return
	}
	switch strings.ToLower(parts[0]) {
	case "run":
		if len(parts) < 2 {
			r.reply("evals run", "Usage: evals run <suite>")
			return
		}
		suite := parts[1]
		out, err := r.commJSON("POST", "/api/evals/run?suite="+url.QueryEscape(suite), "")
		if err != nil {
			r.reply("evals run", err.Error())
			return
		}
		r.reply("evals run "+suite, prettyJSON(out))
	case "runs":
		path := "/api/evals/runs"
		if len(parts) > 1 {
			path = path + "?suite=" + url.QueryEscape(parts[1])
		}
		out, err := r.commGet(path, nil)
		if err != nil {
			r.reply("evals runs", err.Error())
			return
		}
		r.reply("evals runs", prettyJSON(out))
	case "get-run":
		if len(parts) < 2 {
			r.reply("evals get-run", "Usage: evals get-run <id>")
			return
		}
		out, err := r.commGet("/api/evals/runs/"+parts[1], nil)
		if err != nil {
			r.reply("evals get-run", err.Error())
			return
		}
		r.reply("evals "+parts[1], prettyJSON(out))
	default:
		r.reply("evals", evalsUsage)
	}
}

// BL258 v6.9.0 — comm-channel verbs for Algorithm Mode.
//
//	algorithm                                  → list
//	algorithm get <session-id>                 → read state
//	algorithm start <session-id>               → register session
//	algorithm advance <session-id> [output...] → close phase + advance
//	algorithm edit <session-id> <output...>    → replace last phase output
//	algorithm abort <session-id>               → terminate
//	algorithm reset <session-id>               → clear state

package router

import (
	"encoding/json"
	"strings"
)

const algorithmUsage = `Usage:
  algorithm                                       list active sessions
  algorithm get <session-id>                      read state
  algorithm start <session-id>                    register session in Algorithm Mode
  algorithm advance <session-id> [output...]      close phase + advance
  algorithm edit <session-id> <output...>         replace last phase output
  algorithm abort <session-id>                    terminate
  algorithm reset <session-id>                    clear state
  algorithm measure <session-id> <suite>          run eval suite + advance Measure phase (BL259 P2)`

func (r *Router) handleAlgorithmCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)

	if text == "" {
		out, err := r.commGet("/api/algorithm", nil)
		if err != nil {
			r.reply("algorithm", err.Error())
			return
		}
		r.reply("algorithm", prettyJSON(out))
		return
	}

	parts := strings.Fields(text)
	if len(parts) < 2 {
		r.reply("algorithm", algorithmUsage)
		return
	}
	verb := strings.ToLower(parts[0])
	id := parts[1]
	rest := ""
	if len(parts) > 2 {
		rest = strings.TrimSpace(text[len(parts[0])+1+len(id)+1:])
	}

	switch verb {
	case "get":
		out, err := r.commGet("/api/algorithm/"+id, nil)
		if err != nil {
			r.reply("algorithm get", err.Error())
			return
		}
		r.reply("algorithm "+id, prettyJSON(out))
	case "start":
		out, err := r.commJSON("POST", "/api/algorithm/"+id+"/start", "")
		if err != nil {
			r.reply("algorithm start", err.Error())
			return
		}
		r.reply("algorithm "+id+" started", prettyJSON(out))
	case "advance":
		body, _ := json.Marshal(map[string]any{"output": rest})
		out, err := r.commJSON("POST", "/api/algorithm/"+id+"/advance", string(body))
		if err != nil {
			r.reply("algorithm advance", err.Error())
			return
		}
		r.reply("algorithm "+id+" advanced", prettyJSON(out))
	case "edit":
		if rest == "" {
			r.reply("algorithm edit", "edit requires output text")
			return
		}
		body, _ := json.Marshal(map[string]any{"output": rest})
		out, err := r.commJSON("POST", "/api/algorithm/"+id+"/edit", string(body))
		if err != nil {
			r.reply("algorithm edit", err.Error())
			return
		}
		r.reply("algorithm "+id+" edited", prettyJSON(out))
	case "abort":
		out, err := r.commJSON("POST", "/api/algorithm/"+id+"/abort", "")
		if err != nil {
			r.reply("algorithm abort", err.Error())
			return
		}
		r.reply("algorithm "+id+" aborted", prettyJSON(out))
	case "reset":
		out, err := r.commJSON("DELETE", "/api/algorithm/"+id, "")
		if err != nil {
			r.reply("algorithm reset", err.Error())
			return
		}
		r.reply("algorithm "+id+" reset", prettyJSON(out))
	case "measure":
		// BL259 P2 v6.10.1 — measure-with-eval bridge. rest = suite name.
		if rest == "" {
			r.reply("algorithm measure", "Usage: algorithm measure <session-id> <suite>")
			return
		}
		out, err := r.commJSON("POST", "/api/algorithm/"+id+"/measure?suite="+rest, "")
		if err != nil {
			r.reply("algorithm measure", err.Error())
			return
		}
		r.reply("algorithm "+id+" measured", prettyJSON(out))
	default:
		r.reply("algorithm", algorithmUsage)
	}
}

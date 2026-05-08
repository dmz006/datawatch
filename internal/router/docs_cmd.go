// BL274 Sprint 1, v6.16.0 — comm-channel handler for `docs ...` verbs.
//
//   docs search <query>
//   docs read <path> [<anchor>]
//   docs list-howtos
//   docs apply <howto-id>                   (mode=plan, returns approval_token)
//   docs execute <howto-id> <token>         (mode=execute, consumes token)
//   docs execute-gated <howto-id> <token>   (mode=execute, risk_gate=true)
//   docs trust list
//   docs trust add <source>
//   docs trust remove <source>
//   docs trust pending
//   docs trust accept <source>...
//   docs trust dismiss <source>...
//   docs trust export

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func (r *Router) handleDocsCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	switch {
	case strings.HasPrefix(lower, "search "):
		query := strings.TrimSpace(text[len("search "):])
		q := url.Values{}
		q.Set("q", query)
		out, err := r.commGet("/api/docs/search?"+q.Encode(), nil)
		r.replyResult("docs search", out, err)
		return

	case strings.HasPrefix(lower, "read "):
		args := strings.Fields(text[len("read "):])
		if len(args) == 0 {
			r.reply("docs read", "Usage: docs read <path> [<anchor>]")
			return
		}
		q := url.Values{}
		q.Set("path", args[0])
		if len(args) > 1 {
			q.Set("anchor", args[1])
		}
		out, err := r.commGet("/api/docs/read?"+q.Encode(), nil)
		r.replyResult("docs read", out, err)
		return

	case lower == "list-howtos" || lower == "howtos":
		out, err := r.commGet("/api/docs/list-howtos", nil)
		r.replyResult("docs howtos", out, err)
		return

	case strings.HasPrefix(lower, "apply "):
		howto := strings.TrimSpace(text[len("apply "):])
		body, _ := json.Marshal(map[string]interface{}{
			"howto_id": howto,
			"mode":     "plan",
		})
		out, err := r.commJSON(http.MethodPost, "/api/docs/apply", string(body))
		r.replyResult("docs apply (plan)", out, err)
		return

	// BL274 v6.22.0 — comm-channel execute mode (audit-honesty backfill).
	// S3 claimed 7-surface parity for execute mode but the comm verb only
	// proxied plan; verified gap during operator's spot-check 2026-05-08.
	case strings.HasPrefix(lower, "execute "):
		args := strings.Fields(text[len("execute "):])
		if len(args) < 2 {
			r.reply("docs execute", "Usage: docs execute <howto-id> <approval-token> [risk-gate]")
			return
		}
		body, _ := json.Marshal(map[string]interface{}{
			"howto_id":       args[0],
			"approval_token": args[1],
			"mode":           "execute",
			"risk_gate":      false,
		})
		out, err := r.commJSON(http.MethodPost, "/api/docs/apply", string(body))
		r.replyResult("docs execute", out, err)
		return

	case strings.HasPrefix(lower, "execute-gated "):
		args := strings.Fields(text[len("execute-gated "):])
		if len(args) < 2 {
			r.reply("docs execute-gated", "Usage: docs execute-gated <howto-id> <approval-token>")
			return
		}
		body, _ := json.Marshal(map[string]interface{}{
			"howto_id":       args[0],
			"approval_token": args[1],
			"mode":           "execute",
			"risk_gate":      true,
		})
		out, err := r.commJSON(http.MethodPost, "/api/docs/apply", string(body))
		r.replyResult("docs execute-gated", out, err)
		return

	case lower == "trust" || lower == "trust list":
		out, err := r.commGet("/api/docs/trust", nil)
		r.replyResult("docs trust", out, err)
		return

	case strings.HasPrefix(lower, "trust add "):
		src := strings.TrimSpace(text[len("trust add "):])
		body, _ := json.Marshal(map[string]string{"source": src, "granted_by": "comm"})
		out, err := r.commJSON(http.MethodPost, "/api/docs/trust", string(body))
		r.replyResult("docs trust add", out, err)
		return

	case strings.HasPrefix(lower, "trust remove "):
		src := strings.TrimSpace(text[len("trust remove "):])
		out, err := r.commJSON(http.MethodDelete, "/api/docs/trust/"+src, "")
		r.replyResult("docs trust remove", out, err)
		return

	case lower == "trust pending":
		out, err := r.commGet("/api/docs/trust/pending", nil)
		r.replyResult("docs trust pending", out, err)
		return

	case strings.HasPrefix(lower, "trust accept "):
		args := strings.Fields(text[len("trust accept "):])
		body, _ := json.Marshal(map[string][]string{"sources": args})
		out, err := r.commJSON(http.MethodPost, "/api/docs/trust/accept", string(body))
		r.replyResult("docs trust accept", out, err)
		return

	case strings.HasPrefix(lower, "trust dismiss "):
		args := strings.Fields(text[len("trust dismiss "):])
		body, _ := json.Marshal(map[string][]string{"sources": args})
		out, err := r.commJSON(http.MethodPost, "/api/docs/trust/dismiss", string(body))
		r.replyResult("docs trust dismiss", out, err)
		return

	case lower == "trust export":
		out, err := r.commGet("/api/docs/trust/export", nil)
		r.replyResult("docs trust export", out, err)
		return

	case lower == "" || lower == "help":
		r.reply("docs", `Usage:
  docs search <query>
  docs read <path> [<anchor>]
  docs list-howtos
  docs apply <howto-id>                       (mode=plan; returns approval_token)
  docs execute <howto-id> <approval-token>    (mode=execute, runs all steps)
  docs execute-gated <howto-id> <token>       (mode=execute, pauses before each mutating step)
  docs trust list
  docs trust add <source>          (skill:<n> | plugin:<n>)
  docs trust remove <source>
  docs trust pending
  docs trust accept <source>...    (multi-arg bulk accept)
  docs trust dismiss <source>...   (multi-arg bulk dismiss)
  docs trust export                (YAML for committing to config)`)
		return

	default:
		r.reply("docs", "Unknown subcommand. Try `docs help`.")
		return
	}
}

// replyResult is a small helper that mirrors the secrets/skills pattern.
func (r *Router) replyResult(label, body string, err error) {
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, prettyJSON(body))
}

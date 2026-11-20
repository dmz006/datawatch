// autonomous supervisor-setup over the chat channel: the idempotent
// supervisor set-up, state-record edits and corrections, as thin proxies of
// the /api/supervisor-setup routes. Fields are key=value words; the words
// after a key belong to its value until the next known key, so a text or a
// reason can hold spaces.

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const supervisorSetupUsage = "usage: autonomous supervisor-setup run project=P model=M [schedules=name,name]\n" +
	"       autonomous supervisor-setup state get project=P [role=R]\n" +
	"       autonomous supervisor-setup state set|change|remove project=P id=I [section=S] [text=..] [owner=..] [next_step=..] who=W why=..\n" +
	"       autonomous supervisor-setup correction list project=P\n" +
	"       autonomous supervisor-setup correction record|promote project=P id=I [class=C] [text=..] [evidence=..] [workaround=..] who=W why=.."

// SupervisorSetupCommVerbs lists the supervisor set-up comm verbs for the parity test.
func SupervisorSetupCommVerbs() []string {
	return []string{"supervisor-setup", "supervisor_setup"}
}

var supervisorSetupKeys = map[string]bool{
	"project": true, "model": true, "schedules": true, "role": true, "id": true, "section": true,
	"text": true, "owner": true, "next_step": true, "who": true, "why": true,
	"class": true, "evidence": true, "workaround": true,
}

// parseSupervisorSetupFields reads key=value words; unknown leading words are an error.
func parseSupervisorSetupFields(words []string) (map[string]string, string) {
	out := map[string]string{}
	cur := ""
	for _, w := range words {
		if k, v, ok := strings.Cut(w, "="); ok && supervisorSetupKeys[k] {
			cur = k
			out[k] = v
			continue
		}
		if cur == "" {
			return nil, "unknown argument " + w
		}
		out[cur] += " " + w
	}
	return out, ""
}

func (r *Router) handleSupervisorSetupVerb(args []string) {
	const title = "autonomous supervisor-setup failed"
	rest := args[1:]
	if len(rest) == 0 {
		r.reply(title, supervisorSetupUsage)
		return
	}
	group := rest[0]
	action := ""
	fieldWords := rest[1:]
	if group == "state" || group == "correction" {
		if len(rest) < 2 {
			r.reply(title, supervisorSetupUsage)
			return
		}
		action, fieldWords = rest[1], rest[2:]
	} else if group != "run" {
		r.reply(title, "unknown sub-verb "+group+"\n"+supervisorSetupUsage)
		return
	}
	f, bad := parseSupervisorSetupFields(fieldWords)
	if bad != "" {
		r.reply(title, bad+"\n"+supervisorSetupUsage)
		return
	}
	for k := range f {
		f[k] = strings.TrimSpace(f[k])
	}
	var out string
	var err error
	switch {
	case group == "run":
		body := map[string]any{"project": f["project"], "model": f["model"]}
		if s := f["schedules"]; s != "" {
			list := []map[string]string{}
			for _, n := range strings.Split(s, ",") {
				if n = strings.TrimSpace(n); n != "" {
					list = append(list, map[string]string{"name": n})
				}
			}
			body["schedules"] = list
		}
		out, err = r.commPostJSON(http.MethodPost, "/api/supervisor-setup", body)
	case (group == "state" && action == "get") || (group == "correction" && action == "list"):
		q := url.Values{"project": {f["project"]}}
		if f["role"] != "" {
			q.Set("role", f["role"])
		}
		path := "/api/supervisor-setup/state"
		if group == "correction" {
			path = "/api/supervisor-setup/corrections"
		}
		out, err = r.commGet(path, q)
	case group == "state" && (action == "set" || action == "change" || action == "remove"):
		method := map[string]string{"set": http.MethodPost, "change": http.MethodPatch, "remove": http.MethodDelete}[action]
		body := map[string]any{}
		for k, v := range f {
			body[k] = v
		}
		out, err = r.commPostJSON(method, "/api/supervisor-setup/state", body)
	case group == "correction" && (action == "record" || action == "promote"):
		path := "/api/supervisor-setup/corrections"
		if action == "promote" {
			path += "/promote"
		}
		body := map[string]any{}
		for k, v := range f {
			body[k] = v
		}
		out, err = r.commPostJSON(http.MethodPost, path, body)
	default:
		r.reply(title, "unknown action "+action+"\n"+supervisorSetupUsage)
		return
	}
	if err != nil {
		r.reply(title, err.Error())
		return
	}
	r.reply("autonomous supervisor-setup "+group, strings.TrimSpace(out))
}

func (r *Router) commPostJSON(method, path string, body map[string]any) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return r.commJSON(method, path, string(b))
}

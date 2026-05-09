// v7.0.0 S2 — comm-channel verbs for the LLM-inference registry.
//
//	llm                                              → list LLMs
//	llm list                                         → list LLMs
//	llm get <name>                                   → fetch one
//	llm add <name> kind=<k> [k=v ...]                → add
//	llm update <name> [k=v ...]                      → update
//	llm delete <name>                                → remove
//	llm test <name> [prompt=<text>]                  → one-shot probe

package router

import (
	"encoding/json"
	"strings"
)

const llmUsage = `Usage:
  llm                                              list LLMs
  llm list                                         list LLMs
  llm get <name>                                   fetch one
  llm add <name> kind=<k> model=<m> [k=v ...]      add (kind=ollama|openwebui|opencode|claude)
  llm update <name> [k=v ...]                      update
  llm delete <name>                                remove
  llm test <name> [prompt=<text>]                  one-shot inference probe

Common kv pairs:
  model=<name>            compute_nodes=<csv>      api_key_ref=<literal-or-${secret:name}>
  timeout_seconds=<N>     tags=<csv>`

func (r *Router) handleLLMCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" || strings.EqualFold(text, "list") {
		out, err := r.commGet("/api/llms", nil)
		if err != nil {
			r.reply("llm", err.Error())
			return
		}
		r.reply("llm", prettyJSON(out))
		return
	}
	if strings.EqualFold(text, "help") {
		r.reply("llm", llmUsage)
		return
	}
	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])
	tail := ""
	if len(parts) > 1 {
		tail = strings.TrimSpace(parts[1])
	}
	switch verb {
	case "get":
		if tail == "" {
			r.reply("llm get", "Usage: llm get <name>")
			return
		}
		out, err := r.commGet("/api/llms/"+tail, nil)
		if err != nil {
			r.reply("llm get", err.Error())
			return
		}
		r.reply("llm "+tail, prettyJSON(out))
	case "add", "update":
		nameAndKV := strings.SplitN(tail, " ", 2)
		if len(nameAndKV) < 1 || nameAndKV[0] == "" {
			r.reply("llm "+verb, "Usage: llm "+verb+" <name> kind=<k> model=<m> [k=v ...]")
			return
		}
		name := nameAndKV[0]
		kvText := ""
		if len(nameAndKV) > 1 {
			kvText = nameAndKV[1]
		}
		body := parseLLMKVPairs(kvText)
		body["name"] = name
		method := "POST"
		path := "/api/llms"
		if verb == "update" {
			method = "PUT"
			path = "/api/llms/" + name
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON(method, path, string(bodyJSON))
		if err != nil {
			r.reply("llm "+verb, err.Error())
			return
		}
		r.reply("llm "+verb, prettyJSON(out))
	case "delete":
		if tail == "" {
			r.reply("llm delete", "Usage: llm delete <name>")
			return
		}
		out, err := r.commJSON("DELETE", "/api/llms/"+tail, "")
		if err != nil {
			r.reply("llm delete", err.Error())
			return
		}
		r.reply("llm delete "+tail, prettyJSON(out))
	case "test":
		bits := strings.SplitN(tail, " ", 2)
		if len(bits) < 1 || bits[0] == "" {
			r.reply("llm test", "Usage: llm test <name> [prompt=<text>]")
			return
		}
		body := map[string]any{}
		if len(bits) > 1 {
			kv := parseLLMKVPairs(bits[1])
			if p, ok := kv["prompt"]; ok {
				body["prompt"] = p
			}
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON("POST", "/api/llms/"+bits[0]+"/test", string(bodyJSON))
		if err != nil {
			r.reply("llm test", err.Error())
			return
		}
		r.reply("llm test "+bits[0], prettyJSON(out))
	default:
		r.reply("llm", llmUsage)
	}
}

func parseLLMKVPairs(s string) map[string]any {
	out := map[string]any{}
	for _, kv := range strings.Fields(s) {
		eq := strings.IndexRune(kv, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(kv[:eq])
		v := kv[eq+1:]
		switch k {
		case "compute_nodes", "tags":
			out[k] = strings.Split(v, ",")
		case "timeout_seconds":
			out[k] = atoiOrZero(v)
		default:
			out[k] = v
		}
	}
	return out
}

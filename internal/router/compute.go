// v7.0.0 S1 — comm-channel verbs for the ComputeNode registry.
//
//	compute                       → list nodes (alias for `compute node list`)
//	compute node                  → list nodes
//	compute node list             → list nodes
//	compute node get <name>       → fetch one
//	compute node add <name> kind=remote address=https://gpu-1:11434 [k=v ...]
//	compute node update <name> [k=v ...]
//	compute node delete <name>
//	compute node health <name>
//	compute node detail <name>

package router

import (
	"encoding/json"
	"fmt"
	"strings"
)

const computeUsage = `Usage:
  compute                                          list nodes
  compute node                                     list nodes
  compute node list                                list nodes
  compute node get <name>                          fetch one
  compute node add <name> kind=<k> [k=v ...]       add (kind = local|ssh|docker|k8s|remote|remote-proxy)
  compute node update <name> [k=v ...]             update
  compute node delete <name>                       remove
  compute node health <name>                       declared capacity + maintenance
  compute node detail <name>                       on-demand pull from monitoring sidecar
  compute node attach-observer <name> <peer>       bind a registered observer peer (alpha.23b)
  compute node detach-observer <name>              clear observer-peer binding
  compute node observer-free                       list peers with no bound ComputeNode
  compute node observer-by-node                    local peers grouped by ComputeNode (alpha.24)
  compute node federation-meta-peers               federation meta view (alpha.24)

Common kv pairs:
  address=<host:port-or-url>  monitoring_endpoint=<url>
  max_concurrent_models=<N>   gpu_mem_gb=<N>     scheduling_priority=<0..100>
  tags=<csv>                  cost_per_hour=<usd>`

func (r *Router) handleComputeCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" || strings.EqualFold(text, "node") || strings.EqualFold(text, "node list") {
		out, err := r.commGet("/api/compute/nodes", nil)
		if err != nil {
			r.reply("compute", err.Error())
			return
		}
		r.reply("compute", prettyJSON(out))
		return
	}
	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])
	if verb == "help" {
		r.reply("compute", computeUsage)
		return
	}
	if verb != "node" {
		r.reply("compute", computeUsage)
		return
	}
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}
	bits := strings.SplitN(rest, " ", 2)
	subVerb := strings.ToLower(bits[0])
	tail := ""
	if len(bits) > 1 {
		tail = strings.TrimSpace(bits[1])
	}
	switch subVerb {
	case "list", "":
		out, err := r.commGet("/api/compute/nodes", nil)
		if err != nil {
			r.reply("compute node list", err.Error())
			return
		}
		r.reply("compute node list", prettyJSON(out))
	case "get":
		if tail == "" {
			r.reply("compute node get", "Usage: compute node get <name>")
			return
		}
		out, err := r.commGet("/api/compute/nodes/"+tail, nil)
		if err != nil {
			r.reply("compute node get", err.Error())
			return
		}
		r.reply("compute node "+tail, prettyJSON(out))
	case "add", "update":
		// "add <name> k=v k=v ..."
		nameAndKV := strings.SplitN(tail, " ", 2)
		if len(nameAndKV) < 1 || nameAndKV[0] == "" {
			r.reply("compute node "+subVerb, "Usage: compute node "+subVerb+" <name> kind=<k> [k=v ...]")
			return
		}
		name := nameAndKV[0]
		kvText := ""
		if len(nameAndKV) > 1 {
			kvText = nameAndKV[1]
		}
		body := parseKVPairs(kvText)
		body["name"] = name
		method := "POST"
		path := "/api/compute/nodes"
		if subVerb == "update" {
			method = "PUT"
			path = "/api/compute/nodes/" + name
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON(method, path, string(bodyJSON))
		if err != nil {
			r.reply("compute node "+subVerb, err.Error())
			return
		}
		r.reply("compute node "+subVerb, prettyJSON(out))
	case "delete":
		if tail == "" {
			r.reply("compute node delete", "Usage: compute node delete <name>")
			return
		}
		out, err := r.commJSON("DELETE", "/api/compute/nodes/"+tail, "")
		if err != nil {
			r.reply("compute node delete", err.Error())
			return
		}
		r.reply("compute node delete "+tail, prettyJSON(out))
	case "health":
		if tail == "" {
			r.reply("compute node health", "Usage: compute node health <name>")
			return
		}
		out, err := r.commGet("/api/compute/nodes/"+tail+"/health", nil)
		if err != nil {
			r.reply("compute node health", err.Error())
			return
		}
		r.reply("compute node health "+tail, prettyJSON(out))
	case "detail":
		if tail == "" {
			r.reply("compute node detail", "Usage: compute node detail <name>")
			return
		}
		out, err := r.commGet("/api/compute/nodes/"+tail+"/detail", nil)
		if err != nil {
			r.reply("compute node detail", err.Error())
			return
		}
		r.reply("compute node detail "+tail, prettyJSON(out))
	case "attach-observer":
		// alpha.23b — `compute node attach-observer <name> <peer>`
		bits := strings.Fields(tail)
		if len(bits) != 2 {
			r.reply("compute node attach-observer", "Usage: compute node attach-observer <name> <peer>")
			return
		}
		bodyJSON, _ := json.Marshal(map[string]any{"peer": bits[1]})
		out, err := r.commJSON("PUT", "/api/compute/nodes/"+bits[0]+"/observer-peer", string(bodyJSON))
		if err != nil {
			r.reply("compute node attach-observer", err.Error())
			return
		}
		r.reply("compute node attach-observer "+bits[0], prettyJSON(out))
	case "detach-observer":
		if tail == "" {
			r.reply("compute node detach-observer", "Usage: compute node detach-observer <name>")
			return
		}
		out, err := r.commJSON("DELETE", "/api/compute/nodes/"+tail+"/observer-peer", "")
		if err != nil {
			r.reply("compute node detach-observer", err.Error())
			return
		}
		r.reply("compute node detach-observer "+tail, prettyJSON(out))
	case "observer-free":
		out, err := r.commGet("/api/observer/peers/free", nil)
		if err != nil {
			r.reply("compute node observer-free", err.Error())
			return
		}
		r.reply("compute node observer-free", prettyJSON(out))
	case "observer-by-node":
		// alpha.24 #231 — grouped by ComputeNode
		out, err := r.commGet("/api/observer/peers/by-node", nil)
		if err != nil {
			r.reply("compute node observer-by-node", err.Error())
			return
		}
		r.reply("compute node observer-by-node", prettyJSON(out))
	case "federation-meta-peers":
		// alpha.24 #231 — federation meta view
		out, err := r.commGet("/api/federation/meta-peers", nil)
		if err != nil {
			r.reply("compute node federation-meta-peers", err.Error())
			return
		}
		r.reply("compute node federation-meta-peers", prettyJSON(out))
	case "pull-model":
		// alpha.33 #244 — `compute node pull-model <name> <model>`
		bits := strings.Fields(tail)
		if len(bits) != 2 {
			r.reply("compute node pull-model", "Usage: compute node pull-model <name> <model>")
			return
		}
		bodyJSON, _ := json.Marshal(map[string]any{"model": bits[1]})
		out, err := r.commJSON("POST", "/api/compute/nodes/"+bits[0]+"/models/pull", string(bodyJSON))
		if err != nil {
			r.reply("compute node pull-model", err.Error())
			return
		}
		r.reply("compute node pull-model "+bits[0], prettyJSON(out))
	case "remove-model":
		bits := strings.Fields(tail)
		if len(bits) != 2 {
			r.reply("compute node remove-model", "Usage: compute node remove-model <name> <model>")
			return
		}
		out, err := r.commJSON("DELETE", "/api/compute/nodes/"+bits[0]+"/models/"+bits[1], "")
		if err != nil {
			r.reply("compute node remove-model", err.Error())
			return
		}
		r.reply("compute node remove-model "+bits[0], prettyJSON(out))
	default:
		r.reply("compute", computeUsage)
	}
}

// parseKVPairs converts "kind=remote address=https://gpu-1:11434" into a
// map suitable for the REST POST/PUT body. Nested capacity fields are
// recognized (max_concurrent_models, gpu_mem_gb, gpus, ram_gb,
// gpu_vendor, gpu_model) and emitted under declared_capacity. tags
// are split on comma. Numeric fields are coerced to int when possible.
func parseKVPairs(s string) map[string]any {
	out := map[string]any{}
	cap := map[string]any{}
	for _, kv := range strings.Fields(s) {
		eq := strings.IndexRune(kv, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(kv[:eq])
		v := kv[eq+1:]
		switch k {
		case "tags":
			out["tags"] = strings.Split(v, ",")
		case "max_concurrent_models", "gpu_mem_gb", "gpus", "ram_gb":
			cap[k] = atoiOrZero(v)
		case "gpu_vendor", "gpu_model":
			cap[k] = v
		case "scheduling_priority":
			out[k] = atoiOrZero(v)
		case "cost_per_hour":
			out[k] = parseFloatOrZero(v)
		default:
			out[k] = v
		}
	}
	if len(cap) > 0 {
		out["declared_capacity"] = cap
	}
	return out
}

func atoiOrZero(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func parseFloatOrZero(s string) float64 {
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0
	}
	return f
}

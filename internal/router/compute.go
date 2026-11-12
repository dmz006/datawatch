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
//	compute node drain <name> [until=<RFC3339>] [reason=<text>]
//	compute node undrain <name>
//	compute node models [name] [model=<m>] [backend=<b>] [refresh]
//	compute pin                   → show pin_check_seconds
//	compute pin <seconds>         → set pin_check_seconds (0 = keeper off)
//	compute placement             → list pending placement asks
//	compute placement approve <id> → approve an ask (one-shot)
//	compute placement deny <id>    → deny an ask (one-shot)
//	compute headroom <node>       → stored headroom measurement (read only)
//	compute headroom measure <node> → measure the node now while idle, then store it

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

const computeUsage = `Usage:
  compute                                          list nodes
  compute node                                     list nodes
  compute node list                                list nodes, observed peers with no node, and auto-created nodes that never answered
  compute node get <name>                          fetch one
  compute node add <name> kind=<k> [k=v ...]       add (kind = ollama|openai-compat|gemini-api|opencode-api)
                                                   pin: node_pinned_model=<model> allow_model_swap=true|false
                                                   tls: tls_ca_file=<path> tls_cert_fingerprint=<sha256-hex> tls_insecure_skip_verify=true|false verify_tls=true|false
  compute node update <name> [k=v ...]             update (replaces the node; repeat node_pinned_model and the tls_ keys to keep them)
  compute pin                                      show pin_check_seconds (0 = keeper off)
  compute pin <seconds>                            set pin_check_seconds
  compute placement                                list pending placement asks (missing-model "ask" policy)
  compute placement approve <id>                    approve a placement ask (one-shot)
  compute placement deny <id>                       deny a placement ask (one-shot)
  compute headroom <node>                          show the stored headroom measurement (read only)
  compute headroom measure <node>                  measure the node now while idle and store it
  compute node delete <name>                       remove
  compute node health <name>                       declared capacity + maintenance
  compute node detail <name>                       on-demand pull from monitoring sidecar
  compute node drain <name> [until=<RFC3339>] [reason=<text>]  stop routing new sessions (until = when the drain ends)
  compute node undrain <name>                       clear the drain state
  compute node models [name] [model=<m>] [backend=<b>] [refresh]
                                                   models each node serves now; with model= also whether a node would accept it
                                                   (served models are kept for autonomous.model_routing.served_models_cache_seconds; refresh reads again)
  compute node attach-observer <name> <peer>       bind a registered observer peer (alpha.23b)
  compute node detach-observer <name>              clear observer-peer binding
  compute node observer-free                       list peers with no bound ComputeNode
  compute node observer-by-node                    local peers grouped by ComputeNode (alpha.24)
  compute node federation-meta-peers               federation meta view (alpha.24)

Routing (v8.0):
  routing=direct|docker-network|datawatch-proxy
  docker-network: image=<img> network=<net> port=<N> container=<name> auto_start=true auto_pull=true
  datawatch-proxy: peer=<server-name> remote_llm=<llm-name> timeout=<seconds>

Example:
  compute node add gpu-1 kind=ollama routing=docker-network image=ollama/ollama:latest network=datawatch-llm port=11434
  compute node add remote-gpu kind=ollama routing=datawatch-proxy peer=dc2 remote_llm=llama3

Common kv pairs:
  address=<host:port-or-url>  monitoring_endpoint=<url>
  max_concurrent_models=<N>   gpu_mem_gb=<N>     scheduling_priority=<0..100>
  tags=<csv>                  cost_per_hour=<usd>
  session_slots=<N>           (0 = derive from observer GPU data)
  verifier_slots=<N>          (worker slots for verifiers; leave out = 1 if
                               the node hosts a local engine, else 0; 0 = none;
                               above 1 needs a fresh headroom measurement)
  verifier_min_free_mem_mb=<N>(free GPU MB required before a verifier slot
                               starts; leave out = 1024 MB; 0 = check off)
  verifier_context_mb=<N>     (context MB one verifier slot holds, for the
                               advice figure; leave out = 0)

TLS keys (https nodes):
  tls_ca_file=<path>               extra PEM CA bundle to trust for this node
  tls_cert_fingerprint=<hex>       pin the node's leaf certificate (SHA-256, colons allowed)
  tls_insecure_skip_verify=true    turn certificate checking off for this node; audited
                                   and shown in the PWA; cannot be combined with the two above
  verify_tls=true                  model pulls to this node verify its certificate against the
                                   system roots; default off
  update replaces the whole node, so a tls_ key or verify_tls you leave out is cleared`

// replyComputeNodeList replies with the node list as plain text ending in
// the observed-peers and unanswered-auto-created sections.
func (r *Router) replyComputeNodeList(label string) {
	out, err := r.commGet("/api/compute/nodes", nil)
	if err != nil {
		r.reply(label, err.Error())
		return
	}
	if txt, ferr := compute.FormatListText([]byte(out), compute.CommAddLine); ferr == nil {
		r.reply(label, txt)
		return
	}
	r.reply(label, prettyJSON(out))
}

func (r *Router) handleComputeCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" || strings.EqualFold(text, "node") || strings.EqualFold(text, "node list") {
		r.replyComputeNodeList("compute")
		return
	}
	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])
	if verb == "help" {
		r.reply("compute", computeUsage)
		return
	}
	if verb == "pin" {
		arg := ""
		if len(parts) > 1 {
			arg = strings.TrimSpace(parts[1])
		}
		r.handleComputePinCmd(arg)
		return
	}
	if verb == "placement" || verb == "placements" {
		arg := ""
		if len(parts) > 1 {
			arg = strings.TrimSpace(parts[1])
		}
		r.handleComputePlacementCmd(arg)
		return
	}
	if verb == "headroom" {
		arg := ""
		if len(parts) > 1 {
			arg = strings.TrimSpace(parts[1])
		}
		r.handleComputeHeadroomCmd(arg)
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
		r.replyComputeNodeList("compute node list")
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
		r.reply("compute node "+tail, withTLSSummary(out))
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
		r.reply("compute node "+subVerb, withTLSSummary(out))
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
	case "drain":
		r.handleComputeNodeDrainComm(tail, true)
	case "undrain":
		r.handleComputeNodeDrainComm(tail, false)
	case "models", "served-models":
		r.replyComputeNodeModels(tail)
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

// handleComputePinCmd reads or sets pin_check_seconds. With no argument it
// replies with the current value; with a whole number it PUTs /api/config.
// handleComputeHeadroomCmd runs `compute headroom <node>` (the stored view,
// GET) and `compute headroom measure <node>` (a fresh idle measurement, POST).
// Both call the node's /headroom REST route, the same service the REST, MCP
// and CLI surfaces use.
func (r *Router) handleComputeHeadroomCmd(arg string) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		r.reply("compute headroom", headroomUsage)
		return
	}
	if strings.EqualFold(fields[0], "measure") {
		if len(fields) != 2 {
			r.reply("compute headroom measure", "Usage: compute headroom measure <node>")
			return
		}
		node := fields[1]
		out, err := r.commJSON(http.MethodPost, "/api/compute/nodes/"+node+"/headroom", "")
		if err != nil {
			r.reply("compute headroom measure "+node, err.Error())
			return
		}
		r.reply("compute headroom measure "+node, prettyJSON(out))
		return
	}
	if len(fields) != 1 {
		r.reply("compute headroom", headroomUsage)
		return
	}
	node := fields[0]
	out, err := r.commGet("/api/compute/nodes/"+node+"/headroom", nil)
	if err != nil {
		r.reply("compute headroom "+node, err.Error())
		return
	}
	r.reply("compute headroom "+node, prettyJSON(out))
}

// headroomUsage is the help text for the headroom verb. A measurement runs
// only on an idle node (a busy node is refused) and only when asked for.
const headroomUsage = `Usage:
  compute headroom <node>            show the stored headroom measurement (read only)
  compute headroom measure <node>    measure the node now while it is idle and store it
A measurement is refused while the node holds a lease (busy) and when its
resident or probe model is not on the model-pull consent list.`

func (r *Router) handleComputePinCmd(arg string) {
	if arg == "" {
		out, err := r.commGet("/api/config", nil)
		if err != nil {
			r.reply("compute pin", err.Error())
			return
		}
		var cfg struct {
			PinCheckSeconds *int `json:"pin_check_seconds"`
		}
		if err := json.Unmarshal([]byte(out), &cfg); err != nil || cfg.PinCheckSeconds == nil {
			r.reply("compute pin", "pin_check_seconds is not reported by this daemon")
			return
		}
		r.reply("compute pin", fmt.Sprintf("pin_check_seconds = %d (0 = pin keeper off)", *cfg.PinCheckSeconds))
		return
	}
	secs, err := strconv.Atoi(arg)
	if err != nil || secs < 0 {
		r.reply("compute pin", "Usage: compute pin <seconds>  (whole number >= 0; 0 = pin keeper off)")
		return
	}
	body, _ := json.Marshal(map[string]any{"pin_check_seconds": secs})
	if _, err := r.commJSON("PUT", "/api/config", string(body)); err != nil {
		r.reply("compute pin", err.Error())
		return
	}
	r.reply("compute pin", fmt.Sprintf("pin_check_seconds = %d. Takes effect after the next daemon restart.", secs))
}

// handleComputeNodeDrainComm implements the comm-channel
// `compute node drain <name> [until=<RFC3339>] [reason=<text>]` and
// `compute node undrain <name>` verbs. Drain sends
// POST /api/compute/nodes/<name>/drain with the until/reason body keys the
// daemon understands; undrain sends DELETE on the same path (idempotent).
// Replies are one line: "drained <name> until <time>", "drained <name> with
// no end time" or "undrained <name>".
func (r *Router) handleComputeNodeDrainComm(tail string, drain bool) {
	verb := "drain"
	if !drain {
		verb = "undrain"
	}
	fields := strings.Fields(tail)
	if len(fields) == 0 || fields[0] == "" {
		r.reply("compute node "+verb, "Usage: compute node "+verb+" <name>"+drainUntilUsage(drain))
		return
	}
	name := fields[0]
	until := ""
	reason := ""
	for _, tok := range fields[1:] {
		switch {
		case strings.HasPrefix(tok, "until="):
			until = strings.TrimSpace(strings.TrimPrefix(tok, "until="))
		case strings.HasPrefix(tok, "reason="):
			reason = strings.TrimPrefix(tok, "reason=")
		default:
			r.reply("compute node "+verb, "Usage: compute node "+verb+" <name>"+drainUntilUsage(drain))
			return
		}
	}
	if drain && until != "" {
		if _, err := time.Parse(time.RFC3339, until); err != nil {
			r.reply("compute node drain", "until must be an RFC3339 timestamp (got "+until+")")
			return
		}
	}
	if !drain && len(fields) > 1 {
		r.reply("compute node undrain", "Usage: compute node undrain <name>")
		return
	}
	method := http.MethodDelete
	body := ""
	if drain {
		method = http.MethodPost
		m := map[string]any{}
		if until != "" {
			m["until"] = until
		}
		if reason != "" {
			m["reason"] = reason
		}
		b, _ := json.Marshal(m)
		body = string(b)
	}
	out, err := r.commJSON(method, "/api/compute/nodes/"+name+"/drain", body)
	if err != nil {
		r.reply("compute node "+verb, err.Error())
		return
	}
	var health struct {
		Drained    bool   `json:"drained"`
		DrainUntil string `json:"drain_until"`
	}
	if err := json.Unmarshal([]byte(out), &health); err != nil {
		r.reply("compute node "+verb, prettyJSON(out))
		return
	}
	if !drain {
		r.reply("compute node undrain "+name, "undrained "+name)
		return
	}
	if health.DrainUntil != "" {
		r.reply("compute node drain "+name, "drained "+name+" until "+health.DrainUntil)
		return
	}
	r.reply("compute node drain "+name, "drained "+name+" with no end time")
}

// drainUntilUsage is the usage hint the drain verbs append when the name is
// missing; undrain takes no extra keys.
func drainUntilUsage(drain bool) string {
	if drain {
		return " [until=<RFC3339>] [reason=<text>]"
	}
	return ""
}

// handleComputePlacementCmd lists or decides a placement ask
// (missing-model policy = "ask"). It proxies the placement REST surface:
//
//	compute placement                → GET  /api/compute/placements
//	compute placement approve <id>   → POST /api/compute/placements/<id>/approve
//	compute placement deny <id>      → POST /api/compute/placements/<id>/deny
//
// Setting the policy itself goes through the generic configure verb:
// `configure compute_placement.missing_model=ask` and friends.
func (r *Router) handleComputePlacementCmd(arg string) {
	bits := strings.Fields(arg)
	if len(bits) == 0 || bits[0] == "list" {
		out, err := r.commGet("/api/compute/placements", nil)
		if err != nil {
			r.reply("compute placement", err.Error())
			return
		}
		r.reply("compute placement", prettyJSON(out))
		return
	}
	action := strings.ToLower(bits[0])
	if action != "approve" && action != "deny" {
		r.reply("compute placement", "Usage: compute placement [list|approve <id>|deny <id>]")
		return
	}
	if len(bits) != 2 || bits[1] == "" {
		r.reply("compute placement "+action, "Usage: compute placement "+action+" <ask-id>")
		return
	}
	id := bits[1]
	out, err := r.commJSON("POST", "/api/compute/placements/"+strings.Trim(id, "/")+"/"+action, "")
	if err != nil {
		r.reply("compute placement "+action+" "+id, err.Error())
		return
	}
	r.reply("compute placement "+action+" "+id, prettyJSON(out))
}

// withTLSSummary pretty-prints a node reply and appends a one-line summary
// of how the node's certificate is checked. Anything that is not a single
// node object (an error body, a list) comes back unchanged.
func withTLSSummary(body string) string {
	pretty := prettyJSON(body)
	var n struct {
		Name                  string `json:"name"`
		TLSCAFile             string `json:"tls_ca_file"`
		TLSCertFingerprint    string `json:"tls_cert_fingerprint"`
		TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify"`
		VerifyTLS             bool   `json:"verify_tls"`
	}
	if err := json.Unmarshal([]byte(body), &n); err != nil || n.Name == "" {
		return pretty
	}
	var summary string
	switch {
	case n.TLSInsecureSkipVerify:
		summary = "verification OFF"
	case n.TLSCAFile != "" && n.TLSCertFingerprint != "":
		summary = "ca file + pinned"
	case n.TLSCAFile != "":
		summary = "ca file"
	case n.TLSCertFingerprint != "":
		summary = "pinned"
	default:
		summary = "verify"
	}
	if n.VerifyTLS {
		summary += ", model pulls verify certificate"
	}
	return pretty + "\ntls: " + summary
}

// parseKVPairs converts "kind=ollama routing=docker-network image=ollama/ollama" into a
// map suitable for the REST POST/PUT body. Nested capacity fields are
// recognized (max_concurrent_models, gpu_mem_gb, gpus, ram_gb,
// gpu_vendor, gpu_model) and emitted under declared_capacity. BL322-S6
// routing sub-fields (image, network, container, port, auto_start,
// auto_pull, peer, remote_llm, timeout) are collected and emitted under
// routing_docker_network / routing_datawatch_proxy. tags split on comma.
// Numeric fields coerced to int when possible.
func parseKVPairs(s string) map[string]any {
	out := map[string]any{}
	cap := map[string]any{}
	dn := map[string]any{} // docker-network sub-config
	dp := map[string]any{} // datawatch-proxy sub-config
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
		case "node_pinned_model":
			out[k] = strings.TrimSpace(v)
		case "allow_model_swap", "tls_insecure_skip_verify", "verify_tls":
			out[k] = v == "true" || v == "1" || v == "yes"
		case "tls_ca_file", "tls_cert_fingerprint":
			out[k] = strings.TrimSpace(v)
		case "scheduling_priority", "session_slots":
			out[k] = atoiOrZero(v)
		case "verifier_slots", "verifier_min_free_mem_mb", "verifier_context_mb":
			// Atoi (not atoiOrZero) so a typed-in negative passes through and is
			// refused by the daemon with its "must be >= 0" wording, instead of
			// silently becoming 0 (= "no verifier slots", which is a different setting).
			if n, err := strconv.Atoi(v); err == nil {
				out[k] = n
			} else {
				out[k] = 0
			}
		case "cost_per_hour":
			out[k] = parseFloatOrZero(v)
		// BL322-S6: docker-network routing sub-fields.
		case "image":
			dn["image"] = v
		case "network":
			dn["network_name"] = v
		case "container":
			dn["container_name"] = v
		case "port":
			dn["port"] = atoiOrZero(v)
		case "docker_endpoint":
			dn["docker_endpoint"] = v
		case "auto_start":
			dn["auto_start"] = v == "true" || v == "1" || v == "yes"
		case "auto_pull":
			dn["auto_pull"] = v == "true" || v == "1" || v == "yes"
		// BL322-S6: datawatch-proxy routing sub-fields.
		case "peer":
			dp["peer"] = v
		case "remote_llm":
			dp["remote_llm_name"] = v
		case "timeout":
			dp["timeout_seconds"] = atoiOrZero(v)
		default:
			out[k] = v
		}
	}
	if len(cap) > 0 {
		out["declared_capacity"] = cap
	}
	if len(dn) > 0 {
		out["routing_docker_network"] = dn
	}
	if len(dp) > 0 {
		out["routing_datawatch_proxy"] = dp
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

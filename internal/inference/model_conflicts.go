// Model-to-node collision detection.
//
// Several config fields name a model that runs on a local ollama node. When
// two different model names land on the same node, a call to one can evict
// the other from that node's memory. This file finds those pairs so the
// daemon can warn about them at startup, on every config write, and in the
// config and capacity API responses.
//
// The config walk lives in internal/config (which imports this package), so
// this package takes plain inputs and never needs the config type.

package inference

import (
	"fmt"
	"slices"
	"strings"
)

// defaultOllamaHost is the address an ollama reference uses when no host is set.
const defaultOllamaHost = "http://localhost:11434"

// ModelReference is one configured model name and where it would run.
type ModelReference struct {
	// Model is the configured name. A leading "ollama/" marks it as an
	// ollama model whatever Backend says.
	Model string
	// Backend is the backend the setting runs on, such as "ollama" or
	// "claude-code". Only "ollama" marks a bare name as an ollama model.
	Backend string
	// Host is the ollama address the model is sent to. Empty means the
	// default ollama host.
	Host string
}

// NodeAddress maps a compute node name to its address, so a warning can
// name the node instead of a raw URL.
type NodeAddress struct {
	Name    string
	Address string
}

// ModelNodeInputs is everything DetectModelNodeConflicts reads.
type ModelNodeInputs struct {
	References []ModelReference
	Nodes      []NodeAddress
}

// ModelNodeConflict is one warning: two or more different model names
// target the same node.
type ModelNodeConflict struct {
	Node    string   `json:"node"`
	Models  []string `json:"models"`
	Message string   `json:"message"`
}

// DetectModelNodeConflicts returns one warning for each node that two or
// more different model names target. Repeats of the same name do not warn,
// and references that do not run on ollama are ignored.
func DetectModelNodeConflicts(in ModelNodeInputs) []ModelNodeConflict {
	type nodeGroup struct {
		name   string
		models []string
	}
	var order []string
	groups := map[string]*nodeGroup{}
	for _, ref := range in.References {
		model, prefixed := strings.CutPrefix(strings.TrimSpace(ref.Model), "ollama/")
		model = strings.TrimSpace(model)
		if model == "" || (!prefixed && ref.Backend != "ollama") {
			continue
		}
		host := normalizeNodeAddress(ref.Host)
		if host == "" {
			host = normalizeNodeAddress(defaultOllamaHost)
		}
		g, ok := groups[host]
		if !ok {
			g = &nodeGroup{name: nodeNameFor(in.Nodes, host)}
			groups[host] = g
			order = append(order, host)
		}
		if !slices.Contains(g.models, model) {
			g.models = append(g.models, model)
		}
	}
	var out []ModelNodeConflict
	for _, host := range order {
		g := groups[host]
		if len(g.models) < 2 {
			continue
		}
		out = append(out, ModelNodeConflict{
			Node:    g.name,
			Models:  g.models,
			Message: collisionMessage(g.models, g.name),
		})
	}
	return out
}

// collisionMessage states the conflict in plain language.
func collisionMessage(models []string, node string) string {
	return fmt.Sprintf("models %s target node %s — the node may not hold them all at once, so a call to one can evict the others",
		joinNamesWithAnd(models), node)
}

// joinNamesWithAnd renders ["a", "b", "c"] as "a, b and c".
func joinNamesWithAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " and " + names[last]
}

// nodeNameFor returns the compute node name whose address matches host,
// or host itself when no compute node has that address.
func nodeNameFor(nodes []NodeAddress, host string) string {
	for _, n := range nodes {
		if n.Name != "" && normalizeNodeAddress(n.Address) == host {
			return n.Name
		}
	}
	return host
}

// normalizeNodeAddress makes two spellings of one address compare equal.
func normalizeNodeAddress(addr string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(addr), "/"))
}

// Per-service model-use reports (GET /api/plugins/{name}/models).
//
// For one service the report answers: which models the service calls, on
// which Ollama node each of them is resident (and the RAM/VRAM that node
// says the model holds), how many calls the service documents per model
// over its window, and two findings with parameterised thresholds:
//
//   - small_model_large_context — a small model (below the small-model
//     memory threshold) is pinned with a context length above the context
//     threshold, from the Ollama reader's reported context;
//   - node_overloaded — the model's node holds materially more resident
//     model memory than another node that could serve the same model
//     (ratio parameter node_load_ratio).
//
// Finding thresholds are function parameters (ModelUseThresholds; zero
// values normalise to the named defaults) and are never read from config.
// When the service's contract does not document per-model call counts the
// report carries only what the nodes report and says so (ServiceUsage.
// Documented is false, and a note records that fact). When a node does
// not answer the report marks it (ModelNode.Error) and findings come
// only from answering nodes. The model_node_unreachable condition itself
// is raised (and cleared) by the events task (ModelNodeEvents /
// RecordModelUseNodes) through the shared ServiceEventRecorder — once
// per transition — not by a request to the model-use endpoint.
package plugins

import (
	"fmt"
	"sort"
	"strings"
)

// Finding identifiers for the two parameterised findings.
const (
	// FindingSmallModelLargeContext — a small model (held memory below
	// ModelUseThresholds.SmallModelMaxBytes) is pinned with a context
	// length above ModelUseThresholds.MinContextTokens.
	FindingSmallModelLargeContext = "small_model_large_context"
	// FindingNodeOverloaded — the model's node holds more resident model
	// memory than ModelUseThresholds.NodeLoadRatio times another node
	// that could serve the same model.
	FindingNodeOverloaded = "node_overloaded"
)

const gib = int64(1) << 30

// Default finding thresholds; zero or negative ModelUseThresholds fields
// normalise to these.
const (
	// DefaultModelUseSmallModelMaxBytes — a model holding at most this
	// much memory (RAM+VRAM) counts as "small".
	DefaultModelUseSmallModelMaxBytes = 2 * gib
	// DefaultModelUseMinContextTokens — the context threshold: a small
	// model pinned with a context above this many tokens is flagged.
	DefaultModelUseMinContextTokens = 8192
	// DefaultModelUseNodeLoadRatio — the node_load_ratio parameter:
	// the model's node holds more than this multiple of another
	// eligible node's resident model memory.
	DefaultModelUseNodeLoadRatio = 2.0
)

// ModelUseThresholds are the finding thresholds as function parameters.
// Zero or negative fields normalise to the named defaults above.
type ModelUseThresholds struct {
	SmallModelMaxBytes int64
	MinContextTokens   int64
	NodeLoadRatio      float64
}

// Normalised returns t with zero or negative thresholds replaced by the
// named defaults.
func (t ModelUseThresholds) Normalised() ModelUseThresholds {
	if t.SmallModelMaxBytes <= 0 {
		t.SmallModelMaxBytes = DefaultModelUseSmallModelMaxBytes
	}
	if t.MinContextTokens <= 0 {
		t.MinContextTokens = DefaultModelUseMinContextTokens
	}
	if t.NodeLoadRatio <= 0 {
		t.NodeLoadRatio = DefaultModelUseNodeLoadRatio
	}
	return t
}

// LoadedModel is one node-resident model and the memory it holds.
type LoadedModel struct {
	Name      string `json:"name"`
	RAMBytes  int64  `json:"ram_bytes"`
	VRAMBytes int64  `json:"vram_bytes"`
	// ContextTokens is the context length (in tokens) the model is
	// loaded with, from the Ollama reader (/api/ps details.num_ctx);
	// zero when the server does not report it.
	ContextTokens int64 `json:"context_tokens"`
}

// HeldBytes is the total memory the model holds on its node (RAM+VRAM).
func (m LoadedModel) HeldBytes() int64 { return m.RAMBytes + m.VRAMBytes }

// ModelNode is what the report knows about one node: which models it
// serves, which are resident with how much memory they hold, and — when
// the node did not answer — the error that says so.
type ModelNode struct {
	Name   string        `json:"name"`
	Served []string      `json:"served,omitempty"`
	Loaded []LoadedModel `json:"loaded,omitempty"`
	Error  string        `json:"error,omitempty"`
}

// ServiceModel is one model the service says it calls and the call count
// the service documents for it over its window.
type ServiceModel struct {
	Name  string `json:"name"`
	Calls int64  `json:"calls"`
}

// ServiceUsage is the service's side of the inputs. Documented is true
// when the service's contract reports the models it calls; when it is
// false (no documented per-model call counts) the report carries only
// what the nodes report and says so.
type ServiceUsage struct {
	Documented bool           `json:"documented"`
	Models     []ServiceModel `json:"models,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// ModelUseInputs are the per-node and per-service observations one
// service's report is built from.
type ModelUseInputs struct {
	Nodes   []ModelNode  `json:"nodes"`
	Service ServiceUsage `json:"service"`
}

// ModelEntry is one row of the report's per-model table: the model, the
// node it is resident on ("" when it is on no answering node), the memory
// it holds, and the calls the service documented for it.
type ModelEntry struct {
	Model     string `json:"model"`
	Node      string `json:"node,omitempty"`
	RAMBytes  int64  `json:"ram_bytes"`
	VRAMBytes int64  `json:"vram_bytes"`
	HeldBytes int64  `json:"held_bytes"`
	// ContextTokens is the context length (in tokens) the model is
	// loaded with, as the node's Ollama reader reports it; zero when the
	// server does not report one.
	ContextTokens   int64 `json:"context_tokens"`
	Calls           int64 `json:"calls"`
	CallsDocumented bool  `json:"calls_documented"`
	UsedByService   bool  `json:"used_by_service"`
}

// Finding is one raised finding: a plain sentence plus the next action.
type Finding struct {
	ID         string `json:"id"`
	Sentence   string `json:"sentence"`
	NextAction string `json:"next_action"`
	Model      string `json:"model,omitempty"`
	Node       string `json:"node,omitempty"`
}

// ModelUseReport is the full per-service report.
type ModelUseReport struct {
	Plugin   string       `json:"plugin"`
	Nodes    []ModelNode  `json:"nodes"`
	Models   []ModelEntry `json:"models"`
	Findings []Finding    `json:"findings"`
	Notes    []string     `json:"notes,omitempty"`
}

// MatchModelName reports whether a service-side model name (possibly
// provider-prefixed, e.g. "ollama:qwen3:1.7b") matches a node-side model
// name (e.g. "qwen3:1.7b").
func MatchModelName(a, b string) bool {
	if a == b {
		return true
	}
	return strings.TrimPrefix(a, "ollama:") == strings.TrimPrefix(b, "ollama:")
}

// CollectModelUse merges one service's usage inputs into the report: the
// per-model table (node, memory held, calls) and the two parameterised
// findings. Pure — no I/O, deterministic ordering.
func CollectModelUse(plugin string, in ModelUseInputs, t ModelUseThresholds) *ModelUseReport {
	t = t.Normalised()
	rep := &ModelUseReport{
		Plugin:   plugin,
		Nodes:    in.Nodes,
		Models:   []ModelEntry{},
		Findings: []Finding{},
		Notes:    []string{},
	}

	// Per-model table: the service's models first (input order), then the
	// node-resident models the service does not name.
	added := map[string]bool{}
	locate := func(model string) (node string, lm LoadedModel, ok bool) {
		byServed := func(n ModelNode) (LoadedModel, bool) {
			for _, s := range n.Served {
				if MatchModelName(model, s) {
					return loadedNamed(n, model)
				}
			}
			return LoadedModel{}, false
		}
		for _, n := range in.Nodes {
			if n.Error != "" {
				continue
			}
			if lm, ok := byServed(n); ok {
				return n.Name, lm, true
			}
		}
		for _, n := range in.Nodes {
			if n.Error != "" {
				continue
			}
			lm, ok := loadedNamed(n, model)
			if ok {
				return n.Name, lm, true
			}
		}
		return "", LoadedModel{}, false
	}
	for _, sm := range in.Service.Models {
		node, lm, ok := locate(sm.Name)
		e := ModelEntry{
			Model:           sm.Name,
			Node:            node,
			RAMBytes:        lm.RAMBytes,
			VRAMBytes:       lm.VRAMBytes,
			HeldBytes:       lm.HeldBytes(),
			ContextTokens:   lm.ContextTokens,
			Calls:           sm.Calls,
			CallsDocumented: in.Service.Documented,
			UsedByService:   true,
		}
		if !ok {
			e.Node = ""
		}
		added[sm.Name] = true
		added[strings.TrimPrefix(sm.Name, "ollama:")] = true
		rep.Models = append(rep.Models, e)
	}
	for _, n := range in.Nodes {
		if n.Error != "" {
			continue
		}
		for _, lm := range n.Loaded {
			if added[lm.Name] || added[strings.TrimPrefix(lm.Name, "ollama:")] {
				continue
			}
			added[lm.Name] = true
			rep.Models = append(rep.Models, ModelEntry{
				Model:         lm.Name,
				Node:          n.Name,
				RAMBytes:      lm.RAMBytes,
				VRAMBytes:     lm.VRAMBytes,
				HeldBytes:     lm.HeldBytes(),
				ContextTokens: lm.ContextTokens,
				UsedByService: false,
			})
		}
	}

	rep.Findings = EvaluateModelUseFindings(in.Nodes, rep.Models, t)

	// Notes: say what the report cannot say.
	if !in.Service.Documented && in.Service.Error == "" {
		rep.Notes = append(rep.Notes,
			"the service's contract does not document per-model call counts; "+
				"the report carries only what the nodes report and per-model calls are 0")
	}
	for _, n := range in.Nodes {
		if n.Error != "" {
			rep.Notes = append(rep.Notes, fmt.Sprintf("node %s did not answer: %s", n.Name, n.Error))
		}
	}
	if in.Service.Error != "" {
		rep.Notes = append(rep.Notes, fmt.Sprintf("the service's usage endpoint did not answer: %s", in.Service.Error))
	}
	return rep
}

// loadedNamed returns the node-resident model matching name, or the zero
// LoadedModel when the node serves it but reports no resident memory for
// it yet.
func loadedNamed(n ModelNode, model string) (LoadedModel, bool) {
	for _, lm := range n.Loaded {
		if MatchModelName(lm.Name, model) {
			return lm, true
		}
	}
	// Node serves the model (Served) but it is not loaded right now.
	for _, s := range n.Served {
		if MatchModelName(s, model) {
			return LoadedModel{Name: model}, true
		}
	}
	return LoadedModel{}, false
}

// EvaluateModelUseFindings raises the two parameterised findings over the
// report's table and node views: for each of the service's models that an
// answering node reports as resident.
func EvaluateModelUseFindings(nodes []ModelNode, models []ModelEntry, t ModelUseThresholds) []Finding {
	t = t.Normalised()
	out := []Finding{}

	// small_model_large_context — a model under the small-model memory
	// threshold, resident on an answering node and used by the service,
	// loaded with a context above the context threshold.
	for _, e := range models {
		if !e.UsedByService || e.HeldBytes <= 0 {
			continue
		}
		if e.HeldBytes > t.SmallModelMaxBytes {
			continue
		}
		if e.ContextTokens <= t.MinContextTokens {
			continue // at/under the threshold; 0 means unreported
		}
		out = append(out, Finding{
			ID:    FindingSmallModelLargeContext,
			Model: e.Model,
			Node:  e.Node,
			Sentence: fmt.Sprintf(
				"%s holds only %s on %s yet is loaded with a context of %d tokens — above the %d-token context threshold for a model this small",
				e.Model, formatBytes(e.HeldBytes), e.Node, e.ContextTokens, t.MinContextTokens),
			NextAction: fmt.Sprintf(
				"give %s a context the small model can hold, or route this workload to a larger model on another node",
				e.Model),
		})
	}

	// node_overloaded — the model's node holds more resident model
	// memory than NodeLoadRatio times another node that could also serve
	// the model. Only answering nodes can hold a resident model, and
	// only answering nodes that serve it are eligible candidates.
	totalLoaded := func(n ModelNode) int64 {
		var total int64
		for _, lm := range n.Loaded {
			total += lm.HeldBytes()
		}
		return total
	}
	nodeByName := map[string]ModelNode{}
	for _, n := range nodes {
		nodeByName[n.Name] = n
	}
	for _, e := range models {
		if !e.UsedByService || e.Node == "" {
			continue
		}
		home, ok := nodeByName[e.Node]
		if !ok || home.Error != "" {
			continue
		}
		homeTotal := totalLoaded(home)

		best := ""
		var bestTotal int64 = -1
		for _, n := range nodes {
			if n.Error != "" || n.Name == e.Node {
				continue
			}
			if !nodeServes(e.Model, n.Served) {
				continue // cannot serve the model: not an eligible node
			}
			candTotal := totalLoaded(n)
			if float64(homeTotal) > float64(candTotal)*t.NodeLoadRatio {
				if bestTotal < 0 || candTotal < bestTotal {
					best, bestTotal = n.Name, candTotal // strongest witness
				}
			}
		}
		if best == "" {
			continue
		}
		out = append(out, Finding{
			ID:    FindingNodeOverloaded,
			Model: e.Model,
			Node:  e.Node,
			Sentence: fmt.Sprintf(
				"%s holds %s of resident models — more than %sx the %s on %s, which also serves %s",
				e.Node, formatBytes(homeTotal), formatRatio(t.NodeLoadRatio),
				formatBytes(bestTotal), best, e.Model),
			NextAction: fmt.Sprintf(
				"move %s from %s to %s to relieve the busier node",
				e.Model, e.Node, best),
		})
	}

	// Deterministic order: by id, then by model/node.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].Node < out[j].Node
	})
	return out
}

// nodeServes reports whether the node's served list includes model.
func nodeServes(model string, served []string) bool {
	for _, s := range served {
		if MatchModelName(s, model) {
			return true
		}
	}
	return false
}

// formatRatio renders a ratio compactly ("2", "2.5").
func formatRatio(r float64) string {
	if r == float64(int64(r)) {
		return fmt.Sprintf("%d", int64(r))
	}
	s := fmt.Sprintf("%.1f", r)
	return strings.TrimSuffix(s, "0")
}

// RecordModelUseNodes feeds the report's per-node reachability to the
// shared recorder: every node that did not answer raises
// model_node_unreachable (once per transition) and every node that did
// answer clears the condition. nil-safe — a nil recorder or report is a
// no-op.
func RecordModelUseNodes(rec *ServiceEventRecorder, rep *ModelUseReport) {
	if rec == nil || rep == nil {
		return
	}
	for _, n := range rep.Nodes {
		rec.RecordModelNode(rep.Plugin, n.Name, n.Error == "")
	}
}

// formatBytes renders byte counts as a plain "12.5 GB" style string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	v := float64(b)
	div := float64(unit)
	suffix := "kB"
	for _, s := range []string{"MB", "GB", "TB", "PB"} {
		div *= unit
		suffix = s
		if v < div {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", v/div, suffix)
}

// Per-service model-use reports (GET /api/plugins/{name}/models).
//
// For one service the report answers: which models the service calls, on
// which Ollama node each of them is resident (and the RAM/VRAM that node
// says the model holds), how many calls the service documents per model
// over its window, and two findings with parameterised thresholds:
//
//   - small_model_large_context — a model that is small relative to its
//     node's memory carries a heavy documented call load;
//   - node_overloaded — the node holds more resident model memory than
//     the limit.
//
// Finding thresholds are function parameters (ModelUseThresholds; zero
// values normalise to the named defaults) and are never read from config.
// When the service's contract does not document per-model call counts the
// report carries only what the nodes report and says so (ServiceUsage.
// Documented is false, and a note records that fact). When a node does
// not answer the report marks it (ModelNode.Error) and
// RecordModelUseNodes raises model_node_unreachable through the shared
// ServiceEventRecorder — once per transition, with a clear when the node
// answers again.
package plugins

import (
	"fmt"
	"sort"
	"strings"
)

// Finding identifiers for the two parameterised findings.
const (
	// FindingSmallModelLargeContext — a small model (memory below
	// ModelUseThresholds.SmallModelMaxBytes) carries at least
	// ModelUseThresholds.MinCalls documented calls.
	FindingSmallModelLargeContext = "small_model_large_context"
	// FindingNodeOverloaded — the node's total resident model memory is
	// at least ModelUseThresholds.NodeMemMaxBytes.
	FindingNodeOverloaded = "node_overloaded"
)

const gib = int64(1) << 30

// Default finding thresholds; zero or negative ModelUseThresholds fields
// normalise to these.
const (
	// DefaultModelUseSmallModelMaxBytes — a model holding at most this
	// much memory (RAM+VRAM) counts as "small".
	DefaultModelUseSmallModelMaxBytes = 2 * gib
	// DefaultModelUseMinCalls — the documented call load on a small
	// model that counts as a "large context".
	DefaultModelUseMinCalls = 1000
	// DefaultModelUseNodeMemMaxBytes — the node's total resident model
	// memory that counts as "overloaded".
	DefaultModelUseNodeMemMaxBytes = 24 * gib
)

// ModelUseThresholds are the finding thresholds as function parameters.
// Zero or negative fields normalise to the named defaults above.
type ModelUseThresholds struct {
	SmallModelMaxBytes int64
	MinCalls           int64
	NodeMemMaxBytes    int64
}

// Normalised returns t with zero or negative thresholds replaced by the
// named defaults.
func (t ModelUseThresholds) Normalised() ModelUseThresholds {
	if t.SmallModelMaxBytes <= 0 {
		t.SmallModelMaxBytes = DefaultModelUseSmallModelMaxBytes
	}
	if t.MinCalls <= 0 {
		t.MinCalls = DefaultModelUseMinCalls
	}
	if t.NodeMemMaxBytes <= 0 {
		t.NodeMemMaxBytes = DefaultModelUseNodeMemMaxBytes
	}
	return t
}

// LoadedModel is one node-resident model and the memory it holds.
type LoadedModel struct {
	Name      string `json:"name"`
	RAMBytes  int64  `json:"ram_bytes"`
	VRAMBytes int64  `json:"vram_bytes"`
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
	Model           string `json:"model"`
	Node            string `json:"node,omitempty"`
	RAMBytes        int64  `json:"ram_bytes"`
	VRAMBytes       int64  `json:"vram_bytes"`
	HeldBytes       int64  `json:"held_bytes"`
	Calls           int64  `json:"calls"`
	CallsDocumented bool   `json:"calls_documented"`
	UsedByService   bool   `json:"used_by_service"`
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
	Plugin   string    `json:"plugin"`
	Nodes    []ModelNode `json:"nodes"`
	Models   []ModelEntry `json:"models"`
	Findings []Finding   `json:"findings"`
	Notes    []string    `json:"notes,omitempty"`
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
				Model:       lm.Name,
				Node:        n.Name,
				RAMBytes:    lm.RAMBytes,
				VRAMBytes:   lm.VRAMBytes,
				HeldBytes:   lm.HeldBytes(),
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
// report's table. Exposed separately so callers and tests can vary the
// thresholds without re-building inputs.
func EvaluateModelUseFindings(nodes []ModelNode, models []ModelEntry, t ModelUseThresholds) []Finding {
	t = t.Normalised()
	out := []Finding{}

	for _, e := range models {
		if !e.CallsDocumented || e.HeldBytes <= 0 {
			continue
		}
		if e.HeldBytes > t.SmallModelMaxBytes {
			continue
		}
		if e.Calls < t.MinCalls {
			continue
		}
		out = append(out, Finding{
			ID:     FindingSmallModelLargeContext,
			Model:  e.Model,
			Node:   e.Node,
			Sentence: fmt.Sprintf(
				"%s holds only %s on %s yet the service documented %d calls over the window",
				e.Model, formatBytes(e.HeldBytes), e.Node, e.Calls),
			NextAction: fmt.Sprintf(
				"cap the context length routed to %s or move it to a dedicated node so co-located models keep their memory",
				e.Model),
		})
	}

	for _, n := range nodes {
		if n.Error != "" || len(n.Loaded) == 0 {
			continue
		}
		var total int64
		largestName := ""
		var largest int64 = -1
		for _, lm := range n.Loaded {
			h := lm.HeldBytes()
			total += h
			if h > largest {
				largest, largestName = h, lm.Name
			}
		}
		if total < t.NodeMemMaxBytes {
			continue
		}
		out = append(out, Finding{
			ID:     FindingNodeOverloaded,
			Node:   n.Name,
			Sentence: fmt.Sprintf(
				"%s holds %s across %d resident models, more than the %s limit",
				n.Name, formatBytes(total), len(n.Loaded), formatBytes(t.NodeMemMaxBytes)),
			NextAction: fmt.Sprintf(
				"unload or relocate %s — the largest resident model on %s — to another node",
				largestName, n.Name),
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

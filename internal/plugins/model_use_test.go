package plugins

// Model-use collector: pure Go over the inputs (node views + service
// view), thresholds as parameters (small-model memory, context tokens,
// node_load_ratio), findings with a sentence + next action, and the
// model_node_unreachable raise-once recorder feed.

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
)

// smallModelInputs is the standard shape: one node carrying a small
// model (held 1 GiB, under the 2 GiB small-model default) with the given
// loaded context length, and a service that documents it.
func smallModelInputs(ctx int64) ModelUseInputs {
	return ModelUseInputs{
		Nodes: []ModelNode{{
			Name: "node-a",
			Loaded: []LoadedModel{
				{Name: "small-1.7b", RAMBytes: 512 << 20, VRAMBytes: 512 << 20, ContextTokens: ctx},
			},
		}},
		Service: ServiceUsage{
			Documented: true,
			Models:     []ServiceModel{{Name: "small-1.7b", Calls: 42}},
		},
	}
}

// TestModelUseMatch_Name — service-side "ollama:qwen3:1.7b" and
// node-side "qwen3:1.7b" are the same model.
func TestModelUseMatch_Name(t *testing.T) {
	if !MatchModelName("ollama:qwen3:1.7b", "qwen3:1.7b") || !MatchModelName("qwen3:1.7b", "ollama:qwen3:1.7b") {
		t.Fatal("ollama: prefix must not prevent a match")
	}
	if MatchModelName("qwen3:1.7b", "qwen3:8b") {
		t.Fatal("different models must not match")
	}
	if MatchModelName("", "qwen3:1.7b") || MatchModelName("qwen3:1.7b", "") {
		t.Fatal("empty names must not match")
	}
}

// TestModelUse_SmallModelLargeContext_Raised — a small model loaded with
// a context above the context threshold is flagged; the finding names
// the model + node and carries a next action.
func TestModelUse_SmallModelLargeContext_Raised(t *testing.T) {
	rep := CollectModelUse("imap-mcp", smallModelInputs(16384), ModelUseThresholds{})
	f := find(rep.Findings, FindingSmallModelLargeContext)
	if f == nil {
		t.Fatalf("small_model_large_context not raised; findings=%+v", rep.Findings)
	}
	if f.Model != "small-1.7b" || f.Node != "node-a" {
		t.Errorf("finding must name model+node, got %+v", f)
	}
	if !strings.Contains(f.Sentence, "16384") || !strings.Contains(f.Sentence, "8192") {
		t.Errorf("sentence must carry the context length and the threshold: %q", f.Sentence)
	}
	if f.Sentence == "" || f.NextAction == "" {
		t.Errorf("finding missing sentence or next action: %+v", f)
	}
}

// TestModelUse_SmallModelLargeContext_NotRaised — a small model loaded
// with a context at/under the threshold is not flagged, an unreported
// context (0) is not flagged, and a model over the small-model memory
// threshold is not flagged no matter its context.
func TestModelUse_SmallModelLargeContext_NotRaised(t *testing.T) {
	// At the default 8192-token threshold.
	if f := find(CollectModelUse("imap-mcp", smallModelInputs(8192), ModelUseThresholds{}).Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("finding raised at the context threshold: %+v", f)
	}
	// Context 0 — the server did not report one.
	if f := find(CollectModelUse("imap-mcp", smallModelInputs(0), ModelUseThresholds{}).Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("finding raised with an unreported context: %+v", f)
	}
	// Over the small-model memory threshold with an oversized context.
	in := smallModelInputs(64 << 10)
	for i := range in.Nodes {
		for j := range in.Nodes[i].Loaded {
			in.Nodes[i].Loaded[j].RAMBytes = 4 << 30
			in.Nodes[i].Loaded[j].VRAMBytes = 16 << 30
		}
	}
	if f := find(CollectModelUse("imap-mcp", in, ModelUseThresholds{}).Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("finding raised for a model over the small-model threshold: %+v", f)
	}
}

// TestModelUse_NodeOverloaded_Raised — the model's node holds materially
// more resident model memory than another node that could serve the same
// model; the finding names the busier node and the eligible witness.
func TestModelUse_NodeOverloaded_Raised(t *testing.T) {
	in := ModelUseInputs{
		Nodes: []ModelNode{
			{
				Name:   "node-a",
				Served: []string{"small-1.7b"},
				Loaded: []LoadedModel{
					{Name: "big-70b", RAMBytes: 20 << 30, VRAMBytes: 10 << 30},
					{Name: "small-1.7b", RAMBytes: 1 << 30, VRAMBytes: 1 << 30},
				},
			},
			{
				Name:   "node-b",
				Served: []string{"small-1.7b"},
				Loaded: []LoadedModel{{Name: "other", VRAMBytes: 100 << 20}},
			},
		},
		Service: ServiceUsage{Documented: true, Models: []ServiceModel{{Name: "small-1.7b", Calls: 5}}},
	}
	rep := CollectModelUse("imap-mcp", in, ModelUseThresholds{})
	f := find(rep.Findings, FindingNodeOverloaded)
	if f == nil {
		t.Fatalf("node_overloaded not raised; findings=%+v", rep.Findings)
	}
	if f.Node != "node-a" || f.Model != "small-1.7b" {
		t.Errorf("finding must name the busier node and the model, got %+v", f)
	}
	if !strings.Contains(f.Sentence, "node-b") {
		t.Errorf("sentence must name the eligible witness node: %q", f.Sentence)
	}
	if !strings.Contains(f.NextAction, "node-b") {
		t.Errorf("next action must name the node to move the model to: %q", f.NextAction)
	}
}

// TestModelUse_NodeOverloaded_NotRaised — a node that holds at most
// node_load_ratio times its eligible witness is not flagged, and a node
// that merely holds less memory but cannot serve the model is not an
// eligible witness.
func TestModelUse_NodeOverloaded_NotRaised(t *testing.T) {
	// home (512 MiB) holds no more than 2x the witness (256 MiB).
	in := ModelUseInputs{
		Nodes: []ModelNode{
			{
				Name:   "node-a",
				Served: []string{"small-1.7b"},
				Loaded: []LoadedModel{
					{Name: "small-1.7b", VRAMBytes: 512 << 20},
				},
			},
			{
				Name:   "node-b",
				Served: []string{"small-1.7b"},
				Loaded: []LoadedModel{{Name: "other", VRAMBytes: 256 << 20}},
			},
		},
		Service: ServiceUsage{Documented: true, Models: []ServiceModel{{Name: "small-1.7b", Calls: 3}}},
	}
	if f := find(CollectModelUse("imap-mcp", in, ModelUseThresholds{}).Findings, FindingNodeOverloaded); f != nil {
		t.Fatalf("node_overloaded raised inside the ratio: %+v", f)
	}
	// node-b holds less but does not serve the model: not a witness.
	in.Nodes[1].Served = nil
	if f := find(CollectModelUse("imap-mcp", in, ModelUseThresholds{}).Findings, FindingNodeOverloaded); f != nil {
		t.Fatalf("node_overloaded raised against a node that cannot serve the model: %+v", f)
	}
}

// TestModelUse_Thresholds_AreParameters — changing the thresholds as
// function arguments flips the same inputs from raised to not raised;
// this is the contract the task asks for (parameterised, not config).
func TestModelUse_Thresholds_AreParameters(t *testing.T) {
	// small_model_large_context: at the default 8192-token threshold the
	// 16k-context model raises; pushing the threshold to 32k stops it.
	in := smallModelInputs(16384)
	if f := find(CollectModelUse("imap-mcp", in, ModelUseThresholds{}).Findings, FindingSmallModelLargeContext); f == nil {
		t.Fatalf("finding not raised at the defaults")
	}
	high := ModelUseThresholds{MinContextTokens: 32768}
	if f := find(CollectModelUse("imap-mcp", in, high).Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("raising the context threshold must stop the raise: %+v", f)
	}
	low := ModelUseThresholds{MinContextTokens: 4096}
	if f := find(CollectModelUse("imap-mcp", smallModelInputs(8192), low).Findings, FindingSmallModelLargeContext); f == nil {
		t.Fatal("lowering the context threshold must raise the finding")
	}

	// node_overloaded: home 300 MiB vs a 150 MiB witness — inside the
	// 2x default (not raised); at a 1.5x parameter it raises.
	ovl := ModelUseInputs{
		Nodes: []ModelNode{
			{Name: "node-a", Served: []string{"common-7b"}, Loaded: []LoadedModel{{Name: "common-7b", VRAMBytes: 300 << 20}}},
			{Name: "node-b", Served: []string{"common-7b"}, Loaded: []LoadedModel{{Name: "tiny", VRAMBytes: 150 << 20}}},
		},
		Service: ServiceUsage{Documented: true, Models: []ServiceModel{{Name: "common-7b", Calls: 1}}},
	}
	if f := find(CollectModelUse("imap-mcp", ovl, ModelUseThresholds{}).Findings, FindingNodeOverloaded); f != nil {
		t.Fatalf("node_overloaded raised inside the 2x default: %+v", f)
	}
	tight := ModelUseThresholds{NodeLoadRatio: 1.5}
	if f := find(CollectModelUse("imap-mcp", ovl, tight).Findings, FindingNodeOverloaded); f == nil {
		t.Fatal("tightening node_load_ratio must raise the finding")
	}
}

// TestModelUse_NormalisedDefaults — zero thresholds normalise to the
// named defaults.
func TestModelUse_NormalisedDefaults(t *testing.T) {
	got := (ModelUseThresholds{}).Normalised()
	if got.SmallModelMaxBytes != DefaultModelUseSmallModelMaxBytes {
		t.Errorf("SmallModelMaxBytes = %d, want %d", got.SmallModelMaxBytes, DefaultModelUseSmallModelMaxBytes)
	}
	if got.MinContextTokens != DefaultModelUseMinContextTokens {
		t.Errorf("MinContextTokens = %d, want %d", got.MinContextTokens, DefaultModelUseMinContextTokens)
	}
	if got.NodeLoadRatio != DefaultModelUseNodeLoadRatio {
		t.Errorf("NodeLoadRatio = %v, want %v", got.NodeLoadRatio, DefaultModelUseNodeLoadRatio)
	}
}

// TestModelUse_Table_CarriesContext — the per-model table carries the
// context length the node reports, for service-modeled and co-resident
// models alike.
func TestModelUse_Table_CarriesContext(t *testing.T) {
	in := smallModelInputs(16384)
	in.Nodes[0].Loaded = append(in.Nodes[0].Loaded, LoadedModel{Name: "co-7b", VRAMBytes: 8 << 30, ContextTokens: 4096})
	in.Service.Models = append(in.Service.Models, ServiceModel{})
	rep := CollectModelUse("imap-mcp", in, ModelUseThresholds{})
	got := map[string]ModelEntry{}
	for _, e := range rep.Models {
		got[e.Model] = e
	}
	if got["small-1.7b"].ContextTokens != 16384 {
		t.Errorf("small-1.7b row must carry the context length: %+v", got["small-1.7b"])
	}
	if got["co-7b"].ContextTokens != 4096 {
		t.Errorf("co-7b row must carry the context length: %+v", got["co-7b"])
	}
}

// TestModelUse_NodeDoesNotAnswer — a node that errors: the report keeps
// going, marks the node, carries what the service documents (node
// unknown), and raises no finding off the dead node.
func TestModelUse_NodeDoesNotAnswer(t *testing.T) {
	in := ModelUseInputs{
		Nodes: []ModelNode{
			{Name: "node-a", Error: "connection refused"},
			{Name: "node-b", Loaded: []LoadedModel{{Name: "big", VRAMBytes: 16 << 30}}},
		},
		Service: ServiceUsage{
			Documented: true,
			Models:     []ServiceModel{{Name: "small-1.7b", Calls: 5000}},
		},
	}
	rep := CollectModelUse("imap-mcp", in, ModelUseThresholds{})
	if len(rep.Notes) == 0 || !strings.Contains(strings.Join(rep.Notes, " "), "node-a") {
		t.Errorf("report must note which node did not answer; notes=%v", rep.Notes)
	}
	// The service's model has no healthy node to attribute to.
	var entry *ModelEntry
	for i := range rep.Models {
		if rep.Models[i].Model == "small-1.7b" {
			entry = &rep.Models[i]
		}
	}
	if entry == nil {
		t.Fatal("service model missing from the report table")
	}
	if entry.Node != "" || entry.HeldBytes != 0 {
		t.Errorf("unattributable entry must carry no node/memory: %+v", entry)
	}
	for _, id := range []string{FindingNodeOverloaded, FindingSmallModelLargeContext} {
		if f := find(rep.Findings, id); f != nil && f.Node == "node-a" {
			t.Errorf("%s must not be raised off a node that did not answer: %+v", id, f)
		}
	}
}

// TestModelUse_RecordModelUseNodes_RaiseOnce — the recorder feed raises
// model_node_unreachable once per transition, clears on recovery, and
// ignores healthy nodes.
func TestModelUse_RecordModelUseNodes_RaiseOnce(t *testing.T) {
	sinks := &eventSinks{}
	rec := newTestRecorder(sinks, ServiceEventOptions{})

	report := func(nodeErr map[string]string) *ModelUseReport {
		rep := &ModelUseReport{Plugin: "imap-mcp"}
		for _, n := range []string{"node-a"} {
			mn := ModelNode{Name: n}
			if e := nodeErr[n]; e != "" {
				mn.Error = e
			} else {
				mn.Loaded = []LoadedModel{{Name: "m"}}
			}
			rep.Nodes = append(rep.Nodes, mn)
		}
		return rep
	}

	RecordModelUseNodes(rec, report(map[string]string{"node-a": "refused"}))
	RecordModelUseNodes(rec, report(map[string]string{"node-a": "refused"})) // same state: no re-raise
	if got := sinks.countAlerts(); got != 1 {
		t.Fatalf("alerts after two failing reports = %d, want 1: %+v", got, sinks.alerts)
	}
	last := sinks.lastAlert()
	if !containsIgnoreCase(last.title, "imap-mcp") {
		t.Errorf("alert must name the plugin: %+v", last)
	}
	if last.level != alerts.LevelWarn {
		t.Errorf("raise must alert at warn level, got %q: %+v", last.level, last)
	}
	if !containsIgnoreCase(last.body, "node-a") || !containsIgnoreCase(last.body, "did not answer") {
		t.Errorf("alert body must name the node and be actionable: %q", last.body)
	}
	if sinks.countAudits() != 1 {
		t.Errorf("audit entries = %d, want 1", sinks.countAudits())
	}

	// Recovery clears the condition: one info alert, no re-raise.
	RecordModelUseNodes(rec, report(nil))
	if got := sinks.countAlerts(); got != 2 {
		t.Fatalf("alerts after recovery = %d, want 2 (warn + info clear): %+v", got, sinks.alerts)
	}
	if last := sinks.lastAlert(); last.level != alerts.LevelInfo || !containsIgnoreCase(last.title, "imap-mcp") {
		t.Errorf("clear must be an info alert naming the plugin: %+v", last)
	}
	if sinks.countAudits() != 2 {
		t.Errorf("audit entries after clear = %d, want 2 (raise + clear)", sinks.countAudits())
	}

	// Nil-safe.
	RecordModelUseNodes(nil, report(nil))
	RecordModelUseNodes(rec, nil)
}

// find reports the first finding with the given id.
func find(fs []Finding, id string) *Finding {
	for i := range fs {
		if fs[i].ID == id {
			return &fs[i]
		}
	}
	return nil
}

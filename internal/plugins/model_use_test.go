package plugins

// Model-use collector: pure Go over the inputs (node views + service
// view), thresholds as parameters, findings with a sentence + next
// action, and the model_node_unreachable raise-once recorder feed.

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
)

func inputsOneModel(calls int64, documented bool) ModelUseInputs {
	return ModelUseInputs{
		Nodes: []ModelNode{{
			Name: "node-a",
			Loaded: []LoadedModel{
				{Name: "small-1.7b", RAMBytes: 512 << 20, VRAMBytes: 512 << 20},
			},
		}},
		Service: ServiceUsage{
			Documented: documented,
			Models:     []ServiceModel{{Name: "small-1.7b", Calls: calls}},
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

// TestModelUse_SmallModelLargeContext_Raised — a small model the service
// really leans on is flagged; the finding names the model + node and
// carries a next action.
func TestModelUse_SmallModelLargeContext_Raised(t *testing.T) {
	rep := CollectModelUse("imap-mcp", inputsOneModel(1234, true), ModelUseThresholds{})
	f := find(rep.Findings, FindingSmallModelLargeContext)
	if f == nil {
		t.Fatalf("small_model_large_context not raised; findings=%+v", rep.Findings)
	}
	if f.Model != "small-1.7b" || f.Node != "node-a" {
		t.Errorf("finding must name model+node, got %+v", f)
	}
	if f.Sentence == "" || f.NextAction == "" {
		t.Errorf("finding missing sentence or next action: %+v", f)
	}
}

// TestModelUse_SmallModelLargeContext_NotRaised — the same shape below
// the parameterized call floor is not flagged.
func TestModelUse_SmallModelLargeContext_NotRaised(t *testing.T) {
	// Below the default floor (1000 calls).
	rep := CollectModelUse("imap-mcp", inputsOneModel(999, true), ModelUseThresholds{})
	if f := find(rep.Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("finding raised below the call floor: %+v", f)
	}
	// Undocumented counts never raise the call-count finding.
	rep = CollectModelUse("imap-mcp", inputsOneModel(0, false), ModelUseThresholds{})
	if f := find(rep.Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("finding raised without documented counts: %+v", f)
	}
	if len(rep.Notes) == 0 || !strings.Contains(strings.Join(rep.Notes, " "), "does not document per-model call counts") {
		t.Errorf("report must say counts are undocumented; notes=%v", rep.Notes)
	}
}

// TestModelUse_NodeOverloaded_Raised — a node holding well past the
// parameterized memory is flagged; the next action names the largest
// resident model.
func TestModelUse_NodeOverloaded_Raised(t *testing.T) {
	in := ModelUseInputs{
		Nodes: []ModelNode{{
			Name: "node-a",
			Loaded: []LoadedModel{
				{Name: "big-70b", RAMBytes: 20 << 30, VRAMBytes: 10 << 30},
				{Name: "small-1.7b", RAMBytes: 1 << 30, VRAMBytes: 1 << 30},
			},
		}},
		Service: ServiceUsage{Documented: true, Models: []ServiceModel{{Name: "small-1.7b", Calls: 5}}},
	}
	rep := CollectModelUse("imap-mcp", in, ModelUseThresholds{})
	f := find(rep.Findings, FindingNodeOverloaded)
	if f == nil {
		t.Fatalf("node_overloaded not raised; findings=%+v", rep.Findings)
	}
	if f.Node != "node-a" {
		t.Errorf("finding must name the node, got %+v", f)
	}
	if !strings.Contains(f.NextAction, "big-70b") {
		t.Errorf("next action must name the largest resident model: %q", f.NextAction)
	}
}

// TestModelUse_NodeOverloaded_NotRaised — a lightly used node is not
// flagged even if it carries real memory.
func TestModelUse_NodeOverloaded_NotRaised(t *testing.T) {
	in := ModelUseInputs{
		Nodes: []ModelNode{{
			Name: "node-a",
			Loaded: []LoadedModel{{Name: "mid-7b", RAMBytes: 1 << 30, VRAMBytes: 1 << 30}},
		}},
		Service: ServiceUsage{Documented: true, Models: []ServiceModel{{Name: "mid-7b", Calls: 3}}},
	}
	rep := CollectModelUse("imap-mcp", in, ModelUseThresholds{})
	if f := find(rep.Findings, FindingNodeOverloaded); f != nil {
		t.Fatalf("node_overloaded raised on a light node: %+v", f)
	}
}

// TestModelUse_Thresholds_AreParameters — lowering the parameters as a
// function argument flips the same inputs from not-raised to raised;
// this is the contract the task asks for (parameterized, not config).
func TestModelUse_Thresholds_AreParameters(t *testing.T) {
	in := inputsOneModel(50000, true)
	// Default floor is 1000 — already raised. Raise the floor above the
	// calls and the same inputs stop raising.
	high := ModelUseThresholds{
		SmallModelMaxBytes: 2 << 30,
		MinCalls:           51000,
		NodeMemMaxBytes:    24 << 30,
	}
	rep := CollectModelUse("imap-mcp", in, high)
	if f := find(rep.Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("raising the floor must stop the raise: %+v", f)
	}
	// And a smaller floor than default re-raises it on a light input.
	low := ModelUseThresholds{
		SmallModelMaxBytes: 2 << 30,
		MinCalls:           10,
		NodeMemMaxBytes:    24 << 30,
	}
	in2 := inputsOneModel(11, true)
	if f := find(CollectModelUse("imap-mcp", in2, low).Findings, FindingSmallModelLargeContext); f == nil {
		t.Fatal("lowering the floor must raise the finding")
	}
	if f := find(CollectModelUse("imap-mcp", in2, high).Findings, FindingSmallModelLargeContext); f != nil {
		t.Fatalf("same inputs must not raise at the high floor: %+v", f)
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
	if f := find(rep.Findings, FindingNodeOverloaded); f != nil && f.Node == "node-a" {
		t.Errorf("finding must not be raised off a node that did not answer: %+v", f)
	}
	if f := find(rep.Findings, FindingSmallModelLargeContext); f != nil && f.Node == "node-a" {
		t.Errorf("finding must not be raised off a node that did not answer: %+v", f)
	}
}

// TestModelUse_Table_Merge — the report table is one row per model:
// service calls joined to node memory, co-resident models included.
func TestModelUse_Table_Merge(t *testing.T) {
	in := ModelUseInputs{
		Nodes: []ModelNode{{
			Name: "node-a",
			Loaded: []LoadedModel{
				{Name: "small-1.7b", RAMBytes: 256 << 20, VRAMBytes: 256 << 20},
				{Name: "big-70b", VRAMBytes: 40 << 30},
			},
		}},
		Service: ServiceUsage{
			Documented: true,
			Models:     []ServiceModel{{Name: "small-1.7b", Calls: 42}},
		},
	}
	rep := CollectModelUse("imap-mcp", in, ModelUseThresholds{})
	got := map[string]ModelEntry{}
	for _, e := range rep.Models {
		got[e.Model] = e
	}
	if len(got) != 2 {
		t.Fatalf("table must list both models (service used + co-resident); got %+v", rep.Models)
	}
	small := got["small-1.7b"]
	if !small.UsedByService || small.Node != "node-a" || small.HeldBytes != 512<<20 || small.Calls != 42 || !small.CallsDocumented {
		t.Errorf("small-1.7b row wrong: %+v", small)
	}
	big := got["big-70b"]
	if big.UsedByService || big.Node != "node-a" || big.HeldBytes != 40<<30 || big.Calls != 0 || big.CallsDocumented {
		t.Errorf("big-70b row wrong: %+v", big)
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

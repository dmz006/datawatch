// Tests for the missing-model placement planner (internal/compute/placement.go).
package compute

import (
	"strings"
	"testing"
	"time"
)

const testGIB = uint64(1024) * 1024 * 1024

var placeTestNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func placeTestNode(name string) *Node {
	return &Node{
		Name:    name,
		Kind:    KindOllama,
		Address: "http://" + name + ":11434",
	}
}

func maintenanceNode(name string) *Node {
	n := placeTestNode(name)
	n.MaintenanceWindows = []MaintenanceWindow{{
		From:   placeTestNow.Add(-time.Hour),
		To:     placeTestNow.Add(time.Hour),
		Reason: "planned reboot",
	}}
	return n
}

// placementReq builds a request placing the named model (14 GiB) with the
// compute_placement defaults (10% memory headroom, 5 GiB disk headroom).
func placementReq(model string, cands ...PlacementCandidate) PlacementRequest {
	return PlacementRequest{
		Model:          model,
		Consumer:       "ask",
		Now:            placeTestNow,
		ModelSizeGB:    14,
		MemHeadroomPct: 10,
		DiskHeadroomGB: 5,
		Candidates:     cands,
	}
}

func TestPlaceServingNodeWins(t *testing.T) {
	big := placeTestNode("big")
	result := PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{
			Node:          placeTestNode("serving"),
			MemTotalBytes: 16 * testGIB,
			MemFreeBytes:  16 * testGIB, // enough for itself already
			DiskFreeBytes: 100 * testGIB,
			ServesModel:   true,
		},
		PlacementCandidate{
			Node:          big,
			MemTotalBytes: 512 * testGIB,
			MemFreeBytes:  400 * testGIB,
			DiskFreeBytes: 2 * 1024 * testGIB,
		},
	))
	if result.Node != "serving" {
		t.Fatalf("serving node must win over a bigger free-memory node: want serving, got %q (refusal %v)", result.Node, result.Refusal)
	}
}

// A serving node wins even when it is the least free of the candidates.
func TestPlaceServingNodeWinsOverFittingOnes(t *testing.T) {
	result := PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{
			Node:          placeTestNode("roomy"),
			MemTotalBytes: 512 * testGIB,
			MemFreeBytes:  480 * testGIB,
			DiskFreeBytes: 2 * 1024 * testGIB,
		},
		PlacementCandidate{
			Node:          placeTestNode("serving"),
			MemTotalBytes: 32 * testGIB,
			MemFreeBytes:  20 * testGIB,
			DiskFreeBytes: 40 * testGIB,
			ServesModel:   true,
		},
	))
	if result.Node != "serving" {
		t.Fatalf("serving node should win even when least free: got %q (refusal %v)", result.Node, result.Refusal)
	}
}

func TestPlaceMostFreeNodeChosen(t *testing.T) {
	result := PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{
			Node:          placeTestNode("small"),
			MemTotalBytes: 64 * testGIB,
			MemFreeBytes:  48 * testGIB,
			DiskFreeBytes: 60 * testGIB,
		},
		PlacementCandidate{
			Node:          placeTestNode("large"),
			MemTotalBytes: 128 * testGIB,
			MemFreeBytes:  120 * testGIB,
			DiskFreeBytes: 200 * testGIB,
		},
	))
	if result.Node != "large" {
		t.Fatalf("most-free node should be chosen: want large, got %q (refusal %v)", result.Node, result.Refusal)
	}
}

func TestPlaceRefusalListsEveryNode(t *testing.T) {
	req := placementReq("gpt-oss-120b",
		PlacementCandidate{
			Node:          placeTestNode("alpha"),
			MemTotalBytes: 32 * testGIB,
			MemFreeBytes:  3 * testGIB,
			DiskFreeBytes: 50 * testGIB,
		},
		PlacementCandidate{
			Node:          placeTestNode("beta"),
			MemTotalBytes: 32 * testGIB,
			MemFreeBytes:  24 * testGIB, // fits after 10% headroom...
			DiskFreeBytes: 10 * testGIB, // ...but not the disk need
		},
		PlacementCandidate{
			Node:          placeTestNode("gamma"),
			MemTotalBytes: 16 * testGIB,
			MemFreeBytes:  4 * testGIB,
			DiskFreeBytes: 500 * testGIB,
		},
	)
	p := PlaceMissingModel(req)
	if p.Chosen() {
		t.Fatalf("expected refusal, chose %q", p.Node)
	}
	if len(p.Refusal) != 3 {
		t.Fatalf("refusal must list every checked node, want 3 lines, got %d: %v", len(p.Refusal), p.Refusal)
	}
	want := map[string]string{
		"alpha": "alpha: free memory 3.0 GB, needs 14.0 GB plus 10% headroom",
		"beta":  "beta: free disk 10.0 GB, needs 14.0 GB plus 5.0 GB disk headroom",
		"gamma": "gamma: free memory 4.0 GB, needs 14.0 GB plus 10% headroom",
	}
	seen := map[string]string{}
	for _, line := range p.Refusal {
		for name := range want {
			if strings.HasPrefix(line, name+": ") {
				seen[name] = line
			}
		}
	}
	for name, wantLine := range want {
		if seen[name] != wantLine {
			t.Errorf("line for %s: want %q, got %q", name, wantLine, seen[name])
		}
	}
}

func TestPlaceTaskPinOnlyChecksPinned(t *testing.T) {
	req := placementReq("gpt-oss-120b",
		PlacementCandidate{
			Node:          placeTestNode("pinned"),
			MemTotalBytes: 16 * testGIB,
			MemFreeBytes:  8 * testGIB,
			DiskFreeBytes: 100 * testGIB,
		},
		PlacementCandidate{
			Node:          placeTestNode("roomy"),
			MemTotalBytes: 512 * testGIB,
			MemFreeBytes:  480 * testGIB,
			DiskFreeBytes: 2 * 1024 * testGIB,
		},
	)
	req.PinnedTo = "pinned"
	p := PlaceMissingModel(req)
	if p.Chosen() {
		t.Fatalf("pinned node lacks room for a 14 GiB model with 10%% headroom; got choice %q", p.Node)
	}
	if len(p.Refusal) != 1 {
		t.Fatalf("a task pin restricts candidates to the pinned node; want 1 refusal line naming only it, got %v", p.Refusal)
	}
	if !strings.HasPrefix(p.Refusal[0], "pinned: ") {
		t.Fatalf("refusal must name the pinned node first, got %q", p.Refusal[0])
	}
	if strings.Contains(p.Refusal[0], "roomy") {
		t.Fatalf("unpinned node must not appear in a pinned refusal, got %q", p.Refusal[0])
	}
}

// A task pin that has room still places on the pinned node.
func TestPlaceTaskPinHonoredWhenItFits(t *testing.T) {
	req := placementReq("gpt-oss-120b",
		PlacementCandidate{
			Node:          placeTestNode("pinned"),
			MemTotalBytes: 128 * testGIB,
			MemFreeBytes:  100 * testGIB,
			DiskFreeBytes: 300 * testGIB,
		},
		PlacementCandidate{
			Node:          placeTestNode("roomy"),
			MemTotalBytes: 512 * testGIB,
			MemFreeBytes:  480 * testGIB,
			DiskFreeBytes: 2 * 1024 * testGIB,
		},
	)
	req.PinnedTo = "pinned"
	p := PlaceMissingModel(req)
	if p.Node != "pinned" {
		t.Fatalf("pinned node with room must be chosen, got %q (refusal %v)", p.Node, p.Refusal)
	}
}

func TestPlacePinNamedButAbsentFromCandidates(t *testing.T) {
	req := placementReq("gpt-oss-120b",
		PlacementCandidate{
			Node:          placeTestNode("roomy"),
			MemTotalBytes: 512 * testGIB,
			MemFreeBytes:  480 * testGIB,
			DiskFreeBytes: 2 * 1024 * testGIB,
		},
	)
	req.PinnedTo = "missing-node"
	p := PlaceMissingModel(req)
	if p.Chosen() || len(p.Refusal) != 1 || !strings.HasPrefix(p.Refusal[0], "missing-node: ") {
		t.Fatalf("pin naming an absent node must refuse naming that node, got %+v", p)
	}
}

func TestPlaceExclusionsDisabledMaintenanceConsumer(t *testing.T) {
	disabled := placeTestNode("off")
	disabled.Disabled = true

	req := placementReq("gpt-oss-120b",
		PlacementCandidate{Node: disabled, MemTotalBytes: 512 * testGIB, MemFreeBytes: 480 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
		PlacementCandidate{Node: maintenanceNode("maint"), MemTotalBytes: 512 * testGIB, MemFreeBytes: 480 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
		PlacementCandidate{Node: placeTestNode("no-ask"), MemTotalBytes: 512 * testGIB, MemFreeBytes: 480 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
		PlacementCandidate{Node: placeTestNode("healthy"), MemTotalBytes: 512 * testGIB, MemFreeBytes: 480 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
	)
	req.Candidates[2].Node.Permissions = Permissions{DeniedConsumers: []string{"ask"}}

	p := PlaceMissingModel(req)
	if p.Node != "healthy" {
		t.Fatalf("healthy node should be chosen over excluded ones: got %q (refusal %v)", p.Node, p.Refusal)
	}
}

// With no candidate allowed to take the model, every refusal line must say
// what that node lacks.
func TestPlaceExclusionOnlyRefusal(t *testing.T) {
	disabled := placeTestNode("off")
	disabled.Disabled = true
	denied := placeTestNode("no-agent")
	denied.Permissions = Permissions{AllowedConsumers: []string{"council"}}

	req := placementReq("gpt-oss-120b",
		PlacementCandidate{Node: disabled, MemFreeBytes: 480 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB, MemTotalBytes: 512 * testGIB},
		PlacementCandidate{Node: maintenanceNode("maint"), MemFreeBytes: 480 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB, MemTotalBytes: 512 * testGIB},
		PlacementCandidate{Node: denied, MemFreeBytes: 480 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB, MemTotalBytes: 512 * testGIB},
	)
	req.Consumer = "agent"

	p := PlaceMissingModel(req)
	if p.Chosen() {
		t.Fatalf("everything is excluded; got choice %q", p.Node)
	}
	want := map[string]string{
		"off":      "off: disabled",
		"maint":    "maint: in a maintenance window",
		"no-agent": "no-agent: consumer \"agent\" not allowed",
	}
	got := map[string]string{}
	for _, line := range p.Refusal {
		name, _, _ := strings.Cut(line, ": ")
		got[name] = line
	}
	for name, wantLine := range want {
		if got[name] != wantLine {
			t.Errorf("%s: want %q, got %q", name, wantLine, got[name])
		}
	}
}

func TestPlaceNodePinnedToDifferentModel(t *testing.T) {
	pinned := placeTestNode("pinned-llm")
	pinned.NodePinnedModel = "qwen3.8:27b"

	p := PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{Node: pinned, MemFreeBytes: 480 * testGIB, MemTotalBytes: 512 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
	))
	if p.Chosen() || len(p.Refusal) != 1 {
		t.Fatalf("node pinned to another model must refuse: %+v", p)
	}
	if !strings.Contains(p.Refusal[0], "pinned to model \"qwen3.8:27b\"") {
		t.Fatalf("refusal must name the conflicting pin, got %q", p.Refusal[0])
	}

	// allow_model_swap lets the model replace the pin.
	pinned.AllowModelSwap = true
	p = PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{Node: pinned, MemFreeBytes: 480 * testGIB, MemTotalBytes: 512 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
	))
	if p.Node != "pinned-llm" {
		t.Fatalf("allow_model_swap should admit the node: %+v", p)
	}

	// The pin admits its own model.
	q := placeTestNode("q")
	q.NodePinnedModel = "qwen3.8:27b"
	p = PlaceMissingModel(placementReq("qwen3.8:27b",
		PlacementCandidate{Node: q, MemFreeBytes: 480 * testGIB, MemTotalBytes: 512 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
	))
	if p.Node != "q" {
		t.Fatalf("a node is never excluded by its own pin: %+v", p)
	}
}

func TestPlaceNoAddress(t *testing.T) {
	bare := placeTestNode("bare")
	bare.Address = ""
	p := PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{Node: bare, MemFreeBytes: 480 * testGIB, MemTotalBytes: 512 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
	))
	if p.Chosen() || len(p.Refusal) != 1 || !strings.HasSuffix(p.Refusal[0], ": no address") {
		t.Fatalf("node without an address is unusable: %+v", p)
	}
}

func TestPlaceUnknownSize(t *testing.T) {
	req := placementReq("gpt-oss-120b",
		PlacementCandidate{Node: placeTestNode("a"), MemFreeBytes: 480 * testGIB, MemTotalBytes: 512 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
		PlacementCandidate{Node: placeTestNode("b"), MemFreeBytes: 960 * testGIB, MemTotalBytes: 1024 * testGIB, DiskFreeBytes: 4 * 1024 * testGIB},
	)
	req.ModelSizeGB = 0 // unknown

	p := PlaceMissingModel(req)
	if p.Chosen() || len(p.Refusal) != 2 {
		t.Fatalf("unknown size must refuse every non-serving node: %+v", p)
	}
	for _, line := range p.Refusal {
		if !strings.HasSuffix(line, ": model size unknown") {
			t.Fatalf("each line must say the size is unknown, got %q", line)
		}
	}
}

// Unknown size never blocks a node that already serves the model.
func TestPlaceUnknownSizeStillChoosesServingNode(t *testing.T) {
	req := placementReq("gpt-oss-120b",
		PlacementCandidate{Node: placeTestNode("a"), MemFreeBytes: 480 * testGIB, MemTotalBytes: 512 * testGIB, DiskFreeBytes: 2 * 1024 * testGIB},
		PlacementCandidate{Node: placeTestNode("serving"), MemFreeBytes: 8 * testGIB, MemTotalBytes: 16 * testGIB, DiskFreeBytes: 80 * testGIB, ServesModel: true},
	)
	req.ModelSizeGB = 0

	p := PlaceMissingModel(req)
	if p.Node != "serving" {
		t.Fatalf("serving node is the only safe choice with an unknown size: %+v", p)
	}
}

func TestPlaceTieGoToSmallerName(t *testing.T) {
	p := PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{Node: placeTestNode("zeta"), MemFreeBytes: 64 * testGIB, MemTotalBytes: 128 * testGIB, DiskFreeBytes: 200 * testGIB},
		PlacementCandidate{Node: placeTestNode("beta"), MemFreeBytes: 64 * testGIB, MemTotalBytes: 128 * testGIB, DiskFreeBytes: 200 * testGIB},
	))
	if p.Node != "beta" {
		t.Fatalf("ties on free memory go to the smaller name: got %q (refusal %v)", p.Node, p.Refusal)
	}
}

func TestMemHeadroomBytes(t *testing.T) {
	total := 64 * testGIB
	if got := memHeadroomBytes(total, 10); got != total/100*10 {
		t.Errorf("10%% headroom: want %d, got %d", total/100*10, got)
	}
	if got := memHeadroomBytes(total, 0); got != 0 {
		t.Errorf("0%% headroom: want 0, got %d", got)
	}
	if got := memHeadroomBytes(total, 100); got != total {
		t.Errorf("100%% headroom: want full total, got %d", got)
	}
	if got := memHeadroomBytes(total, 250); got != total {
		t.Errorf("pct > 100 clamps to total, got %d", got)
	}
	if got := memHeadroomBytes(0, 10); got != 0 {
		t.Errorf("no total: want 0, got %d", got)
	}
}

// With no number, no candidate can be checked against a model size — only
// an already-serving one is chosen.
func TestPlaceNoNumbersRefuses(t *testing.T) {
	p := PlaceMissingModel(placementReq("gpt-oss-120b",
		PlacementCandidate{Node: placeTestNode("a"), MemFreeBytes: 0, MemTotalBytes: 0, DiskFreeBytes: 0},
	))
	if p.Chosen() || len(p.Refusal) != 1 {
		t.Fatalf("node with no resource numbers cannot fit a known-size model: %+v", p)
	}
	if !strings.Contains(p.Refusal[0], "free memory 0.0 GB") {
		t.Fatalf("refusal should show the zero free memory, got %q", p.Refusal[0])
	}
}

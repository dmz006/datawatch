// This file plans the placement of a missing model — the model the
// operator wants but that no candidate node currently has resident.
//
// Given the candidate nodes (already filtered by the caller) and the live
// resource numbers the caller gathered for each node — free and total
// memory, the free disk on the mount with the most room, and whether the
// node already serves the model — PlaceMissingModel picks the one node the
// model should be loaded onto, or refuses with one line per checked node:
// the node's name first, then what that node lacks, so the operator sees
// every node and why it was not chosen.
//
// The planner is pure: no I/O, no clock (the caller passes now), no
// registry access. Nothing here mutates a Node.
//
// Headroom comes from the compute_placement config (mem_headroom_pct and
// disk_headroom_gb); the caller reads each key through its default when
// unset and passes the effective value.
//
// Pins — three different things, do not conflate them:
//   - Node.NodePinnedModel (the node_pinned_model registry field) is the
//     node-level pin: the node keeps one model and refuses a different
//     model unless AllowModelSwap is set.
//   - Request.PreferNode in internal/capacity/ledger.go is only a tie
//     breaker between equally loaded candidates; it never outranks load or
//     adds a candidate. It is not a pin.
//   - The optional task pin (PlacementRequest.PinnedTo) names exactly one
//     node: candidates are restricted to it and the task is never moved
//     elsewhere. A pin is not a bypass — a pinned node that is disabled, in
//     maintenance, or short of resources is refused like any other node.

package compute

import (
	"fmt"
	"strconv"
	"time"
)

// placementGB is one gigabyte (GiB, 2^30 bytes) of model size or disk need.
const placementGB = uint64(1024) * 1024 * 1024

// PlacementRequest is everything the planner needs to answer "which node
// should this missing model be loaded onto?".
type PlacementRequest struct {
	// Model is the name of the model to place.
	Model string
	// Consumer names the consumer asking for the placement (e.g. "ask",
	// "council"); it is checked against each node's Permissions.
	Consumer string
	// Now is the planning instant, used for the maintenance-window check.
	Now time.Time

	// ModelSizeGB is the size of the model in gigabytes. 0 means unknown:
	// a node that does not already serve the model cannot be checked
	// against an unknown size and is refused with that reason.
	ModelSizeGB int

	// MemHeadroomPct is compute_placement.mem_headroom_pct (effective): the
	// percentage of the node's total memory placement keeps free for
	// everything that is not the model it is placing.
	MemHeadroomPct int

	// DiskHeadroomGB is compute_placement.disk_headroom_gb (effective): the
	// gigabytes of disk placement keeps free on the node.
	DiskHeadroomGB int

	// PinnedTo is the optional task pin: exactly one node name. When set,
	// candidates are restricted to that node and the model is never placed
	// elsewhere. Empty = no task pin.
	PinnedTo string

	// Candidates are the nodes the caller shortlisted, each with its Node
	// and the live resource numbers gathered for it (see
	// PlacementCandidate). Order is preserved in the refusal lines.
	Candidates []PlacementCandidate
}

// PlacementCandidate is one candidate node plus the live resource numbers
// the caller gathered for it: from the bound observer peer's latest
// snapshot (the peer's LastPayload — Mem.TotalBytes / Mem.UsedBytes for
// memory and the disk mount with the most free bytes for disk), falling
// back to the node's declared capacity when no observer bound to the node
// has pushed.
type PlacementCandidate struct {
	// Node is the registry entry; must be non-nil.
	Node *Node
	// MemTotalBytes is the node's total memory (0 = caller has no number).
	MemTotalBytes uint64
	// MemFreeBytes is the node's free memory (Mem.UsedBytes already out of
	// Mem.TotalBytes, or derived from declared capacity by the caller).
	MemFreeBytes uint64
	// DiskFreeBytes is the free disk on the mount with the most room.
	DiskFreeBytes uint64
	// ServesModel reports that the node already has the model resident.
	// Such a node wins outright (subject to the exclusion checks), no load
	// required.
	ServesModel bool
}

// Placement is the planner's answer: either one chosen node or a refusal.
type Placement struct {
	// Node is the chosen node's name; empty when the planner refused.
	Node string
	// Refusal is one line per checked node, the node's name first and then
	// what that node lacks. Empty when a node was chosen.
	Refusal []string
}

// Chosen reports whether the planner placed the model on a node.
func (p Placement) Chosen() bool { return p.Node != "" }

// PlaceMissingModel decides which candidate node a missing model should be
// loaded onto. Rules:
//
//   - A node that already serves the model wins (no load needed): among
//     such nodes the one with the most free memory is picked.
//   - Otherwise pick the node with the most free memory after headroom
//     that also carries the model's disk need plus the disk headroom;
//     ties on free memory go to the lexicographically smaller name.
//   - With an unknown model size, only a serving node can be chosen.
//   - PinnedTo restricts the candidates to that one node.
//   - Every candidate that is not chosen contributes one refusal line:
//     node name first, then the first problem found for it.
func PlaceMissingModel(req PlacementRequest) Placement {
	cands := req.Candidates
	if req.PinnedTo != "" {
		var kept []PlacementCandidate
		for _, c := range cands {
			if c.Node != nil && c.Node.Name == req.PinnedTo {
				kept = append(kept, c)
			}
		}
		if len(kept) == 0 {
			return Placement{Refusal: []string{fmt.Sprintf(
				"%s: pinned by the task but not in the candidate list", req.PinnedTo)}}
		}
		cands = kept
	}

	sizeGB := req.ModelSizeGB
	var lines []string
	var serving, fitting []PlacementCandidate

	for i := range cands {
		c := &cands[i]
		if c.Node == nil {
			lines = append(lines, "(unnamed candidate): no node")
			continue
		}
		name := c.Node.Name
		if reason := exclusionReason(c.Node, req); reason != "" {
			lines = append(lines, name+": "+reason)
			continue
		}
		if c.ServesModel {
			serving = append(serving, *c)
			continue
		}
		if sizeGB <= 0 {
			lines = append(lines, name+": model size unknown")
			continue
		}
		sizeBytes := uint64(sizeGB) * placementGB
		var usable uint64
		if head := memHeadroomBytes(c.MemTotalBytes, req.MemHeadroomPct); c.MemFreeBytes > head {
			usable = c.MemFreeBytes - head
		}
		if sizeBytes > usable {
			lines = append(lines, fmt.Sprintf(
				"%s: free memory %s, needs %s plus %d%% headroom",
				name, formatGB(c.MemFreeBytes), formatGB(sizeBytes), req.MemHeadroomPct))
			continue
		}
		if need := sizeBytes + uint64(req.DiskHeadroomGB)*placementGB; c.DiskFreeBytes < need {
			lines = append(lines, fmt.Sprintf(
				"%s: free disk %s, needs %s plus %s disk headroom",
				name, formatGB(c.DiskFreeBytes), formatGB(sizeBytes), formatGB(uint64(req.DiskHeadroomGB)*placementGB)))
			continue
		}
		fitting = append(fitting, *c)
	}

	pick := func(group []PlacementCandidate) Placement {
		best := &group[0]
		for i := range group {
			c := &group[i]
			if c.MemFreeBytes > best.MemFreeBytes ||
				(c.MemFreeBytes == best.MemFreeBytes && c.Node.Name < best.Node.Name) {
				best = c
			}
		}
		return Placement{Node: best.Node.Name}
	}

	if len(serving) > 0 {
		return pick(serving)
	}
	if len(fitting) > 0 {
		return pick(fitting)
	}
	return Placement{Refusal: lines}
}

// exclusionReason reports the first disqualifying condition on n, in
// operator-priority order, or "" when none applies.
func exclusionReason(n *Node, req PlacementRequest) string {
	if n.Disabled {
		return "disabled"
	}
	if n.InMaintenance(req.Now) {
		return "in a maintenance window"
	}
	if !n.AllowsConsumer(req.Consumer) {
		return fmt.Sprintf("consumer %q not allowed", req.Consumer)
	}
	if n.NodePinnedModel != "" && n.NodePinnedModel != req.Model && !n.AllowModelSwap {
		return fmt.Sprintf("pinned to model %q without allow_model_swap", n.NodePinnedModel)
	}
	if n.Address == "" {
		return "no address"
	}
	return ""
}

// memHeadroomBytes is the memory kept free: pct percent of the node's
// total memory (pct clamped to 0-100).
func memHeadroomBytes(total uint64, pct int) uint64 {
	switch {
	case pct <= 0 || total == 0:
		return 0
	case pct >= 100:
		return total
	default:
		return total / 100 * uint64(pct)
	}
}

// formatGB renders b as "<n.n> GB" to one decimal place.
func formatGB(b uint64) string {
	return strconv.FormatFloat(float64(b)/float64(placementGB), 'f', 1, 64) + " GB"
}

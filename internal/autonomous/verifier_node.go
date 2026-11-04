// The verification job follows the worker's node.
//
// A worker's lease is handed to its verifier when the verifier can run on it
// (judgeHandoff). When the verifier uses a different backend or model the
// worker's lease is released and the verifier acquires its own through
// AdmitJudge, so placement could send it to another node: one that may not host
// the verifier's model, or whose resources the worker's work does not share.
//
// With autonomous.model_routing.verifier_follows_worker_node on (the default)
// the verifier is placed on the node its worker ran on, and waits for that node
// instead of moving, whenever that node is one of the verifier's candidates and
// serves the verifier's model. Otherwise normal placement applies and a
// verifier_node_not_followed Decision on the Automaton records why.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// verifierNodeNotFollowedKind is the Decision kind recorded when a verifier is
// placed without following its worker's node although following is on.
const verifierNodeNotFollowedKind = "verifier_node_not_followed"

// verifierNodeInput is what decideVerifierNode needs to know.
type verifierNodeInput struct {
	// Settings are the live model routing settings.
	Settings ModelRoutingSettings
	// WorkerNode is the node the worker ran on, "" when unknown.
	WorkerNode string
	// Candidates are the verifier's candidate nodes.
	Candidates []capacity.NodeChoice
	// ServesModel reports whether the verifier's model is served on WorkerNode.
	ServesModel bool
}

// verifierNodeDecision is the answer of decideVerifierNode.
type verifierNodeDecision struct {
	// ExcludeNodes is the exclude list for the verifier's request: every
	// candidate except the worker's node when it is followed, else nil.
	ExcludeNodes []string
	// Followed reports whether the worker's node was followed.
	Followed bool
	// Reason is a short sentence saying why the node was or was not followed.
	Reason string
}

// resolveWorkerNode returns the node the worker ran on: the pin of a pinned
// task, else the node its lease holds, else the node recorded on the task.
func resolveWorkerNode(pin, leaseNode, computeNode string) string {
	for _, n := range []string{pin, leaseNode, computeNode} {
		if n = strings.TrimSpace(n); n != "" {
			return n
		}
	}
	return ""
}

// decideVerifierNode is the pure rule for where a verifier runs relative to its
// worker. With the setting off it follows nothing. Otherwise the worker's node
// is followed when it is a candidate of the verifier and serves the verifier's
// model, and then every other candidate is excluded so the verifier can only be
// placed there.
func decideVerifierNode(in verifierNodeInput) verifierNodeDecision {
	if !in.Settings.VerifierFollowsWorkerNode {
		return verifierNodeDecision{Reason: "verifier_follows_worker_node is off"}
	}
	if in.WorkerNode == "" {
		return verifierNodeDecision{Reason: "the worker's node is not known"}
	}
	isCandidate := slices.ContainsFunc(in.Candidates, func(c capacity.NodeChoice) bool { return c.Name == in.WorkerNode })
	if !isCandidate {
		return verifierNodeDecision{Reason: fmt.Sprintf("the worker's node %q is not a candidate for the verifier's backend and model", in.WorkerNode)}
	}
	if !in.ServesModel {
		return verifierNodeDecision{Reason: fmt.Sprintf("the worker's node %q does not serve the verifier's model", in.WorkerNode)}
	}
	var exclude []string
	for _, c := range in.Candidates {
		if c.Name != "" && c.Name != in.WorkerNode && !slices.Contains(exclude, c.Name) {
			exclude = append(exclude, c.Name)
		}
	}
	return verifierNodeDecision{
		ExcludeNodes: exclude, Followed: true,
		Reason: fmt.Sprintf("the verifier follows the worker's node %q", in.WorkerNode),
	}
}

// verifierTaskID returns the task id of a verifier holder ("verify:<task>"), or
// false for any other holder, so a guardrail judge never follows a worker.
func verifierTaskID(holder string) (string, bool) {
	id, ok := strings.CutPrefix(holder, VerifyHolder(""))
	return id, ok && id != ""
}

// verifierModelServedOn asks the model host check whether node serves model for
// backend. With no check wired, a verdict that does not apply, or an answer the
// check could not give, the candidate list is the only evidence and the node
// counts as serving; an unregistered node or a refusal does not serve.
// No lock is held across the check: it can read the node over the network.
func (m *Manager) verifierModelServedOn(ctx context.Context, backend, model, node string) bool {
	m.mu.Lock()
	check := m.modelHostCheck
	m.mu.Unlock()
	if check == nil || model == "" {
		return true
	}
	cctx, cancel := context.WithTimeout(ctx, modelHostCheckTimeout)
	defer cancel()
	v := check(cctx, backend, model, node)
	if !v.Applicable {
		return true
	}
	return !v.NodeUnknown && !v.Refused
}

// followWorkerNode works out whether the verifier of j follows its worker's
// node and, when it does, sets j.followNode and j.followExclude. leaseNode is
// the node a lease of the task still holds, "" when none. It does nothing for a
// holder that is not a verifier, with the setting off, or for an entry with
// fewer than two candidate nodes, so those paths are exactly what they were.
// It returns the decision and whether one was made. When record is set a
// verifier that was not placed on its worker's node leaves a
// verifier_node_not_followed Decision on the Automaton.
func (m *Manager) followWorkerNode(ctx context.Context, j *JudgeAdmit, choices []capacity.NodeChoice, leaseNode string, record bool) (verifierNodeDecision, bool) {
	taskID, ok := verifierTaskID(j.Holder)
	if !ok || len(choices) < 2 {
		return verifierNodeDecision{}, false
	}
	settings := m.modelRouting()
	if !settings.VerifierFollowsWorkerNode {
		return verifierNodeDecision{}, false
	}
	pin, computeNode := "", ""
	if task, found := m.store.TaskSnapshot(taskID); found {
		computeNode = task.ComputeNode
		var story *Story
		if prd, okPRD := m.store.GetPRD(j.PRDID); okPRD {
			story = findStory(prd, task.StoryID)
		}
		pin = effectiveNodePin(&task, story)
	}
	node := resolveWorkerNode(pin, leaseNode, computeNode)
	served := node != "" && m.verifierModelServedOn(ctx, j.Backend, j.Model, node)
	d := decideVerifierNode(verifierNodeInput{Settings: settings, WorkerNode: node, Candidates: choices, ServesModel: served})
	if d.Followed {
		j.followNode, j.followExclude = node, d.ExcludeNodes
	} else if record {
		m.recordVerifierNodeNotFollowed(j.PRDID, taskID, d.Reason)
	}
	return d, true
}

// recordVerifierNodeNotFollowed appends the verifier_node_not_followed Decision
// to the Automaton. The same reason for the same task is not recorded twice in
// a row, so a verifier that is admitted again does not repeat itself.
func (m *Manager) recordVerifierNodeNotFollowed(prdID, taskID, reason string) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	note := fmt.Sprintf("task=%s %s", taskID, reason)
	appended := false
	m.store.WithLock(func() {
		if n := len(prd.Decisions); n > 0 && prd.Decisions[n-1].Kind == verifierNodeNotFollowedKind && prd.Decisions[n-1].Note == note {
			return
		}
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: verifierNodeNotFollowedKind, Actor: "autonomous", Note: note,
		})
		appended = true
	})
	if !appended {
		return
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s save %s decision: %v", prdID, verifierNodeNotFollowedKind, err)
	}
}

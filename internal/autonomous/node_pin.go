// Per-task and per-story compute node pin.
//
// Placement chooses a node for a worker from the candidates of its LLM entry,
// which is how workers ended up on nodes without their model. A task or a story
// can now name the node its worker must run on. The setters here store that
// pin after asking the model host check whether the node is registered and
// serves the model the unit resolves to; acquireLease in capacity.go then lets
// only the pinned node be chosen.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// ErrNodeUnknown is wrapped by the error a node pin is refused with when the
// node is not a registered compute node. It is refused in both refusal modes:
// a node that does not exist cannot be warned about and used anyway.
var ErrNodeUnknown = errors.New("compute node is not registered")

// ErrPinnedNodeUnavailable is wrapped by the error a worker admission fails
// with when its pinned node cannot be used: the node is not a candidate for the
// worker's backend and model, or the node failed for this attempt. The task
// fails with this reason instead of running on another node.
var ErrPinnedNodeUnavailable = errors.New("pinned compute node is not available")

// Decision kinds recorded when a node pin changes.
const (
	setTaskNodeKind  = "set_task_node"
	setStoryNodeKind = "set_story_node"
)

// effectiveNodePin returns the compute node a task must run on: the task's own
// pin, else its story's, trimmed. Empty means placement chooses the node.
func effectiveNodePin(task *Task, story *Story) string {
	if task != nil {
		if n := strings.TrimSpace(task.PinnedNode); n != "" {
			return n
		}
	}
	if story != nil {
		return strings.TrimSpace(story.PinnedNode)
	}
	return ""
}

// SetTaskNode pins a task's worker to a compute node, or clears the pin when
// node is empty. It is accepted under the same rule as SetTaskLLM
// (autonomous.task_override_statuses). A non-empty node must be registered, and
// must serve the model the task resolves to: when it does not, the refusal mode
// of autonomous.model_routing decides between refusing the pin and applying it
// with a model_not_served_warning Decision.
func (m *Manager) SetTaskNode(prdID, taskID, node, actor string) (*PRD, error) {
	node = strings.TrimSpace(node)
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("no Automaton with id %q", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	task, story := findTaskInPRD(prd, taskID)
	if task == nil {
		return nil, fmt.Errorf("task %q not found in Automaton %q", taskID, prdID)
	}
	var warning string
	if node != "" {
		var err error
		warning, err = m.checkNodePin(prdID, "task "+taskID, node, task, story, prd)
		if err != nil {
			return nil, err
		}
	}
	m.store.WithLock(func() {
		task.PinnedNode = node
		task.UpdatedAt = time.Now()
		prd.UpdatedAt = time.Now()
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: setTaskNodeKind, Actor: actor,
			Note: fmt.Sprintf("task=%s node=%s", taskID, node),
		})
		recordModelWarning(prd, warning, actor)
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetStoryNode pins every task of a story that has no pin of its own to a
// compute node, or clears the story's pin when node is empty. The rules are
// those of SetTaskNode; the model checked is the one the story's tasks resolve
// to.
func (m *Manager) SetStoryNode(prdID, storyID, node, actor string) (*PRD, error) {
	node = strings.TrimSpace(node)
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("no Automaton with id %q", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var story *Story
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			story = &prd.Story[i]
			break
		}
	}
	if story == nil {
		return nil, fmt.Errorf("story %q not found in Automaton %q", storyID, prdID)
	}
	var warning string
	if node != "" {
		var err error
		warning, err = m.checkNodePin(prdID, "story "+storyID, node, &Task{}, story, prd)
		if err != nil {
			return nil, err
		}
	}
	m.store.WithLock(func() {
		story.PinnedNode = node
		story.UpdatedAt = time.Now()
		prd.UpdatedAt = time.Now()
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: setStoryNodeKind, Actor: actor,
			Note: fmt.Sprintf("story=%s node=%s", storyID, node),
		})
		recordModelWarning(prd, warning, actor)
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// checkNodePin asks the model host check whether node is registered and serves
// the model the unit resolves to. An unregistered node is always refused with
// an error wrapping ErrNodeUnknown. For a unit with no resolved model only the
// registration is checked. A node that does not serve the model follows the
// refusal mode: refuse returns an error wrapping ErrModelNotServed that names
// the node and the model, warn returns the same text as the warning. With no
// check wired nothing is known about nodes, so the pin is accepted.
//
// No lock is held across the check: it can read the node over the network.
func (m *Manager) checkNodePin(prdID, unit, node string, task *Task, story *Story, prd *PRD) (warning string, err error) {
	m.mu.Lock()
	check := m.modelHostCheck
	m.mu.Unlock()
	if check == nil {
		return "", nil
	}
	backend, model := resolveTaskBackendModel(task, story, prd, m.AutomationForPRD(prd), "")
	ctx, cancel := context.WithTimeout(context.Background(), modelHostCheckTimeout)
	defer cancel()
	v := check(ctx, backend, model, node)
	if v.Applicable && v.NodeUnknown {
		return "", fmt.Errorf("cannot pin %s to node %q: %w", unit, node, ErrNodeUnknown)
	}
	if model == "" {
		return "", nil
	}
	return m.nodeVerdictOutcome(prdID, unit, model, node, v)
}

// checkModelHostedOn is checkModelHosted for one node. With an empty node it is
// the any-node check. With a node, an unregistered node is refused with an
// error wrapping ErrNodeUnknown in both modes, and a refusal or warning names
// the node and the model.
func (m *Manager) checkModelHostedOn(prdID, unit, backend, model, node string) (string, error) {
	if node == "" {
		return m.checkModelHosted(prdID, unit, backend, model, "")
	}
	if model == "" {
		return "", nil
	}
	m.mu.Lock()
	check := m.modelHostCheck
	m.mu.Unlock()
	if check == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelHostCheckTimeout)
	defer cancel()
	v := check(ctx, backend, model, node)
	if v.Applicable && v.NodeUnknown {
		return "", fmt.Errorf("%s is pinned to node %q: %w", unit, node, ErrNodeUnknown)
	}
	return m.nodeVerdictOutcome(prdID, unit, model, node, v)
}

// nodeVerdictOutcome is modelVerdictOutcome with the node and the model named in
// the refusal or the warning, whatever wording the check used for its message.
func (m *Manager) nodeVerdictOutcome(prdID, unit, model, node string, v ModelHostVerdict) (string, error) {
	warning, err := m.modelVerdictOutcome(prdID, unit, model, v)
	if warning != "" {
		return nodePinText(warning, node, model), nil
	}
	var refused *modelNotServedError
	if errors.As(err, &refused) {
		return "", &modelNotServedError{message: nodePinText(refused.message, node, model)}
	}
	return "", err
}

// nodePinText makes msg name node and model: a message that already names both
// is returned as it is, otherwise a leading sentence names them.
func nodePinText(msg, node, model string) string {
	if strings.Contains(msg, node) && strings.Contains(msg, model) {
		return msg
	}
	return fmt.Sprintf("node %q does not serve model %q: %s", node, model, msg)
}

// pinsInheritingAutomatonModel returns the distinct node pins of the tasks that
// take their worker backend and model from the Automaton: no task-level and no
// story-level backend. A model set on the Automaton reaches those tasks on
// those nodes. noPin reports whether any such task has no pin (or there is no
// such task yet), so the model must also be served by some node.
func pinsInheritingAutomatonModel(prd *PRD) (pins []string, noPin bool) {
	inherits := 0
	for si := range prd.Story {
		story := &prd.Story[si]
		if story.Backend != "" {
			continue
		}
		for ti := range story.Tasks {
			t := &story.Tasks[ti]
			if t.Backend != "" {
				continue
			}
			inherits++
			pin := effectiveNodePin(t, story)
			if pin == "" {
				noPin = true
			} else if !slices.Contains(pins, pin) {
				pins = append(pins, pin)
			}
		}
	}
	return pins, noPin || inherits == 0
}

// checkAutomatonModelHosted is the pin-time check for a worker model set on the
// Automaton. The model is checked against any node when some task that inherits
// it is not pinned, and against each pinned node of the tasks that are, so a
// pinned task never inherits a model its node does not serve. The warnings of
// every pin checked come back joined.
func (m *Manager) checkAutomatonModelHosted(prdID string, prd *PRD, backend, model *string) (string, error) {
	if model == nil || *model == "" {
		return "", nil
	}
	pins, anyNode := pinsInheritingAutomatonModel(prd)
	var warnings []string
	if anyNode {
		w, err := m.checkLevelModelHosted(prdID, "Automaton worker", backend, model, prd.Backend)
		if err != nil {
			return "", err
		}
		if w != "" {
			warnings = append(warnings, w)
		}
	}
	for _, pin := range pins {
		w, err := m.checkModelHostedOn(prdID, "Automaton worker (tasks pinned to "+pin+")", pickString(backend, prd.Backend), *model, pin)
		if err != nil {
			return "", err
		}
		if w != "" {
			warnings = append(warnings, w)
		}
	}
	return strings.Join(warnings, "; "), nil
}

// applyNodePin restricts req to the pinned node. With two or more candidates
// every other candidate joins ExcludeNodes, so only the pin can be chosen; with
// fewer the single node the request already names must be the pin. failover is
// the caller's exclude list for this attempt. It returns an error wrapping
// ErrPinnedNodeUnavailable, naming the node, when the pin cannot be honoured:
// the node is excluded, or is not a candidate for the backend and model.
// An empty pin returns req unchanged.
func applyNodePin(req capacity.Request, pin string, failover []string) (capacity.Request, error) {
	if pin == "" {
		return req, nil
	}
	if slices.Contains(failover, pin) {
		return req, fmt.Errorf("%w: node %q failed for this attempt and the task is pinned to it", ErrPinnedNodeUnavailable, pin)
	}
	if len(req.NodeChoices) >= 2 {
		exclude := slices.Clone(failover)
		found := false
		for _, c := range req.NodeChoices {
			switch {
			case c.Name == pin:
				found = true
			case c.Name != "" && !slices.Contains(exclude, c.Name):
				exclude = append(exclude, c.Name)
			}
		}
		if !found {
			return req, fmt.Errorf("%w: node %q is not a candidate for this backend and model", ErrPinnedNodeUnavailable, pin)
		}
		req.ExcludeNodes = exclude
		return req, nil
	}
	if req.Node != pin {
		return req, fmt.Errorf("%w: node %q is not a candidate for this backend and model", ErrPinnedNodeUnavailable, pin)
	}
	return req, nil
}

// unplaceableRequest returns req changed so that the ledger can place it on no
// node. A waiter whose pin stopped being honourable while it queued (the
// candidate list changed under it) must not be granted on another node; it
// waits, with the ledger naming every node as excluded, until the pin is
// honourable again or the wait times out.
func unplaceableRequest(req capacity.Request, pin string) capacity.Request {
	names := []string{pin}
	choices := []capacity.NodeChoice{{Name: pin}}
	for _, c := range req.NodeChoices {
		if c.Name != "" && c.Name != pin {
			names = append(names, c.Name)
			choices = append(choices, c)
		}
	}
	if req.Node != "" && !slices.Contains(names, req.Node) {
		names = append(names, req.Node)
		choices = append(choices, capacity.NodeChoice{Name: req.Node})
	}
	if len(choices) < 2 {
		// A single name cannot form a candidate list; a blank second entry
		// makes the request multi-node and is dropped by the ledger.
		choices = append(choices, capacity.NodeChoice{})
	}
	req.NodeChoices, req.ExcludeNodes, req.Node = choices, names, ""
	return req
}

// workerNodePin returns the compute node a worker admission of t is pinned to:
// the task's own pin, else its story's. It reads the stored task and story so a
// pin set after the task object was loaded still counts. Only a worker request
// is pinned; the verifier and the planner place freely.
func (m *Manager) workerNodePin(prd *PRD, t *Task, role capacity.Role) string {
	if role != capacity.RoleWorker {
		return ""
	}
	task, story := t, findStory(prd, t.StoryID)
	if cur, ok := m.store.TaskSnapshot(t.ID); ok {
		task = &cur
	}
	return effectiveNodePin(task, story)
}

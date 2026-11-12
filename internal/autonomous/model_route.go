// Pin-time model routing check.
//
// A worker or verifier model pinned on a task, story or Automaton used to be
// stored without asking whether any node can serve it, which is how workers
// landed on nodes that did not have their model. The setters in manager.go now
// ask a ModelHostCheckFn first. The function is injected by the daemon, so this
// package never imports the compute package, and its behaviour (refuse or only
// warn, whether a refusal raises an alert) is read live from the
// autonomous.model_routing configuration block through ModelRoutingSettings.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// ErrModelNotServed is wrapped by the error a pin is refused with when no
// registered node serves the pinned model.
var ErrModelNotServed = errors.New("no registered node serves the model")

// modelNotServedAlertTitle is the title of the operator alert a refusal or a
// warning raises.
const modelNotServedAlertTitle = "Automaton model not served by any node"

// modelNotServedWarningKind is the Decision kind recorded when a pin proceeds
// in warn mode although no node serves the model.
const modelNotServedWarningKind = "model_not_served_warning"

// ModelHostVerdict is the answer of a ModelHostCheckFn for one model.
// Applicable is false when the check does not apply (a hosted backend, or a
// backend that runs on no node), and then nothing else is read. Refused means
// no node serves the model. NodeUnknown means the node argument is not a
// registered node. Message names the model and the nodes checked.
type ModelHostVerdict struct {
	Applicable  bool
	Refused     bool
	NodeUnknown bool
	Model       string
	Node        string
	Message     string
	// ServedBy lists the nodes that serve the model, or are assumed to under
	// the unknown-node policy. Checked holds the per-node reasoning. Both are
	// empty when the check does not apply.
	ServedBy []string
	Checked  []ModelHostNodeState
}

// ModelHostNodeState is one node's answer inside a ModelHostVerdict: State is
// serves, not_serving or unknown, and Reason says why.
type ModelHostNodeState struct {
	Node   string `json:"node"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// ModelHostCheckFn decides whether a node serves model for backend. An empty
// node means any node; an empty backend means every enabled registered node is
// checked. It can read from the nodes over the network, so a caller must not
// hold a lock across it.
type ModelHostCheckFn func(ctx context.Context, backend, model, node string) ModelHostVerdict

// ModelRoutingSettings is the part of autonomous.model_routing the pin-time
// check reads: the refusal mode ("refuse" or "warn"), whether a refusal raises
// an operator alert, and whether a verifier follows its worker's node.
type ModelRoutingSettings struct {
	RefusalMode               string
	AlertOnRefusal            bool
	VerifierFollowsWorkerNode bool
}

// defaultModelRoutingSettings are the settings used when none are wired.
func defaultModelRoutingSettings() ModelRoutingSettings {
	return ModelRoutingSettings{
		RefusalMode:               config.ModelRoutingDefaultRefusalMode,
		AlertOnRefusal:            config.ModelRoutingDefaultAlertOnRefusal,
		VerifierFollowsWorkerNode: config.ModelRoutingDefaultVerifierFollowsWorkerNode,
	}
}

// modelHostCheckTimeout bounds one pin-time check as a whole, so a setter never
// waits longer than the probes of a few slow nodes.
const modelHostCheckTimeout = 60 * time.Second

// SetModelHostCheck wires the function the pin setters ask whether a node
// serves a model. Nil disables the check.
func (m *Manager) SetModelHostCheck(fn ModelHostCheckFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelHostCheck = fn
}

// SetModelRoutingSettings wires the function that returns the live model
// routing settings. It is called on every use, so a configuration change takes
// effect at once. Nil means the defaults: refuse, alert on, verifier follows.
func (m *Manager) SetModelRoutingSettings(fn func() ModelRoutingSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelRoutingSettings = fn
}

// modelRouting returns the live model routing settings, or the defaults when
// none are wired. An empty refusal mode reads as the default.
func (m *Manager) modelRouting() ModelRoutingSettings {
	m.mu.Lock()
	fn := m.modelRoutingSettings
	m.mu.Unlock()
	if fn == nil {
		return defaultModelRoutingSettings()
	}
	s := fn()
	if s.RefusalMode != config.ModelRoutingRefusalWarn {
		s.RefusalMode = config.ModelRoutingRefusalRefuse
	}
	return s
}

// checkModelHosted asks the wired check whether a node serves model for backend
// before a pin is stored. unit names what is being pinned (for example
// "task t1"). With no check wired, an empty model or a verdict that is not
// applicable it returns nothing. A refusal in refuse mode returns an error
// wrapping ErrModelNotServed whose text is the verdict message; in warn mode
// the message comes back as warning with a nil error. Either way a refusal
// raises one operator alert when alert_on_refusal is on.
//
// No lock is held across the check: it can read the nodes over the network.
func (m *Manager) checkModelHosted(prdID, unit, backend, model, node string) (warning string, err error) {
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
	return m.modelVerdictOutcome(prdID, unit, model, check(ctx, backend, model, node))
}

// modelVerdictOutcome turns one verdict into the outcome of a pin: nothing for
// a verdict that does not apply or does not refuse, otherwise the alert, then a
// warning in warn mode or the refusal error in refuse mode. checkModelHosted
// and the node pin check share it, so both read the settings the same way.
func (m *Manager) modelVerdictOutcome(prdID, unit, model string, v ModelHostVerdict) (warning string, err error) {
	if !v.Applicable || !v.Refused {
		return "", nil
	}
	msg := v.Message
	if msg == "" {
		msg = fmt.Sprintf("no registered node serves model %q", model)
	}
	settings := m.modelRouting()
	if settings.AlertOnRefusal {
		m.raiseModelNotServedAlert(prdID, unit, settings.RefusalMode, msg)
	}
	if settings.RefusalMode == config.ModelRoutingRefusalWarn {
		return msg, nil
	}
	return "", &modelNotServedError{message: msg}
}

// modelNotServedError is the refusal a pin gets: its text is the verdict
// message, which already names the model and the nodes checked, and it
// unwraps to ErrModelNotServed so callers can test for it with errors.Is.
type modelNotServedError struct{ message string }

func (e *modelNotServedError) Error() string { return e.message }

func (e *modelNotServedError) Unwrap() error { return ErrModelNotServed }

// raiseModelNotServedAlert sends the one operator alert for a pin that no node
// can serve. Nothing is sent when no alert sink is wired.
func (m *Manager) raiseModelNotServedAlert(prdID, unit, mode, msg string) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	level := "error"
	outcome := "The pin was refused and nothing changed."
	if mode == config.ModelRoutingRefusalWarn {
		level = "warning"
		outcome = "The pin was applied anyway because model routing is set to warn."
	}
	body := fmt.Sprintf("Automaton: %s\nPinned: %s\n%s\n%s", prdID, unit, msg, outcome)
	alert(level, modelNotServedAlertTitle, body)
}

// recordModelWarning appends the model_not_served_warning Decision to prd when
// a pin proceeded in warn mode. It does nothing for an empty warning.
func recordModelWarning(prd *PRD, warning, actor string) {
	if warning == "" {
		return
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: modelNotServedWarningKind, Actor: actor, Note: warning,
	})
}

// checkPinnedModelHosted runs the pin-time check for a task or story worker
// pin. The backend checked is the one the executor would run the pinned model
// with, resolved through the task, story and Automaton exactly as a run does.
//
// node is the compute node the unit is pinned to, or "" when it is not pinned:
// a pinned unit is checked against its node, so a model the pinned node does
// not serve is refused or warned like any other.
func (m *Manager) checkPinnedModelHosted(prdID, unit string, task *Task, story *Story, prd *PRD, model, node string) (string, error) {
	if model == "" {
		return "", nil
	}
	backend, _ := resolveTaskBackendModel(task, story, prd, m.AutomationForPRD(prd), "")
	return m.checkModelHostedOn(prdID, unit, backend, model, node)
}

// checkLevelModelHosted runs the pin-time check for a call that sets a model
// through a presence-aware pointer (an Automaton worker or a verifier at any
// level). A call that leaves the model out, or clears it, is not checked. The
// backend is the one the call sets, or the level's current one.
func (m *Manager) checkLevelModelHosted(prdID, unit string, backend, model *string, curBackend string) (string, error) {
	if model == nil || *model == "" {
		return "", nil
	}
	return m.checkModelHosted(prdID, unit, pickString(backend, curBackend), *model, "")
}

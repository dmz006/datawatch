package agents

import (
	"context"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// TeardownPushReport is the outcome of the push-first step that runs
// inside Manager.Terminate, BEFORE the driver tears the worker down.
//
//   Attempted — the driver actually tried to run the push (false when
//     the driver cannot run commands in the worker, e.g. the k8s
//     driver, or when no container existed).
//   Pushed    — the worker's branch reached its remote (plain,
//     fast-forward-only push; never a force).
//   Count     — commits on the worker's HEAD that were not present on
//     any remote ref at teardown time (0 when unknown).
//   Branch    — the branch that was (attempted to be) pushed.
//   Reason    — operator-readable failure detail; empty on success.
type TeardownPushReport struct {
	Attempted bool   `json:"attempted"`
	Pushed    bool   `json:"pushed"`
	Count     int    `json:"count"`
	Branch    string `json:"branch,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// TeardownPusher is the optional capability a Driver implements when
// it can run commands inside the worker container and therefore push
// the worker's unpushed commits back to its remote before the
// container is destroyed. The Manager calls PushUnpushed with a
// bounded context inside Terminate, strictly before Driver.Terminate,
// so a worker that dies on teardown does not take its unpushed
// commits with it.
//
// Contract:
//
//   PushUnpushed — best-effort. It must NEVER return a value or an
//     error that blocks termination; the Manager caps the call with a
//     timeout, records the report on the Agent, and still runs
//     Driver.Terminate no matter what happened here.
type TeardownPusher interface {
	PushUnpushed(ctx context.Context, a *Agent) (TeardownPushReport, error)
}

// TeardownPushTimeout caps the push-first step inside Terminate, on
// top of the caller's context.
const TeardownPushTimeout = 2 * time.Minute

// runTeardownPush is the Manager's push-first step. It runs strictly
// before Driver.Terminate so a worker destroyed on teardown does not
// lose its unpushed commits.
//
// Policy: when Manager.PushPolicy is set and reports the "never"
// (git.push_on_complete=never) policy the step is skipped entirely —
// no report, no hook.
//
// Failure semantics: a pusher error or a timed-out push is recorded
// on the Agent just like a plain failed push and surfaced via
// Manager.OnTeardownPushFailure; neither blocks termination. The hook
// fires whenever a report exists and no push succeeded (including
// drivers that report Attempted=false, like the k8s driver).
func (m *Manager) runTeardownPush(ctx context.Context, a *Agent, driver Driver) {
	if m.PushPolicy != nil {
		policy := strings.ToLower(strings.TrimSpace(m.PushPolicy()))
		if policy == string(git.PushNever) {
			return
		}
	}
	pusher, ok := driver.(TeardownPusher)
	if !ok {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, TeardownPushTimeout)
	rep, perr := pusher.PushUnpushed(pctx, a)
	cancel()
	if perr != nil {
		rep = TeardownPushReport{
			Attempted: true,
			Branch:    a.Branch,
			Reason:    "teardown push: " + perr.Error(),
		}
	}
	if rep.Branch == "" {
		rep.Branch = a.Branch
	}
	m.mu.Lock()
	cp := rep
	a.TeardownPush = &cp
	m.mu.Unlock()
	if !rep.Pushed && m.OnTeardownPushFailure != nil {
		m.OnTeardownPushFailure(a, rep)
	}
}

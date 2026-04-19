// F10 sprint 8 (S8.5) — Cloud Foundry driver placeholder.
//
// CF support is a deliberate non-implementation in F10: the
// architecture must accommodate it (which the Driver interface
// already does — same as DockerDriver / K8sDriver), but no
// concrete impl ships in this release. The Cluster Profile schema
// already accepts kind=cf so operators can author profiles ahead
// of time; spawning against a CF cluster fails fast with a clear
// error pointing operators at the open future-work item.
//
// When CF concrete support lands, this file gets the shell-out to
// `cf` CLI (mirroring docker / kubectl drivers). The interface is
// stable; no caller updates required.

package agents

import (
	"context"
	"errors"
)

// ErrCFNotImplemented is returned by every CFDriver method until
// the concrete implementation lands. Callers should surface the
// error verbatim so operators see exactly which path is unbacked.
var ErrCFNotImplemented = errors.New(
	"cf driver: not implemented (F10 plan accepts kind=cf at the schema level " +
		"but the concrete spawner is future work — see docs/plans/2026-04-17-ephemeral-agents.md S8.5)",
)

// CFDriver is the Cloud Foundry stub.
type CFDriver struct{}

// NewCFDriver returns the stub.
func NewCFDriver() *CFDriver { return &CFDriver{} }

// Kind implements Driver.
func (*CFDriver) Kind() string { return "cf" }

// Spawn returns ErrCFNotImplemented.
func (*CFDriver) Spawn(_ context.Context, _ *Agent) error { return ErrCFNotImplemented }

// Status returns ErrCFNotImplemented.
func (*CFDriver) Status(_ context.Context, _ *Agent) (State, error) {
	return "", ErrCFNotImplemented
}

// Logs returns ErrCFNotImplemented.
func (*CFDriver) Logs(_ context.Context, _ *Agent, _ int) (string, error) {
	return "", ErrCFNotImplemented
}

// Terminate returns ErrCFNotImplemented.
func (*CFDriver) Terminate(_ context.Context, _ *Agent) error { return ErrCFNotImplemented }

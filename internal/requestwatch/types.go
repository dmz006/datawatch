package requestwatch

import (
	"context"
	"time"
)

// State is what the watch currently believes about a session's model request.
type State string

const (
	// StateGenerating means the node showed a sign of progress at the last poll.
	StateGenerating State = "generating"
	// StateIdle means the node was readable and showed no sign of progress.
	StateIdle State = "idle"
	// StateUnreachable means the node could not be read at the last poll.
	StateUnreachable State = "unreachable"
	// StateUnknown means no sample has been taken yet.
	StateUnknown State = "unknown"
)

// Roles a watched session can have.
const (
	RoleWorker   = "worker"
	RoleVerifier = "verifier"
)

// NodeRef names the compute node a session's model runs on.
type NodeRef struct {
	// Name is the node's registered name.
	Name string
	// Address is the node's base URL or host:port.
	Address string
	// Kind is the node's server kind.
	Kind string
	// ObserverPeer is the attached observer peer, empty when none is attached.
	ObserverPeer string
}

// Sample is one reading of a node's signal for one model.
type Sample struct {
	// Fingerprint changes whenever the node's state for the model changes.
	Fingerprint string
	// ModelLoaded reports whether the node has the model resident.
	ModelLoaded bool
	// GPUPercent is the node's GPU utilisation, valid only when GPUKnown.
	GPUPercent float64
	// GPUKnown reports whether GPUPercent was read.
	GPUKnown bool
}

// Source reads the node signal. An error means the node could not be read.
// An implementation must never make a model call.
type Source interface {
	Sample(ctx context.Context, node NodeRef, model string) (Sample, error)
}

// Params are the thresholds the tracker applies. They are read fresh on every
// Observe call so a configuration change applies at the next poll.
type Params struct {
	// NoProgress is how long without progress counts as over the threshold.
	// Zero or less never fires.
	NoProgress time.Duration
	// UseGPU lets a busy GPU reading count as progress.
	UseGPU bool
	// GPUBusyPercent is the utilisation at or above which the GPU is busy.
	GPUBusyPercent float64
	// UnreachableCounts keeps unreachable time counting toward no progress.
	UnreachableCounts bool
}

// Observation is the watch's report for one session.
type Observation struct {
	SessionID         string `json:"session_id"`
	AutomatonID       string `json:"automaton_id"`
	TaskID            string `json:"task_id"`
	Role              string `json:"role"`
	Node              string `json:"node"`
	Model             string `json:"model"`
	State             State  `json:"state"`
	LastChange        string `json:"last_change,omitempty"`
	NoProgressSeconds int64  `json:"no_progress_seconds"`
	LastPoll          string `json:"last_poll,omitempty"`
	OverThreshold     bool   `json:"over_threshold"`
	Error             string `json:"error,omitempty"`
	Summary           string `json:"summary"`
}

// formatTime renders t as RFC 3339 in UTC, or "" for the zero time.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

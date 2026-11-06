package server

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

func TestDeployHeldLeaseCountNilLedger(t *testing.T) {
	n, err := sessionRestartSource{}.HeldLeaseCount(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("got %d, %v", n, err)
	}
}

func TestDeployHeldLeaseCountReadsLedger(t *testing.T) {
	l := capacity.New(capacity.Options{})
	n, err := sessionRestartSource{ledger: l}.HeldLeaseCount(context.Background())
	if err != nil || n != len(l.Snapshot().Leases) {
		t.Fatalf("got %d, %v", n, err)
	}
}

// The sequence runner must record its pre-restart baseline from the same
// source the resume side compares against, not the zero source.
func TestDeployRunnerUsesGivenRestartSource(t *testing.T) {
	src := sessionRestartSource{}
	r := newDeployRunner(t.TempDir(), config.DeployConfig{}, src)
	if _, ok := r.Restart.(sessionRestartSource); !ok {
		t.Fatalf("runner restart source = %T, want sessionRestartSource", r.Restart)
	}
	if _, ok := newDeployRunner(t.TempDir(), config.DeployConfig{}, nil).Restart.(noRestartRecord); !ok {
		t.Fatal("nil source should fall back to noRestartRecord")
	}
}

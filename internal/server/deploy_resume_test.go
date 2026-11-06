package server

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
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

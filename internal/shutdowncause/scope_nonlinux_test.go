//go:build !linux

package shutdowncause

import "testing"

// scopeProcScanToDescendants is a no-op: the procscan is Linux-only.
func scopeProcScanToDescendants(t *testing.T, root int) { t.Helper() }

//go:build !linux

package shutdowncause

import (
	"io"
	"testing"
)

// awaitChildReady needs /proc; the only caller skips itself off Linux.
func awaitChildReady(t *testing.T, _ io.Reader, _ int, _ string) {
	t.Helper()
	t.Skip("child readiness check requires /proc (Linux only)")
}

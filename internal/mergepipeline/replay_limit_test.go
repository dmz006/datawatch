//go:build unix

package mergepipeline

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/proclimit"
)

// noTreeLimit stands in where this system cannot give the limit, so that the
// shadow's other behaviour is still tested there. The tests of the limit
// itself skip on such a system.
type noTreeLimit struct{}

func (noTreeLimit) Available(context.Context) error { return nil }

func (noTreeLimit) Prepare(*exec.Cmd, time.Duration) (func() proclimit.Result, error) {
	return func() proclimit.Result { return proclimit.Result{} }, nil
}

func (noTreeLimit) Describe() string { return "no limit: a test on a system without one" }

// testLimit is the limit the sandbox shadows run under: this system's own
// where it can be had.
func testLimit(t *testing.T) proclimit.Starter {
	t.Helper()
	if err := proclimit.Host().Available(context.Background()); err != nil {
		t.Logf("no tree limit here: %v", err)
		return noTreeLimit{}
	}
	return nil
}

package autonomous

import (
	"errors"
	"fmt"
	"testing"

	pibackend "github.com/dmz006/datawatch/internal/llm/backends/pi"
)

// A launch deferred by the pi preflight is classified as model unresponsive,
// so the executor requeues it without consuming an attempt.
func TestLaunchDeferredClassifiedModelUnresponsive(t *testing.T) {
	err := fmt.Errorf("pi-prompt launch: %w: DATAWATCH_PI_FAILED: node n1 not launched, Request timed out: probe failed", pibackend.ErrLaunchDeferred)
	class, ok := ClassifyInfraFailure(err)
	if !ok || class != InfraClassModelUnresponsive {
		t.Fatalf("class=%q ok=%v", class, ok)
	}
	if !errors.Is(err, pibackend.ErrLaunchDeferred) {
		t.Fatal("lost sentinel")
	}
}

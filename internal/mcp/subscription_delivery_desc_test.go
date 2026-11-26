// The subscription list tool says what the delivery state of a one-time
// message means.

package mcp

import (
	"strings"
	"testing"
)

func TestSubscriptionListToolDescribesTheDeliveryState(t *testing.T) {
	got := (&Server{}).toolAutonomousSubscriptions().Description
	for _, want := range []string{"delivery", "not_typed", "review_needed"} {
		if !strings.Contains(got, want) {
			t.Errorf("description lacks %q: %s", want, got)
		}
	}
}

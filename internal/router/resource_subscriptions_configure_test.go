// Comm-channel configure path for mcp.resource_subscriptions: the key and
// value are forwarded to the daemon's config patch path, and the configure
// key list names the key.

package router

import (
	"strings"
	"testing"
)

func TestResourceSubscriptionConfigureForwardedToDaemon(t *testing.T) {
	var gotKey, gotValue string
	sent := runConfigureCapture(t, func(key, value string) error {
		gotKey, gotValue = key, value
		return nil
	}, "mcp.resource_subscriptions=true")
	if gotKey != "mcp.resource_subscriptions" || gotValue != "true" {
		t.Fatalf("forwarded %q=%q, want mcp.resource_subscriptions=true", gotKey, gotValue)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "mcp.resource_subscriptions") {
		t.Fatalf("sent = %q, want confirmation naming the key", sent)
	}
}

func TestResourceSubscriptionConfigureListNamesKey(t *testing.T) {
	sent := runConfigureCapture(t, func(string, string) error { return nil }, "list")
	if len(sent) != 1 || !strings.Contains(sent[0], "mcp.resource_subscriptions") {
		t.Fatalf("configure list = %q, want it to name mcp.resource_subscriptions", sent)
	}
}

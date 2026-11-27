package federation_test

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

// delivery:read goes wherever pause:read goes.
func TestDeliveryReadFollowsPauseRead(t *testing.T) {
	for _, g := range federation.ListBuiltinGroups() {
		got := federation.Resolve([]string{g.Name}, nil)
		want := federation.Check(got, federation.CapPauseRead)
		if has := federation.Check(got, federation.CapDeliveryRead); has != want {
			t.Errorf("%s: delivery:read = %v, pause:read = %v", g.Name, has, want)
		}
	}
}

// delivery:write is held by full-control and the session default only. The
// rule about who may act is recorded, not enforced; no peer group carries it.
func TestDeliveryWriteGrants(t *testing.T) {
	want := map[string]bool{"full-control": true, "session-default": true}
	for _, g := range federation.ListBuiltinGroups() {
		has := federation.Check(federation.Resolve([]string{g.Name}, nil), federation.CapDeliveryWrite)
		if has != want[g.Name] {
			t.Errorf("%s: delivery:write = %v, want %v", g.Name, has, want[g.Name])
		}
	}
}

func TestDeliveryCapNames(t *testing.T) {
	if federation.CapDeliveryRead != "delivery:read" || federation.CapDeliveryWrite != "delivery:write" {
		t.Fatalf("cap names: %q %q", federation.CapDeliveryRead, federation.CapDeliveryWrite)
	}
}

func TestDeliveryMCPToolCaps(t *testing.T) {
	want := map[string]string{
		"delivery_status":       federation.CapDeliveryRead,
		"delivery_check":        federation.CapDeliveryRead,
		"delivery_ack":          federation.CapDeliveryWrite,
		"delivery_accept":       federation.CapDeliveryWrite,
		"delivery_drop":         federation.CapDeliveryWrite,
		"delivery_test_message": federation.CapDeliveryWrite,
	}
	for tool, cp := range want {
		if got := federation.MCPToolCap[tool]; got != cp {
			t.Errorf("MCPToolCap[%s] = %q, want %q", tool, got, cp)
		}
	}
}

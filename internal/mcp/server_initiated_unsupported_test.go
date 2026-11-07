package mcp

import (
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// mcp-go v1.x returns ErrServerInitiatedRequestUnsupported for clients on
// protocol 2026-07-28+; sampling must degrade like "no capable client".
func TestIsNoSessionErr_ServerInitiatedUnsupported(t *testing.T) {
	wrapped := fmt.Errorf("%q: %w", "sampling/createMessage", server.ErrServerInitiatedRequestUnsupported)
	if !isNoSessionErr(wrapped) {
		t.Fatal("ErrServerInitiatedRequestUnsupported must map to the no-session fallback")
	}
	if isNoSessionErr(fmt.Errorf("boom")) {
		t.Fatal("unrelated error must not map")
	}
}

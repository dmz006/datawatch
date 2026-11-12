package mcp

import (
	"fmt"
	"os"
	"strings"
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

// The daemon server must opt in to legacy server-initiated requests so that
// sampling and elicitation keep working for 2026-07-28+ clients.
func TestServerOptsInToLegacyServerInitiatedRequests(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "server.WithLegacyServerInitiatedRequests()") {
		t.Fatal("NewMCPServer must pass WithLegacyServerInitiatedRequests")
	}
}

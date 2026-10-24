// Comm-channel configure path for mcp.public_url: the operator types
// `configure mcp.public_url=<url>` and the key and value are forwarded to
// the daemon's PUT /api/config patch path (the configure func), which
// validates the value. A rejection is reported back to the operator.

package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func runConfigureCapture(t *testing.T, fn func(key, value string) error, text string) []string {
	t.Helper()
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		sent []string
	)
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(fn)
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), sent...)
}

func TestCommConfigure_MCPPublicURLForwardedToDaemon(t *testing.T) {
	var gotKey, gotValue string
	sent := runConfigureCapture(t, func(key, value string) error {
		gotKey, gotValue = key, value
		return nil
	}, "mcp.public_url=https://host.tailnet.ts.net:8081")
	if gotKey != "mcp.public_url" || gotValue != "https://host.tailnet.ts.net:8081" {
		t.Fatalf("forwarded %q=%q, want mcp.public_url=https://host.tailnet.ts.net:8081", gotKey, gotValue)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "mcp.public_url") {
		t.Fatalf("sent = %q, want confirmation naming the key", sent)
	}
}

func TestCommConfigure_MCPPublicURLRejectionReported(t *testing.T) {
	sent := runConfigureCapture(t, func(string, string) error {
		return errors.New("API returned 400")
	}, "mcp.public_url=ftp://x")
	if len(sent) != 1 || !strings.Contains(sent[0], "Configure failed") {
		t.Fatalf("sent = %q, want a Configure failed message", sent)
	}
}

func TestCommConfigureList_MentionsMCPPublicURL(t *testing.T) {
	sent := runConfigureCapture(t, func(string, string) error { return nil }, "list")
	if len(sent) != 1 || !strings.Contains(sent[0], "mcp.public_url") {
		t.Fatalf("configure list = %q, want it to name mcp.public_url", sent)
	}
}

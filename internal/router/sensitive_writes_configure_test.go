// Chat `configure mcp.allow_sensitive_writes=<bool>` is accepted the same way
// as the other mcp.* keys: the router hands the full key and the typed value
// to the config set path, replies with the apply confirmation, and the key is
// listed. A key the config set path does not know is still refused.

package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestSensitiveWritesConfigureVerb(t *testing.T) {
	var mu sync.Mutex
	var gotKey, gotValue string
	var sent []string
	wg := sync.WaitGroup{}
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		mu.Lock()
		defer mu.Unlock()
		gotKey, gotValue = key, value
		if key != "mcp.allow_sensitive_writes" {
			return errors.New("unknown config key " + key)
		}
		return nil
	})

	run := func(text string) (string, string, []string) {
		mu.Lock()
		gotKey, gotValue, sent = "", "", nil
		mu.Unlock()
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		return gotKey, gotValue, append([]string(nil), sent...)
	}

	t.Run("set false", func(t *testing.T) {
		key, value, out := run("mcp.allow_sensitive_writes=false")
		if key != "mcp.allow_sensitive_writes" || value != "false" {
			t.Fatalf("configureFn got %q = %q", key, value)
		}
		want := "[h] Set mcp.allow_sensitive_writes = false. Restart may be required."
		if len(out) != 1 || out[0] != want {
			t.Fatalf("reply = %v, want %q", out, want)
		}
	})

	t.Run("set true", func(t *testing.T) {
		key, value, out := run("mcp.allow_sensitive_writes=true")
		if key != "mcp.allow_sensitive_writes" || value != "true" {
			t.Fatalf("configureFn got %q = %q", key, value)
		}
		if len(out) != 1 || !strings.Contains(out[0], "Set mcp.allow_sensitive_writes = true") {
			t.Fatalf("reply = %v", out)
		}
	})

	t.Run("unknown key regression", func(t *testing.T) {
		key, _, out := run("mcp.allow_sensitive_writez=false")
		if key != "mcp.allow_sensitive_writez" {
			t.Fatalf("configureFn got key %q", key)
		}
		if len(out) != 1 || !strings.Contains(out[0], "Configure failed") || !strings.Contains(out[0], "unknown config key mcp.allow_sensitive_writez") {
			t.Fatalf("an unknown key must be refused, got %v", out)
		}
		if strings.Contains(out[0], "Set mcp.") {
			t.Fatalf("an unknown key must not be confirmed as set: %v", out)
		}
	})

	t.Run("listed", func(t *testing.T) {
		_, _, out := run("list")
		if len(out) != 1 || !strings.Contains(out[0], "mcp.allow_sensitive_writes") {
			t.Fatalf("configure list does not name the key:\n%v", out)
		}
	})
}

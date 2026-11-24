// PUT /api/config says, per key, whether the change is live now or needs a
// restart, so the CLI, MCP, chat and web surfaces can tell the operator.

package server

import (
	"encoding/json"
	"reflect"
	"testing"
)

func decodeKeyList(t *testing.T, resp map[string]json.RawMessage, field string) []string {
	t.Helper()
	raw, ok := resp[field]
	if !ok {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", field, err)
	}
	return out
}

func TestPutConfig_ListsLiveAndRestartKeys(t *testing.T) {
	s, _ := newAllowedRootsServer(t)
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.tmux_kill_guard": false,
		"server.port":                18999,
	})
	if got, want := decodeKeyList(t, resp, "applied"), []string{"autonomous.tmux_kill_guard"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("applied = %v, want %v (response %v)", got, want, resp)
	}
	if got, want := decodeKeyList(t, resp, "requires_restart"), []string{"server.port"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("requires_restart = %v, want %v", got, want)
	}
	if string(resp["status"]) != `"ok"` {
		t.Fatalf("status = %s, want \"ok\"", resp["status"])
	}
}

func TestPutConfig_OnlyLiveKeysOmitsRestartList(t *testing.T) {
	s, _ := newAllowedRootsServer(t)
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.tmux_kill_guard": true})
	if got := decodeKeyList(t, resp, "applied"); !reflect.DeepEqual(got, []string{"autonomous.tmux_kill_guard"}) {
		t.Fatalf("applied = %v", got)
	}
	if _, ok := resp["requires_restart"]; ok {
		t.Fatalf("requires_restart must be absent when no key needs one: %v", resp)
	}
}

// A key the daemon refused to store is in neither list.
func TestPutConfig_SkippedKeyIsInNeitherList(t *testing.T) {
	s, _ := newAllowedRootsServer(t)
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"server.token": "x", "autonomous.tmux_kill_guard": true})
	if got := decodeKeyList(t, resp, "applied"); !reflect.DeepEqual(got, []string{"autonomous.tmux_kill_guard"}) {
		t.Fatalf("applied = %v", got)
	}
	for _, k := range decodeKeyList(t, resp, "requires_restart") {
		if k == "server.token" {
			t.Fatal("a skipped key was listed as needing a restart")
		}
	}
	if got := decodeKeyList(t, resp, "skipped"); !reflect.DeepEqual(got, []string{"server.token"}) {
		t.Fatalf("skipped = %v", got)
	}
}

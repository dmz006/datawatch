package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// newOverrideConfigAPI wires a real manager through the API adapter so the
// tests exercise the same SetConfig merge path REST and MCP use.
func newOverrideConfigAPI(t *testing.T) (*API, *Manager) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewAPI(m), m
}

func TestSetConfig_TaskOverrideStatuses_UnknownNameRejected(t *testing.T) {
	api, m := newOverrideConfigAPI(t)
	before := m.Config().TaskOverrideStatuses

	err := api.SetConfig(json.RawMessage(`{"task_override_statuses":["nope"]}`))
	if err == nil {
		t.Fatal("SetConfig with unknown status: want error, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the unknown status", err)
	}
	if !reflect.DeepEqual(m.Config().TaskOverrideStatuses, before) {
		t.Errorf("rejected Set mutated the live config: got %v, want %v", m.Config().TaskOverrideStatuses, before)
	}
}

func TestSetConfig_TaskOverrideStatuses_ValidListMerged(t *testing.T) {
	api, m := newOverrideConfigAPI(t)
	if err := api.SetConfig(json.RawMessage(`{"task_override_statuses":["blocked","running"]}`)); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	want := []string{"blocked", "running"}
	if got := m.Config().TaskOverrideStatuses; !reflect.DeepEqual(got, want) {
		t.Fatalf("Config().TaskOverrideStatuses = %v, want %v", got, want)
	}
}

func TestSetConfig_TaskOverrideStatuses_AbsentKeyPreservesList(t *testing.T) {
	api, m := newOverrideConfigAPI(t)
	if err := api.SetConfig(json.RawMessage(`{"task_override_statuses":["blocked","running"]}`)); err != nil {
		t.Fatalf("seed SetConfig: %v", err)
	}
	if err := api.SetConfig(json.RawMessage(`{}`)); err != nil {
		t.Fatalf("empty SetConfig: %v", err)
	}
	want := []string{"blocked", "running"}
	if got := m.Config().TaskOverrideStatuses; !reflect.DeepEqual(got, want) {
		t.Fatalf("absent key changed the list: got %v, want %v", got, want)
	}
}

func TestSetConfig_TaskOverrideStatuses_EmptyListAllowed(t *testing.T) {
	api, m := newOverrideConfigAPI(t)
	if err := api.SetConfig(json.RawMessage(`{"task_override_statuses":["blocked"]}`)); err != nil {
		t.Fatalf("seed SetConfig: %v", err)
	}
	if err := api.SetConfig(json.RawMessage(`{"task_override_statuses":[]}`)); err != nil {
		t.Fatalf("explicit [] SetConfig: %v", err)
	}
	if got := m.Config().TaskOverrideStatuses; len(got) != 0 {
		t.Fatalf("explicit [] should clear the stored list, got %v", got)
	}
	if got := EffectiveTaskOverrideStatuses(m.Config().TaskOverrideStatuses); len(got) != 4 {
		t.Fatalf("effective list after [] = %v, want the 4-item default", got)
	}
}

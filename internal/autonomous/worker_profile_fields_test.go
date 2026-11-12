package autonomous

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// Project-layer note: models.go defines no project record type (the project
// profile lives in internal/profile and is not part of this package's models),
// so no project-level worker_profile field is added here. Project overrides
// are carried by the global layer: the global worker_profiles config and the
// role and task-kind defaults apply to every project's workers.

// TestTaskWorkerProfileJSONRoundTrip checks that Task.WorkerProfile survives a
// JSON encode and decode under the worker_profile key.
func TestTaskWorkerProfileJSONRoundTrip(t *testing.T) {
	in := Task{ID: "t1", WorkerProfile: "minimal"}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"worker_profile":"minimal"`) {
		t.Fatalf("encoded task missing worker_profile key: %s", raw)
	}
	var out Task
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.WorkerProfile != "minimal" {
		t.Errorf("WorkerProfile round trip = %q, want %q", out.WorkerProfile, "minimal")
	}
}

// TestTaskWorkerProfileOmittedWhenEmpty checks the omitempty tag: an unset
// task-level override does not appear in the encoded JSON.
func TestTaskWorkerProfileOmittedWhenEmpty(t *testing.T) {
	raw, err := json.Marshal(Task{ID: "t1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "worker_profile") {
		t.Errorf("empty WorkerProfile should be omitted, got %s", raw)
	}
}

// TestPRDWorkerProfileJSONRoundTrip checks that the Automaton (PRD) struct's
// WorkerProfile field survives a JSON round trip under the worker_profile key.
func TestPRDWorkerProfileJSONRoundTrip(t *testing.T) {
	in := PRD{ID: "p1", WorkerProfile: "full"}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"worker_profile":"full"`) {
		t.Fatalf("encoded PRD missing worker_profile key: %s", raw)
	}
	var out PRD
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.WorkerProfile != "full" {
		t.Errorf("WorkerProfile round trip = %q, want %q", out.WorkerProfile, "full")
	}
}

// TestPRDWorkerProfileOmittedWhenEmpty checks the omitempty tag on the PRD
// (Automaton) field.
func TestPRDWorkerProfileOmittedWhenEmpty(t *testing.T) {
	raw, err := json.Marshal(PRD{ID: "p1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "worker_profile") {
		t.Errorf("empty WorkerProfile should be omitted, got %s", raw)
	}
}

// TestValidateWorkerProfileNameEmptyAccepted checks that an empty name is
// accepted, with and without a user map.
func TestValidateWorkerProfileNameEmptyAccepted(t *testing.T) {
	if err := ValidateWorkerProfileName("", nil); err != nil {
		t.Errorf("empty name with nil user map: unexpected error %v", err)
	}
	user := map[string]workerprofile.WorkerProfile{"mine": {Name: "mine"}}
	if err := ValidateWorkerProfileName("", user); err != nil {
		t.Errorf("empty name with user map: unexpected error %v", err)
	}
}

// TestValidateWorkerProfileNameBuiltinAndUser checks that built-in names and
// user-defined names are both accepted.
func TestValidateWorkerProfileNameBuiltinAndUser(t *testing.T) {
	user := map[string]workerprofile.WorkerProfile{"mine": {Name: "mine"}}
	for _, name := range workerprofile.Names() {
		if err := ValidateWorkerProfileName(name, nil); err != nil {
			t.Errorf("built-in %q refused: %v", name, err)
		}
	}
	if err := ValidateWorkerProfileName("mine", user); err != nil {
		t.Errorf("user-defined %q refused: %v", "mine", err)
	}
}

// TestValidateWorkerProfileNameUnknownRefused checks that a name that is
// neither built-in nor user-defined is refused.
func TestValidateWorkerProfileNameUnknownRefused(t *testing.T) {
	user := map[string]workerprofile.WorkerProfile{"mine": {Name: "mine"}}
	if err := ValidateWorkerProfileName("no-such-profile", user); err == nil {
		t.Error("unknown name accepted, want refusal")
	}
	if err := ValidateWorkerProfileName("no-such-profile", nil); err == nil {
		t.Error("unknown name with nil user map accepted, want refusal")
	}
}

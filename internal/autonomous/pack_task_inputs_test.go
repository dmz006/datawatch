package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/packs"
)

func testInputPack() *packs.Pack {
	return &packs.Pack{Inputs: []packs.Input{
		{Name: "branch", Default: "main"},
		{Name: "token", Secret: true, Required: true},
	}}
}

func TestRecordPackTaskInputsStoresRedacted(t *testing.T) {
	task := &Task{ID: "t1"}
	auto := map[string]string{"token": "${secret:GH}"}
	if err := RecordPackTaskInputs(task, testInputPack(), auto, nil, nil); err != nil {
		t.Fatal(err)
	}
	if task.PackInputs["branch"] != "main" || task.PackInputs["token"] != "${secret:GH}" {
		t.Fatalf("got %v", task.PackInputs)
	}
	raw := &Task{ID: "t2"}
	if err := RecordPackTaskInputs(raw, testInputPack(), map[string]string{"token": "plain-secret"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if raw.PackInputs["token"] != "***" {
		t.Fatalf("secret not masked: %v", raw.PackInputs)
	}
}

func TestRecordPackTaskInputsUnchangedByLaterConfig(t *testing.T) {
	task := &Task{ID: "t1"}
	proj := map[string]string{"branch": "dev", "token": "${secret:A}"}
	if err := RecordPackTaskInputs(task, testInputPack(), nil, proj, nil); err != nil {
		t.Fatal(err)
	}
	proj["branch"] = "release"
	proj["token"] = "${secret:B}"
	if task.PackInputs["branch"] != "dev" || task.PackInputs["token"] != "${secret:A}" {
		t.Fatalf("stored values changed: %v", task.PackInputs)
	}
}

func TestRecordPackTaskInputsMissingRequired(t *testing.T) {
	task := &Task{ID: "t1"}
	if err := RecordPackTaskInputs(task, testInputPack(), nil, nil, nil); err == nil {
		t.Fatal("want error")
	}
	if task.PackInputs != nil {
		t.Fatalf("task changed: %v", task.PackInputs)
	}
}

package compute

import (
	"strings"
	"testing"
)

func known(node string, models ...string) ServedModels {
	return ServedModels{Node: node, Kind: "ollama", Models: models, Known: true}
}

func unreadable(node, msg string) ServedModels {
	return ServedModels{Node: node, Kind: "ollama", Models: []string{}, Error: msg}
}

func TestModelCheck_NoNodeServesModelNamesModelAndNodes(t *testing.T) {
	served := []ServedModels{
		known("gpu-a", "llama3:70b", "qwen3:8b", "mistral:7b", "phi4:14b"),
		unreadable("gpu-b", "connection refused"),
	}
	res := CheckModel("qwen3-coder:30b", "", served, "assume_not")
	if !res.Refused {
		t.Fatalf("expected a refusal: %+v", res)
	}
	err := &NoNodeServesModelError{Result: res}
	want := `no registered node serves model "qwen3-coder:30b"; nodes checked: gpu-a (serves 4 models, none is qwen3-coder:30b), gpu-b (could not read models: connection refused)`
	if err.Error() != want {
		t.Fatalf("error text:\n got %s\nwant %s", err.Error(), want)
	}
	for _, s := range []string{"qwen3-coder:30b", "gpu-a", "gpu-b", "connection refused"} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error text lacks %q: %s", s, err.Error())
		}
	}
}

func TestModelCheck_NodeServesModel(t *testing.T) {
	served := []ServedModels{
		known("gpu-a", "llama3:70b"),
		known("gpu-b", "qwen3-coder:30b"),
	}
	res := CheckModel("qwen3-coder:30b", "", served, "assume_serves")
	if res.Refused || len(res.Served) != 1 || res.Served[0] != "gpu-b" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(res.Checked) != 2 || res.Checked[0].State != VerdictNotServing || res.Checked[1].State != VerdictServes {
		t.Fatalf("verdicts wrong: %+v", res.Checked)
	}
}

func TestModelCheck_ProviderPrefixAndLatestTagMatch(t *testing.T) {
	if got := NodeModelName("ollama/qwen3:8b"); got != "qwen3:8b" {
		t.Fatalf("NodeModelName = %q", got)
	}
	if got := NodeModelName("qwen3:8b"); got != "qwen3:8b" {
		t.Fatalf("NodeModelName without prefix = %q", got)
	}
	cases := []struct {
		served, model string
		want          bool
	}{
		{"qwen3:8b", "ollama/qwen3:8b", true},
		{"llama3", "llama3:latest", true},
		{"llama3:latest", "ollama/llama3", true},
		{"llama3:latest", "llama3", true},
		{"llama3:8b", "llama3", false},
		{"qwen3:8b", "qwen3:30b", false},
		{"", "llama3", false},
	}
	for _, c := range cases {
		if got := MatchesModel(c.served, c.model); got != c.want {
			t.Errorf("MatchesModel(%q, %q) = %v, want %v", c.served, c.model, got, c.want)
		}
	}
	res := CheckModel("ollama/llama3", "", []ServedModels{known("gpu-a", "llama3:latest")}, "assume_serves")
	if res.Refused {
		t.Fatalf("prefix and :latest should match: %+v", res)
	}
}

func TestModelCheck_UnknownNodePolicy(t *testing.T) {
	served := []ServedModels{
		known("gpu-a", "llama3:70b"),
		unreadable("gpu-b", "timeout"),
	}
	if res := CheckModel("qwen3:8b", "", served, "assume_serves"); res.Refused || res.Served[0] != "gpu-b" {
		t.Fatalf("assume_serves should let the unreadable node count: %+v", res)
	}
	res := CheckModel("qwen3:8b", "", served, "assume_not")
	if !res.Refused || len(res.Served) != 0 {
		t.Fatalf("assume_not should refuse: %+v", res)
	}
	if res.Checked[1].State != VerdictUnknown {
		t.Fatalf("unreadable node should be reported unknown: %+v", res.Checked[1])
	}
}

func TestModelCheck_PinnedNodeNotServing(t *testing.T) {
	served := []ServedModels{
		known("gpu-a", "llama3:70b"),
		known("gpu-b", "qwen3:8b"),
	}
	res := CheckModel("qwen3:8b", "gpu-a", served, "assume_serves")
	if !res.Refused || len(res.Checked) != 1 || res.Checked[0].Node != "gpu-a" {
		t.Fatalf("pinned node should be the only one looked at: %+v", res)
	}
	msg := (&NoNodeServesModelError{Result: res}).Error()
	if !strings.Contains(msg, `"gpu-a"`) || !strings.Contains(msg, "does not serve") || strings.Contains(msg, "gpu-b") {
		t.Fatalf("pinned error text wrong: %s", msg)
	}
	if res := CheckModel("qwen3:8b", "gpu-b", served, "assume_serves"); res.Refused {
		t.Fatalf("gpu-b serves the model: %+v", res)
	}

	missing := CheckModel("qwen3:8b", "gpu-z", served, "assume_serves")
	if !missing.Refused || !missing.NodeUnknown {
		t.Fatalf("absent pinned node should be refused and unknown: %+v", missing)
	}
	if m := (&NoNodeServesModelError{Result: missing}).Error(); !strings.Contains(m, "gpu-z") || !strings.Contains(m, "does not serve") {
		t.Fatalf("absent pinned error text wrong: %s", m)
	}
}

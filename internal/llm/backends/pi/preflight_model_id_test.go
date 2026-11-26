package pi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// nodeDoer answers like a model server: it knows models only by their own ids
// and answers 404 for any other name, including pi's "provider/id" form.
type nodeDoer struct {
	serves string
	asked  []string
}

func (n *nodeDoer) Do(req *http.Request) (*http.Response, error) {
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, err
	}
	n.asked = append(n.asked, body.Model)
	status := http.StatusOK
	if body.Model != n.serves {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("model '" + body.Model + "' not found"))}, nil
}

// A pi launch names its model "provider/id". The provider is pi's own name for
// the endpoint; the node is asked for the id alone, which may itself hold
// slashes. A node that serves the model must pass the gate.
func TestPreflightAsksNodeForModelWithoutPiProvider(t *testing.T) {
	cases := []struct{ name, model, id string }{
		{"provider prefix", "ollama/qwen3.8-64k:27b", "qwen3.8-64k:27b"},
		{"id with slashes", "ollama/hf.co/org/repo:Q4_K_M", "hf.co/org/repo:Q4_K_M"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mk := &fakeMarker{}
			node := &nodeDoer{serves: tc.id}
			withGate(t, mk, node)
			if err := preflightBeforeLaunch(context.Background(), "node-b:11434", tc.model); err != nil {
				t.Fatalf("launch gate refused a served model: %v", err)
			}
			if len(node.asked) != 1 || node.asked[0] != tc.id {
				t.Fatalf("node asked for %q, want one request for %q", node.asked, tc.id)
			}
			if len(mk.marked) != 0 || len(mk.cleared) != 1 {
				t.Fatalf("marked=%v cleared=%v, want no mark and one clear", mk.marked, mk.cleared)
			}
		})
	}
}

// A model the node does not serve still defers the launch and marks the node.
func TestPreflightStillRefusesUnservedModel(t *testing.T) {
	mk := &fakeMarker{}
	node := &nodeDoer{serves: "other:1b"}
	withGate(t, mk, node)
	if err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/qwen3.8-64k:27b"); err == nil {
		t.Fatal("launch gate passed a model the node does not serve")
	}
	if !strings.Contains(mk.marked["node-x"], "status 404") {
		t.Fatalf("marks = %v", mk.marked)
	}
}

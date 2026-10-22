// Per-node model pinning for local ComputeNodes.
//
// A compute.Node may pin one model (node_pinned_model). Pinning keeps
// that model resident on the node, so datawatch never swaps it
// implicitly:
//
//   - a request with no explicit model runs on the pinned model, even
//     when the caller passed the backend default;
//   - an explicit request for the pinned model runs on it;
//   - an explicit request for a different model is refused unless the
//     node sets allow_model_swap, in which case that model runs.
//
// A node with no pin passes the request through unchanged. A node name
// the registry does not know also passes through, because it carries no
// pin to enforce.

package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/compute"
)

// PinResolver chooses the model a request on a ComputeNode runs,
// honouring that node's pin and allow_model_swap settings.
type PinResolver struct {
	lookup func(name string) (*compute.Node, error)
}

// NewPinResolver builds a resolver that reads node settings through
// lookup. compute.Registry.Get matches the lookup signature directly.
func NewPinResolver(lookup func(name string) (*compute.Node, error)) *PinResolver {
	return &PinResolver{lookup: lookup}
}

// ResolveModel returns the model a request on nodeName should run.
//
// requestedModel is the model the caller would use by default and may
// be empty. explicitOverride is a model the caller asked for by name
// and is empty when the caller did not ask for one. When both are set,
// explicitOverride wins as the target.
//
// It returns an error only when the node cannot be looked up, or when an
// explicit override would replace a pinned model on a node that does not
// allow swapping. Use Generate to also send the request.
func (r *PinResolver) ResolveModel(ctx context.Context, nodeName, requestedModel, explicitOverride string) (string, error) {
	node, err := r.node(nodeName)
	if err != nil {
		return "", err
	}
	return pinnedModel(node, nodeName, requestedModel, explicitOverride)
}

// Generate resolves the model for nodeName exactly as ResolveModel does,
// then posts one non-streaming /api/generate request to the node's
// Ollama endpoint. It returns the model that ran and the response text.
// A refused model never reaches the node.
func (r *PinResolver) Generate(ctx context.Context, nodeName, requestedModel, explicitOverride, prompt string) (string, string, error) {
	node, err := r.node(nodeName)
	if err != nil {
		return "", "", err
	}
	if node == nil {
		return "", "", fmt.Errorf("compute node %q not found", nodeName)
	}
	if strings.TrimSpace(node.Address) == "" {
		return "", "", fmt.Errorf("compute node %q has no address", nodeName)
	}
	model, err := pinnedModel(node, nodeName, requestedModel, explicitOverride)
	if err != nil {
		return "", "", err
	}
	if model == "" {
		return "", "", fmt.Errorf("compute node %q: no model (pin one or pass a model)", nodeName)
	}
	body, err := json.Marshal(map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	})
	if err != nil {
		return "", "", fmt.Errorf("ollama: encode request: %w", err)
	}
	url := strings.TrimRight(node.Address, "/") + "/api/generate"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", "", &ErrTransient{Err: fmt.Errorf("ollama: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", &ErrTransient{Err: fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("ollama decode: %w", err)
	}
	return model, strings.TrimSpace(out.Response), nil
}

// node returns the named ComputeNode, or nil with no error when the
// registry does not know the name. Any other lookup failure is returned.
func (r *PinResolver) node(nodeName string) (*compute.Node, error) {
	node, err := r.lookup(nodeName)
	if errors.Is(err, compute.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("compute node %q: %w", nodeName, err)
	}
	return node, nil
}

// pinnedModel applies the pin rules to one request. A nil node means the
// name is unconfigured, so the request passes through.
func pinnedModel(node *compute.Node, nodeName, requestedModel, explicitOverride string) (string, error) {
	target := requestedModel
	explicit := explicitOverride != ""
	if explicit {
		target = explicitOverride
	}
	if node == nil || node.NodePinnedModel == "" {
		return target, nil
	}
	pinned := node.NodePinnedModel
	// Implicit requests and explicit requests for the pinned model both
	// run on the pin. Datawatch never swaps a pinned model implicitly.
	if !explicit || target == pinned {
		return pinned, nil
	}
	if node.AllowModelSwap {
		return target, nil
	}
	return "", fmt.Errorf("refused: node %s is pinned to %s; request for %s would evict it (set compute allow_model_swap for this node to allow)", nodeName, pinned, target)
}

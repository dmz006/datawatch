// Model loading for compute nodes that serve one LLM entry from two or more
// nodes.
//
// Once admission picks a node for a worker, that node must have the entry's
// model resident, or the worker's first request pays a load that can take
// minutes. ModelLoader checks the node's running-models listing and, when the
// model is absent, asks ollama to load it and waits until the listing shows it
// or a bounded wait ends.
//
// Pins come first. A node with node_pinned_model always loads its pin for an
// implicit request (see pinnedModel), so a load never swaps a pinned model out.

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
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

const (
	// loadPSTimeout bounds one GET /api/ps check.
	loadPSTimeout = 5 * time.Second
	// loadRequestTimeout bounds the load request. Ollama holds the request
	// until the model is in memory, which can take minutes for a large model.
	loadRequestTimeout = 10 * time.Minute
	// loadKeepAlive keeps a loaded model resident after its last use. A
	// running worker sends the model with each request, which renews it.
	loadKeepAlive = "10m"
	// DefaultLoadWait bounds how long a node may take to report a model loaded.
	DefaultLoadWait = 10 * time.Minute
	// defaultLoadPoll is the time between running-model checks while waiting.
	defaultLoadPoll = 2 * time.Second
	// maxLoadErrBody caps how much of a failed response is quoted in an error.
	maxLoadErrBody = 512
)

// ModelLoader makes a model resident on a compute node. The zero value is
// ready to use.
type ModelLoader struct {
	// Client sends the ollama requests. Nil uses http.DefaultClient.
	Client *http.Client
	// Wait bounds how long the model may take to show as loaded. Zero uses
	// DefaultLoadWait.
	Wait time.Duration
	// Poll is the time between running-model checks while waiting. Zero uses
	// defaultLoadPoll.
	Poll time.Duration
}

// NodeLoadError reports that a node could not make a model resident: it is
// unreachable, the load request failed, or the model did not show as loaded
// within the wait.
type NodeLoadError struct {
	Node  string
	Model string
	Cause error
}

func (e *NodeLoadError) Error() string {
	return fmt.Sprintf("node %s could not load %s: %v", e.Node, e.Model, e.Cause)
}

// Unwrap returns the cause, so errors.Is reaches the underlying failure.
func (e *NodeLoadError) Unwrap() error { return e.Cause }

// Ensure makes model resident on node before a worker spawns there. The
// model that loads is the node's pin when it has one, otherwise model.
//
// wait is called with a reason text when the node has to load the model. It
// is not called when the model is already resident. Ensure returns a
// *NodeLoadError when the node cannot make the model resident, and the
// context's error when ctx ends first.
func (l *ModelLoader) Ensure(ctx context.Context, node *compute.Node, model string, wait func(reason string)) error {
	if node == nil {
		return &NodeLoadError{Model: model, Cause: errors.New("unknown compute node")}
	}
	target, err := pinnedModel(node, node.Name, model, "")
	if err != nil {
		return &NodeLoadError{Node: node.Name, Model: model, Cause: err}
	}
	if strings.TrimSpace(node.Address) == "" {
		return &NodeLoadError{Node: node.Name, Model: target, Cause: errors.New("node has no address")}
	}
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resident, err := loadResident(ctx, client, node.Address, target)
	if err != nil {
		return loadFailure(node, target, fmt.Errorf("unreachable: %w", err))
	}
	if resident {
		return nil
	}
	if wait != nil {
		wait(fmt.Sprintf("loading %s on node %s", target, node.Name))
	}
	if err := requestLoad(ctx, client, node.Address, target); err != nil {
		return loadFailure(node, target, err)
	}
	bound := l.Wait
	if bound <= 0 {
		bound = DefaultLoadWait
	}
	poll := l.Poll
	if poll <= 0 {
		poll = defaultLoadPoll
	}
	deadline := time.Now().Add(bound)
	for {
		resident, err := loadResident(ctx, client, node.Address, target)
		if err != nil {
			return loadFailure(node, target, fmt.Errorf("unreachable: %w", err))
		}
		if resident {
			return nil
		}
		if !time.Now().Before(deadline) {
			return loadFailure(node, target, fmt.Errorf("not loaded after %s", bound))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// loadFailure wraps a cause as the NodeLoadError for node and model.
func loadFailure(node *compute.Node, model string, cause error) error {
	return &NodeLoadError{Node: node.Name, Model: model, Cause: cause}
}

// loadResident reports whether ollama at base lists model among the models
// resident on the node (GET /api/ps).
func loadResident(ctx context.Context, client *http.Client, base, model string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, loadPSTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/ps", nil)
	if err != nil {
		return false, fmt.Errorf("ollama /api/ps: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("ollama /api/ps: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ollama /api/ps: HTTP %d", resp.StatusCode)
	}
	var ps struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		return false, fmt.Errorf("ollama /api/ps decode: %w", err)
	}
	for _, m := range ps.Models {
		if sameModelName(m.Name, model) || sameModelName(m.Model, model) {
			return true, nil
		}
	}
	return false, nil
}

// requestLoad asks ollama at base to load model without generating: a
// non-streaming /api/generate with no prompt. The call returns once the model
// is in memory.
func requestLoad(ctx context.Context, client *http.Client, base, model string) error {
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"prompt":     "",
		"stream":     false,
		"keep_alive": loadKeepAlive,
	})
	if err != nil {
		return fmt.Errorf("encode load request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, loadRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama /api/generate: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama /api/generate: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxLoadErrBody))
		return fmt.Errorf("ollama /api/generate: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// sameModelName compares two ollama model names. A name without a tag means
// the latest tag, so "qwen3" and "qwen3:latest" name the same model.
func sameModelName(a, b string) bool {
	return withLatestTag(a) == withLatestTag(b)
}

func withLatestTag(name string) string {
	name = strings.TrimSpace(name)
	if !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		return name + ":latest"
	}
	return name
}

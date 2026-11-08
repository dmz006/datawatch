// Package inference — pulling a model onto a compute node.
//
// Two callers need to move a model from "not on this node" to "here": the
// marketplace's background pull worker (operator asked for the pull) and the
// model loader (the dispatcher picked a node that must end up with the model
// resident). Both speak the same Ollama /api/pull contract: a streaming,
// line-delimited JSON progress feed ending in a success line (or an error
// line). PullModel owns that one implementation; each caller decides what to
// do with the progress report and the terminal error.

package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PullProgress is one report from a model pull. Callers treat it as a
// snapshot of the pull's current state, never as a delta.
type PullProgress struct {
	// Status is the status text of the newest progress line.
	Status string
	// BytesTotal / BytesDone come from the newest line that carries byte
	// counters. Both are zero while the pull has not reported sizes yet.
	BytesTotal int64
	BytesDone  int64
	// Percent is BytesDone/BytesTotal in 0..1; meaningful only when
	// BytesTotal is positive.
	Percent float64
	// Success is true only on the final line that reports a completed pull.
	Success bool
}

// PullModel pulls model onto the Ollama server at addr and returns when the
// pull completes or fails. progress, when non-nil, is called for every
// progress line, including the final success report, and must be safe for
// use from a single goroutine (it is, PullModel runs one stream at a time per
// call). A nil client uses http.DefaultClient; a caller that needs a longer
// horizon or its own TLS policy passes a client — a pull of a large model
// can run for a long time and nothing here imposes its own deadline, so the
// context and client are the only bounds.
func PullModel(ctx context.Context, client *http.Client, addr, model string, progress func(PullProgress)) error {
	if strings.TrimSpace(addr) == "" {
		return fmt.Errorf("no node address to pull %s from", model)
	}
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]any{"name": model, "stream": true})
	if err != nil {
		return fmt.Errorf("encode pull request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(addr, "/")+"/api/pull", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("ollama /api/pull: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama /api/pull: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxLoadErrBody))
		return fmt.Errorf("ollama /api/pull: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var msg struct {
			Status    string `json:"status"`
			Total     int64  `json:"total,omitempty"`
			Completed int64  `json:"completed,omitempty"`
			Error     string `json:"error,omitempty"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				// The feed ended without an error line: the pull ran to
				// the end, which is a completion.
				return nil
			}
			return fmt.Errorf("ollama /api/pull: decode error: %w", err)
		}
		if msg.Error != "" {
			return fmt.Errorf("ollama pull of %s failed: %s", model, msg.Error)
		}
		p := PullProgress{Status: msg.Status}
		if msg.Total > 0 {
			p.BytesTotal = msg.Total
			p.BytesDone = msg.Completed
			p.Percent = float64(msg.Completed) / float64(msg.Total)
		}
		if strings.Contains(strings.ToLower(msg.Status), "success") {
			p.Success = true
		}
		if progress != nil {
			progress(p)
		}
	}
}

// modelTagsTimeout bounds one GET /api/tags listing.
const modelTagsTimeout = 5 * time.Second

// nodeHasModel reports whether ollama at base already has model downloaded
// (it appears in the node's /api/tags model list), as opposed to merely
// resident in memory (/api/ps).
func nodeHasModel(ctx context.Context, client *http.Client, base, model string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, modelTagsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/tags", nil)
	if err != nil {
		return false, fmt.Errorf("ollama /api/tags: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("ollama /api/tags: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ollama /api/tags: HTTP %d", resp.StatusCode)
	}
	var tags struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return false, fmt.Errorf("ollama /api/tags decode: %w", err)
	}
	for _, m := range tags.Models {
		if sameModelName(m.Name, model) || sameModelName(m.Model, model) {
			return true, nil
		}
	}
	return false, nil
}

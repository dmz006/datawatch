// preflight.go probes an OpenAI-compatible node with a one-token chat
// completion so a stalled or unreachable model is known before a worker
// is launched against it.
package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// preflightTimeout bounds the whole probe request.
const preflightTimeout = 20 * time.Second

// HTTPDoer is the minimal HTTP client surface the probe needs; *http.Client
// satisfies it and tests supply a fake.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Preflight sends a one-token chat completion (max_tokens 1) to the node at
// baseURL for model. Any transport error, non-2xx status or timeout is
// returned as an error naming the node URL and model.
func Preflight(ctx context.Context, client HTTPDoer, baseURL, model string) error {
	ctx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		return fmt.Errorf("preflight %s model %q: %w", baseURL, model, err)
	}
	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("preflight %s model %q: %w", baseURL, model, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("preflight %s model %q: %w", baseURL, model, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("preflight %s model %q: status %d: %s", baseURL, model, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

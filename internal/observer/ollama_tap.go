// BL180 Phase 1 — ollama runtime tap. Polls the configured ollama
// HTTP endpoint's `/api/ps` for currently-loaded models, and emits
// each loaded model as a sub-envelope of the parent ollama process
// envelope. Operators get per-model attribution (which model is
// eating GPU / RAM right now) without waiting for the full eBPF
// cross-correlation slice (Phase 2).
//
// When the parent process tree includes an `ollama serve` envelope
// AND this tap is enabled, the collector folds the per-model
// metrics in via the loaded model list. We don't claim caller
// attribution in Phase 1 — bare ollama doesn't surface "which client
// triggered this load" — but we do mark the envelope `Caller =
// "<model_name>"`, `CallerKind = "ollama_model"` so the PWA can
// render "ollama → llama3.1:8b → (waiting for caller)".

package observer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OllamaTap polls /api/ps and exposes the loaded-model list as
// per-model sub-envelopes via Snapshot().
type OllamaTap struct {
	endpoint string
	httpc    *http.Client

	mu       sync.Mutex
	loaded   []ollamaLoadedModel
	lastErr  error
	lastSeen time.Time
}

type ollamaLoadedModel struct {
	Name     string `json:"name"`
	Model    string `json:"model"`
	Size     int64  `json:"size"`
	SizeVRAM int64  `json:"size_vram"`
	// ExpiresAt is when ollama will unload if idle. We don't act on
	// it here; just surface in the envelope for operator visibility.
	ExpiresAt time.Time `json:"expires_at"`
	// Details carries Ollama's manifest details for the loaded model;
	// the field is optional — servers that omit it leave it zero.
	Details struct {
		// NumCtx is the context length (in tokens) the model is
		// loaded with, when the server reports it.
		NumCtx int64 `json:"num_ctx"`
	} `json:"details"`
}

// HeldBytes is the total memory the model holds (RAM+VRAM).
func (m ollamaLoadedModel) HeldBytes() int64 { return m.Size + m.SizeVRAM }

// ContextTokens is the context length (in tokens) the model is loaded
// with, from /api/ps's details.num_ctx; zero when the server does not
// report it.
func (m ollamaLoadedModel) ContextTokens() int64 { return m.Details.NumCtx }

// LoadedName is the operator-visible model name: Name, falling back to
// Model when ollama only filled one of the two.
func (m ollamaLoadedModel) LoadedName() string {
	if m.Name != "" {
		return m.Name
	}
	return m.Model
}

// NewOllamaTap constructs a tap. endpoint is the base URL (no
// trailing slash); empty disables (returns nil).
func NewOllamaTap(endpoint string) *OllamaTap {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil
	}
	return &OllamaTap{
		endpoint: endpoint,
		httpc:    &http.Client{Timeout: 3 * time.Second},
	}
}

// Start spawns the polling goroutine. Cancel ctx to stop.
// Polling cadence is fixed at 5 s — ollama loads/unloads aren't
// frequent enough to need finer granularity, and the API is cheap.
func (t *OllamaTap) Start(ctx context.Context) {
	if t == nil {
		return
	}
	go func() {
		t.poll(ctx) // first read immediately so the snapshot is fresh
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				t.poll(ctx)
			}
		}
	}()
}

func (t *OllamaTap) poll(ctx context.Context) {
	if t == nil {
		return
	}
	models, err := t.Read(ctx)
	if err != nil {
		t.setErr(err)
		return
	}
	t.mu.Lock()
	t.loaded = models
	t.lastErr = nil
	t.lastSeen = time.Now()
	t.mu.Unlock()
}

// Read performs a single synchronous /api/ps read and returns the
// models currently loaded with the memory they hold (RAM and VRAM),
// without touching the tap's cached state. Background pollers keep
// using Start/Snapshot; request-driven consumers (e.g. the
// per-service model-use report) use Read for one bounded read.
func (t *OllamaTap) Read(ctx context.Context) ([]ollamaLoadedModel, error) {
	if t == nil {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.endpoint+"/api/ps", nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/ps: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Models []ollamaLoadedModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ollama /api/ps decode: %w", err)
	}
	return body.Models, nil
}

func (t *OllamaTap) setErr(err error) {
	t.mu.Lock()
	t.lastErr = err
	t.mu.Unlock()
}

// Snapshot returns one envelope per currently-loaded model. Caller
// attribution stays empty in Phase 1 — bare ollama doesn't surface
// the requesting client. Returns nil + nil when the tap is
// disabled or no models are loaded.
func (t *OllamaTap) Snapshot() ([]Envelope, error) {
	if t == nil {
		return nil, nil
	}
	t.mu.Lock()
	loaded := append([]ollamaLoadedModel(nil), t.loaded...)
	lastErr := t.lastErr
	t.mu.Unlock()
	if lastErr != nil {
		return nil, lastErr
	}
	if len(loaded) == 0 {
		return nil, nil
	}
	out := make([]Envelope, 0, len(loaded))
	for _, m := range loaded {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		env := Envelope{
			ID:                 "ollama_model:" + name,
			Kind:               EnvelopeBackend,
			Label:              "ollama / " + name,
			RSSBytes:           uint64(m.Size),
			GPUMemBytes:        uint64(m.SizeVRAM),
			Caller:             name,
			CallerKind:         "ollama_model",
			LastActivityUnixMs: time.Now().UnixMilli(),
		}
		out = append(out, env)
	}
	return out, nil
}

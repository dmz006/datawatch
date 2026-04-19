// BL100 — worker-side HTTP memory client.
//
// F10 S6.2 ships the bootstrap memory bundle (Mode + Namespace) +
// DATAWATCH_MEMORY_MODE / DATAWATCH_MEMORY_NAMESPACE env. BL100 is
// the client workers use to route memory ops back through the parent
// rather than maintain their own local SQLite (per the federation
// modes):
//
//   shared    — every Save + Search hits the parent in real time
//   sync-back — Save buffers locally + flushes on session end (or
//               via Flush()); Search hits the parent
//   ephemeral — neither — workers keep their own local store
//
// The HTTPClient covers the Save + Search shape only; the full
// Backend interface (ListRecent, Stats, Prune, …) stays on the
// parent side. Workers that need richer operations should call the
// parent's REST endpoints directly.

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// MemoryMode mirrors profile.MemoryMode at the wire level so this
// package doesn't drag profile in.
type MemoryMode string

const (
	ModeShared    MemoryMode = "shared"
	ModeSyncBack  MemoryMode = "sync-back"
	ModeEphemeral MemoryMode = "ephemeral"
)

// HTTPClient calls the parent's memory REST surface from inside a
// worker. The parent URL + bearer token are read from the bootstrap
// env (DATAWATCH_BOOTSTRAP_URL + DATAWATCH_TOKEN); the namespace +
// mode come from the per-spawn DATAWATCH_MEMORY_* envs.
type HTTPClient struct {
	BaseURL   string     // parent base URL (DATAWATCH_BOOTSTRAP_URL)
	Token     string     // optional bearer token (DATAWATCH_TOKEN)
	Namespace string     // worker's namespace (DATAWATCH_MEMORY_NAMESPACE)
	Profile   string     // optional Project Profile name; used for
	                     // server-side cross-profile expansion (BL101)
	Mode      MemoryMode // federation mode

	HTTP *http.Client // optional override; defaults to a 30s client

	// Sync-back buffer. Saves accumulate here in mode=sync-back and
	// flush on Flush() / session end.
	mu     sync.Mutex
	queued []syncBackEntry
}

type syncBackEntry struct {
	Content    string
	ProjectDir string
	Role       string
	SessionID  string
}

// NewHTTPClientFromEnv reads the bootstrap envs and returns a client
// or nil when memory federation isn't enabled for this worker.
func NewHTTPClientFromEnv() *HTTPClient {
	mode := MemoryMode(os.Getenv("DATAWATCH_MEMORY_MODE"))
	if mode == "" || mode == ModeEphemeral {
		return nil
	}
	base := os.Getenv("DATAWATCH_BOOTSTRAP_URL")
	if base == "" {
		base = os.Getenv("DATAWATCH_PARENT_URL")
	}
	if base == "" {
		return nil
	}
	return &HTTPClient{
		BaseURL:   strings.TrimRight(base, "/"),
		Token:     os.Getenv("DATAWATCH_TOKEN"),
		Namespace: os.Getenv("DATAWATCH_MEMORY_NAMESPACE"),
		Profile:   os.Getenv("DATAWATCH_PROFILE"),
		Mode:      mode,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Remember records a memory. In shared mode the call hits the parent
// synchronously; in sync-back mode it buffers for Flush().
func (c *HTTPClient) Remember(ctx context.Context, projectDir, content, role, sessionID string) error {
	if c == nil {
		return nil
	}
	if c.Mode == ModeSyncBack {
		c.mu.Lock()
		c.queued = append(c.queued, syncBackEntry{
			Content: content, ProjectDir: projectDir, Role: role, SessionID: sessionID,
		})
		c.mu.Unlock()
		return nil
	}
	return c.postSave(ctx, content, projectDir)
}

// Search queries the parent's /api/memory/search. When Profile is
// set the parent performs cross-profile namespace expansion (BL101);
// otherwise the search runs against the global namespace.
func (c *HTTPClient) Search(ctx context.Context, query string) ([]map[string]interface{}, error) {
	if c == nil {
		return nil, nil
	}
	q := url.Values{}
	q.Set("q", query)
	if c.Profile != "" {
		q.Set("profile", c.Profile)
	}
	endpoint := c.BaseURL + "/api/memory/search?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("memory search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("memory search: HTTP %d: %s", resp.StatusCode, body)
	}
	var out []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return out, nil
}

// Flush drains the sync-back buffer to the parent. Safe to call in
// any mode (no-op for shared/ephemeral). Failures stop at the first
// error so partial flushes leave the unfailed-yet entries queued for
// the next call.
func (c *HTTPClient) Flush(ctx context.Context) (int, error) {
	if c == nil || c.Mode != ModeSyncBack {
		return 0, nil
	}
	c.mu.Lock()
	pending := c.queued
	c.queued = nil
	c.mu.Unlock()

	flushed := 0
	for i, ent := range pending {
		if err := c.postSave(ctx, ent.Content, ent.ProjectDir); err != nil {
			// Re-queue the unflushed remainder.
			c.mu.Lock()
			c.queued = append(c.queued, pending[i:]...)
			c.mu.Unlock()
			return flushed, fmt.Errorf("flush at index %d: %w", i, err)
		}
		flushed++
	}
	return flushed, nil
}

// QueuedLen returns the number of entries waiting to flush. Useful
// for tests + UI badges.
func (c *HTTPClient) QueuedLen() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.queued)
}

func (c *HTTPClient) postSave(ctx context.Context, content, projectDir string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"content":     content,
		"project_dir": projectDir,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/api/memory/save", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("memory save: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("memory save: HTTP %d: %s", resp.StatusCode, raw)
	}
	return nil
}

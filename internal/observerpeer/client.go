// Package observerpeer — peer-side registration + push loop for the
// /api/observer/peers/* surface (BL172). Originally lived under
// cmd/datawatch-stats; hoisted here in S13 so cmd/datawatch-agent
// can reuse it for the agent-as-peer flow.
//
// Wire contract (Phase A — bearer token; HMAC sig is a future task):
//
//   First boot:  POST <parent>/api/observer/peers
//                {name, shape, version, host_info}
//                  → {token, …}; persisted to <token-file> 0600.
//
//   Each tick:   POST <parent>/api/observer/peers/{name}/stats
//                Authorization: Bearer <token>
//                {shape, peer_name, snapshot}
//
//   On 401:      drop the cached token + re-register, then retry once.
//                Operators rotate by hitting DELETE on the parent.
//
// The agent-as-peer (S13) flavour skips the register step — the
// parent's Manager.Spawn mints the token + injects it via env.
// SetToken() records the supplied token; the first Push goes
// straight to /stats with the bearer.

package observerpeer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/observer"
)

// ErrUnauthorized is returned when the parent rejects the bearer
// token. Callers wrap it via errors.Is to distinguish "auth bounce
// → re-register" from real transport errors.
var ErrUnauthorized = errors.New("parent rejected token (401)")

// Client is the peer-side handle. Goroutine-safe: token reads/writes
// are mutex-guarded; the embedded http.Client is fine to share.
type Client struct {
	parentURL  string
	name       string
	shape      string // "A" / "B" / "C" — sent in every push body
	tokenPath  string
	insecure   bool
	httpClient *http.Client

	mu    sync.Mutex
	token string
}

// Config bundles the constructor inputs. ParentURL + Name are
// required; everything else has sensible defaults.
type Config struct {
	ParentURL string        // primary datawatch base URL ("https://primary:8443")
	Name      string        // stable peer name (hostname / agent id)
	Shape     string        // "A" agent / "B" standalone / "C" cluster — defaults to "B"
	TokenPath string        // file to persist the bearer token; "" disables persistence
	Insecure  bool          // skip TLS verify (dev / self-signed parent)
	Timeout   time.Duration // per-request timeout; defaults to 10s
}

// New constructs a Client. Ensures the token-file parent dir exists
// with mode 0700 when TokenPath is set.
func New(cfg Config) (*Client, error) {
	parent := strings.TrimRight(cfg.ParentURL, "/")
	if parent == "" {
		return nil, errors.New("parent URL required")
	}
	if cfg.Name == "" {
		return nil, errors.New("peer name required")
	}
	shape := strings.ToUpper(strings.TrimSpace(cfg.Shape))
	if shape == "" {
		shape = "B"
	}
	if cfg.TokenPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.TokenPath), 0o700); err != nil {
			return nil, fmt.Errorf("mkdir token dir: %w", err)
		}
	}
	tr := &http.Transport{}
	if cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		parentURL:  parent,
		name:       cfg.Name,
		shape:      shape,
		tokenPath:  cfg.TokenPath,
		insecure:   cfg.Insecure,
		httpClient: &http.Client{Timeout: timeout, Transport: tr},
	}, nil
}

// LoadToken reads the persisted token from disk if present.
func (c *Client) LoadToken() {
	if c.tokenPath == "" {
		return
	}
	data, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.token = strings.TrimSpace(string(data))
	c.mu.Unlock()
}

// SetToken records a token supplied externally (e.g. injected by the
// parent at agent spawn). Skips the register step on the next Push.
func (c *Client) SetToken(token string) {
	c.mu.Lock()
	c.token = strings.TrimSpace(token)
	c.mu.Unlock()
}

// HasToken reports whether a token is currently held in memory.
func (c *Client) HasToken() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token != ""
}

// SaveToken persists the token to disk via tempfile + rename, mode 0600.
// Caller normally does not need this directly — Register saves on success.
func (c *Client) SaveToken(token string) error {
	if c.tokenPath == "" {
		return nil
	}
	tmp := c.tokenPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(token), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.tokenPath)
}

// Register hits POST /api/observer/peers and stores the resulting
// token both in memory and on disk.
func (c *Client) Register(ctx context.Context, version string, hostInfo map[string]any) error {
	body, _ := json.Marshal(map[string]any{
		"name":      c.name,
		"shape":     c.shape,
		"version":   version,
		"host_info": hostInfo,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.parentURL+"/api/observer/peers", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register %d: %s", resp.StatusCode, string(buf))
	}
	var got struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return fmt.Errorf("register decode: %w", err)
	}
	if got.Token == "" {
		return errors.New("register returned empty token")
	}
	c.mu.Lock()
	c.token = got.Token
	c.mu.Unlock()
	if err := c.SaveToken(got.Token); err != nil {
		// Non-fatal — in-memory token still works; operator just
		// loses persistence across restarts.
		fmt.Fprintf(os.Stderr, "[observerpeer] warn: persist token: %v\n", err)
	}
	return nil
}

// Push sends one snapshot. On 401 it auto-re-registers and retries
// once. version + hostInfo are only used by the implicit re-register.
func (c *Client) Push(ctx context.Context, snap *observer.StatsResponse, version string, hostInfo map[string]any) error {
	return c.PushWithChain(ctx, snap, nil, version, hostInfo)
}

// PushWithChain sends one snapshot annotated with a federation
// chain — the ordered list of peer names the snapshot has already
// transited. The receiving primary rejects pushes whose chain
// already contains itself (S14a loop prevention). Empty chain is
// equivalent to Push (single-hop). On 401 it auto-re-registers
// and retries once.
func (c *Client) PushWithChain(ctx context.Context, snap *observer.StatsResponse, chain []string, version string, hostInfo map[string]any) error {
	if snap == nil {
		return errors.New("nil snapshot")
	}
	if err := c.pushOnce(ctx, snap, chain); err == nil {
		return nil
	} else if !errors.Is(err, ErrUnauthorized) {
		return err
	}
	if err := c.Register(ctx, version, hostInfo); err != nil {
		return fmt.Errorf("re-register after 401: %w", err)
	}
	return c.pushOnce(ctx, snap, chain)
}

// pushOnce sends one snapshot using the currently-cached token.
func (c *Client) pushOnce(ctx context.Context, snap *observer.StatsResponse, chain []string) error {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	if token == "" {
		return ErrUnauthorized
	}
	payload := map[string]any{
		"shape":     c.shape,
		"peer_name": c.name,
		"snapshot":  snap,
	}
	if len(chain) > 0 {
		payload["chain"] = chain
	}
	body, _ := json.Marshal(payload)
	url := c.parentURL + "/api/observer/peers/" + c.name + "/stats"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("push POST: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("push %d", resp.StatusCode)
	}
	return nil
}

// HostInfo returns a minimal envelope of host-identity fields suitable
// for the registration body. Pure read.
func HostInfo() map[string]any {
	host, _ := os.Hostname()
	return map[string]any{
		"hostname": host,
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
	}
}

// BL172 Task 3 — peer-side registration + push loop.
//
// Wire contract (Phase A — bearer token; HMAC sig comes later):
//
//   First boot:  POST <parent>/api/observer/peers
//                {name, shape, version, host_info}
//                  → {token, …}; token persisted to <token-file> 0600.
//
//   Each tick:   POST <parent>/api/observer/peers/{name}/stats
//                Authorization: Bearer <token>
//                {shape, peer_name, snapshot}
//
//   On 401:      drop the cached token + re-register, then retry once.
//                Operators rotate by hitting DELETE on the parent.

package main

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

// peerClient holds the (re-usable) HTTP client + cached token + the
// persistence path for the token file.
type peerClient struct {
	parentURL  string
	name       string
	tokenPath  string
	insecure   bool          // skip TLS verify (dev / self-signed parent)
	httpClient *http.Client

	mu    sync.Mutex
	token string
}

// newPeerClient constructs the client and ensures the token file's
// parent directory exists with mode 0700.
func newPeerClient(parentURL, name, tokenPath string, insecure bool) (*peerClient, error) {
	parentURL = strings.TrimRight(parentURL, "/")
	if parentURL == "" {
		return nil, errors.New("parent URL required")
	}
	if name == "" {
		return nil, errors.New("peer name required")
	}
	if tokenPath != "" {
		if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
			return nil, fmt.Errorf("mkdir token dir: %w", err)
		}
	}
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &peerClient{
		parentURL:  parentURL,
		name:       name,
		tokenPath:  tokenPath,
		insecure:   insecure,
		httpClient: &http.Client{Timeout: 10 * time.Second, Transport: tr},
	}, nil
}

// loadToken reads the persisted token from disk if present.
func (p *peerClient) loadToken() {
	if p.tokenPath == "" {
		return
	}
	data, err := os.ReadFile(p.tokenPath)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.token = strings.TrimSpace(string(data))
	p.mu.Unlock()
}

// saveToken persists the token to disk with mode 0600.
func (p *peerClient) saveToken(token string) error {
	if p.tokenPath == "" {
		return nil
	}
	tmp := p.tokenPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(token), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.tokenPath)
}

// register hits POST /api/observer/peers and stores the resulting
// token both in memory and on disk.
func (p *peerClient) register(ctx context.Context, shape, version string, hostInfo map[string]any) error {
	body, _ := json.Marshal(map[string]any{
		"name":      p.name,
		"shape":     shape,
		"version":   version,
		"host_info": hostInfo,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.parentURL+"/api/observer/peers", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
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
	p.mu.Lock()
	p.token = got.Token
	p.mu.Unlock()
	if err := p.saveToken(got.Token); err != nil {
		// Non-fatal — we still have the token in memory; operator
		// just loses persistence across restarts.
		fmt.Fprintf(os.Stderr, "[stats] warn: persist token: %v\n", err)
	}
	return nil
}

// push sends one snapshot. On 401 it auto-re-registers and retries
// once. Returns nil on 200, otherwise the error / non-2xx body.
func (p *peerClient) push(ctx context.Context, snap *observer.StatsResponse, shape, version string, hostInfo map[string]any) error {
	if snap == nil {
		return errors.New("nil snapshot")
	}
	if err := p.pushOnce(ctx, snap); err == nil {
		return nil
	} else if !errors.Is(err, errUnauthorized) {
		return err
	}
	// 401 — token rotated or revoked; re-register + retry once.
	if err := p.register(ctx, shape, version, hostInfo); err != nil {
		return fmt.Errorf("re-register after 401: %w", err)
	}
	return p.pushOnce(ctx, snap)
}

var errUnauthorized = errors.New("parent rejected token (401)")

// pushOnce posts a single snapshot using the currently-cached token.
func (p *peerClient) pushOnce(ctx context.Context, snap *observer.StatsResponse) error {
	p.mu.Lock()
	token := p.token
	p.mu.Unlock()
	if token == "" {
		return errUnauthorized
	}
	body, _ := json.Marshal(map[string]any{
		"shape":     "B",
		"peer_name": p.name,
		"snapshot":  snap,
	})
	url := p.parentURL + "/api/observer/peers/" + p.name + "/stats"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("push POST: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("push %d", resp.StatusCode)
	}
	return nil
}

// hasToken reports whether the client currently holds a non-empty
// token (in memory). Useful for the startup decision: skip register
// when we already loaded a token from disk.
func (p *peerClient) hasToken() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.token != ""
}

// runtimeHostInfo collects a minimal envelope of host-identity fields
// to send with the registration body. Pure read.
func runtimeHostInfo() map[string]any {
	host, _ := os.Hostname()
	return map[string]any{
		"hostname": host,
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
	}
}

// BL243 Phase 1 — Tailscale k8s sidecar: headscale/tailscale API client.
//
// Supports two backends:
//   - Headscale (self-hosted): coordinator_url set, headscale admin API v1.
//   - Commercial Tailscale:    coordinator_url empty, api.tailscale.com v2.
//
// All network calls are best-effort from the daemon's perspective; a
// down headscale API does not prevent agent pods from being spawned.

package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	tailscaleAPIBase  = "https://api.tailscale.com/api/v2"
	headscaleAPIv1    = "/api/v1"
	defaultHTTPTimeout = 10 * time.Second
)

// NodeInfo is a normalised node/device record returned by Status and Nodes.
type NodeInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	IP      string   `json:"ip"`
	Online  bool     `json:"online"`
	Tags    []string `json:"tags,omitempty"`
	OS      string   `json:"os,omitempty"`
}

// StatusResponse is returned by GET /api/tailscale/status.
type StatusResponse struct {
	Enabled        bool       `json:"enabled"`
	CoordinatorURL string     `json:"coordinator_url,omitempty"`
	Backend        string     `json:"backend"` // "headscale" | "tailscale"
	NodeCount      int        `json:"node_count"`
	Nodes          []NodeInfo `json:"nodes"`
	Error          string     `json:"error,omitempty"`
}

// Client calls the headscale or commercial tailscale admin API.
type Client struct {
	cfg        *Config
	httpClient *http.Client
}

// NewClient creates a Client from the resolved config (secrets already
// substituted by ResolveConfig at daemon startup).
func NewClient(cfg *Config) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// Status returns the aggregated tailscale status for the operator UI.
func (c *Client) Status(ctx context.Context) (*StatusResponse, error) {
	nodes, err := c.Nodes(ctx)
	resp := &StatusResponse{
		Enabled:        c.cfg.Enabled,
		CoordinatorURL: c.cfg.CoordinatorURL,
		Backend:        c.backend(),
	}
	if err != nil {
		resp.Error = err.Error()
		return resp, nil
	}
	resp.Nodes = nodes
	resp.NodeCount = len(nodes)
	return resp, nil
}

// Nodes returns the list of nodes/devices visible to the admin API.
func (c *Client) Nodes(ctx context.Context) ([]NodeInfo, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("api_key not configured")
	}
	if c.cfg.CoordinatorURL != "" {
		return c.headscaleNodes(ctx)
	}
	return nil, fmt.Errorf("commercial tailscale nodes listing requires tailnet name; use coordinator_url for headscale")
}

// PushACL pushes an HCL/JSON ACL policy string to the headscale admin API.
// Only supported with headscale (coordinator_url set).
func (c *Client) PushACL(ctx context.Context, policy string) error {
	if c.cfg.APIKey == "" {
		return fmt.Errorf("api_key not configured")
	}
	if c.cfg.CoordinatorURL == "" {
		return fmt.Errorf("acl push is only supported with headscale (coordinator_url required)")
	}
	return c.headscalePushACL(ctx, policy)
}

// --- headscale helpers ---

func (c *Client) headscaleNodes(ctx context.Context) ([]NodeInfo, error) {
	base := strings.TrimRight(c.cfg.CoordinatorURL, "/")
	url := base + headscaleAPIv1 + "/node"

	body, err := c.apiGet(ctx, url)
	if err != nil {
		return nil, err
	}

	// Headscale v0.22+ response: {"nodes": [...]}
	var wrapper struct {
		Nodes []struct {
			ID              string   `json:"id"`
			Name            string   `json:"name"`
			IPAddresses     []string `json:"ipAddresses"`
			Online          bool     `json:"online"`
			ForcedTags      []string `json:"forcedTags"`
			ValidTags       []string `json:"validTags"`
			OS              string   `json:"os"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("parse headscale nodes: %w", err)
	}

	nodes := make([]NodeInfo, 0, len(wrapper.Nodes))
	for _, n := range wrapper.Nodes {
		ip := ""
		if len(n.IPAddresses) > 0 {
			ip = n.IPAddresses[0]
		}
		tags := append(n.ForcedTags, n.ValidTags...) //nolint:gocritic
		nodes = append(nodes, NodeInfo{
			ID:     n.ID,
			Name:   n.Name,
			IP:     ip,
			Online: n.Online,
			Tags:   tags,
			OS:     n.OS,
		})
	}
	return nodes, nil
}

func (c *Client) headscalePushACL(ctx context.Context, policy string) error {
	base := strings.TrimRight(c.cfg.CoordinatorURL, "/")
	url := base + headscaleAPIv1 + "/policy"

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(policy))
	if err != nil {
		return fmt.Errorf("build acl push request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("headscale acl push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("headscale acl push %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// --- Phase 2: pre-auth key generation ---

// PreAuthKeyOptions controls the headscale pre-auth key parameters.
type PreAuthKeyOptions struct {
	// Reusable allows multiple nodes to use the same key. Default: false (single-use).
	Reusable bool
	// Ephemeral causes nodes that use this key to be removed when they go offline.
	Ephemeral bool
	// Tags is the list of ACL tags assigned to nodes that join with this key.
	// Must match the tailnet's configured tag owners. Defaults to the client's
	// configured Tags when empty.
	Tags []string
	// ExpiryHours sets how many hours until the key expires. Default: 24.
	ExpiryHours int
}

// PreAuthKeyResult holds the newly generated pre-auth key.
type PreAuthKeyResult struct {
	Key       string `json:"key"`
	Reusable  bool   `json:"reusable"`
	Ephemeral bool   `json:"ephemeral"`
	Tags      []string `json:"tags,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// GeneratePreAuthKey creates a new headscale pre-auth key. Only supported
// when coordinator_url is set (headscale). Commercial Tailscale uses OAuth
// client credentials — not yet implemented.
func (c *Client) GeneratePreAuthKey(ctx context.Context, opts PreAuthKeyOptions) (*PreAuthKeyResult, error) {
	if c.cfg.CoordinatorURL == "" {
		return nil, fmt.Errorf("pre-auth key generation requires headscale (coordinator_url must be set)")
	}
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("api_key not configured")
	}

	tags := opts.Tags
	if len(tags) == 0 {
		tags = c.cfg.EffectiveTags()
	}
	expiryHours := opts.ExpiryHours
	if expiryHours <= 0 {
		expiryHours = 24
	}

	expiresAt := time.Now().UTC().Add(time.Duration(expiryHours) * time.Hour).Format(time.RFC3339)

	payload := map[string]interface{}{
		"expiration": expiresAt,
		"reusable":   opts.Reusable,
		"ephemeral":  opts.Ephemeral,
		"aclTags":    tags,
	}
	body, _ := json.Marshal(payload)

	base := strings.TrimRight(c.cfg.CoordinatorURL, "/")
	url := base + headscaleAPIv1 + "/preauthkey"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("build preauthkey request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("headscale preauthkey: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("headscale preauthkey %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// Headscale response: {"preAuthKey": {"key": "...", "reusable": bool, "ephemeral": bool, "expiration": "...", "aclTags": [...]}}
	var wrapper struct {
		PreAuthKey struct {
			Key        string   `json:"key"`
			Reusable   bool     `json:"reusable"`
			Ephemeral  bool     `json:"ephemeral"`
			Expiration string   `json:"expiration"`
			ACLTags    []string `json:"aclTags"`
		} `json:"preAuthKey"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return nil, fmt.Errorf("parse preauthkey response: %w", err)
	}

	return &PreAuthKeyResult{
		Key:       wrapper.PreAuthKey.Key,
		Reusable:  wrapper.PreAuthKey.Reusable,
		Ephemeral: wrapper.PreAuthKey.Ephemeral,
		Tags:      wrapper.PreAuthKey.ACLTags,
		ExpiresAt: wrapper.PreAuthKey.Expiration,
	}, nil
}

// --- shared helpers ---

func (c *Client) apiGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("api %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// Backend returns "headscale" when coordinator_url is set, "tailscale" otherwise.
func (c *Client) Backend() string { return c.backend() }

func (c *Client) backend() string {
	if c.cfg.CoordinatorURL != "" {
		return "headscale"
	}
	return "tailscale"
}

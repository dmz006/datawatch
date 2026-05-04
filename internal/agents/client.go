// Worker-side bootstrap client for F10 sprint 3 (S3.4).
//
// When a container is spawned by the parent, three env vars are
// injected by the Driver:
//
//   DATAWATCH_BOOTSTRAP_URL    — parent base URL (e.g. http://203.0.113.5:8080)
//   DATAWATCH_BOOTSTRAP_TOKEN  — single-use 32-byte hex token
//   DATAWATCH_AGENT_ID         — UUID the parent assigned us
//
// On startup the worker calls POST {url}/api/agents/bootstrap with
// {agent_id, token}; the parent burns the token, returns the
// effective config, and the worker proceeds.
//
// Network is in flight when the worker starts so retries matter:
// docker-bridge networking can take a beat to settle and the parent
// might still be processing the spawn. We retry with exponential
// backoff up to a deadline before giving up.
//
// The HTTP client deliberately accepts self-signed parent certs —
// in dev the parent will be running on a private CA the worker
// doesn't yet trust (Sprint 4's trusted_cas[] solves that properly).

package agents

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
	"strings"
	"time"
)

// BootstrapEnv groups the three env vars the spawn driver injects.
// Empty fields mean "not running as a worker"; callers branch on
// IsWorker() before doing anything.
type BootstrapEnv struct {
	URL     string
	Token   string
	AgentID string
}

// LoadBootstrapEnv reads the three DATAWATCH_BOOTSTRAP_* vars from
// the process environment.
func LoadBootstrapEnv() BootstrapEnv {
	return BootstrapEnv{
		URL:     os.Getenv("DATAWATCH_BOOTSTRAP_URL"),
		Token:   os.Getenv("DATAWATCH_BOOTSTRAP_TOKEN"),
		AgentID: os.Getenv("DATAWATCH_AGENT_ID"),
	}
}

// IsWorker is true when all three bootstrap env vars are set —
// the worker mode signal.
func (e BootstrapEnv) IsWorker() bool {
	return e.URL != "" && e.Token != "" && e.AgentID != ""
}

// bootstrapRequest mirrors server.BootstrapRequest. Duplicated to
// avoid a server→agents import cycle (server already imports agents).
type bootstrapRequest struct {
	AgentID string `json:"agent_id"`
	Token   string `json:"token"`
}

// BootstrapResponse mirrors server.BootstrapResponse. Same reason.
type BootstrapResponse struct {
	AgentID        string            `json:"agent_id"`
	ProjectProfile string            `json:"project_profile"`
	ClusterProfile string            `json:"cluster_profile"`
	Task           string            `json:"task,omitempty"`
	Git            BootstrapGit      `json:"git,omitempty"`
	Memory         BootstrapMemory   `json:"memory,omitempty"`
	Comm           BootstrapComm     `json:"comm,omitempty"`
	Env            map[string]string `json:"env"`
	// SecretsToken + SecretsURL (BL242 Phase 5c) — per-agent bearer
	// token for scope-enforced runtime secret access. Use FetchSecret
	// to retrieve individual secrets; both are also set as env vars
	// DATAWATCH_SECRETS_TOKEN / DATAWATCH_SECRETS_URL by
	// ApplyBootstrapEnv. Empty when the parent has no secrets store.
	SecretsToken string `json:"secrets_token,omitempty"`
	SecretsURL   string `json:"secrets_url,omitempty"`
}

// BootstrapGit mirrors server.BootstrapGit (F10 S5.3 — git clone
// bundle delivered to the worker on bootstrap).
type BootstrapGit struct {
	URL      string `json:"url,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Token    string `json:"token,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// BootstrapMemory mirrors server.BootstrapMemory (F10 S6.2 — memory
// federation mode + namespace assigned to this worker).
type BootstrapMemory struct {
	Mode      string `json:"mode,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// BootstrapComm mirrors server.BootstrapComm (F10 S7.7 — list of
// parent comm channels the worker should route alerts through).
type BootstrapComm struct {
	Channels []string `json:"channels,omitempty"`
}

// CallBootstrap POSTs to /api/agents/bootstrap with retry+backoff.
// Retries any transport error or 5xx; gives up on 4xx since the parent
// has explicitly rejected the token (e.g. token mismatch, agent state).
//
// Total wall time is bounded by ctx; pass context.WithTimeout(ctx, …)
// at the call site.
//
// TLS — when the parent injects DATAWATCH_PARENT_CERT_FINGERPRINT
// into the spawn env (F10 sprint 4 S4.3) the worker pins that
// fingerprint and refuses any other cert. When the env var is empty
// (legacy / dev / non-TLS parent) we fall back to InsecureSkipVerify
// — same behaviour as Sprint 3 shipped with.
func CallBootstrap(ctx context.Context, env BootstrapEnv) (*BootstrapResponse, error) {
	if !env.IsWorker() {
		return nil, errors.New("bootstrap env not set")
	}
	body, _ := json.Marshal(bootstrapRequest{AgentID: env.AgentID, Token: env.Token})
	url := env.URL + "/api/agents/bootstrap"

	tlsCfg := &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- bootstrap fallback for legacy/dev parents; production path upgrades to PinnedTLSConfig when DATAWATCH_PARENT_CERT_FINGERPRINT is set (see following lines)
	if fp := os.Getenv("DATAWATCH_PARENT_CERT_FINGERPRINT"); fp != "" {
		pinned, err := PinnedTLSConfig(fp)
		if err != nil {
			return nil, fmt.Errorf("invalid pinned fingerprint: %w", err)
		}
		tlsCfg = pinned
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	backoff := 500 * time.Millisecond
	const maxBackoff = 5 * time.Second
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return nil, fmt.Errorf("%w (last attempt: %v)", err, lastErr)
			}
			return nil, err
		}

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			// 4xx is terminal — token bad / agent state wrong; no point retrying.
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				msg, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return nil, fmt.Errorf("bootstrap rejected (%d): %s", resp.StatusCode, bytes.TrimSpace(msg))
			}
			if resp.StatusCode == http.StatusOK {
				var out BootstrapResponse
				err := json.NewDecoder(resp.Body).Decode(&out)
				resp.Body.Close()
				if err != nil {
					return nil, fmt.Errorf("decode bootstrap response: %w", err)
				}
				return &out, nil
			}
			// 5xx — retry
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("bootstrap server error (%d): %s", resp.StatusCode, bytes.TrimSpace(body))
		}

		// Sleep with backoff, but bail early if ctx cancelled mid-wait.
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, fmt.Errorf("%w (last attempt: %v)", ctx.Err(), lastErr)
		case <-t.C:
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// ApplyBootstrapEnv exports each key of resp.Env into the current
// process environment. Subsequent config loaders (env-aware ones)
// will pick them up automatically. Pre-existing values are
// overwritten — the parent's bootstrap response is authoritative.
//
// Also surfaces the F10 S6.2 memory federation bundle as env so
// the worker's memory client can configure itself without parsing
// the BootstrapResponse twice:
//
//   DATAWATCH_MEMORY_MODE       — shared | sync-back | ephemeral
//   DATAWATCH_MEMORY_NAMESPACE  — per-Project-Profile bucket name
func ApplyBootstrapEnv(resp *BootstrapResponse) {
	if resp == nil {
		return
	}
	for k, v := range resp.Env {
		_ = os.Setenv(k, v)
	}
	if resp.Memory.Mode != "" {
		_ = os.Setenv("DATAWATCH_MEMORY_MODE", resp.Memory.Mode)
		_ = os.Setenv("DATAWATCH_MEMORY_NAMESPACE", resp.Memory.Namespace)
	}
	// F10 S7.7 — comm-channel inheritance. Comma-separated for env
	// transport; worker's outbound alert path parses + dispatches
	// to parent's /api/proxy/comm/{channel}/send when set.
	if len(resp.Comm.Channels) > 0 {
		_ = os.Setenv("DATAWATCH_COMM_INHERIT", strings.Join(resp.Comm.Channels, ","))
	}
	// BL242 Phase 5c — secrets token for runtime secret access.
	if resp.SecretsToken != "" {
		_ = os.Setenv("DATAWATCH_SECRETS_TOKEN", resp.SecretsToken)
	}
	if resp.SecretsURL != "" {
		_ = os.Setenv("DATAWATCH_SECRETS_URL", resp.SecretsURL)
	}
}

// ErrSecretsUnavailable is returned by FetchSecret when
// DATAWATCH_SECRETS_TOKEN or DATAWATCH_SECRETS_URL are not set —
// meaning the parent daemon either has no secrets store or is
// pre-v6.4.7. Callers should treat this as "secret unavailable" and
// fall back to environment variables or fail gracefully.
var ErrSecretsUnavailable = errors.New("secrets unavailable: DATAWATCH_SECRETS_TOKEN or DATAWATCH_SECRETS_URL not set")

// FetchSecret retrieves a named secret from the parent's secrets store
// using the per-agent bearer token set by ApplyBootstrapEnv. The
// parent scope-checks the request against the agent's project profile
// before returning a value. Returns ErrSecretsUnavailable when the
// required env vars are absent. Callers should not cache the returned
// value across long-running loops — use this at task start or when the
// secret is first needed.
func FetchSecret(ctx context.Context, name string) (string, error) {
	tok := os.Getenv("DATAWATCH_SECRETS_TOKEN")
	baseURL := os.Getenv("DATAWATCH_SECRETS_URL")
	if tok == "" || baseURL == "" {
		return "", ErrSecretsUnavailable
	}
	return fetchSecretFrom(ctx, baseURL, tok, name)
}

func fetchSecretFrom(ctx context.Context, baseURL, token, name string) (string, error) {
	url := strings.TrimRight(baseURL, "/") + "/api/agents/secrets/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	tlsCfg := &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- dev/local parent; same trust model as bootstrap
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch secret %q: %w", name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		var result struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("fetch secret %q: decode: %w", name, err)
		}
		return result.Value, nil
	case http.StatusNotFound:
		return "", fmt.Errorf("secret %q not found", name)
	case http.StatusForbidden:
		return "", fmt.Errorf("secret %q: access denied (not in scope for this agent)", name)
	default:
		return "", fmt.Errorf("fetch secret %q: status %d: %s", name, resp.StatusCode, bytes.TrimSpace(body))
	}
}

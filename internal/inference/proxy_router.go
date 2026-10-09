// BL318 — ProxyRouter: inference via a peer datawatch instance.
//
// Used by the dispatcher when a Node has Routing=datawatch-proxy.

package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

// ProxyRouter makes inference calls through a peer datawatch instance.
type ProxyRouter struct {
	PeerURL   string
	PeerToken string
}

// Infer POSTs the inference request to <PeerURL>/api/proxy/llm/<remoteLLM>.
func (p *ProxyRouter) Infer(ctx context.Context, remoteLLM string, req Request) (Response, error) {
	if p.PeerURL == "" {
		return Response{}, fmt.Errorf("proxy router: peer URL is empty")
	}
	timeout := 120 * time.Second
	body, _ := json.Marshal(map[string]any{
		"prompt":        req.Prompt,
		"system_prompt": req.SystemPrompt,
		"model":         req.ModelOverride,
	})
	url := strings.TrimRight(p.PeerURL, "/") + "/api/proxy/llm/" + remoteLLM
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return Response{}, fmt.Errorf("proxy router: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.PeerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.PeerToken)
	}
	// GH#201 Phase 2 — thread origin-actor attribution across this hop.
	// Best-effort: ctx only carries a resolvable principal/prior chain
	// when the call path up to here derived its context from the
	// original HTTP request (confirmed for the direct handler surfaces
	// in internal/server/proxy.go); where it doesn't, BuildOrExtendChain
	// returns nil and this degrades safely to no header at all — the
	// same behavior as before this phase existed, never a failure.
	if chain, err := federation.BuildOrExtendChain(ctx, selfDaemonLabel(), p.PeerToken); err == nil {
		if enc := federation.EncodeChain(chain); enc != "" {
			httpReq.Header.Set(federation.HopChainHeader, enc)
		}
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Response{}, &ErrTransient{Err: fmt.Errorf("proxy router: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusNotFound {
		return Response{}, fmt.Errorf("proxy router: LLM %q not found on peer", remoteLLM)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Response{}, fmt.Errorf("proxy router: peer rejected auth (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Response{}, &ErrTransient{Err: fmt.Errorf("proxy router: HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Response{}, fmt.Errorf("proxy router: HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Text       string `json:"text"`
		UsedModel  string `json:"used_model"`
		DurationMs int64  `json:"duration_ms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Response{}, fmt.Errorf("proxy router: decode: %w", err)
	}
	return Response{
		Text:       out.Text,
		UsedModel:  out.UsedModel,
		DurationMs: out.DurationMs,
		Backend:    "datawatch-proxy",
	}, nil
}

// selfDaemonLabel (GH#201 Phase 2) is this process's own identity for
// hop-chain Daemon fields. internal/inference has no access to
// cfg.Hostname (that's internal/server/internal/config territory, and
// importing either here would cycle), so this is a direct os.Hostname()
// call — a label-only field with no bearing on the chain's security
// property, which comes entirely from the HMAC signature.
func selfDaemonLabel() string {
	h, _ := os.Hostname()
	return h
}

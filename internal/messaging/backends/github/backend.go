// Package github implements a messaging.Backend that receives GitHub webhook events.
// Start an HTTP listener; configure your GitHub repo webhook to point to it.
// Supported events: issue_comment, pull_request_review_comment, workflow_dispatch.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend listens for GitHub webhook POST requests.
type Backend struct {
	addr   string
	secret string
	srv    *http.Server
	msgs   chan messaging.Message
}

// New creates a new GitHub webhook backend.
func New(addr, secret string) *Backend {
	b := &Backend{addr: addr, secret: secret, msgs: make(chan messaging.Message, 64)}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", b.handleWebhook)
	// G112 fix (v6.22.2): ReadHeaderTimeout prevents Slowloris attacks.
	b.srv = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	// SEC-004 (docs/plans/historical-plans/2026-08-28-security-assessment-core.md)
	// -- github_webhook.secret has always been stored and passed in here,
	// but handleWebhook never actually checked it; see verifySignature's
	// own comment below. Leaving it unset is still allowed (matches this
	// codebase's existing "optional secret" convention elsewhere, e.g.
	// the generic webhook backend's bearer token), but it's loud about
	// the risk rather than silent, since unlike most of those, a GitHub
	// webhook is commonly exposed off-loopback (the default bind here IS
	// loopback-only, but GitHub.com itself can only reach a publicly
	// routable address, so most real deployments proxy or rebind this).
	if secret == "" {
		fmt.Printf("[github webhook] WARNING: github_webhook.secret is empty -- signature verification is DISABLED, any party that can reach %s can forge events and inject commands. Set github_webhook.secret to the value configured on the GitHub repo's webhook.\n", addr)
	}
	return b
}

// verifySignature checks GitHub's HMAC-SHA256 webhook signature
// (SEC-004). GitHub signs the raw request body with the configured
// webhook secret and sends it as "X-Hub-Signature-256: sha256=<hex>" --
// this was never checked before this fix, so the secret sat stored and
// unused while the endpoint accepted any payload from anyone who could
// reach it. hmac.Equal is used (not == or bytes.Equal) specifically for
// its constant-time comparison, so this check itself doesn't leak timing
// information about how much of the expected signature a forged one got
// right.
func verifySignature(secret string, body []byte, sigHeader string) bool {
	if secret == "" {
		return true // no secret configured -- see New()'s own warning
	}
	const prefix = "sha256="
	if !strings.HasPrefix(sigHeader, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(sigHeader, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body) //nolint:errcheck // hash.Hash.Write never returns an error
	got := mac.Sum(nil)
	return hmac.Equal(got, want)
}

func (b *Backend) Name() string { return "github" }

func (b *Backend) Send(recipient, message string) error { return nil } // read-only source

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	go func() {
		if err := b.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[github webhook] server error: %v\n", err)
		}
	}()
	defer b.srv.Shutdown(context.Background()) //nolint:errcheck
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg := <-b.msgs:
			handler(msg)
		}
	}
}

func (b *Backend) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}
	_ = r.Body.Close()
	if !verifySignature(b.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	eventType := r.Header.Get("X-GitHub-Event")
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		w.WriteHeader(200)
		return
	}
	var text, sender, groupID string
	switch eventType {
	case "issue_comment":
		text, _ = payload["comment"].(map[string]interface{})["body"].(string)
		senderMap, _ := payload["sender"].(map[string]interface{})
		sender, _ = senderMap["login"].(string)
		issueMap, _ := payload["issue"].(map[string]interface{})
		groupID = fmt.Sprintf("issue:%v", issueMap["number"])
	case "pull_request_review_comment":
		text, _ = payload["comment"].(map[string]interface{})["body"].(string)
		senderMap, _ := payload["sender"].(map[string]interface{})
		sender, _ = senderMap["login"].(string)
		prMap, _ := payload["pull_request"].(map[string]interface{})
		groupID = fmt.Sprintf("pr:%v", prMap["number"])
	case "workflow_dispatch":
		inputsMap, _ := payload["inputs"].(map[string]interface{})
		text, _ = inputsMap["task"].(string)
		senderMap, _ := payload["sender"].(map[string]interface{})
		sender, _ = senderMap["login"].(string)
		groupID = "workflow_dispatch"
	default:
		w.WriteHeader(200)
		return
	}
	if text == "" {
		w.WriteHeader(200)
		return
	}
	b.msgs <- messaging.Message{
		GroupID: groupID, Sender: sender, Text: text, Backend: "github",
	}
	w.WriteHeader(200)
}

func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                  { return "github-webhook" }
func (b *Backend) Close() error                                    { return b.srv.Shutdown(context.Background()) }

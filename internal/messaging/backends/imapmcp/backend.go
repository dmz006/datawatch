// Package imapmcp implements a messaging.Backend that bridges datawatch to an
// imap-mcp instance. It subscribes to imap-mcp's SSE event stream and acts
// only on verified inbound.command events — the trust boundary lives in
// imap-mcp, so this backend never re-validates. Outbound messages are sent
// via imap-mcp's REST send endpoint.
//
// Transport:
//   - Receive: GET <url>/api/events (SSE); reconnects with backoff on error.
//   - Send:    POST <url>/api/accounts/{account}/messages/send (JSON body).
//
// Config (in datawatch config.yaml):
//
//	imap_mcp:
//	  enabled: true
//	  url: "http://localhost:8765"
//	  account: ""             # empty = imap-mcp default account
//	  subject_prefix: "datawatch"
//	  token: "${secret:imap_mcp_token_datawatch}"  # GH#203, imap-mcp >= 0.5.3
package imapmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

// token is process-wide (GH#203), set via SetToken once at daemon startup
// after secretspkg.ResolveConfig resolves a ${secret:name} reference —
// mirrors the established pattern for backends constructed before the
// secrets store exists (e.g. openwebui.SetAPIKey). Read per-request rather
// than copied into a Backend field at construction time, so a late
// SetToken call (the normal startup order) still takes effect.
var (
	tokenMu sync.RWMutex
	token   string
)

// SetToken sets the bearer token sent on every imap-mcp request except
// GET /api/health. Safe to call before or after any Backend is
// constructed; empty clears it (no Authorization header sent).
func SetToken(t string) {
	tokenMu.Lock()
	token = t
	tokenMu.Unlock()
}

func getToken() string {
	tokenMu.RLock()
	defer tokenMu.RUnlock()
	return token
}

// setAuthHeader adds the configured bearer token to req, if one is set.
func setAuthHeader(req *http.Request) {
	if t := getToken(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
}

// authError (GH#203) distinguishes a 401/403 from imap-mcp -- a
// configuration problem (missing/wrong token, or a token lacking the
// needed scope) -- from an ordinary transient network error. Subscribe
// uses this to back off slowly instead of hot-looping reconnect attempts
// that will keep failing for the same reason every time.
type authError struct {
	status int
	body   string
}

func (e *authError) Error() string {
	kind := "unauthorized"
	if e.status == http.StatusForbidden {
		kind = "forbidden"
	}
	msg := strings.TrimSpace(e.body)
	if msg == "" {
		return fmt.Sprintf("imap-mcp %s (HTTP %d)", kind, e.status)
	}
	return fmt.Sprintf("imap-mcp %s (HTTP %d): %s", kind, e.status, msg)
}

// newAuthError builds an authError from a non-2xx response, extracting
// imap-mcp's documented {"error":"..."} body shape when present.
func newAuthError(status int, body []byte) *authError {
	var decoded struct {
		Error string `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &decoded) == nil && decoded.Error != "" {
		msg = decoded.Error
	}
	return &authError{status: status, body: msg}
}

// Backend connects datawatch to a running imap-mcp server.
type Backend struct {
	url           string
	account       string // imap-mcp account name (empty = default)
	subjectPrefix string
	httpClient    *http.Client
}

// New creates a new imap-mcp backend.
// url is the imap-mcp API base URL (e.g. "http://localhost:8765").
// account is the imap-mcp account to use; empty = use imap-mcp default.
// subjectPrefix is prepended to reply subjects; empty defaults to "datawatch".
func New(url, account, subjectPrefix string) *Backend {
	if subjectPrefix == "" {
		subjectPrefix = "datawatch"
	}
	return &Backend{
		url:           strings.TrimRight(url, "/"),
		account:       account,
		subjectPrefix: subjectPrefix,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (b *Backend) Name() string { return "imap_mcp" }

// SelfID returns the from address of the configured account, fetched live from
// imap-mcp. Returns empty string if the server is unreachable.
func (b *Backend) SelfID() string {
	type acctInfo struct {
		Name    string `json:"name"`
		Default bool   `json:"default"`
	}
	req, err := http.NewRequest(http.MethodGet, b.url+"/api/accounts", nil)
	if err != nil {
		return ""
	}
	setAuthHeader(req)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var accounts []acctInfo
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		return ""
	}
	if b.account != "" {
		for _, a := range accounts {
			if a.Name == b.account {
				return a.Name + "@imap-mcp"
			}
		}
		return ""
	}
	for _, a := range accounts {
		if a.Default {
			return a.Name + "@imap-mcp"
		}
	}
	if len(accounts) > 0 {
		return accounts[0].Name + "@imap-mcp"
	}
	return ""
}

// Link is a no-op — email accounts are configured in imap-mcp, not linked here.
func (b *Backend) Link(_ string, _ func(string)) error { return nil }

// Close is a no-op — Subscribe manages its own lifecycle via context.
func (b *Backend) Close() error { return nil }

// Send sends a plain-text reply via imap-mcp's REST send endpoint.
// recipient is the To address; message is the plain-text body.
func (b *Backend) Send(recipient, message string) error {
	account := b.account
	if account == "" {
		account = "_default"
	}
	payload := map[string]string{
		"to":      recipient,
		"subject": b.subjectPrefix + " response",
		"body":    message,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("imap_mcp send marshal: %w", err)
	}
	req, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("%s/api/accounts/%s/messages/send", b.url, account),
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("imap_mcp send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("imap_mcp send: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("imap_mcp send: %w", newAuthError(resp.StatusCode, body))
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("imap_mcp send: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Subscribe connects to the imap-mcp SSE event stream and calls handler for
// each verified inbound.command event. Reconnects with exponential backoff
// on network errors; a 401/403 (GH#203 — a configuration problem, not a
// transient one) jumps straight to and stays at maxBackoff instead of
// hot-looping reconnect attempts that would keep failing for the same
// reason every time. Blocks until ctx is cancelled.
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	backoff := 2 * time.Second
	const maxBackoff = 60 * time.Second
	loggedAuthErr := false

	for {
		err := b.stream(ctx, handler)
		if err == nil {
			// clean exit means ctx was cancelled
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}

		var authErr *authError
		if errors.As(err, &authErr) {
			if !loggedAuthErr {
				fmt.Printf("[imap_mcp] SSE connect failed: %v — this is a configuration problem (missing/wrong imap_mcp.token, or a token lacking the \"read\" scope), not a transient error; backing off at %s intervals until it's fixed\n", authErr, maxBackoff)
				loggedAuthErr = true
			}
			backoff = maxBackoff
		} else {
			loggedAuthErr = false
			backoff = nextBackoff(backoff, maxBackoff)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
	}
}

// nextBackoff doubles cur, capped at max. Extracted as a pure function so
// the backoff-selection logic is unit-testable without real sleeping.
func nextBackoff(cur, max time.Duration) time.Duration {
	cur *= 2
	if cur > max {
		cur = max
	}
	return cur
}

// stream opens one SSE connection and reads events until the connection drops
// or ctx is cancelled. Returns an *authError on a 401/403 response.
func (b *Backend) stream(ctx context.Context, handler func(messaging.Message)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url+"/api/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	setAuthHeader(req)

	client := &http.Client{} // no timeout — SSE is long-lived
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return newAuthError(resp.StatusCode, body)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE endpoint returned %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // heartbeat comment or empty line
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		b.handleSSELine(raw, handler)
	}
	if ctx.Err() != nil {
		return nil
	}
	return scanner.Err()
}

// sseEvent mirrors bus.Event for JSON decoding.
type sseEvent struct {
	Type    string          `json:"type"`
	Account string          `json:"account"`
	Payload json.RawMessage `json:"payload"`
}

// verifiedCommand mirrors imap-mcp/internal/trust.VerifiedCommand for JSON decoding.
// Field names match Go's default JSON encoding (exported, no json tags in source).
type verifiedCommand struct {
	Account string `json:"Account"`
	From    string `json:"From"`
	Command struct {
		Verb  string `json:"Verb"`
		Args  string `json:"Args"`
		Nonce string `json:"Nonce"`
	} `json:"Command"`
	Gates []string `json:"Gates"`
}

func (b *Backend) handleSSELine(raw string, handler func(messaging.Message)) {
	var e sseEvent
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return
	}
	if e.Type != "inbound.command" {
		return
	}
	var cmd verifiedCommand
	if err := json.Unmarshal(e.Payload, &cmd); err != nil {
		return
	}

	text := strings.TrimSpace(cmd.Command.Verb)
	if cmd.Command.Args != "" {
		text += " " + strings.TrimSpace(cmd.Command.Args)
	}
	handler(messaging.Message{
		ID:         cmd.Command.Nonce,
		Sender:     cmd.From,
		Text:       text,
		Backend:    b.Name(),
		GroupID:    cmd.Account,
		GroupName:  cmd.Account,
		SenderName: cmd.From,
	})
}

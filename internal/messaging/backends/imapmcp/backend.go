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
package imapmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

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
	resp, err := b.httpClient.Get(b.url + "/api/accounts")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
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
	resp, err := b.httpClient.Post(
		fmt.Sprintf("%s/api/accounts/%s/messages/send", b.url, account),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("imap_mcp send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("imap_mcp send: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Subscribe connects to the imap-mcp SSE event stream and calls handler for
// each verified inbound.command event. Reconnects with exponential backoff
// on network errors. Blocks until ctx is cancelled.
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	backoff := 2 * time.Second
	const maxBackoff = 60 * time.Second

	for {
		if err := b.stream(ctx, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		// clean exit means ctx was cancelled
		return nil
	}
}

// stream opens one SSE connection and reads events until the connection drops
// or ctx is cancelled.
func (b *Backend) stream(ctx context.Context, handler func(messaging.Message)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url+"/api/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{} // no timeout — SSE is long-lived
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
		ID:          cmd.Command.Nonce,
		Sender:      cmd.From,
		Text:        text,
		Backend:     b.Name(),
		GroupID:     cmd.Account,
		GroupName:   cmd.Account,
		SenderName:  cmd.From,
	})
}

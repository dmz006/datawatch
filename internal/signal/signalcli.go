package signal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const scannerBufSize = 4 * 1024 * 1024 // 4 MB — handles large group messages

// SignalCLIBackend implements SignalBackend using signal-cli in jsonRpc mode.
type SignalCLIBackend struct {
	configDir     string
	accountNumber string
	verbose       bool

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	pendingMu sync.Mutex       // guards pending map
	writeMu   sync.Mutex       // guards stdin writes
	idCtr     int64
	pending   map[int]chan *JSONRPCResponse

	subMu      sync.Mutex
	subHandler func(IncomingMessage)

	done chan struct{}
}

// NewSignalCLIBackend creates and starts a signal-cli jsonRpc subprocess.
func NewSignalCLIBackend(configDir, accountNumber string) (*SignalCLIBackend, error) {
	b := &SignalCLIBackend{
		configDir:     configDir,
		accountNumber: accountNumber,
		pending:       make(map[int]chan *JSONRPCResponse),
		done:          make(chan struct{}),
	}

	cmd := exec.Command("signal-cli",
		"--config", configDir,
		"-u", accountNumber,
		"jsonRpc",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("signal-cli stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("signal-cli stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("signal-cli stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("signal-cli start: %w", err)
	}

	b.cmd = cmd
	b.stdin = stdin

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, scannerBufSize), scannerBufSize)
	b.stdout = sc

	// Capture and log signal-cli stderr so errors are always visible.
	go b.readStderr(stderr)
	go b.readLoop()

	return b, nil
}

// SetVerbose enables verbose raw-line logging of all signal-cli JSON-RPC traffic.
func (b *SignalCLIBackend) SetVerbose(v bool) { b.verbose = v }

// readStderr logs all output from signal-cli's stderr stream.
// signal-cli writes startup errors, warnings, and Java exceptions here.
func (b *SignalCLIBackend) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			fmt.Printf("[signal-cli stderr] %s\n", line)
		}
	}
}

// readLoop reads JSON lines from signal-cli stdout and dispatches them.
func (b *SignalCLIBackend) readLoop() {
	defer close(b.done)
	for b.stdout.Scan() {
		line := b.stdout.Text()
		if line == "" {
			continue
		}

		if b.verbose {
			fmt.Printf("[signal raw] %s\n", line)
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			fmt.Printf("[signal] WARN: non-JSON line from signal-cli: %s\n", line)
			continue
		}

		// Notification (no ID, has method == "receive")
		if resp.ID == nil && resp.Method == "receive" {
			b.dispatchNotification(resp)
			continue
		}

		// Response to a pending RPC call
		if resp.ID != nil {
			b.pendingMu.Lock()
			ch, ok := b.pending[*resp.ID]
			b.pendingMu.Unlock()
			if ok {
				ch <- &resp
			}
		}
	}

	if err := b.stdout.Err(); err != nil {
		fmt.Printf("[signal] stdout scanner error: %v\n", err)
	}

	// Signal pending callers that the backend is gone
	b.pendingMu.Lock()
	for _, ch := range b.pending {
		close(ch)
	}
	b.pendingMu.Unlock()
}

// dispatchNotification parses a "receive" notification and calls the subscription handler.
func (b *SignalCLIBackend) dispatchNotification(resp JSONRPCResponse) {
	b.subMu.Lock()
	handler := b.subHandler
	b.subMu.Unlock()
	if handler == nil {
		return
	}

	// params is {"envelope": {...}, "account": "...", "subscription": N}
	var params struct {
		Envelope Envelope `json:"envelope"`
		Account  string   `json:"account"`
	}
	if err := json.Unmarshal(resp.Params, &params); err != nil {
		fmt.Printf("[signal] WARN: failed to parse notification params: %v\n", err)
		return
	}

	env := params.Envelope

	// Handle syncMessage.sentMessage — these are messages sent by the user from
	// their primary phone (or another linked device). signal-cli on a linked device
	// receives these instead of a dataMessage for the account holder's own sends.
	if env.DataMessage == nil {
		if env.SyncMessage != nil && env.SyncMessage.SentMessage != nil {
			sm := env.SyncMessage.SentMessage
			if sm.Message != "" && sm.GroupInfo != nil {
				fmt.Printf("[signal] sync-sent in group %q: %q\n",
					sm.GroupInfo.GroupID, strings.TrimSpace(sm.Message))
				handler(IncomingMessage{
					Envelope:   env,
					GroupID:    sm.GroupInfo.GroupID,
					Text:       sm.Message,
					Sender:     env.EffectiveSource(),
					SenderName: env.SourceName,
				})
			}
		} else if b.verbose {
			fmt.Printf("[signal] notification: non-data envelope from %s (type=%s) — skipping\n",
				env.Source, env.EnvelopeType())
		}
		return
	}

	dm := env.DataMessage
	if dm.Message == "" {
		if b.verbose {
			fmt.Printf("[signal] notification: empty message body from %s — skipping\n", env.Source)
		}
		return
	}
	if dm.GroupInfo == nil {
		if b.verbose {
			fmt.Printf("[signal] notification: direct message from %s (no group) — skipping\n", env.Source)
		}
		return
	}

	// Filter out echo-looped self-messages (dataMessage from self would cause
	// command loops). syncMessage.sentMessage (above) is intentionally allowed
	// since those are the user's own commands sent from their phone.
	effectiveSrc := env.EffectiveSource()
	src := strings.TrimPrefix(effectiveSrc, "+")
	acct := strings.TrimPrefix(b.accountNumber, "+")
	if src == acct {
		if b.verbose {
			fmt.Printf("[signal] self-dataMessage filtered (source=%s account=%s)\n",
				effectiveSrc, b.accountNumber)
		}
		return
	}

	fmt.Printf("[signal] message from %s (%s) in group %q: %q\n",
		effectiveSrc, env.SourceName, dm.GroupInfo.GroupID, strings.TrimSpace(dm.Message))

	handler(IncomingMessage{
		Envelope:    env,
		GroupID:     dm.GroupInfo.GroupID,
		Text:        dm.Message,
		Sender:      effectiveSrc,
		SenderName:  env.SourceName,
		Attachments: dm.Attachments,
	})
}

// nextID returns the next request ID.
func (b *SignalCLIBackend) nextID() int {
	return int(atomic.AddInt64(&b.idCtr, 1))
}

// call sends a JSON-RPC request and waits for the response using the default
// 30s timeout. Suitable for most interactive RPCs (send, listGroups, etc.).
func (b *SignalCLIBackend) call(method string, params interface{}) (*JSONRPCResponse, error) {
	return b.callCtx(context.Background(), method, params, 30*time.Second)
}

// callCtx is like call but honours a caller-supplied context and timeout.
// A long timeout is useful for subscribeReceive after a Signal reset, where
// signal-cli drains a large sync backlog before acknowledging.
func (b *SignalCLIBackend) callCtx(ctx context.Context, method string, params interface{}, timeout time.Duration) (*JSONRPCResponse, error) {
	id := b.nextID()
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if b.verbose {
		fmt.Printf("[signal rpc ->] %s\n", data)
	}

	ch := make(chan *JSONRPCResponse, 1)
	b.pendingMu.Lock()
	b.pending[id] = ch
	b.pendingMu.Unlock()

	b.writeMu.Lock()
	_, writeErr := fmt.Fprintf(b.stdin, "%s\n", data)
	b.writeMu.Unlock()

	if writeErr != nil {
		b.pendingMu.Lock()
		delete(b.pending, id)
		b.pendingMu.Unlock()
		return nil, fmt.Errorf("write request: %w", writeErr)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	cleanup := func() {
		b.pendingMu.Lock()
		delete(b.pending, id)
		b.pendingMu.Unlock()
	}

	// Wait for response, ctx cancellation, backend death, or timeout.
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("backend closed while waiting for response")
		}
		if b.verbose {
			respJSON, _ := json.Marshal(resp)
			fmt.Printf("[signal rpc <-] %s\n", respJSON)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-b.done:
		return nil, fmt.Errorf("backend closed while waiting for response")
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	case <-timer.C:
		cleanup()
		return nil, fmt.Errorf("rpc timeout waiting for response to %s (id=%d)", method, id)
	}
}

// Link runs signal-cli link -n <deviceName> in a subprocess, captures the sgnl:// URI,
// calls onQR, then waits for linking to complete.
func (b *SignalCLIBackend) Link(deviceName string, onQR func(qrURI string)) error {
	cmd := exec.Command("signal-cli",
		"--config", b.configDir,
		"link",
		"-n", deviceName,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("link stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("link stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("link start: %w", err)
	}

	// Search both streams concurrently — signal-cli writes the URI to stderr.
	var once sync.Once
	scan := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "sgnl://") {
				once.Do(func() { onQR(line) })
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scan(stderr)
	}()
	scan(stdout)
	wg.Wait()

	return cmd.Wait()
}

// Send sends a text message to a Signal group.
func (b *SignalCLIBackend) Send(groupID, message string) error {
	params := map[string]interface{}{
		"groupId": groupID,
		"message": message,
	}
	_, err := b.call("send", params)
	return err
}

// Subscribe starts receiving messages. Calls handler for each incoming message.
// Blocks until ctx is cancelled.
//
// subscribeReceive can be slow on startup: after a Signal reset or a long offline
// period, signal-cli drains a large sync-message backlog before acknowledging.
// We use a generous per-attempt timeout and retry transient failures so a slow
// first connect doesn't kill the router.
func (b *SignalCLIBackend) Subscribe(ctx context.Context, handler func(IncomingMessage)) error {
	b.subMu.Lock()
	b.subHandler = handler
	b.subMu.Unlock()

	const (
		perAttemptTimeout = 5 * time.Minute
		maxAttempts       = 6
		maxBackoff        = 30 * time.Second
	)

	backoff := 2 * time.Second
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		resp, err := b.callCtx(ctx, "subscribeReceive", nil, perAttemptTimeout)
		if err == nil {
			fmt.Printf("[signal] subscribeReceive OK (subscription=%v)\n", resp.Result)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-b.done:
				return fmt.Errorf("signal-cli backend closed unexpectedly")
			}
		}

		// Context cancellation is a clean shutdown, not a failure to retry.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Dead subprocess: no point hammering a corpse — return so the outer
		// retry loop can recreate the backend.
		if strings.Contains(err.Error(), "backend closed") {
			return fmt.Errorf("subscribeReceive: %w", err)
		}

		lastErr = err
		fmt.Printf("[signal] subscribeReceive attempt %d/%d failed: %v (retrying in %s)\n",
			attempt, maxAttempts, err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.done:
			return fmt.Errorf("signal-cli backend closed unexpectedly")
		case <-time.After(backoff):
		}

		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}

	return fmt.Errorf("subscribeReceive: giving up after %d attempts: %w", maxAttempts, lastErr)
}

// ListGroups returns the list of joined Signal groups.
func (b *SignalCLIBackend) ListGroups(_ context.Context) ([]Group, error) {
	resp, err := b.call("listGroups", nil)
	if err != nil {
		return nil, err
	}

	// Result is an array of group objects
	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal listGroups result: %w", err)
	}

	var raw []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse listGroups result: %w", err)
	}

	groups := make([]Group, 0, len(raw))
	for _, g := range raw {
		groups = append(groups, Group{ID: g.ID, Name: g.Name})
	}
	return groups, nil
}

// CreateGroup creates a new Signal group with the given name. The account's own
// number is added as a member automatically. Returns the new group's base64 ID.
func (b *SignalCLIBackend) CreateGroup(name string) (string, error) {
	resp, err := b.call("updateGroup", map[string]interface{}{
		"name":    name,
		"members": []string{b.accountNumber},
	})
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return "", fmt.Errorf("marshal createGroup result: %w", err)
	}

	var result struct {
		GroupID string `json:"groupId"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse createGroup result: %w", err)
	}
	if result.GroupID == "" {
		return "", fmt.Errorf("signal-cli returned empty groupId for createGroup")
	}
	return result.GroupID, nil
}

// SelfNumber returns the registered phone number.
func (b *SignalCLIBackend) SelfNumber() string {
	return b.accountNumber
}

// Close shuts down the signal-cli subprocess.
func (b *SignalCLIBackend) Close() error {
	_ = b.stdin.Close()
	return b.cmd.Wait()
}

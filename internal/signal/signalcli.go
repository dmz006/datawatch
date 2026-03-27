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
)

// SignalCLIBackend implements SignalBackend using signal-cli in jsonRpc mode.
type SignalCLIBackend struct {
	configDir     string
	accountNumber string

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

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("signal-cli start: %w", err)
	}

	b.cmd = cmd
	b.stdin = stdin
	b.stdout = bufio.NewScanner(stdout)

	go b.readLoop()

	return b, nil
}

// readLoop reads JSON lines from signal-cli stdout and dispatches them.
func (b *SignalCLIBackend) readLoop() {
	defer close(b.done)
	for b.stdout.Scan() {
		line := b.stdout.Text()
		if line == "" {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			// Not valid JSON, ignore
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

	// params is {"envelope": {...}}
	var params struct {
		Envelope Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(resp.Params, &params); err != nil {
		return
	}

	env := params.Envelope
	if env.DataMessage == nil {
		return
	}
	dm := env.DataMessage
	if dm.Message == "" {
		return
	}
	if dm.GroupInfo == nil {
		return
	}

	// Filter out self (messages sent by this device)
	// Use contains check to handle format variations (+1234 vs 1234)
	if env.Source == b.accountNumber || strings.HasSuffix(env.Source, b.accountNumber) || strings.HasSuffix(b.accountNumber, env.Source) {
		return
	}

	msg := IncomingMessage{
		Envelope:   env,
		GroupID:    dm.GroupInfo.GroupID,
		Text:       dm.Message,
		Sender:     env.Source,
		SenderName: env.SourceName,
	}
	fmt.Printf("[signal] incoming message from %s in group %q: %q\n",
		env.Source, dm.GroupInfo.GroupID, strings.TrimSpace(dm.Message))
	handler(msg)
}

// nextID returns the next request ID.
func (b *SignalCLIBackend) nextID() int {
	return int(atomic.AddInt64(&b.idCtr, 1))
}

// call sends a JSON-RPC request and waits for the response.
func (b *SignalCLIBackend) call(method string, params interface{}) (*JSONRPCResponse, error) {
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

	resp, ok := <-ch
	if !ok {
		return nil, fmt.Errorf("backend closed while waiting for response")
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp, nil
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
func (b *SignalCLIBackend) Subscribe(ctx context.Context, handler func(IncomingMessage)) error {
	b.subMu.Lock()
	b.subHandler = handler
	b.subMu.Unlock()

	// Send subscribeReceive request
	_, err := b.call("subscribeReceive", nil)
	if err != nil {
		return fmt.Errorf("subscribeReceive: %w", err)
	}

	// Block until context is cancelled or backend closes
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		return fmt.Errorf("signal-cli backend closed")
	}
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

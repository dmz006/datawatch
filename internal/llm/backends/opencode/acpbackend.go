// Package opencode - ACP backend uses opencode's HTTP server API for richer interaction.
// It starts "opencode serve" in a tmux session and communicates via REST + SSE.
package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// acpSessionState holds the per-session ACP state (port, opencode session ID).
type acpSessionState struct {
	baseURL  string
	sessionID string
	fullID   string // datawatch session full_id (for channel reply routing)
}

// acpStateMap stores ACP state keyed by tmux session name so SendInput can POST
// to the running opencode server instead of using tmux send-keys.
var acpStateMap sync.Map // key: tmuxSession string, value: *acpSessionState

// acpFullIDs stores pending full_id associations set before acpStateMap is populated.
// Once acpStateMap is populated, the full_id is transferred into the state struct.
var acpFullIDs sync.Map // key: tmuxSession string, value: string (full_id)

// OnChannelReply is called with (fullID, text) when opencode sends a reply
// via its SSE event stream. Set this at daemon startup to route replies to
// the web UI and messaging backends (same path as claude channel replies).
var OnChannelReply func(fullID, text string)

// ACPBackend starts opencode as an HTTP server and communicates via its REST API.
type ACPBackend struct {
	binary         string
	startupTimeout time.Duration
	healthInterval time.Duration
	messageTimeout time.Duration
}

// NewACP creates an ACP-mode opencode backend.
func NewACP(binary string) llm.Backend {
	return NewACPWithTimeouts(binary, 0, 0, 0)
}

// NewACPWithTimeouts creates an ACP backend with configurable timeouts.
// Zero values use defaults (30s startup, 5s health, 30s message).
func NewACPWithTimeouts(binary string, startupTimeout, healthInterval, messageTimeout int) llm.Backend {
	if binary == "" {
		binary = "opencode"
	}
	st := time.Duration(startupTimeout) * time.Second
	if st <= 0 { st = 30 * time.Second }
	hi := time.Duration(healthInterval) * time.Second
	if hi <= 0 { hi = 5 * time.Second }
	mt := time.Duration(messageTimeout) * time.Second
	if mt <= 0 { mt = 30 * time.Second }
	return &ACPBackend{
		binary: resolveBinary(binary),
		startupTimeout: st,
		healthInterval: hi,
		messageTimeout: mt,
	}
}

func (b *ACPBackend) Name() string                  { return "opencode-acp" }
func (b *ACPBackend) SupportsInteractiveInput() bool { return true }

func (b *ACPBackend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Launch starts opencode serve in the tmux session, then drives it via HTTP API.
// Output is written to logFile as clean text lines from SSE events.
func (b *ACPBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	port, err := freePort()
	if err != nil {
		return fmt.Errorf("find free port: %w", err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Start opencode serve in the tmux session.
	binary := strings.ReplaceAll(b.binary, "'", `'\''`)
	projDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	serveCmd := fmt.Sprintf("cd '%s' && '%s' serve --port %d 2>&1", projDir, binary, port)
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, serveCmd, "Enter").Run(); err != nil {
		return fmt.Errorf("start opencode serve in %s: %w", tmuxSession, err)
	}

	// Background goroutine: wait for server ready, create session, send task, stream events.
	go func() {
		bgCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := waitForServer(bgCtx, baseURL, b.startupTimeout); err != nil {
			writeLogLine(logFile, fmt.Sprintf("[opencode-acp] server not ready: %v", err))
			return
		}
		writeLogLine(logFile, fmt.Sprintf("[opencode-acp] server ready on %s", baseURL))

		sessID, err := createSession(bgCtx, baseURL, projectDir)
		if err != nil {
			writeLogLine(logFile, fmt.Sprintf("[opencode-acp] create session failed: %v", err))
			return
		}
		writeLogLine(logFile, fmt.Sprintf("[opencode-acp] session %s created", sessID))

		// Store ACP state so SendInput can route HTTP POSTs.
		// Absorb any full_id that was registered via SetACPFullID before we were ready.
		st := &acpSessionState{baseURL: baseURL, sessionID: sessID}
		if v, ok := acpFullIDs.LoadAndDelete(tmuxSession); ok {
			st.fullID = v.(string)
		}
		acpStateMap.Store(tmuxSession, st)
		defer acpStateMap.Delete(tmuxSession)

		// Subscribe to SSE events and write to logFile.
		go streamEvents(bgCtx, baseURL, logFile, st)

		// Send the initial task (non-blocking: events arrive via SSE stream).
		if task != "" {
			writeLogLine(logFile, task)
			if err := sendMessage(bgCtx, baseURL, sessID, task); err != nil {
				writeLogLine(logFile, fmt.Sprintf("[opencode-acp] send initial task failed: %v", err))
			}
		} else {
			writeLogLine(logFile, "[opencode-acp] ready")
			writeLogLine(logFile, "[opencode-acp] awaiting input")
		}

		// Keep the goroutine alive until context cancelled or server dies.
		ticker := time.NewTicker(b.healthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := http.Get(baseURL + "/session"); err != nil {
					writeLogLine(logFile, "[opencode-acp] server gone")
					return
				}
			}
		}
	}()

	return nil
}

// LaunchResume resumes an opencode session by ID.
func (b *ACPBackend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	port, err := freePort()
	if err != nil {
		return fmt.Errorf("find free port: %w", err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	binary := strings.ReplaceAll(b.binary, "'", `'\''`)
	projDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	serveCmd := fmt.Sprintf("cd '%s' && '%s' serve --port %d 2>&1", projDir, binary, port)
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, serveCmd, "Enter").Run(); err != nil {
		return fmt.Errorf("start opencode serve in %s: %w", tmuxSession, err)
	}

	go func() {
		bgCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := waitForServer(bgCtx, baseURL, b.startupTimeout); err != nil {
			writeLogLine(logFile, fmt.Sprintf("[opencode-acp] server not ready: %v", err))
			return
		}

		st := &acpSessionState{baseURL: baseURL, sessionID: resumeID}
		if v, ok := acpFullIDs.LoadAndDelete(tmuxSession); ok {
			st.fullID = v.(string)
		}
		acpStateMap.Store(tmuxSession, st)
		defer acpStateMap.Delete(tmuxSession)

		go streamEvents(bgCtx, baseURL, logFile, st)

		if task != "" {
			if err := sendMessage(bgCtx, baseURL, resumeID, task); err != nil {
				writeLogLine(logFile, fmt.Sprintf("[opencode-acp] send task failed: %v", err))
			}
		}

		ticker := time.NewTicker(b.healthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return nil
}

// SetACPFullID associates a datawatch session full_id with an ACP tmux session.
// May be called before or after Launch() stores the acpSessionState — it stores
// the full_id in acpFullIDs and, if the state is already present, patches it in.
func SetACPFullID(tmuxSession, fullID string) {
	acpFullIDs.Store(tmuxSession, fullID)
	if val, ok := acpStateMap.Load(tmuxSession); ok {
		val.(*acpSessionState).fullID = fullID
	}
}

// SendMessageACP sends a follow-up message to an active opencode-acp session.
// Returns false if no ACP session is active for this tmux session.
func SendMessageACP(tmuxSession, text string) bool {
	val, ok := acpStateMap.Load(tmuxSession)
	if !ok {
		return false
	}
	state := val.(*acpSessionState)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sendMessage(ctx, state.baseURL, state.sessionID, text); err != nil {
		return false
	}
	return true
}

// ----- HTTP helpers ---------------------------------------------------------

func waitForServer(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := http.Get(baseURL + "/session")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("server did not become ready within %s", timeout)
}

func createSession(ctx context.Context, baseURL, projectDir string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"title":     "datawatch",
		"directory": projectDir,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/session", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode session response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("empty session ID in response")
	}
	return result.ID, nil
}

func sendMessage(ctx context.Context, baseURL, sessionID, text string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"parts": []map[string]string{
			{"type": "text", "text": text},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/session/"+sessionID+"/message", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// streamEvents subscribes to the opencode SSE event stream and writes
// human-readable lines to logFile. The text content from message parts
// is extracted and written as plain text, and also dispatched via OnChannelReply
// so the web UI can render ACP replies as amber channel-reply lines.
func streamEvents(ctx context.Context, baseURL, logFile string, st *acpSessionState) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/event", nil)
	if err != nil {
		writeLogLine(logFile, fmt.Sprintf("[opencode-acp] SSE request error: %v", err))
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeLogLine(logFile, fmt.Sprintf("[opencode-acp] SSE connect error: %v", err))
		return
	}
	defer resp.Body.Close()
	writeLogLine(logFile, "[opencode-acp] SSE stream connected")
	var pendingText strings.Builder // accumulates streaming deltas until step-finish

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var evt struct {
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		// Extract text content from message part events.
		switch evt.Type {
		case "message.part.updated":
			var props struct {
				Part struct {
					Type   string `json:"type"`
					Text   string `json:"text"`
					Reason string `json:"reason"`
				} `json:"part"`
			}
			if err := json.Unmarshal(evt.Properties, &props); err == nil {
				switch props.Part.Type {
				case "step-start":
					pendingText.Reset()
					writeLogLine(logFile, "[opencode-acp] thinking...")
				case "step-finish":
					// Flush accumulated response text
					if pendingText.Len() > 0 {
						text := pendingText.String()
						writeLogLine(logFile, text)
						if OnChannelReply != nil && st.fullID != "" {
							OnChannelReply(st.fullID, text)
						}
						pendingText.Reset()
					}
					writeLogLine(logFile, "[opencode-acp] done")
				case "text":
					// Text snapshot — captures response if deltas were missed or partial.
					// Also accumulates into pendingText for step-finish flush.
					if props.Part.Text != "" {
						if pendingText.Len() == 0 {
							// No deltas received — use the full text directly
							pendingText.WriteString(props.Part.Text)
						}
					}
				}
			}
		case "message.part.delta":
			var props struct {
				Delta string `json:"delta"`
				Field string `json:"field"`
			}
			if err := json.Unmarshal(evt.Properties, &props); err == nil {
				if props.Field == "text" && props.Delta != "" {
					pendingText.WriteString(props.Delta)
				}
			}
		case "session.status":
			var props struct {
				Status struct {
					Type string `json:"type"`
				} `json:"status"`
			}
			if err := json.Unmarshal(evt.Properties, &props); err == nil {
				switch props.Status.Type {
				case "busy":
					writeLogLine(logFile, "[opencode-acp] processing...")
				case "idle":
					writeLogLine(logFile, "[opencode-acp] ready")
				}
			}
		case "session.idle":
			// Session is idle and ready for the next prompt
			writeLogLine(logFile, "[opencode-acp] awaiting input")
		case "session.error":
			var props struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(evt.Properties, &props); err == nil && props.Error != "" {
				writeLogLine(logFile, fmt.Sprintf("[opencode-acp] error: %s", props.Error))
			}
		case "session.completed", "message.completed":
			writeLogLine(logFile, "DATAWATCH_COMPLETE: opencode-acp done")
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func writeLogLine(logFile, text string) {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	io.WriteString(f, text) //nolint:errcheck
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

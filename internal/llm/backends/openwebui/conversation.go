// Package openwebui implements the LLM backend for OpenWebUI (OpenAI-compatible API).
// conversation.go provides a Go-native conversation manager that maintains message
// history and streams responses, replacing the curl/python3 single-shot approach.
package openwebui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// chatMessage represents a single message in the OpenAI chat format.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the OpenAI-compatible chat completion request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// conversationState holds per-session conversation history.
type conversationState struct {
	messages []chatMessage
	mu       sync.Mutex
}

// InteractiveBackend uses Go HTTP to talk to OpenWebUI's OpenAI-compatible API.
// It maintains conversation history for multi-turn interactions.
type InteractiveBackend struct {
	baseURL string
	apiKey  string
	model   string

	// conversations stores per-tmux-session conversation state
	conversations sync.Map // key: tmuxSession, value: *conversationState
}

// NewInteractive creates an interactive OpenWebUI backend.
func NewInteractive(baseURL, apiKey, model string) llm.Backend {
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
	if model == "" {
		model = "llama3"
	}
	return &InteractiveBackend{baseURL: baseURL, apiKey: apiKey, model: model}
}

func (b *InteractiveBackend) Name() string                  { return "openwebui" }
func (b *InteractiveBackend) SupportsInteractiveInput() bool { return true }

func (b *InteractiveBackend) Version() string {
	if b.baseURL == "" || b.apiKey == "" {
		return ""
	}
	req, _ := http.NewRequest("GET", b.baseURL+"/api/v1/models", nil)
	if b.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	resp.Body.Close()
	return b.baseURL
}

// Launch starts a conversation with the initial task. It sends the first message
// to the API, streams the response into the tmux session, then waits for follow-up
// input via SendMessage.
func (b *InteractiveBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	// Initialize conversation state
	conv := &conversationState{}
	b.conversations.Store(tmuxSession, conv)

	// Build the shell command that runs the Go conversation loop.
	// We use a simple approach: send the first message via the API from Go,
	// pipe the streamed response to tmux, then show a prompt for follow-ups.
	// The follow-up loop is handled by datawatch's input system (send_input).

	projEscaped := strings.ReplaceAll(projectDir, "'", `'\''`)

	// Start with a prompt indicator, then stream the first response
	initCmd := fmt.Sprintf("cd '%s' && echo '[openwebui] interactive mode — model: %s'",
		projEscaped, b.model)
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, initCmd, "Enter").Run(); err != nil {
		return fmt.Errorf("init openwebui session: %w", err)
	}
	emitChat(tmuxSession, "system", fmt.Sprintf("Interactive mode — model: %s", b.model), false)

	// Send the initial task if provided
	if task != "" {
		go b.sendAndStream(ctx, tmuxSession, task)
	} else {
		// No task — show prompt and wait
		exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession,
			"echo '[openwebui] ready — send a message to begin'", "Enter").Run() //nolint:errcheck
	}

	return nil
}

// chatMemoryHandler is set from main.go to handle memory commands in chat.
// Returns (response, handled). If handled=true, the message was a memory command.
var chatMemoryHandler func(tmuxSession, text string) (string, bool)

// SetChatMemoryHandler registers a handler for memory commands in chat sessions.
func SetChatMemoryHandler(fn func(tmuxSession, text string) (string, bool)) {
	chatMemoryHandler = fn
}

// SendMessage sends a follow-up message to an active conversation and streams the response.
// Called by the session manager when input is sent to an openwebui session.
func (b *InteractiveBackend) SendMessage(tmuxSession, text string) error {
	// Check if this is a memory command (remember:, recall:, memories, forget, kg, etc.)
	if chatMemoryHandler != nil {
		if response, handled := chatMemoryHandler(tmuxSession, text); handled {
			// Emit as system message in chat
			emitChat(tmuxSession, "user", text, false)
			emitChat(tmuxSession, "system", response, false)
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return b.sendAndStream(ctx, tmuxSession, text)
}

// sendAndStream sends a message to the API, streams the SSE response, and writes
// each content chunk to the tmux session.
func (b *InteractiveBackend) sendAndStream(ctx context.Context, tmuxSession, userMsg string) error {
	// Get or create conversation state
	val, _ := b.conversations.LoadOrStore(tmuxSession, &conversationState{})
	conv := val.(*conversationState)

	// Add user message to history
	conv.mu.Lock()
	conv.messages = append(conv.messages, chatMessage{Role: "user", Content: userMsg})
	messages := make([]chatMessage, len(conv.messages))
	copy(messages, conv.messages)
	conv.mu.Unlock()

	// Emit user message via chat
	emitChat(tmuxSession, "user", userMsg, false)

	// Show the user message in tmux (kept for completion detection / fallback)
	displayMsg := userMsg
	if len(displayMsg) > 200 {
		displayMsg = displayMsg[:197] + "..."
	}
	exec.Command("tmux", "send-keys", "-t", tmuxSession,
		fmt.Sprintf("echo '\\n> %s'", strings.ReplaceAll(displayMsg, "'", "\\'")), "Enter").Run() //nolint:errcheck

	// Build request
	reqBody := chatRequest{
		Model:    b.model,
		Messages: messages,
		Stream:   true,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+"/api/chat/completions",
		strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		errMsg := fmt.Sprintf("echo '[openwebui] error: %s'", strings.ReplaceAll(err.Error(), "'", "\\'"))
		exec.Command("tmux", "send-keys", "-t", tmuxSession, errMsg, "Enter").Run() //nolint:errcheck
		return fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("echo '[openwebui] API error %d: %s'", resp.StatusCode,
			strings.ReplaceAll(string(body[:min(len(body), 200)]), "'", "\\'"))
		exec.Command("tmux", "send-keys", "-t", tmuxSession, errMsg, "Enter").Run() //nolint:errcheck
		return fmt.Errorf("API returned %d", resp.StatusCode)
	}

	// Stream SSE response, collecting chunks and writing to tmux via echo
	var fullResponse strings.Builder
	var lineBuffer strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content := chunk.Choices[0].Delta.Content
			fullResponse.WriteString(content)
			lineBuffer.WriteString(content)
			// Emit streaming chunk via chat
			emitChat(tmuxSession, "assistant", content, true)
			// Flush each line to tmux via printf (not send-keys, which executes as shell)
			if strings.Contains(content, "\n") {
				parts := strings.Split(lineBuffer.String(), "\n")
				for i, p := range parts {
					if i < len(parts)-1 {
						// Complete line — echo it
						escaped := strings.ReplaceAll(p, "'", "'\\''")
						exec.Command("tmux", "send-keys", "-t", tmuxSession,
							fmt.Sprintf("printf '%%s\\n' '%s'", escaped), "Enter").Run() //nolint:errcheck
					}
				}
				lineBuffer.Reset()
				// Keep the incomplete tail
				lineBuffer.WriteString(parts[len(parts)-1])
			}
		}
	}

	// Flush any remaining content in the line buffer
	if lineBuffer.Len() > 0 {
		escaped := strings.ReplaceAll(lineBuffer.String(), "'", "'\\''")
		exec.Command("tmux", "send-keys", "-t", tmuxSession,
			fmt.Sprintf("printf '%%s\\n' '%s'", escaped), "Enter").Run() //nolint:errcheck
	}

	// Add assistant response to history and emit final chat message
	if fullResponse.Len() > 0 {
		conv.mu.Lock()
		conv.messages = append(conv.messages, chatMessage{
			Role:    "assistant",
			Content: fullResponse.String(),
		})
		conv.mu.Unlock()
		// Signal streaming complete (content empty, streaming=false)
		emitChat(tmuxSession, "assistant", "", false)
	}

	// Show ready prompt
	exec.Command("tmux", "send-keys", "-t", tmuxSession,
		"echo '[openwebui] ready for next message'", "Enter").Run() //nolint:errcheck

	return nil
}

// Cleanup removes conversation state for a tmux session.
func (b *InteractiveBackend) Cleanup(tmuxSession string) {
	b.conversations.Delete(tmuxSession)
}

// chatEmitter is set from main.go to broadcast structured chat messages via WS.
// Signature: func(sessionID, role, content string, streaming bool)
var chatEmitter func(string, string, string, bool)

// SetChatEmitter registers the callback for broadcasting chat messages.
func SetChatEmitter(fn func(sessionID, role, content string, streaming bool)) {
	chatEmitter = fn
}

// emitChat sends a structured chat message if the emitter is registered.
func emitChat(tmuxSession, role, content string, streaming bool) {
	if chatEmitter != nil {
		// tmuxSession is like "cs-XXXX", strip "cs-" to get the full session ID
		sessionID := strings.TrimPrefix(tmuxSession, "cs-")
		chatEmitter(sessionID, role, content, streaming)
	}
}

// activeBackend holds a reference to the registered interactive backend
// so SendMessageOWUI can find it from the session manager.
var activeBackend *InteractiveBackend

// SetActiveBackend stores the backend reference for SendMessageOWUI routing.
func SetActiveBackend(b llm.Backend) {
	if ib, ok := b.(*InteractiveBackend); ok {
		activeBackend = ib
	}
}

// SendMessageOWUI routes input through the Go HTTP conversation manager
// instead of tmux send-keys. Returns true if handled, false to fall back to tmux.
func SendMessageOWUI(tmuxSession, text string) bool {
	if activeBackend == nil {
		return false
	}
	// Check if this tmux session has an active conversation
	if _, ok := activeBackend.conversations.Load(tmuxSession); !ok {
		return false
	}
	go func() {
		if err := activeBackend.SendMessage(tmuxSession, text); err != nil {
			fmt.Printf("[openwebui] SendMessage error: %v\n", err)
		}
	}()
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

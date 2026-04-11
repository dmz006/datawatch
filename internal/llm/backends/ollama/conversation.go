package ollama

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ChatMessage is an OpenAI-compatible message (exported for persistence).
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// conversationState holds per-session history.
type conversationState struct {
	messages []ChatMessage
	mu       sync.Mutex
}

// conversations stores per-session state.
var conversations sync.Map

// chatEmitter broadcasts structured chat messages.
var chatEmitter func(string, string, string, bool)

// SaveConversationFn persists conversation history for daemon restart reconnect.
// Signature: func(tmuxSession string, messages []ChatMessage)
var SaveConversationFn func(string, []ChatMessage)

// SetSaveConversationFn registers the conversation persistence callback.
func SetSaveConversationFn(fn func(string, []ChatMessage)) {
	SaveConversationFn = fn
}

// GetConversationHistory returns the current conversation for a session (for persistence).
func GetConversationHistory(tmuxSession string) []ChatMessage {
	val, ok := conversations.Load(tmuxSession)
	if !ok {
		return nil
	}
	conv := val.(*conversationState)
	conv.mu.Lock()
	defer conv.mu.Unlock()
	msgs := make([]ChatMessage, len(conv.messages))
	copy(msgs, conv.messages)
	return msgs
}

// RestoreConversation loads conversation history into a session (for reconnect).
func RestoreConversation(tmuxSession string, messages []ChatMessage, b *Backend) {
	conv := &conversationState{messages: messages}
	conversations.Store(tmuxSession, conv)
	registerBackend(tmuxSession, b)
}

// SetChatEmitter registers the callback for Ollama chat messages.
func SetChatEmitter(fn func(sessionID, role, content string, streaming bool)) {
	chatEmitter = fn
}

func emitChat(tmuxSession, role, content string, streaming bool) {
	if chatEmitter != nil {
		sessionID := strings.TrimPrefix(tmuxSession, "cs-")
		chatEmitter(sessionID, role, content, streaming)
	}
}

// LaunchChat starts an Ollama conversation using the API instead of `ollama run`.
// This enables structured chat_message events for the rich chat UI.
func (b *Backend) LaunchChat(ctx context.Context, task, tmuxSession, projectDir string) error {
	conv := &conversationState{}
	conversations.Store(tmuxSession, conv)

	// Show init in tmux (for fallback/logging)
	exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession,
		fmt.Sprintf("echo '[ollama] chat mode — model: %s'", b.model), "Enter").Run() //nolint:errcheck

	emitChat(tmuxSession, "system", fmt.Sprintf("Chat mode — model: %s", b.model), false)

	if task != "" {
		go b.sendAndStream(ctx, tmuxSession, task, true)
	} else {
		emitChat(tmuxSession, "system", "Ready — send a message to begin", false)
		exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession,
			"echo '[ollama] ready — send a message'", "Enter").Run() //nolint:errcheck
	}
	return nil
}

// SendMessageOllama sends a follow-up message via the API.
func SendMessageOllama(tmuxSession, text string) bool {
	val, ok := conversations.Load(tmuxSession)
	if !ok {
		return false
	}
	_ = val
	// Find the backend instance — use the stored host/model
	go func() {
		b := findBackend(tmuxSession)
		if b == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		b.sendAndStream(ctx, tmuxSession, text, false) //nolint:errcheck
	}()
	return true
}

var backendRegistry sync.Map

func registerBackend(tmuxSession string, b *Backend) {
	backendRegistry.Store(tmuxSession, b)
}

func findBackend(tmuxSession string) *Backend {
	val, ok := backendRegistry.Load(tmuxSession)
	if !ok {
		return nil
	}
	return val.(*Backend)
}

func (b *Backend) sendAndStream(ctx context.Context, tmuxSession, userMsg string, emitUser bool) error {
	val, _ := conversations.LoadOrStore(tmuxSession, &conversationState{})
	conv := val.(*conversationState)

	conv.mu.Lock()
	conv.messages = append(conv.messages, ChatMessage{Role: "user", Content: userMsg})
	messages := make([]ChatMessage, len(conv.messages))
	copy(messages, conv.messages)
	conv.mu.Unlock()

	// emitUser=true for initial launch task (bypasses SendInput).
	// emitUser=false for follow-ups routed through SendInput (manager already emits).
	if emitUser {
		emitChat(tmuxSession, "user", userMsg, false)
	}

	// Show user message in tmux (fallback)
	displayMsg := userMsg
	if len(displayMsg) > 200 {
		displayMsg = displayMsg[:197] + "..."
	}
	exec.Command("tmux", "send-keys", "-t", tmuxSession,
		fmt.Sprintf("echo '> %s'", strings.ReplaceAll(displayMsg, "'", "\\'")), "Enter").Run() //nolint:errcheck

	// Emit processing indicator so the user knows the prompt is being handled
	emitChat(tmuxSession, "system", "Processing...", false)

	// Build request — Ollama uses /api/chat
	host := b.host
	if host == "" {
		host = "http://localhost:11434"
	}

	reqBody := map[string]interface{}{
		"model":    b.model,
		"messages": messages,
		"stream":   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", host+"/api/chat",
		strings.NewReader(string(bodyBytes)))
	if err != nil {
		emitChat(tmuxSession, "system", fmt.Sprintf("Error: %v", err), false)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		emitChat(tmuxSession, "system", fmt.Sprintf("Error: %v", err), false)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		emitChat(tmuxSession, "system", fmt.Sprintf("Ollama error: HTTP %d", resp.StatusCode), false)
		return fmt.Errorf("ollama HTTP %d", resp.StatusCode)
	}

	// Stream response
	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if json.Unmarshal([]byte(line), &chunk) != nil {
			continue
		}
		if chunk.Message.Content != "" {
			fullResponse.WriteString(chunk.Message.Content)
			emitChat(tmuxSession, "assistant", chunk.Message.Content, true)
		}
		if chunk.Done {
			break
		}
	}

	// Finalize
	if fullResponse.Len() > 0 {
		conv.mu.Lock()
		conv.messages = append(conv.messages, ChatMessage{Role: "assistant", Content: fullResponse.String()})
		// Persist conversation for reconnect
		if SaveConversationFn != nil {
			msgs := make([]ChatMessage, len(conv.messages))
			copy(msgs, conv.messages)
			go SaveConversationFn(tmuxSession, msgs)
		}
		conv.mu.Unlock()
		emitChat(tmuxSession, "assistant", "", false) // signal streaming complete
	}

	// Show in tmux (fallback)
	exec.Command("tmux", "send-keys", "-t", tmuxSession,
		"echo '[ollama] ready for next message'", "Enter").Run() //nolint:errcheck

	return nil
}

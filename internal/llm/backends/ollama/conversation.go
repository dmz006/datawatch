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

// chatMessage is an OpenAI-compatible message.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// conversationState holds per-session history.
type conversationState struct {
	messages []chatMessage
	mu       sync.Mutex
}

// conversations stores per-session state.
var conversations sync.Map

// chatEmitter broadcasts structured chat messages.
var chatEmitter func(string, string, string, bool)

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
		go b.sendAndStream(ctx, tmuxSession, task)
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
		b.sendAndStream(ctx, tmuxSession, text) //nolint:errcheck
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

func (b *Backend) sendAndStream(ctx context.Context, tmuxSession, userMsg string) error {
	val, _ := conversations.LoadOrStore(tmuxSession, &conversationState{})
	conv := val.(*conversationState)

	conv.mu.Lock()
	conv.messages = append(conv.messages, chatMessage{Role: "user", Content: userMsg})
	messages := make([]chatMessage, len(conv.messages))
	copy(messages, conv.messages)
	conv.mu.Unlock()

	emitChat(tmuxSession, "user", userMsg, false)

	// Show user message in tmux (fallback)
	displayMsg := userMsg
	if len(displayMsg) > 200 {
		displayMsg = displayMsg[:197] + "..."
	}
	exec.Command("tmux", "send-keys", "-t", tmuxSession,
		fmt.Sprintf("echo '> %s'", strings.ReplaceAll(displayMsg, "'", "\\'")), "Enter").Run() //nolint:errcheck

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
		conv.messages = append(conv.messages, chatMessage{Role: "assistant", Content: fullResponse.String()})
		conv.mu.Unlock()
		emitChat(tmuxSession, "assistant", "", false) // signal streaming complete
	}

	// Show in tmux (fallback)
	exec.Command("tmux", "send-keys", "-t", tmuxSession,
		"echo '[ollama] ready for next message'", "Enter").Run() //nolint:errcheck

	return nil
}

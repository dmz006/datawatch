package memory

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ConversationFormat identifies the source format of a conversation export.
type ConversationFormat string

const (
	FormatClaude  ConversationFormat = "claude"
	FormatChatGPT ConversationFormat = "chatgpt"
	FormatGeneric ConversationFormat = "generic"
)

// ConversationMessage is a normalized message from any format.
type ConversationMessage struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// MineConversation ingests a conversation export, normalizes it, and stores
// each exchange pair as a memory. Returns the number of memories created.
func (r *Retriever) MineConversation(projectDir string, reader io.Reader, format ConversationFormat) (int, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, fmt.Errorf("read conversation: %w", err)
	}

	var messages []ConversationMessage
	switch format {
	case FormatClaude:
		messages, err = parseClaudeTranscript(data)
	case FormatChatGPT:
		messages, err = parseChatGPTExport(data)
	default:
		messages, err = parseGenericJSON(data)
	}
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", format, err)
	}

	count := 0
	// Store each user-assistant exchange pair
	for i := 0; i < len(messages)-1; i++ {
		if messages[i].Role == "user" && i+1 < len(messages) && messages[i+1].Role == "assistant" {
			content := fmt.Sprintf("User: %s\nAssistant: %s",
				truncateForMining(messages[i].Content, 500),
				truncateForMining(messages[i+1].Content, 1000))

			_, err := r.Remember(projectDir, content)
			if err == nil {
				count++
			}
			i++ // skip the assistant message
		}
	}
	return count, nil
}

// parseClaudeTranscript parses Claude Code JSONL transcript format.
func parseClaudeTranscript(data []byte) ([]ConversationMessage, error) {
	var messages []ConversationMessage
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Message struct {
				Role    string      `json:"role"`
				Content interface{} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Message.Role == "" {
			continue
		}
		content := ""
		switch c := entry.Message.Content.(type) {
		case string:
			content = c
		case []interface{}:
			for _, item := range c {
				if m, ok := item.(map[string]interface{}); ok {
					if text, ok := m["text"].(string); ok {
						content += text
					}
				}
			}
		}
		if content != "" {
			messages = append(messages, ConversationMessage{Role: entry.Message.Role, Content: content})
		}
	}
	return messages, nil
}

// parseChatGPTExport parses ChatGPT JSON export format.
func parseChatGPTExport(data []byte) ([]ConversationMessage, error) {
	var export []struct {
		Mapping map[string]struct {
			Message struct {
				Author struct {
					Role string `json:"role"`
				} `json:"author"`
				Content struct {
					Parts []interface{} `json:"parts"`
				} `json:"content"`
			} `json:"message"`
		} `json:"mapping"`
	}
	if err := json.Unmarshal(data, &export); err != nil {
		// Try single conversation format
		var single struct {
			Mapping map[string]struct {
				Message struct {
					Author struct {
						Role string `json:"role"`
					} `json:"author"`
					Content struct {
						Parts []interface{} `json:"parts"`
					} `json:"content"`
				} `json:"message"`
			} `json:"mapping"`
		}
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			return nil, err
		}
		export = append(export, single)
	}

	var messages []ConversationMessage
	for _, conv := range export {
		for _, node := range conv.Mapping {
			role := node.Message.Author.Role
			if role != "user" && role != "assistant" {
				continue
			}
			var parts []string
			for _, p := range node.Message.Content.Parts {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				messages = append(messages, ConversationMessage{Role: role, Content: strings.Join(parts, "\n")})
			}
		}
	}
	return messages, nil
}

// parseGenericJSON parses a simple JSON array of {role, content} messages.
func parseGenericJSON(data []byte) ([]ConversationMessage, error) {
	var messages []ConversationMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func truncateForMining(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

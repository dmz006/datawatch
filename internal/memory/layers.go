package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layers implements the 4-layer memory wake-up stack.
//
//   L0: Identity       (~100 tokens)  — always loaded, from identity.txt
//   L1: Critical facts  (~500 tokens) — auto-generated from top memories
//   L2: Room context    (variable)    — loaded when topic matches a room
//   L3: Deep search     (unlimited)   — on-demand recall (existing functionality)
//
// Wake-up cost: ~600 tokens (L0+L1). Leaves 95%+ of context free.
type Layers struct {
	dataDir   string
	retriever *Retriever
	// peers (BL96) feeds the L5 sibling-visibility layer. Optional.
	peers PeerLister
}

// NewLayers creates a layer stack backed by the given retriever.
func NewLayers(dataDir string, retriever *Retriever) *Layers {
	return &Layers{dataDir: dataDir, retriever: retriever}
}

// L0 returns the identity text from {dataDir}/identity.txt.
// Returns a default message if the file doesn't exist.
func (l *Layers) L0() string {
	path := filepath.Join(l.dataDir, "identity.txt")
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// L1 returns the top critical facts from the memory store.
// These are the highest-value memories across all roles, limited to ~500 tokens.
func (l *Layers) L1(projectDir string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 2000 // ~500 tokens
	}

	// Get top memories by recency (learnings + manual facts first)
	learnings, _ := l.retriever.Store().ListByRole(projectDir, "learning", 5)
	manuals, _ := l.retriever.Store().ListByRole(projectDir, "manual", 5)
	sessions, _ := l.retriever.Store().ListByRole(projectDir, "session", 3)

	var b strings.Builder
	totalChars := 0

	appendMemory := func(m Memory) bool {
		line := fmt.Sprintf("- [%s] %s\n", m.Role, truncateStr(m.Content, 150))
		if totalChars+len(line) > maxChars {
			return false
		}
		b.WriteString(line)
		totalChars += len(line)
		return true
	}

	for _, m := range learnings {
		if !appendMemory(m) {
			break
		}
	}
	for _, m := range manuals {
		if !appendMemory(m) {
			break
		}
	}
	for _, m := range sessions {
		if !appendMemory(m) {
			break
		}
	}

	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

// L2 returns room-specific context when a topic matches an existing room.
func (l *Layers) L2(projectDir, topic string, maxResults int) string {
	if topic == "" || maxResults <= 0 {
		return ""
	}
	wing := filepath.Base(projectDir)
	// Try to find memories in rooms matching the topic
	rooms, err := l.retriever.Store().ListRooms(wing)
	if err != nil || len(rooms) == 0 {
		return ""
	}

	// Find best matching room
	topicLower := strings.ToLower(topic)
	bestRoom := ""
	for _, r := range rooms {
		if strings.Contains(topicLower, strings.ToLower(r)) ||
			strings.Contains(strings.ToLower(r), topicLower) {
			bestRoom = r
			break
		}
	}
	if bestRoom == "" {
		return ""
	}

	memories, err := l.retriever.Store().SearchFiltered(wing, bestRoom, nil, maxResults)
	if err != nil || len(memories) == 0 {
		// Fallback: list by wing+room without vector search
		filtered, _ := l.retriever.Store().ListFiltered(projectDir, "", "", maxResults)
		var roomMemories []Memory
		for _, m := range filtered {
			if m.Room == bestRoom {
				roomMemories = append(roomMemories, m)
			}
		}
		memories = roomMemories
	}

	if len(memories) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Context: %s/%s\n", wing, bestRoom)
	for _, m := range memories {
		fmt.Fprintf(&b, "- %s\n", truncateStr(m.Content, 200))
	}
	return b.String()
}

// WakeUpContext returns the combined L0+L1 context for session start injection.
// This is called automatically when auto_retrieve is enabled.
func (l *Layers) WakeUpContext(projectDir string) string {
	var parts []string

	l0 := l.L0()
	if l0 != "" {
		parts = append(parts, "## Identity\n"+l0)
	}

	l1 := l.L1(projectDir, 2000)
	if l1 != "" {
		parts = append(parts, "## Key Facts\n"+l1)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

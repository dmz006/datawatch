package session

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestPromptDebounce_SuppressesFalsePositives(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir + "/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{FullID: "test-session", ID: "test", State: StateRunning}
	_ = store.Save(sess)

	m := &Manager{
		store:            store,
		promptFirstSeen:   make(map[string]time.Time),
		promptLastNotify:  make(map[string]time.Time),
		promptOscillation: make(map[string][]time.Time),
		monitors:         make(map[string]context.CancelFunc),
		trackers:         make(map[string]*Tracker),
		detection: config.DetectionConfig{
			PromptDebounce: 1, // 1 second debounce for fast test
			NotifyCooldown: 1,
		},
	}

	// First call starts debounce — should NOT transition
	result := m.tryTransitionToWaiting("test-session", "❯", "", nil)
	if result {
		t.Fatal("expected debounce to prevent immediate transition")
	}

	// Second call within debounce window — should still NOT transition
	result = m.tryTransitionToWaiting("test-session", "❯", "", nil)
	if result {
		t.Fatal("expected debounce to prevent transition within window")
	}
}

func TestPromptDebounce_ResetOnNewOutput(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir + "/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{FullID: "test-session", ID: "test", State: StateRunning}
	_ = store.Save(sess)

	m := &Manager{
		store:            store,
		promptFirstSeen:   make(map[string]time.Time),
		promptLastNotify:  make(map[string]time.Time),
		promptOscillation: make(map[string][]time.Time),
		monitors:         make(map[string]context.CancelFunc),
		trackers:         make(map[string]*Tracker),
		detection: config.DetectionConfig{
			PromptDebounce: 1,
			NotifyCooldown: 1,
		},
	}

	// Start debounce
	m.tryTransitionToWaiting("test-session", "❯", "", nil)

	// New output arrives — should reset the debounce
	m.resetPromptDebounce("test-session")

	// Verify the timer was cleared
	m.mu.Lock()
	_, exists := m.promptFirstSeen["test-session"]
	m.mu.Unlock()
	if exists {
		t.Fatal("expected debounce timer to be cleared after resetPromptDebounce")
	}
}

func TestPromptDebounce_SkipDebounce(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir + "/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{FullID: "test-skip", ID: "skip", State: StateRunning}
	_ = store.Save(sess)

	var stateChanged bool
	m := &Manager{
		store:            store,
		promptFirstSeen:   make(map[string]time.Time),
		promptLastNotify:  make(map[string]time.Time),
		promptOscillation: make(map[string][]time.Time),
		monitors:         make(map[string]context.CancelFunc),
		trackers:         make(map[string]*Tracker),
		detection: config.DetectionConfig{
			PromptDebounce: 10, // long debounce
			NotifyCooldown: 1,
		},
		onStateChange: func(s *Session, old State) { stateChanged = true },
	}

	// With skipDebounce=true, should transition immediately despite long debounce
	result := m.tryTransitionToWaiting("test-skip", "DATAWATCH_NEEDS_INPUT: question", "", nil, true)
	if !result {
		t.Fatal("expected skipDebounce to allow immediate transition")
	}
	if !stateChanged {
		t.Fatal("expected onStateChange to fire")
	}
}

func TestNotifyCooldown_SuppressesDuplicates(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir + "/sessions.json")
	if err != nil {
		t.Fatal(err)
	}

	notifyCount := 0
	m := &Manager{
		store:            store,
		promptFirstSeen:   make(map[string]time.Time),
		promptLastNotify:  make(map[string]time.Time),
		promptOscillation: make(map[string][]time.Time),
		monitors:         make(map[string]context.CancelFunc),
		trackers:         make(map[string]*Tracker),
		detection: config.DetectionConfig{
			PromptDebounce: 0, // disabled for this test
			NotifyCooldown: 60, // long cooldown
		},
		onNeedsInput:  func(s *Session, p string) { notifyCount++ },
		onStateChange: func(s *Session, old State) {},
	}

	// First notification — create session as running
	sess1 := &Session{FullID: "test-cool", ID: "cool", State: StateRunning}
	_ = store.Save(sess1)
	m.tryTransitionToWaiting("test-cool", "prompt1", "", nil, true)

	// Reset to running for second transition
	sess1.State = StateRunning
	sess1.LastPrompt = ""
	_ = store.Save(sess1)

	// Second notification within cooldown — should be suppressed
	m.tryTransitionToWaiting("test-cool", "prompt2", "", nil, true)

	if notifyCount != 1 {
		t.Fatalf("expected 1 notification (cooldown suppressed duplicate), got %d", notifyCount)
	}
}

func TestPromptDebounceDefaults(t *testing.T) {
	m := &Manager{
		detection: config.DetectionConfig{}, // zero values
	}
	if m.promptDebounceSeconds() != 3*time.Second {
		t.Fatalf("expected default 3s debounce, got %v", m.promptDebounceSeconds())
	}
	if m.notifyCooldownSeconds() != 15*time.Second {
		t.Fatalf("expected default 15s cooldown, got %v", m.notifyCooldownSeconds())
	}
}

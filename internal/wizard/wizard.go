// Package wizard provides a stateful multi-turn setup wizard engine that works
// over any text channel (Signal, Telegram, Discord, Slack, etc.).
package wizard

import (
	"fmt"
	"strings"
	"sync"
)

// Step is one question in a setup wizard.
type Step struct {
	// Key is the name stored in collected data.
	Key string
	// Prompt is the message sent to the user.
	Prompt string
	// OptionsFunc, if non-nil, is called before sending the prompt. It returns
	// a list of string options; the user replies with a number 1..N. The
	// selected option value (not the number) is stored under Key.
	OptionsFunc func(collected map[string]string) ([]string, error)
	// Validate optionally validates free-text input. Return an error to reject
	// the value and re-ask.
	Validate func(value string) error
	// Optional marks a step as skippable: if the user sends an empty reply,
	// the step is skipped (empty string stored).
	Optional bool
}

// Def defines a wizard for a particular service.
type Def struct {
	// Service is the short service name, e.g. "telegram".
	Service string
	// Intro is sent to the user before the first step.
	Intro string
	// Steps is the ordered list of questions.
	Steps []Step
	// OnComplete is called with the collected data when all steps finish.
	// It should load config, apply the values, and save.
	OnComplete func(cfgPath string, data map[string]string) error
}

// Session tracks an in-progress wizard interaction for one messaging group.
type Session struct {
	def       *Def
	current   int
	collected map[string]string
	options   []string // current numbered list, if any
	sendFn    func(string)
	cfgPath   string
}

// Manager holds all active wizard sessions keyed by groupID.
type Manager struct {
	mu      sync.Mutex
	active  map[string]*Session
	defs    map[string]*Def
	cfgPath string
}

// NewManager creates a wizard Manager. cfgPath is the config file to pass to
// each wizard's OnComplete handler.
func NewManager(cfgPath string) *Manager {
	m := &Manager{
		active:  make(map[string]*Session),
		defs:    make(map[string]*Def),
		cfgPath: cfgPath,
	}
	return m
}

// Register adds a wizard definition. Call this for every supported service
// before StartWizard.
func (m *Manager) Register(def *Def) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defs[strings.ToLower(def.Service)] = def
}

// StartWizard begins a wizard for the given service in the given groupID.
// sendFn is used to send messages back to the channel.
func (m *Manager) StartWizard(groupID, service string, sendFn func(string)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strings.ToLower(strings.TrimSpace(service))
	def, ok := m.defs[key]
	if !ok {
		return fmt.Errorf("unknown service %q — available: %s", service, m.serviceList())
	}

	sess := &Session{
		def:       def,
		current:   0,
		collected: make(map[string]string),
		sendFn:    sendFn,
		cfgPath:   m.cfgPath,
	}
	m.active[groupID] = sess

	// Send intro + first step
	if def.Intro != "" {
		sendFn(def.Intro)
	}
	m.sendStep(sess)
	return nil
}

// HandleMessage routes an incoming message text to the active wizard for groupID.
// Returns true if the message was consumed by the wizard.
func (m *Manager) HandleMessage(groupID, text string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.active[groupID]
	if !ok {
		return false
	}

	text = strings.TrimSpace(text)
	step := sess.def.Steps[sess.current]

	if len(sess.options) > 0 {
		// Expect a numeric selection
		var sel int
		if _, err := fmt.Sscanf(text, "%d", &sel); err != nil || sel < 1 || sel > len(sess.options) {
			if text == "cancel" || text == "abort" {
				delete(m.active, groupID)
				sess.sendFn("Setup cancelled.")
				return true
			}
			sess.sendFn(fmt.Sprintf("Please enter a number between 1 and %d (or 'cancel' to abort):", len(sess.options)))
			return true
		}
		sess.collected[step.Key] = sess.options[sel-1]
		sess.options = nil
	} else {
		// Free-text input
		if text == "cancel" || text == "abort" {
			delete(m.active, groupID)
			sess.sendFn("Setup cancelled.")
			return true
		}
		if text == "" && step.Optional {
			sess.collected[step.Key] = ""
		} else if text == "" && !step.Optional {
			sess.sendFn(fmt.Sprintf("This field is required. %s", step.Prompt))
			return true
		} else {
			if step.Validate != nil {
				if err := step.Validate(text); err != nil {
					sess.sendFn(fmt.Sprintf("Invalid input: %v\n%s", err, step.Prompt))
					return true
				}
			}
			sess.collected[step.Key] = text
		}
	}

	sess.current++

	if sess.current >= len(sess.def.Steps) {
		// All steps done — run completion
		delete(m.active, groupID)
		collected := sess.collected
		sendFn := sess.sendFn
		cfgPath := sess.cfgPath
		def := sess.def
		// Run completion outside the lock to avoid deadlock if OnComplete re-enters
		m.mu.Unlock()
		if err := def.OnComplete(cfgPath, collected); err != nil {
			sendFn(fmt.Sprintf("Setup failed: %v", err))
		} else {
			sendFn(fmt.Sprintf("%s backend configured. Restart the daemon to apply: datawatch stop && datawatch start", def.Service))
		}
		m.mu.Lock()
		return true
	}

	m.sendStep(sess)
	return true
}

// HasActive reports whether there is an active wizard for the given groupID.
func (m *Manager) HasActive(groupID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.active[groupID]
	return ok
}

// sendStep sends the current step prompt (or options list) to the user.
// Must be called with m.mu held.
func (m *Manager) sendStep(sess *Session) {
	step := sess.def.Steps[sess.current]

	if step.OptionsFunc != nil {
		opts, err := step.OptionsFunc(sess.collected)
		if err != nil {
			sess.sendFn(fmt.Sprintf("Could not fetch options: %v\nEnter value manually:", err))
			// Fall through to free-text for this step
		} else if len(opts) > 0 {
			sess.options = opts
			var sb strings.Builder
			sb.WriteString(step.Prompt + "\n")
			for i, o := range opts {
				sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, o))
			}
			sb.WriteString("Reply with a number, or 'cancel' to abort.")
			sess.sendFn(sb.String())
			return
		}
		// No options returned — fall through to free-text
		sess.options = nil
	}

	prompt := step.Prompt
	if step.Optional {
		prompt += " (optional — press Enter to skip)"
	}
	sess.sendFn(prompt)
}

// serviceList returns a comma-separated list of registered service names.
func (m *Manager) serviceList() string {
	names := make([]string, 0, len(m.defs))
	for k := range m.defs {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}

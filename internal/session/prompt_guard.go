// Nothing types an answer to a permission or safety prompt on its own.
//
// A Claude Code approval menu has "Yes" preselected, so ANY text followed by
// Enter approves it. The daemon therefore reads the live pane before every
// send it makes without a person asking for that exact input, and sends
// nothing while a prompt that wants a decision is on screen.

package session

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// ErrPromptOnScreen is returned when an automated send is refused because
// the session is showing (or may be showing) a permission, safety or
// confirmation prompt.
var ErrPromptOnScreen = errors.New("not typed: the session shows a permission or confirmation prompt that only a person may answer")

// Input sources the daemon uses when it types without a person asking for
// that exact input. A new daemon sender must be listed here (the call-site
// audit test enforces that every SendInput caller is classified).
const (
	// FilterInputSource is the source prefix of output-filter input; the
	// full source is "filter:<filter id>".
	FilterInputSource = "filter"
	// StartupAcceptSource is the launcher's startup-dialog auto-accept. It
	// answers the known startup dialogs on purpose, so it is held back only
	// by tool and safety approval prompts.
	StartupAcceptSource = "auto-accept-disclaimer"
)

var automatedInputSources = map[string]bool{
	FilterInputSource:   true,
	StartupAcceptSource: true,
	"schedule":          true,
	"channel-task":      true,
	"automaton":         true,
	"questions-answer":  true,
	"loop-rescue":       true,
	"exit-hook":         true,
	"supervisor-wake":   true,
	"discussion-sub":    true,
	"mcp-reply":         true,
	"lifecycle-sub":     true,
	"idle-nudge":        true,
	"task-redelivery":   true,
}

// IsAutomatedInputSource reports whether source names a daemon sender that
// is not a person asking for that exact input.
func IsAutomatedInputSource(source string) bool {
	if _, ok := filterSourceID(source); ok {
		return true
	}
	return automatedInputSources[source]
}

// filterSourceID returns the filter id of a filter input source.
func filterSourceID(source string) (string, bool) {
	if source == FilterInputSource {
		return "", true
	}
	if id, ok := strings.CutPrefix(source, FilterInputSource+":"); ok {
		return id, true
	}
	return "", false
}

// FilterSource builds the input source for text typed by filter id.
func FilterSource(id string) string { return FilterInputSource + ":" + id }

const filterMarkTail = " typed this, not the operator]"

// MarkFilterInput prefixes text typed by an output filter so that neither a
// session nor a person can take it for the operator's own input.
func MarkFilterInput(id, text string) string {
	if id == "" {
		id = "unknown"
	}
	mark := "[datawatch filter " + id + filterMarkTail
	if strings.HasPrefix(text, mark) {
		return text
	}
	return mark + " " + text
}

// approvalPhrases are the lower-case wordings of tool and safety approval
// prompts. Startup dialogs (folder trust, development channels, MCP server,
// bypass disclaimer) must not match any of them.
var approvalPhrases = []string{
	"do you want to proceed",
	"would you like to proceed",
	"do you want to make this edit",
	"do you want to create",
	"do you want to overwrite",
	"do you want to delete",
	"do you want to allow",
	"do you want to run",
	"requires approval",
	"requires your approval",
	"requires confirmation",
	"requires permission",
	"needs your approval",
	"needs your permission",
	"only a person",
	"must be approved by",
	"don't ask again",
	"don’t ask again",
	"tell claude what to do differently",
	"allow this action",
	"allow this command",
	"allow this tool",
	"allow execution",
	"permission required",
}

var (
	// menuSelectorLine is a numbered choice under the selector cursor.
	menuSelectorLine = regexp.MustCompile(`^[\s│┃|]*❯\s*\d+[.)]\s`)
	// approvalSelectorLine is a selector sitting on a yes/no/allow choice.
	approvalSelectorLine = regexp.MustCompile(`^[\s│┃|]*❯\s*\d+[.)]\s+(yes|no|allow)\b`)
	menuYesLine          = regexp.MustCompile(`^[\s│┃|❯>]*1[.)]\s+yes\b`)
	menuNoLine           = regexp.MustCompile(`^[\s│┃|❯>]*[2-9][.)]\s+no\b`)
	yesNoToken           = regexp.MustCompile(`\[y/n\]|\(y/n\)|\(yes/no(/[^)]*)?\)|\(y/n/always\)|\(y\)es/\(n\)o`)
)

const (
	// promptTailLines is how many non-empty bottom lines of a pane are
	// searched: an active prompt sits at the bottom, older text above it is
	// transcript.
	promptTailLines = 25
	// phraseAloneLines is how close to the bottom a bare approval wording
	// must be when no choice list or footer accompanies it.
	phraseAloneLines = 6
	// yesNoTailLines is how close to the bottom a y/n token must be.
	yesNoTailLines = 4
)

func promptTail(pane string) []string {
	var out []string
	for _, l := range strings.Split(strings.ToLower(StripANSI(pane)), "\n") {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	if len(out) > promptTailLines {
		out = out[len(out)-promptTailLines:]
	}
	return out
}

func anyLine(lines []string, re *regexp.Regexp) bool {
	for _, l := range lines {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

func lastLines(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

// PaneShowsApprovalPrompt reports whether the bottom of pane shows a tool or
// safety approval prompt. Nothing automatic answers one, the launcher's
// startup accept included.
func PaneShowsApprovalPrompt(pane string) bool {
	tail := promptTail(pane)
	if len(tail) == 0 {
		return false
	}
	joined := strings.Join(tail, "\n")
	if !containsAnyPhrase(joined, approvalPhrases...) {
		return false
	}
	if anyLine(tail, menuSelectorLine) || (anyLine(tail, menuYesLine) && anyLine(tail, menuNoLine)) {
		return true
	}
	if strings.Contains(joined, "esc to cancel") || anyLine(lastLines(tail, yesNoTailLines), yesNoToken) {
		return true
	}
	return containsAnyPhrase(strings.Join(lastLines(tail, phraseAloneLines), "\n"), approvalPhrases...)
}

// paneShowsApprovalMenu is the narrow, structural form used after the daemon
// has already typed: an approval wording AND the selector on a yes/no/allow
// choice. The text just typed may itself quote approval wording, so the
// wording alone is not enough here.
func paneShowsApprovalMenu(pane string) bool {
	tail := promptTail(pane)
	return containsAnyPhrase(strings.Join(tail, "\n"), approvalPhrases...) && anyLine(tail, approvalSelectorLine)
}

// PaneShowsConfirmationPrompt reports whether pane shows anything that waits
// for a decision: an approval prompt, a choice menu, a confirm footer, a
// y/n question or a startup dialog.
func PaneShowsConfirmationPrompt(pane string) bool {
	if PaneShowsApprovalPrompt(pane) {
		return true
	}
	tail := promptTail(pane)
	if anyLine(tail, menuSelectorLine) || (anyLine(tail, menuYesLine) && anyLine(tail, menuNoLine)) {
		return true
	}
	joined := strings.Join(tail, "\n")
	if strings.Contains(joined, "enter to confirm") ||
		(strings.Contains(joined, "esc to cancel") && containsAnyPhrase(joined, "tab to amend", "ctrl+e to explain")) {
		return true
	}
	if containsAnyPhrase(joined, activeChoiceDialogPhrases...) {
		return true
	}
	return anyLine(lastLines(tail, yesNoTailLines), yesNoToken)
}

// IsPromptLine reports whether one output line is part of a permission,
// safety or confirmation prompt. An input-typing filter never acts on one.
func IsPromptLine(line string) bool {
	l := strings.ToLower(StripANSI(line))
	if containsAnyPhrase(l, approvalPhrases...) || containsAnyPhrase(l, activeChoiceDialogPhrases...) {
		return true
	}
	if containsAnyPhrase(l, "enter to confirm", "esc to cancel", "do you want to", "would you like to", "are you sure", "proceed?") {
		return true
	}
	return menuSelectorLine.MatchString(l) || menuYesLine.MatchString(l) || yesNoToken.MatchString(l)
}

// ── input from MCP tools ────────────────────────────────────────────────────

// toolRefusal is why an MCP tool's input was refused, in words the calling AI
// session can act on. It is an ErrPromptOnScreen.
type toolRefusal string

func (e toolRefusal) Error() string        { return string(e) }
func (e toolRefusal) Is(target error) bool { return target == ErrPromptOnScreen }

const (
	errToolPromptWaiting    toolRefusal = "refused: a prompt in this session is waiting for the operator; nothing was typed"
	errToolScreenUnreadable toolRefusal = "refused: the session screen could not be read, so a prompt may be waiting for the operator; nothing was typed"
	// The two below follow the re-read before Enter: the text is already in
	// the session, so they must not claim that nothing was typed.
	errToolPromptAppeared toolRefusal = "refused: a prompt waiting for the operator appeared while the text was being typed; Enter was not pressed, and the text may be left in the session"
	errToolRereadFailed   toolRefusal = "refused: the session screen could not be re-read after the text was typed, so a prompt may be waiting for the operator; Enter was not pressed, and the text may be left in the session"
)

// toolRefusalFor words a pane check's error. The bare sentinel means a prompt
// was seen; a wrapped one means the pane could not be read.
func toolRefusalFor(err error, waiting, unreadable toolRefusal) error {
	if err == ErrPromptOnScreen {
		return waiting
	}
	return unreadable
}

// SendToolInput types the input an MCP tool was called with. Whoever calls a
// tool may be an AI session, so it follows the rules for the daemon's own
// senders: nothing is typed while the session shows a prompt waiting for a
// decision or while its screen cannot be read, and the screen is read again
// before Enter. source is only the name recorded for the input; the rules come
// from calling this method, so no source name sent over REST changes them.
func (m *Manager) SendToolInput(fullID, input, source string) error {
	return m.sendInput(fullID, input, source, true, true)
}

// toolRouteAuditSource names, in the audit log, a refused attempt by an MCP
// tool to call a daemon route that types into a session.
const toolRouteAuditSource = "mcp-route"

// AuditRefusedToolRoute records such an attempt. sessionID is the target as
// far as the request named one.
func (m *Manager) AuditRefusedToolRoute(sessionID, reason string) {
	if sess, ok := m.GetSession(sessionID); ok {
		sessionID = sess.FullID
	}
	m.emitInputAudit(InputAuditEvent{Action: InputAuditRefused, SessionFullID: sessionID, Source: toolRouteAuditSource, Reason: reason})
}

// ── enforcement in the session manager ──────────────────────────────────────

// InputAuditEvent is one auditable fact about input the daemon typed, or
// refused to type, on its own.
type InputAuditEvent struct {
	Action        string // InputAuditFilterTyped or InputAuditRefused
	SessionFullID string
	Source        string
	FilterID      string
	Text          string // the typed text, filter input only
	Length        int
	Reason        string
}

// Input audit actions.
const (
	InputAuditFilterTyped = "filter_input"
	InputAuditRefused     = "automated_input_refused"
)

// refusalAuditGap bounds how often one refused (session, source) pair is
// audited: a pending schedule retries every few seconds.
const refusalAuditGap = 10 * time.Minute

// SetInputAuditHook wires the audit sink for daemon-typed input.
func (m *Manager) SetInputAuditHook(fn func(InputAuditEvent)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputAudit = fn
}

// SetFilterInputGate wires the live check that lets an output filter type:
// the daemon-wide switch and the filter's own state. Without a gate no
// filter types anything.
func (m *Manager) SetFilterInputGate(fn func(filterID string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filterInputGate = fn
}

// ErrFilterInputNotAllowed is returned when an output filter may not type.
var ErrFilterInputNotAllowed = errors.New("output filter input is not allowed")

func (m *Manager) checkFilterGate(filterID string) error {
	m.mu.Lock()
	gate := m.filterInputGate
	m.mu.Unlock()
	if gate == nil {
		return ErrFilterInputNotAllowed
	}
	if err := gate(filterID); err != nil {
		return fmt.Errorf("%w: %v", ErrFilterInputNotAllowed, err)
	}
	return nil
}

// ShowsConfirmationPrompt reports whether the session's live pane shows a
// prompt waiting for a decision. An unreadable pane counts as one.
func (m *Manager) ShowsConfirmationPrompt(sessionID string) bool {
	sess, ok := m.GetSession(sessionID)
	if !ok {
		return false
	}
	if sess.OutputMode == "chat" {
		return false
	}
	pane, err := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
	return err != nil || PaneShowsConfirmationPrompt(pane)
}

// FilterMayType reports why output filter filterID may not type into the
// session right now: the gate, then what the pane shows.
func (m *Manager) FilterMayType(sessionID, filterID string) error {
	if err := m.checkFilterGate(filterID); err != nil {
		return err
	}
	sess, ok := m.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if sess.OutputMode == "chat" {
		return nil
	}
	return m.automatedBlocked(sess.TmuxSession, FilterSource(filterID))
}

func (m *Manager) emitInputAudit(ev InputAuditEvent) {
	m.mu.Lock()
	fn := m.inputAudit
	if ev.Action == InputAuditRefused {
		key := ev.SessionFullID + "|" + ev.Source
		if m.inputRefusedAt == nil {
			m.inputRefusedAt = map[string]time.Time{}
		}
		if last, ok := m.inputRefusedAt[key]; ok && time.Since(last) < refusalAuditGap {
			m.mu.Unlock()
			return
		}
		m.inputRefusedAt[key] = time.Now()
		// stderr: under `datawatch mcp`, stdout carries the protocol.
		fmt.Fprintf(os.Stderr, "[input-guard] %s: source %q refused: %s\n", ev.SessionFullID, ev.Source, ev.Reason)
	}
	m.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// automatedBlocked reads the live pane and reports why an automated send to
// it must not happen. A pane that cannot be read blocks the send.
func (m *Manager) automatedBlocked(tmuxSession, source string) error {
	pane, err := m.tmux.CapturePaneLiveTail(tmuxSession)
	if err != nil {
		return fmt.Errorf("%w (the pane could not be read: %v)", ErrPromptOnScreen, err)
	}
	shows := PaneShowsConfirmationPrompt
	if source == StartupAcceptSource {
		shows = PaneShowsApprovalPrompt
	}
	if shows(pane) {
		return ErrPromptOnScreen
	}
	return nil
}

// refuseIfApprovalPrompt guards the daemon's direct keystroke paths (dialog
// dismissal, rate-limit and MCP retries): it returns ErrPromptOnScreen while
// the pane shows a tool or safety approval prompt, or cannot be read.
func (m *Manager) refuseIfApprovalPrompt(sess *Session, source string) error {
	pane, err := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
	if err == nil && !PaneShowsApprovalPrompt(pane) {
		return nil
	}
	m.emitInputAudit(InputAuditEvent{Action: InputAuditRefused, SessionFullID: sess.FullID, Source: source, Reason: ErrPromptOnScreen.Error()})
	return ErrPromptOnScreen
}

// SendAutomatedRawKeys types literal text on the daemon's own initiative:
// control characters (Enter, Esc and friends) are removed, and nothing is
// typed while a prompt waits for a decision.
func (m *Manager) SendAutomatedRawKeys(fullID, data, source string) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return fmt.Errorf("session %s not found", fullID)
	}
	data = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, data)
	if data == "" {
		return nil
	}
	if err := m.automatedBlocked(sess.TmuxSession, source); err != nil {
		m.emitInputAudit(InputAuditEvent{Action: InputAuditRefused, SessionFullID: sess.FullID, Source: source, Length: len(data), Reason: err.Error()})
		return err
	}
	return m.SendRawKeys(sess.FullID, data)
}

func isControlRune(r rune) bool { return r < 0x20 || r == 0x7f }

// guardedSubmitter is a tmux layer that can re-check the pane between typing
// the text and pressing Enter.
type guardedSubmitter interface {
	SendKeysGuarded(session, keys string, settle time.Duration, veto func(pane string) bool) error
}

// rateLimitWaitPhrases identify the usage-limit menu whose first option is
// "stop and wait". The daemon's "1" is only ever meant for that menu.
var rateLimitWaitPhrases = []string{"stop and wait for limit to reset", "wait for limit to reset", "wait for the limit to reset"}

// acceptRateLimitWait picks the wait option of the usage-limit menu, only
// while that menu is on the pane and no approval prompt is.
func (m *Manager) acceptRateLimitWait(sess *Session) {
	pane, err := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
	if err != nil || PaneShowsApprovalPrompt(pane) ||
		!containsAnyPhrase(strings.Join(promptTail(pane), "\n"), rateLimitWaitPhrases...) {
		fmt.Printf("[rate-limit] %s: usage-limit menu not on screen; sent nothing\n", sess.ID)
		return
	}
	m.noteInputSent(sess.FullID)
	_ = m.tmux.SendKeys(sess.TmuxSession, "1")
}

// retryMCPServers types /mcp to restart failed MCP servers. Nothing is typed
// while a prompt waits for a decision, and the follow-up Enter is held back
// when an approval prompt has appeared.
func (m *Manager) retryMCPServers(sess *Session, gap time.Duration) {
	if err := m.automatedBlocked(sess.TmuxSession, "mcp-retry"); err != nil {
		m.emitInputAudit(InputAuditEvent{Action: InputAuditRefused, SessionFullID: sess.FullID, Source: "mcp-retry", Reason: err.Error()})
		return
	}
	m.noteInputSent(sess.FullID)
	_ = m.tmux.SendKeys(sess.TmuxSession, "/mcp")
	time.Sleep(gap)
	if m.refuseIfApprovalPrompt(sess, "mcp-retry") != nil {
		return
	}
	_ = m.tmux.SendKeys(sess.TmuxSession, "")
}

// Typing for requests (delivery job 4). A reminder is one content-free line. A
// typed request is the one thing typed whose words datawatch keeps nowhere
// else: the tracker and the session record say only "request <id> typed". The
// terminal itself still shows what was typed, so the pane's own output log
// holds it.
//
// Both are datawatch's own typing, so both are held to the prompt guard. They
// are also refused when the session is rate limited, when the pane's shell is
// what reads the keyboard (it would run the text), and for backends datawatch
// reaches without typing: chat mode and the HTTP conversations keep their own
// record of what they are sent.

package session

import (
	"errors"
	"fmt"
)

// ErrNothingTyped wraps every refusal that proves no key reached the session.
var ErrNothingTyped = errors.New("nothing was typed")

// The input sources of the two.
const (
	RequestNoticeSource = "request-notice"
	RequestTypedSource  = "request-typed"
)

func init() {
	// IsAutomatedInputSource is what holds a sender to the prompt rules and to
	// the typing guard.
	automatedInputSources[RequestNoticeSource] = true
	automatedInputSources[RequestTypedSource] = true
}

// requestTypedLine is all that is recorded of a typed request.
func requestTypedLine(requestID string) string { return "request " + requestID + " typed" }

func nothingTyped(cause error) error { return fmt.Errorf("%w: %w", ErrNothingTyped, cause) }

// requestTarget returns the session datawatch may type into. content is true
// for a request's text, which is not typed unless datawatch can see that
// something other than the shell reads the keyboard.
func (m *Manager) requestTarget(fullID, source string, content bool) (Session, error) {
	sess, ok := m.store.Get(fullID)
	if !ok || sess.FullID != fullID {
		return Session{}, nothingTyped(fmt.Errorf("session %s not found", fullID))
	}
	snap := m.store.Snapshot(sess)
	switch {
	case snap.State != StateWaitingInput && snap.State != StateRunning:
		return snap, nothingTyped(fmt.Errorf("session %s takes no request input (state: %s)", fullID, snap.State))
	case snap.OutputMode == "chat", snap.BackendFamily == "opencode-acp", snap.BackendFamily == "openwebui",
		snap.BackendFamily == "ollama", snap.BackendFamily == "shell", snap.Subprocess, snap.TmuxSession == "":
		return snap, nothingTyped(fmt.Errorf("session %s has no terminal agent to type to", fullID))
	}
	if err := m.automatedBlocked(snap.TmuxSession, source); err != nil {
		return snap, nothingTyped(err)
	}
	shellPID, tty, procs, listed := m.paneProcesses(snap.TmuxSession)
	if listed && shellReadsKeyboard(shellPID, tty, procs) || content && !listed {
		return snap, nothingTyped(errors.New("the pane's shell may be reading the keyboard"))
	}
	return snap, nil
}

// CanTypeRequest reports why a request's text could not be typed now.
func (m *Manager) CanTypeRequest(fullID string) error {
	_, err := m.requestTarget(fullID, RequestTypedSource, true)
	return err
}

// CanTypeRequestNotice reports why a reminder could not be typed now.
func (m *Manager) CanTypeRequestNotice(fullID string) error {
	_, err := m.requestTarget(fullID, RequestNoticeSource, false)
	return err
}

func (m *Manager) typeForRequest(fullID, input, source, record string, content bool) error {
	if _, err := m.requestTarget(fullID, source, content); err != nil {
		return err
	}
	err := m.sendInputRecorded(fullID, input, source, record, true, false)
	if err != nil && !TextMayBeInSession(err) {
		return nothingTyped(err) // refused before the keys went
	}
	return err
}

// SendRequestNotice types one content-free reminder line.
func (m *Manager) SendRequestNotice(fullID, line string) error {
	return m.typeForRequest(fullID, line, RequestNoticeSource, "", false)
}

// SendRequestText types a request's text and records only that it was typed.
func (m *Manager) SendRequestText(fullID, requestID, text string) error {
	return m.typeForRequest(fullID, text, RequestTypedSource, requestTypedLine(requestID), true)
}

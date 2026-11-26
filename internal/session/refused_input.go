package session

import (
	"errors"
	"strings"
)

// RefusedBeforeTyping reports whether err, as SendInput returned it, proves
// that no keystroke reached the session: the first look at the pane found a
// prompt waiting for a decision, or the pane could not be read.
//
// ErrPromptOnScreen alone does not prove that. The pane is read again between
// the text and Enter, and a refusal there matches ErrPromptOnScreen too,
// although the text is already in the session; SendInput returns it wrapped as
// a send failure. So only an error that begins with the refusal itself counts.
// Every other error means "cannot tell": a caller must not type the same line
// again.
func RefusedBeforeTyping(err error) bool {
	return errors.Is(err, ErrPromptOnScreen) && strings.HasPrefix(err.Error(), ErrPromptOnScreen.Error())
}

// TextMayBeInSession reports whether err is one of the two failures SendInput
// returns after it has sent keys: the send itself failed part-way (a late
// prompt refusal included), or the keys went and the session record was not
// saved. The same text must not be sent again.
func TextMayBeInSession(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, sendFailedPrefix) || strings.HasPrefix(msg, saveFailedPrefix)
}

// The prefixes of those two errors in sendInput; refused_input_test.go checks
// them against the real send path.
const (
	sendFailedPrefix = "send input: "
	saveFailedPrefix = "save session: "
)

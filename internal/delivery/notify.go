// Telling the operator. "Listed" and "told" are separate: an entry is told
// when a message channel accepted a send that named it.

package delivery

import (
	"context"
	"time"
)

const (
	// notifyMaxLines is the most entry lines in one message.
	notifyMaxLines = 20
	// notifyMaxBytes keeps a message under the smallest channel limit
	// (Twilio refuses more than 1600 characters).
	notifyMaxBytes = 1500
	// notifySendHistory is how many send results one entry keeps.
	notifySendHistory = 20
)

// Why a send failed. The channel's own error text is never kept.
const (
	sendCodeFailed    = "send_failed"
	sendCodeTimeout   = "timeout"
	sendCodeCancelled = "cancelled"
)

// notifySendTimeout is how long one send may take before it counts as failed.
var notifySendTimeout = 45 * time.Second

// Notice is the one message due in a pass. Its text holds IDs, kinds, states
// and reason codes only.
type Notice struct {
	// Keys are the entries a successful send marks as told.
	Keys []string
	Text string
	// Reminder marks a digest of entries already told and not acknowledged.
	Reminder bool
}

// DueNotice returns the message due now over entries (all saved entries), or
// false when there is nothing to send. It leaves out closed, backlog, seen and
// currently accepted entries, and entries waiting for their next try.
func DueNotice(host string, entries []*Entry, now time.Time) (Notice, bool) {
	return Notice{}, false
}

// Deliver sends text on every channel of s and returns one result for each. A
// nil sender or no channel returns no result.
func Deliver(ctx context.Context, s Sender, text string, now time.Time) []SendResult { return nil }

// Told reports whether at least one channel accepted the send.
func Told(results []SendResult) bool { return false }

// ApplySend records results on the entries n names: the bounded send history,
// and either told and last-sent times or the next try with backoff.
func ApplySend(entries []*Entry, n Notice, results []SendResult, now time.Time) {}

// CannotRecordText is the one content-free warning sent from memory when the
// records cannot be saved or read.
func CannotRecordText(host string) string { return "" }

// TestText is the test message.
func TestText(host string) string { return "" }

// Telling the operator. "Listed" and "told" are separate: an entry is told
// when a message channel accepted a send that named it.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
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
	// notifyDigestEvery is how often told, unacknowledged entries are sent
	// again.
	notifyDigestEvery = 24 * time.Hour
	// A failed send is tried again after notifyRetryFirst, doubling up to
	// notifyRetryMax.
	notifyRetryFirst = time.Minute
	notifyRetryMax   = time.Hour

	notifyMaxHost = 60
	notifyMaxKey  = 160
)

// Why a send failed. The channel's own error text is never kept.
const (
	sendCodeFailed    = "send_failed"
	sendCodeTimeout   = "timeout"
	sendCodeCancelled = "cancelled"
	sendCodeGone      = "channel_gone"
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
//
// Entries never told go first, oldest first; only those the message has a line
// for are marked by it, and the rest follow in later passes. With none of
// those, one digest covers every told entry once the longest unsent of them was
// last sent a day ago; because it marks them all, there is one digest a day.
func DueNotice(host string, entries []*Entry, now time.Time) (Notice, bool) {
	var fresh, told []*Entry
	digestDue := false
	for _, e := range entries {
		if !notifiable(e, now) {
			continue
		}
		if e.ToldAt == nil {
			fresh = append(fresh, e)
			continue
		}
		told = append(told, e)
		// A last send in the future means the clock went back.
		if e.LastSentAt == nil || now.Sub(*e.LastSentAt) >= notifyDigestEvery || e.LastSentAt.Sub(now) > notifyRetryMax {
			digestDue = true
		}
	}
	switch {
	case len(fresh) > 0:
		text, named := notifyText(host, fresh, false)
		return Notice{Keys: entryKeys(fresh[:named]), Text: text}, true
	case digestDue:
		text, _ := notifyText(host, told, true)
		return Notice{Keys: entryKeys(told), Text: text, Reminder: true}, true
	}
	return Notice{}, false
}

func notifiable(e *Entry, now time.Time) bool {
	if e == nil || e.Key == "" || e.ClosedAt != nil || e.Backlog || e.SeenAt != nil {
		return false
	}
	if e.Accepted != nil && e.Accepted.Until.After(now) {
		return false
	}
	if e.NextTryAt != nil {
		// A next try further off than the longest wait means the clock went
		// back; waiting for it would silence the entry.
		if wait := e.NextTryAt.Sub(now); wait > 0 && wait <= notifyRetryMax {
			return false
		}
	}
	return true
}

func entryKeys(es []*Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Key
	}
	return out
}

// notifyText sorts es oldest first and returns the message and how many of
// them it has a line for. It uses each entry's key, reason code and count and
// nothing else.
func notifyText(host string, es []*Entry, reminder bool) (string, int) {
	sort.SliceStable(es, func(i, j int) bool {
		if !es[i].Since.Equal(es[j].Since) {
			return es[i].Since.Before(es[j].Since)
		}
		return es[i].Key < es[j].Key
	})
	noun := "items need you"
	if len(es) == 1 {
		noun = "item needs you"
	}
	head := fmt.Sprintf("%s Delivery check: %d %s.", notifyPrefix(host), len(es), noun)
	if reminder {
		noun = "items are"
		if len(es) == 1 {
			noun = "item is"
		}
		head = fmt.Sprintf("%s Delivery check reminder: %d %s still not acknowledged.", notifyPrefix(host), len(es), noun)
	}
	const foot = `In chat, "delivery" shows the list and "delivery ack <key>" stops messages about an item. ` +
		"Command line: datawatch delivery status; datawatch delivery ack --key <key>."
	// Room for the footer and for the line that counts what is left out.
	room := notifyMaxBytes - len(head) - len(foot) - 80
	var lines []string
	for _, e := range es {
		line := "- " + notifySafe(e.Key, notifyMaxKey)
		if CheckReasonCode(e.Reason) == nil {
			line += " " + e.Reason
		}
		if e.Count > 1 {
			line += fmt.Sprintf(" x%d", e.Count)
		}
		if len(lines) == notifyMaxLines || (len(lines) > 0 && len(line)+1 > room) {
			break
		}
		room -= len(line) + 1
		lines = append(lines, line)
	}
	out := append([]string{head}, lines...)
	if left := len(es) - len(lines); left > 0 {
		if reminder {
			out = append(out, fmt.Sprintf("Plus %d more.", left))
		} else {
			out = append(out, fmt.Sprintf("Plus %d more, in later messages.", left))
		}
	}
	return strings.Join(append(out, foot), "\n"), len(lines)
}

func notifyPrefix(host string) string {
	return "[" + notifySafe(host, notifyMaxHost) + "]"
}

// notifySafe keeps an identifier to the characters identifiers are made of, so
// nothing in it can start a new line in a message, and bounds its length.
func notifySafe(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._:/@+#=,-", r):
			return r
		}
		return '_'
	}, s)
	if len(s) > max {
		s = s[:max-3] + "..."
	}
	return s
}

// Deliver sends text on every channel of s and returns one result for each. A
// nil sender or no channel returns no result.
//
// The channels are tried at the same time, and one that does not answer is a
// failed send after notifySendTimeout.
func Deliver(ctx context.Context, s Sender, text string, now time.Time) []SendResult {
	if s == nil {
		return nil
	}
	channels := s.Channels()
	if len(channels) == 0 {
		return nil
	}
	out := make([]SendResult, len(channels))
	var wg sync.WaitGroup
	for i, name := range channels {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			err := ctx.Err()
			if err == nil {
				// Longer than a channel sender's own limit, so that the sender
				// gives up first and notes the send it left running.
				err = boundedSend(ctx, notifySendTimeout+notifySendTimeout/2, func() error { return s.Send(ctx, name, text) })
			}
			out[i] = SendResult{At: now, Channel: name, OK: err == nil, Code: sendCode(err)}
		}(i, name)
	}
	wg.Wait()
	return out
}

func sendCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errSendTimeout), errors.Is(err, context.DeadlineExceeded):
		return sendCodeTimeout
	case errors.Is(err, context.Canceled):
		return sendCodeCancelled
	case errors.Is(err, errChannelGone):
		return sendCodeGone
	}
	return sendCodeFailed
}

// Told reports whether at least one channel accepted the send.
func Told(results []SendResult) bool {
	for _, r := range results {
		if r.OK {
			return true
		}
	}
	return false
}

// ApplySend records results on the entries n names: the bounded send history,
// and either told and last-sent times or the next try with backoff. With no
// result nothing was tried, and nothing is recorded.
func ApplySend(entries []*Entry, n Notice, results []SendResult, now time.Time) {
	if len(results) == 0 {
		return
	}
	named := make(map[string]bool, len(n.Keys))
	for _, k := range n.Keys {
		named[k] = true
	}
	told := Told(results)
	for _, e := range entries {
		if e == nil || !named[e.Key] || e.ClosedAt != nil {
			continue
		}
		history := append(append([]SendResult(nil), e.Sends...), results...)
		if over := len(history) - notifySendHistory; over > 0 {
			history = history[over:]
		}
		e.Sends = history
		if told {
			at := now
			if e.ToldAt == nil {
				first := now
				e.ToldAt = &first
			}
			e.LastSentAt = &at
			e.SendFails, e.NextTryAt = 0, nil
			continue
		}
		e.SendFails++
		next := now.Add(notifyBackoff(e.SendFails))
		e.NextTryAt = &next
	}
}

func notifyBackoff(fails int) time.Duration {
	d := notifyRetryFirst
	for i := 1; i < fails && d < notifyRetryMax; i++ {
		d *= 2
	}
	return min(d, notifyRetryMax)
}

// CannotRecordText is the one content-free warning sent from memory when the
// records cannot be saved or read.
func CannotRecordText(host string) string {
	return notifyPrefix(host) + " Delivery check: its records cannot be saved or read, so it cannot tell you what is waiting. " +
		"No other delivery message is sent until this is fixed."
}

// TestText is the test message.
func TestText(host string) string {
	return notifyPrefix(host) + " Delivery check: test message. No action is needed."
}

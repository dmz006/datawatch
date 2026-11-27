// What the parts of the delivery check share: the service the REST, MCP, CLI
// and chat surfaces call, its errors, and the heartbeat the outside timer
// reads.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Service is the delivery check as the surfaces see it. *Engine implements it.
type Service interface {
	// Pass runs the check now. It reports; it blocks nothing.
	Pass(ctx context.Context) (Report, error)
	// Report is the current report, without running a pass.
	Report() Report
	// Ack marks entries as seen. It stops their messages; they stay listed.
	Ack(sel Selector, by string) (int, error)
	// Accept lets entries wait until until, at most 30 days ahead.
	Accept(sel Selector, reason string, until time.Time, by string) (int, error)
	// Drop closes failed or backlog entries.
	Drop(sel Selector, reason, by string) (int, error)
	// TestMessage sends a test message on every channel and says what each
	// answered. It is limited, and refused while messages are switched off.
	TestMessage(ctx context.Context) ([]SendResult, error)
}

var _ Service = (*Engine)(nil)

// Errors the surfaces map to their own refusals.
var (
	// ErrSelector: the action names no entries to apply to.
	ErrSelector = errors.New("name the entries: keys, kind, state, reason match or backlog")
	// ErrReasonCode: the reason is not a code. Records hold codes only.
	ErrReasonCode = errors.New("the reason must be a code: 1 to 64 of a-z, 0-9, dot, dash and underscore, starting with a letter or digit")
	// ErrUntil: the review date is missing, past, or too far ahead.
	ErrUntil = errors.New("the review date must be in the future and at most 30 days ahead")
	// ErrNotDroppable: only failed and backlog entries can be dropped.
	ErrNotDroppable = errors.New("only failed or backlog entries can be dropped; accept the others with a review date")
	// ErrAlarm: an alarm or a clock-jump notice can only be acknowledged.
	ErrAlarm = errors.New("an alarm or a clock-jump notice can only be acknowledged: accepting or dropping it could hide the items behind it; acknowledge it, or name only other entries")
	// ErrNotifyOff: messages are switched off, so no test message is sent.
	ErrNotifyOff = errors.New("messages are switched off (delivery.notify: false), so no test message was sent")
	// ErrTestTooSoon: the test message limit is reached.
	ErrTestTooSoon = errors.New("too many test messages: at most one a minute and 6 an hour")
	// ErrNoMatch: no listed entry matches.
	ErrNoMatch = errors.New("no listed entry matches")
	// ErrCannotRecord: the change could not be saved, so it did not happen.
	ErrCannotRecord = errors.New("the delivery records cannot be saved")
	// ErrRecords: the saved records cannot be read.
	ErrRecords = errors.New("the delivery records cannot be read")
)

var reasonCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// CheckReasonCode returns ErrReasonCode unless s is a reason code.
func CheckReasonCode(s string) error {
	if !reasonCodeRe.MatchString(s) {
		return ErrReasonCode
	}
	return nil
}

// ParseUntil reads a review date: RFC 3339, or YYYY-MM-DD meaning the end of
// that day in UTC. It does not check the 30-day limit; Accept does.
func ParseUntil(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Add(24*time.Hour - time.Second), nil
	}
	return time.Time{}, fmt.Errorf("%w: %q is not an RFC 3339 time or a YYYY-MM-DD date", ErrUntil, s)
}

// NonOperator reports whether by names a session or a federation peer.
func NonOperator(by string) bool {
	return strings.HasPrefix(by, "session:") || strings.HasPrefix(by, "peer:")
}

// Heartbeat is what the outside timer reads, at <data dir>/delivery/heartbeat.json.
// It is written only after a full pass and a disk write, sync and read-back,
// is never sealed, and holds no content.
type Heartbeat struct {
	At              time.Time `json:"at"`
	Unix            int64     `json:"unix"`
	IntervalSeconds int       `json:"interval_seconds"`
	// State is HeartbeatOK, HeartbeatRed or HeartbeatCannotTell.
	State string `json:"state"`
	Pass  int    `json:"pass"`
	// Open is how many unaccepted entries are listed.
	Open int `json:"open"`
}

// HeartbeatDir and HeartbeatFile place the heartbeat under the data folder.
const (
	HeartbeatDir  = "delivery"
	HeartbeatFile = "heartbeat.json"
)

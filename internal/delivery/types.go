package delivery

import (
	"context"
	"time"
)

// Kind is what a listed thing is.
type Kind string

const (
	KindAutomaton   Kind = "automaton"
	KindTask        Kind = "task"
	KindBug         Kind = "bug"
	KindUnlanded    Kind = "unlanded"
	KindPendingItem Kind = "pending_item"
	KindClosedWork  Kind = "closed_work"
	KindSchedule    Kind = "schedule"
	KindSession     Kind = "session"
	KindPipeline    Kind = "pipeline"
	KindDeploy      Kind = "deploy"
	KindAnswer      Kind = "answer"
	KindMessage     Kind = "message"
	KindQuestion    Kind = "question"
	KindFilter      Kind = "filter"
	KindClock       Kind = "clock"
	KindAlarm       Kind = "alarm"
)

// The delivery rules an entry can break.
const (
	InvDeadlines    = "I1"  // every unfinished thing has a start time and a review-by
	InvOverdue      = "I2"  // nothing passes its review-by unlisted
	InvFailed       = "I3"  // failed and outside-cancelled work stays listed
	InvScheduleFire = "I5"  // every pending schedule can still fire
	InvNoHolder     = "I8"  // work addressed to a name nobody live holds
	InvRunning      = "I11" // running work has a live session or a limited wait
	InvCheckRan     = "I15" // the check ran within two intervals
	InvTold         = "I16" // old items were sent or acknowledged
	InvUnopenable   = "I17" // a record that cannot be read is never "nothing there"
)

// WaitsOnOperator is the WaitsOn value for work waiting on the operator.
const WaitsOnOperator = "operator"

// Candidate is one unfinished thing a source reports. A candidate is listed
// when Problem is set, or once it has been seen in State for longer than
// Limit.
type Candidate struct {
	Kind      Kind          `json:"kind"`
	ID        string        `json:"id"`
	State     string        `json:"state"`
	Invariant string        `json:"invariant,omitempty"`
	Since     time.Time     `json:"since"`
	Limit     time.Duration `json:"limit,omitempty"`
	Problem   string        `json:"problem,omitempty"`
	Reason    string        `json:"reason,omitempty"`
	WaitsOn   string        `json:"waits_on,omitempty"`
	// Sticky work failed or was ended from outside: its entry stays until the
	// operator acts, also when the record goes away.
	Sticky bool `json:"sticky,omitempty"`
	// Count is how many occurrences one entry stands for (missed slots).
	Count int `json:"count,omitempty"`
	// TimeDriven marks a Problem that only the passing of time made true.
	TimeDriven bool `json:"time_driven,omitempty"`
}

// Source reads one set of existing records.
type Source struct {
	Name    string
	Collect func(ctx context.Context, now time.Time) ([]Candidate, error)
}

// SendResult is the outcome of one send on one message channel.
type SendResult struct {
	At      time.Time `json:"at"`
	Channel string    `json:"channel"`
	OK      bool      `json:"ok"`
	// Code is a reason code. The channel's own error text is not kept: it can
	// carry credentials or content.
	Code string `json:"code,omitempty"`
}

// Sender sends a message and returns what the channel answered.
type Sender interface {
	Channels() []string
	Send(ctx context.Context, channel, text string) error
}

// Accept is the operator's decision that a listed thing may wait until a date.
type Accept struct {
	Reason string    `json:"reason"`
	Until  time.Time `json:"until"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
}

// Entry is one saved line of the list. It holds IDs, codes and times only.
type Entry struct {
	Key       string    `json:"key"`
	Kind      Kind      `json:"kind"`
	ID        string    `json:"id"`
	State     string    `json:"state"`
	Invariant string    `json:"invariant"`
	Reason    string    `json:"reason"`
	WaitsOn   string    `json:"waits_on"`
	Since     time.Time `json:"since"`
	ReviewBy  time.Time `json:"review_by"`
	Count     int       `json:"count,omitempty"`
	Sticky    bool      `json:"sticky,omitempty"`
	Source    string    `json:"source"`
	Episode   int       `json:"episode"`
	Backlog   bool      `json:"backlog,omitempty"`
	ListedAt  time.Time `json:"listed_at"`
	// AgeSeconds is the observed time the entry has been listed.
	AgeSeconds int64        `json:"age_seconds"`
	Sends      []SendResult `json:"sends,omitempty"`
	ToldAt     *time.Time   `json:"told_at,omitempty"`
	LastSentAt *time.Time   `json:"last_sent_at,omitempty"`
	SendFails  int          `json:"send_fails,omitempty"`
	NextTryAt  *time.Time   `json:"next_try_at,omitempty"`
	SeenAt     *time.Time   `json:"seen_at,omitempty"`
	SeenBy     string       `json:"seen_by,omitempty"`
	Accepted   *Accept      `json:"accepted,omitempty"`
	ClosedAt   *time.Time   `json:"closed_at,omitempty"`
	ClosedAs   string       `json:"closed_as,omitempty"`
	ClosedBy   string       `json:"closed_by,omitempty"`
	DropReason string       `json:"drop_reason,omitempty"`
	// Present is false once the source stops reporting a closed entry.
	Present bool `json:"present,omitempty"`
}

// Results of a check.
const (
	ResultGreen = "green"
	ResultRed   = "red"
)

// Heartbeat states, read by the outside timer.
const (
	HeartbeatOK         = "ok"
	HeartbeatRed        = "red"
	HeartbeatCannotTell = "cannot_tell"
)

// Why a check is red besides its listed entries.
const (
	RedItems            = "listed_items"
	RedSourceUnreadable = "source_unreadable"
	RedStartMark        = "start_mark_missing"
	RedRecords          = "record_unreadable"
	RedCannotRecord     = "cannot_record"
	RedNoChannel        = "no_channel"
	RedSendsFail        = "all_sends_failed"
	RedNotTold          = "not_told"
	RedStale            = "check_stale"
	RedNeverRan         = "check_never_ran"
	RedGuarded          = "mass_expiry_held"
	RedUnrecorded       = "flood_not_recorded"
)

// SourceStatus is how the last read of one source went.
type SourceStatus struct {
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Candidates int    `json:"candidates"`
}

// Report is the result of a check. It holds IDs, codes and times only.
type Report struct {
	ReportOnly bool      `json:"report_only"`
	Result     string    `json:"result"`
	Red        []string  `json:"red,omitempty"`
	At         time.Time `json:"at"`
	Pass       int       `json:"pass"`
	// Items are the listed, unaccepted entries raised after the start mark.
	Items []Entry `json:"items"`
	// Backlog are the entries that were already true at the start mark.
	Backlog  []Entry `json:"backlog"`
	Accepted []Entry `json:"accepted"`
	// Guarded are keys held back by the mass-expiry guard.
	Guarded    []string       `json:"guarded,omitempty"`
	Unrecorded int            `json:"unrecorded,omitempty"`
	ByRule     map[string]int `json:"by_rule"`
	ByKind     map[string]int `json:"by_kind"`
	Sources    []SourceStatus `json:"sources"`
	Channels   []string       `json:"channels"`
	Heartbeat  string         `json:"heartbeat,omitempty"`
	StartMark  *time.Time     `json:"start_mark,omitempty"`
	// NonOperatorActions counts acknowledgements, acceptances and drops made
	// by a session or peer. The rule is that only the operator does these; it
	// is recorded here and not enforced.
	NonOperatorActions int      `json:"non_operator_actions"`
	Uncovered          []string `json:"uncovered"`
}

// Selector names the entries an operator action applies to. Every set field
// narrows the choice; at least one must be set.
type Selector struct {
	Keys    []string `json:"keys,omitempty"`
	Kind    Kind     `json:"kind,omitempty"`
	State   string   `json:"state,omitempty"`
	Reason  string   `json:"match_reason,omitempty"`
	Backlog bool     `json:"backlog,omitempty"`
}

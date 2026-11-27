// Requests: one saved thing that must happen, addressed to a role. These are
// the shared types of delivery job 4 and of the jobs that create requests
// (sources, rule changes). Owner: part A. Change them only through the lead:
// parts B and C and jobs 5 and 6 compile against them.

package delivery

import "time"

// RequestIDPrefix starts every request ID. The older queue routes use it to
// tell a request from a plain queue item.
const RequestIDPrefix = "r-"

// RequestKind says what a request is for. Every kind has one row in
// kindRules; no row, no create.
type RequestKind string

// Every kind. Job 4 creates hello and ask; jobs 5 and 6 create the others
// without changing this package's job 4 files.
const (
	// ReqHello is the harmless request whose take proves that one session can
	// take. It is never assumed from the backend.
	ReqHello RequestKind = "hello"
	// ReqAsk is work somebody asked a role to do.
	ReqAsk RequestKind = "ask"
	// ReqBug asks the project supervisor to act on a bug (job 5).
	ReqBug RequestKind = "bug"
	// ReqWake is a status wake for a supervisor (job 5).
	ReqWake RequestKind = "wake"
	// ReqHandover hands the open requests of a role to its holder (job 5).
	ReqHandover RequestKind = "handover"
	// ReqParentReply is a reply to a parent session or its role (job 5).
	ReqParentReply RequestKind = "parent_reply"
	// ReqAnswer is an answer whose asker is gone (job 5).
	ReqAnswer RequestKind = "answer"
	// ReqScheduleAsk is a schedule fire addressed to a role (job 5).
	ReqScheduleAsk RequestKind = "schedule"
	// ReqExitHook is what an exit hook asks for (job 5).
	ReqExitHook RequestKind = "exit_hook"
	// ReqListGap asks the project supervisor to fix a "done when" list with a
	// missing part, no usable check or no review (job 5).
	ReqListGap RequestKind = "list_gap"
	// ReqRules is a rule bundle (job 6).
	ReqRules RequestKind = "rules"
)

// Class says how a request is meant to be handled.
type Class string

// The four classes.
const (
	ClassAsk       Class = "ask"
	ClassNotice    Class = "notice"
	ClassKeystroke Class = "keystroke"
	ClassAction    Class = "daemon_action"
)

// Role is a job a session holds. A request is addressed to a role in a scope.
type Role string

// The roles. Only RoleSupervisor of a project can be given in job 4;
// RoleSession addresses one live session (hello, parent reply, rules).
const (
	RoleSupervisor Role = "supervisor"
	RoleSession    Role = "session"
	RoleDoer       Role = "doer"
	RoleWatcher    Role = "watcher"
	RoleWorker     Role = "worker"
	RoleVerifier   Role = "verifier"
)

// ScopeKind is how wide a role is.
type ScopeKind string

// The scope kinds.
const (
	ScopeTask       ScopeKind = "task"
	ScopeAutomaton  ScopeKind = "automaton"
	ScopeAutomation ScopeKind = "automation"
	ScopeProject    ScopeKind = "project"
	ScopeHost       ScopeKind = "host"
	ScopeSession    ScopeKind = "session"
)

// Scope names one task, Automaton, Automation, project, computer or session.
// ID is an identifier and is readable on disk, so it never holds content or a
// path (see CheckScopeID).
type Scope struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id"`
}

// RequestState is where a request is.
type RequestState string

// The states. Only datawatch's own check sets StateDone. A worker saying it is
// done moves a request to StateReported and no further.
const (
	StateWaiting RequestState = "waiting" // saved, not yet delivered
	// StateDelivered: the delivery counts under the holder's worker policy. For
	// claude-code that is the holder's take; for pi only the report that the
	// text was in a completed model call. A take that does not count leaves the
	// request waiting, with Delivery.FetchedAt set: "fetched, unconfirmed".
	StateDelivered RequestState = "delivered"
	StateAccepted  RequestState = "accepted" // the holder committed to it
	StateReported  RequestState = "reported" // the holder says done; never expires
	StateDone      RequestState = "done"     // datawatch's check passed
	StateFailed    RequestState = "failed"
	StateExpired   RequestState = "expired"
	StateCancelled RequestState = "cancelled"
)

// RequestStates are all of them, in order.
var RequestStates = []RequestState{StateWaiting, StateDelivered, StateAccepted, StateReported, StateDone, StateFailed, StateExpired, StateCancelled}

// Closed reports a state that no doer call changes.
func (s RequestState) Closed() bool {
	return s == StateDone || s == StateFailed || s == StateExpired || s == StateCancelled
}

// Reason codes. Records and messages hold codes only, never free text.
const (
	WhyDeclined       = "declined"
	WhyHolderGone     = "holder_gone"
	WhyUnknownOutcome = "unknown_outcome"
	WhyUnproven       = "unproven"
	WhySuperseded     = "superseded"
	WhyUndeliverable  = "undeliverable"
	WhyProofFailed    = "proof_failed"
	WhyTakeByPassed   = "take_by_passed"
	WhyDuePassed      = "due_passed"
	WhyNoHolder       = "no_holder"
	WhyTakeNotProven  = "take_not_proven"
	WhyHeldPause      = "held_pause"
	WhyHeldUsage      = "held_usage"
	WhyHeldRateLimit  = "held_rate_limit"
	WhyBlockedPrompt  = "holder_blocked_prompt"
	WhyListsOff       = "lists_off"
	WhyNoList         = "no_list"
	WhyWaitingOnYou   = "waiting_on_you"
	WhyDropped        = "dropped"
	WhyForced         = "forced"
	WhyReissued       = "reissued"
	WhyParentClosed   = "parent_closed"
	// WhyRepaired: rebuilt by a repair of the store. It is not run again until
	// the operator reissues it.
	WhyRepaired = "repaired"
)

// How a done request was proven (Request.ProofAs). Surfaces show it: only
// ProofDone is plain done.
const (
	ProofDone             = "done"
	ProofDelivered        = "delivered" // receipt only; not the same as done
	ProofAcceptedUnproven = "accepted_unproven"
)

// SourceRef names the record a request came from, by ID.
type SourceRef struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

// Holder is the session that holds a request. SessionID is the full ID.
type Holder struct {
	SessionID string    `json:"session_id"`
	Family    string    `json:"family,omitempty"` // backend family, from datawatch's session record
	Since     time.Time `json:"since"`
}

// DeliveryRecord is what datawatch saw of one delivery to the current holder.
// There is one per request and holder: a repeated take changes nothing in it.
type DeliveryRecord struct {
	// NoticeAt is when a content-free line was last typed. It is never proof.
	NoticeAt *time.Time `json:"notice_at,omitempty"`
	// TypeClaimAt is saved before the content is typed into a session;
	// ContentTypedAt after the terminal took the keys. Either one means the
	// content may have been handed over: it is typed at most once and is not
	// sent again by itself.
	TypeClaimAt    *time.Time `json:"type_claim_at,omitempty"`
	ContentTypedAt *time.Time `json:"content_typed_at,omitempty"`
	// FetchedAt is the first take with the holder's own session credential. It
	// shows the content reached a program or a tool call; whether that counts is
	// the worker policy's rule.
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	// FetchedVia is what the caller said it is: ViaTool or ViaProgram.
	FetchedVia string `json:"fetched_via,omitempty"`
	// Digest is the SHA-256, in hex, of the exact content served.
	Digest string `json:"digest,omitempty"`
	// InModelCallAt is when the worker's own program reported ("seen") that the
	// exact served content was in a model call that completed. It does not show
	// that the model heeded it or did it.
	InModelCallAt *time.Time `json:"in_model_call_at,omitempty"`
	// ReporterVersion is the version the reporting program said it is. A claim.
	ReporterVersion string     `json:"reporter_version,omitempty"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	// CodeOK: the accept carried the receipt code only the content holds.
	CodeOK bool `json:"code_ok,omitempty"`
	// Reminders counts notices after the fetch and before the accept.
	Reminders int `json:"reminders,omitempty"`
}

// MaybeHandedOver reports that the content may have reached somebody. Such a
// request is not sent again by itself unless its kind is read-only.
func (d DeliveryRecord) MaybeHandedOver() bool {
	return d.FetchedAt != nil || d.TypeClaimAt != nil || d.ContentTypedAt != nil
}

// ReportMark records a report. The report's words are sealed content.
type ReportMark struct {
	At           time.Time `json:"at"`
	By           string    `json:"by"` // observed caller
	RulesVersion string    `json:"rules_version,omitempty"`
}

// Verdict is what datawatch's check last read. No caller can submit one.
type Verdict struct {
	At time.Time `json:"at"`
	// Status is the "done when" list's own reading (an acceptance status).
	Status string `json:"status"`
	// Closes: the request may become done now.
	Closes bool `json:"closes"`
	// NotMet: a check ran and failed; the work goes back to the holder.
	NotMet bool   `json:"not_met,omitempty"`
	Reason string `json:"reason,omitempty"` // a reason code
}

// CallRecord is one doer or operator call, with the caller datawatch observed.
// Claimed names are free text and are kept in the sealed content.
type CallRecord struct {
	At       time.Time `json:"at"`
	Verb     string    `json:"verb"`
	Observed string    `json:"observed"`
	Surface  Surface   `json:"surface"`
	// Refused is a reason code when the call was refused.
	Refused string `json:"refused,omitempty"`
}

// MaxCallRecords bounds Request.Calls; the oldest after the first are dropped.
const MaxCallRecords = 50

// Request is the readable index of one request. It holds IDs, codes, counts
// and times only; every word a person or model wrote is in RequestContent.
type Request struct {
	ID         string       `json:"id"`
	Kind       RequestKind  `json:"kind"`
	Class      Class        `json:"class"`
	Role       Role         `json:"role"`
	Scope      Scope        `json:"scope"`
	State      RequestState `json:"state"`
	ReasonCode string       `json:"reason_code,omitempty"`
	// KeyHash is the hash of the occurrence key. It is unique over every
	// state, including stubs.
	KeyHash string    `json:"key_hash"`
	Source  SourceRef `json:"source,omitempty"`
	After   string    `json:"after,omitempty"` // parent request
	Creator string    `json:"creator"`         // observed caller
	Created time.Time `json:"created"`
	StateAt time.Time `json:"state_at"`

	// AgeSeconds is observed time: each check adds what passed, at most two
	// intervals. HeldSeconds is the part spent held; take-by does not run then.
	AgeSeconds    int64      `json:"age_seconds"`
	HeldSeconds   int64      `json:"held_seconds,omitempty"`
	HeldSince     *time.Time `json:"held_since,omitempty"`
	HeldCause     string     `json:"held_cause,omitempty"`
	TakeBySeconds int64      `json:"take_by_seconds"`
	DueSeconds    int64      `json:"due_seconds"`
	Overdue       bool       `json:"overdue,omitempty"`
	// ReviewBy is when a report waiting for acceptance is listed as the
	// operator's. It ends nothing.
	ReviewBy *time.Time `json:"review_by,omitempty"`

	Holder        *Holder        `json:"holder,omitempty"`
	FormerHolders []string       `json:"former_holders,omitempty"`
	Delivery      DeliveryRecord `json:"delivery"`
	Report        *ReportMark    `json:"report,omitempty"`
	Verdict       *Verdict       `json:"verdict,omitempty"`
	ProofAs       string         `json:"proof_as,omitempty"`
	ClosedAt      *time.Time     `json:"closed_at,omitempty"`
	ClosedBy      string         `json:"closed_by,omitempty"`

	// RulesVersionGiven is the version datawatch named at the first take.
	RulesVersionGiven string `json:"rules_version_given,omitempty"`

	// SettledAt: somebody dropped a closed request, so it leaves the list.
	// Until then, or a reissue, a failed, expired or outside-cancelled request
	// stays listed.
	SettledAt *time.Time `json:"settled_at,omitempty"`
	SettledBy string     `json:"settled_by,omitempty"`

	ReissueOf   string `json:"reissue_of,omitempty"`
	ReissuedAs  string `json:"reissued_as,omitempty"`
	MayRunTwice bool   `json:"may_run_twice,omitempty"`

	Calls []CallRecord `json:"calls,omitempty"`
	// NonOperatorActions counts operator actions made by a session or peer.
	// The rule is recorded here and not enforced until 11.0.
	NonOperatorActions int `json:"non_operator_actions,omitempty"`

	// Pruned: only the stub is left. The content is gone; the key still counts.
	Pruned bool   `json:"pruned,omitempty"`
	Seq    uint64 `json:"seq"` // write number of this record
}

// DoneItem is one statement of a request's "done when" list, as a creator may
// give it. It has no field for a command or script: request lists run none.
type DoneItem struct {
	ID        string `json:"id,omitempty"`
	Statement string `json:"statement"`
	From      string `json:"from,omitempty"` // the words of the ask it covers
	// Method is "receipt", "state:file", "confirm" or "review".
	Method   string `json:"method"`
	Path     string `json:"path,omitempty"` // state:file, relative to the project
	Contains string `json:"contains,omitempty"`
	Effect   string `json:"effect,omitempty"`
	Change   bool   `json:"change,omitempty"`
	Before   string `json:"before,omitempty"`
	Target   string `json:"target,omitempty"` // receipt: the named recipient
}

// RequestContent is every word of a request. On a secure install it is sealed
// from the first write. It never goes to a notice, the list, a message, a log,
// the audit or an alert.
type RequestContent struct {
	// Key is the occurrence key the hash was made from.
	Key        string            `json:"key,omitempty"`
	Ask        string            `json:"ask,omitempty"`
	Detail     string            `json:"detail,omitempty"`
	Params     map[string]string `json:"params,omitempty"`
	Items      []DoneItem        `json:"items,omitempty"`
	ProjectDir string            `json:"project_dir,omitempty"`
	// ReceiptCode is handed out only inside a take reply.
	ReceiptCode string `json:"receipt_code,omitempty"`
	ReportText  string `json:"report_text,omitempty"`
	ResultID    string `json:"result_id,omitempty"`
	DeclineText string `json:"decline_text,omitempty"`
	Words       string `json:"words,omitempty"` // the operator's words for force, drop, accept
	// ClaimedNames are what callers said they were. Claims, not identities.
	ClaimedNames []string `json:"claimed_names,omitempty"`
}

// RoleBinding is who holds a role in a scope. One holder per role and scope.
type RoleBinding struct {
	Role      Role      `json:"role"`
	Scope     Scope     `json:"scope"`
	SessionID string    `json:"session_id"`
	GivenBy   string    `json:"given_by"` // observed caller
	GivenAt   time.Time `json:"given_at"`
	// SelfGiven: the holder gave itself the role. Recorded, not refused.
	SelfGiven bool `json:"self_given,omitempty"`
	// Generation rises each time the role gets a new holder.
	Generation int `json:"generation"`
	// CanTakeAt is when this session proved it can take (its hello was
	// delivered by its worker policy). Nil: it gets no must-deliver work.
	CanTakeAt *time.Time `json:"can_take_at,omitempty"`
	HelloID   string     `json:"hello_id,omitempty"`
}

// RequestNotice is one reminder datawatch decided to type. It is saved before
// the line is typed, so the hourly limits hold across a restart.
type RequestNotice struct {
	At         time.Time `json:"at"`
	SessionID  string    `json:"session_id"`
	RequestIDs []string  `json:"request_ids"`
}

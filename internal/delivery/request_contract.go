// The job 4 contract: who calls, what the surfaces call, what the parts offer
// each other, and the names of the routes and tools. Owner: part A. A change
// here goes through the lead.
//
// Parts: A is the store and its states (request_{types,contract,store,states,
// keys,import,rules}.go); B is the surfaces; C is the roles, reminders, proof,
// typed input and worker policies (request_{roles,notice,caps,proof,typed,
// workers}.go).

package delivery

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"
)

// ---- who calls --------------------------------------------------------------

// CallerKind is what datawatch saw from the credential.
type CallerKind string

// The caller kinds.
const (
	// CallerOperator is the daemon's own token. It is not proof of a person.
	CallerOperator CallerKind = "operator"
	// CallerSession is a per-session credential whose session datawatch named.
	CallerSession CallerKind = "session"
	// CallerSessionUnknown is a session credential with no known session.
	CallerSessionUnknown CallerKind = "session_unknown"
	CallerPeer           CallerKind = "peer"
	// CallerDatawatch is the daemon itself. No surface may build one.
	CallerDatawatch CallerKind = "datawatch"
)

// Surface is the way a call came in.
type Surface string

// The surfaces.
const (
	SurfaceREST   Surface = "rest"
	SurfaceMCP    Surface = "mcp"
	SurfaceCLI    Surface = "cli"
	SurfaceChat   Surface = "chat"
	SurfaceDaemon Surface = "daemon"
)

// Caller is who made a call. Every field but the two Claimed ones comes from
// the credential. No request body can set them.
type Caller struct {
	Kind      CallerKind
	SessionID string // CallerSession only: the full session ID
	Peer      string // CallerPeer only
	Surface   Surface
	// ClaimedName and ClaimedSession are what the call said about itself.
	// ClaimedSession may only count against a caller (a claimed doer's answer
	// is a claim). It never makes a caller a holder.
	ClaimedName    string
	ClaimedSession string
}

// Observed is the caller as it is recorded.
func (c Caller) Observed() string {
	switch c.Kind {
	case CallerSession:
		return "session:" + c.SessionID
	case CallerSessionUnknown:
		return "session:unknown"
	case CallerPeer:
		return "peer:" + c.Peer
	case CallerDatawatch:
		return "datawatch"
	case CallerOperator:
		return "admin-token"
	}
	return "unknown"
}

// Holds reports whether this caller is the session holderID. It is the only
// test behind take, seen, accept, report and decline: everyone else is
// refused, and the real caller is recorded.
func (c Caller) Holds(holderID string) bool {
	return c.Kind == CallerSession && c.SessionID != "" && c.SessionID == holderID
}

// ---- errors -----------------------------------------------------------------

// Errors of the request calls. HTTPStatus maps them for every surface. A save
// that fails is ErrCannotRecord: nothing happened, nothing is typed, and a
// source must not be marked used.
var (
	ErrRequestsNotBuilt = errors.New("requests: not built yet")
	ErrRequestNotFound  = errors.New("no such request")
	ErrRequestInput     = errors.New("invalid request")
	ErrNotHolder        = errors.New("only the session that holds this request may do that; the call was recorded and nothing changed")
	ErrCrossHost        = errors.New("tracked requests to or from another datawatch computer are not supported yet; nothing was saved")
	ErrRequestState     = errors.New("the request is not in a state that allows this")
	ErrRoleHeld         = errors.New("the role has a live holder; forcing it needs a reason")
	ErrReasonNeeded     = errors.New("this needs a reason code")
	ErrMayRunTwice      = errors.New("work may already have been done: read the report, then say that it may run twice")
	ErrCheckMethod      = errors.New("a request list takes receipt, state:file, confirm or review items; it runs no commands")
)

// HTTPStatus is the status every surface answers with for err.
func HTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrRequestNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrNotHolder):
		return http.StatusForbidden
	case errors.Is(err, ErrCrossHost):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrRequestState), errors.Is(err, ErrRoleHeld), errors.Is(err, ErrMayRunTwice):
		return http.StatusConflict
	case errors.Is(err, ErrCannotRecord), errors.Is(err, ErrRecords):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrRequestsNotBuilt):
		return http.StatusNotImplemented
	case errors.Is(err, ErrRequestInput), errors.Is(err, ErrReasonNeeded), errors.Is(err, ErrCheckMethod), errors.Is(err, ErrReasonCode):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

var scopeIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// CheckScopeID refuses a scope ID that could carry content or a path: scope
// IDs are readable on disk.
func CheckScopeID(id string) error {
	if !scopeIDRe.MatchString(id) {
		return errors.Join(ErrRequestInput, errors.New("a scope ID is 1 to 128 of letters, digits, dot, colon, dash and underscore"))
	}
	return nil
}

// ---- inputs and replies -----------------------------------------------------

// How a take or seen says it was made.
const (
	ViaTool    = "tool"    // the model called a tool or command
	ViaProgram = "program" // the worker's own program (hook, extension)
)

// CreateInput makes one request.
type CreateInput struct {
	Kind  RequestKind `json:"kind"`
	Role  Role        `json:"role"`
	Scope Scope       `json:"scope"`
	// Host names the computer the request is for. Empty or this computer;
	// anything else is ErrCrossHost.
	Host string `json:"host,omitempty"`
	// Key names the occurrence, never the content (see OccurrenceKey). The same
	// key returns the request already made, in any state, or its stub.
	Key    string    `json:"key"`
	Source SourceRef `json:"source,omitempty"`
	After  string    `json:"after,omitempty"`
	// TakeBy and Due of zero take the settings and kind defaults. No request is
	// open-ended.
	TakeBy  time.Duration  `json:"take_by,omitempty"`
	Due     time.Duration  `json:"due,omitempty"`
	Content RequestContent `json:"content"`
}

// TakeInput comes with a take.
type TakeInput struct {
	Via string `json:"via,omitempty"`
}

// TakeReply carries the content. The same take again by the holder gives the
// same content and digest (a lost reply is safe).
type TakeReply struct {
	Request RequestView `json:"request"`
	// Content is the exact text to put before the model; Digest is its SHA-256.
	Content      string `json:"content"`
	Digest       string `json:"digest"`
	RulesVersion string `json:"rules_version,omitempty"`
	// Open are all the open requests this session holds.
	Open []string `json:"open"`
	// Repeat: this holder had fetched it before. A program that puts content
	// into a conversation by itself must not do so for a repeat unless ResendOK:
	// only never-fetched or read-only work is sent again by itself (design v2,
	// break 1). What it does not inject stays unconfirmed and is listed.
	Repeat bool `json:"repeat,omitempty"`
	// ResendOK: the kind is read-only.
	ResendOK bool `json:"resend_ok,omitempty"`
}

// SeenInput is the worker program's report that the exact served content was
// in a model call that completed. Before a take, or with another digest, it is
// ErrRequestState. A program keeps an unrecorded report on disk and retries it.
type SeenInput struct {
	Digest          string `json:"digest"`
	Via             string `json:"via,omitempty"`
	ReporterVersion string `json:"reporter_version,omitempty"`
}

// AcceptInput comes with an accept. An accept on a request that is not yet
// delivered is recorded (AcceptedAt, CodeOK) and moves nothing; when the
// delivery then counts, the request moves on to accepted. Code is the receipt
// code from the content; a wrong one is refused.
type AcceptInput struct {
	Code string `json:"code,omitempty"`
}

// ReportInput says the holder believes the work is done. It never makes a
// request done.
type ReportInput struct {
	Text         string `json:"text,omitempty"`
	ResultID     string `json:"result_id,omitempty"`
	RulesVersion string `json:"rules_version,omitempty"`
}

// DeclineInput says the holder will not do it.
type DeclineInput struct {
	ReasonCode string `json:"reason_code"`
	Text       string `json:"text,omitempty"`
}

// ReasonInput is an operator action: reissue, drop or accept unproven.
type ReasonInput struct {
	ReasonCode string `json:"reason_code"`
	Words      string `json:"words,omitempty"`
	// RepeatOK: the caller saw that work may already have been done.
	RepeatOK bool `json:"repeat_ok,omitempty"`
}

// ForceInput moves a request off its holder, even a live one with accepted
// work. The reply says it may run twice.
type ForceInput struct {
	ReasonCode string `json:"reason_code"`
	ToSession  string `json:"to_session,omitempty"` // empty: back to the role
	Words      string `json:"words,omitempty"`
}

// BacklogInput turns one backlog entry of the delivery list into a request.
type BacklogInput struct {
	EntryKey string         `json:"entry_key"`
	Role     Role           `json:"role"`
	Scope    Scope          `json:"scope"`
	Content  RequestContent `json:"content"`
}

// RoleInput gives or releases a role.
type RoleInput struct {
	Role      Role   `json:"role"`
	Scope     Scope  `json:"scope"`
	SessionID string `json:"session_id"`
	// Force takes the role from a live holder; it needs ReasonCode.
	Force      bool   `json:"force,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// RequestFilter narrows a listing. Empty fields match everything.
type RequestFilter struct {
	State     RequestState
	Kind      RequestKind
	Role      Role
	ScopeKind ScopeKind
	ScopeID   string
	Holder    string
}

// RequestView is a request as a surface shows it: the index and a label. It
// holds no content.
type RequestView struct {
	Request
	// Label is the plain reading, one of the Label constants.
	Label string `json:"label"`
	// ReadOnly: the kind may be sent again after a fetch.
	ReadOnly bool `json:"read_only,omitempty"`
}

// The labels a view carries. B renders them; they never read "done" for
// anything but plain proven done.
const (
	LabelWaiting          = "waiting"
	LabelTypedUnconfirmed = "typed, unconfirmed"
	LabelFetched          = "fetched, unconfirmed"
	LabelDelivered        = "delivered to the holder"
	// LabelInModelCall is how a delivery proven by a program's report reads. It
	// claims no attention, understanding or work.
	LabelInModelCall      = "in a completed model call"
	LabelAccepted         = "accepted by the holder"
	LabelNotAccepted      = "reported: finished, not accepted"
	LabelDone             = "done"
	LabelDeliveredOnly    = "delivered (not the same as done)"
	LabelAcceptedUnproven = "done, accepted without proof"
	LabelFailed           = "failed"
	LabelExpired          = "expired"
	LabelCancelled        = "cancelled"
)

// MineReply is everything open that one session holds.
type MineReply struct {
	SessionID string        `json:"session_id"`
	Items     []RequestView `json:"items"`
	Roles     []RoleBinding `json:"roles"`
}

// ---- what the surfaces call (A implements, B calls) --------------------------

// RequestService is what REST, MCP, CLI and chat call. *Requests implements
// it. WHO rules for operator actions are recorded, not enforced, with two
// exceptions that stay: only the holder may take, see, accept, report or
// decline; and nothing a doer says makes its own request done.
type RequestService interface {
	Create(ctx context.Context, c Caller, in CreateInput) (v RequestView, created bool, err error)
	Take(ctx context.Context, c Caller, id string, in TakeInput) (TakeReply, error)
	Seen(ctx context.Context, c Caller, id string, in SeenInput) (RequestView, error)
	Accept(ctx context.Context, c Caller, id string, in AcceptInput) (RequestView, error)
	Report(ctx context.Context, c Caller, id string, in ReportInput) (RequestView, error)
	Decline(ctx context.Context, c Caller, id string, in DeclineInput) (RequestView, error)
	Mine(ctx context.Context, c Caller) (MineReply, error)

	Get(ctx context.Context, c Caller, id string) (RequestView, error)
	List(ctx context.Context, c Caller, f RequestFilter) ([]RequestView, error)
	Roles(ctx context.Context, c Caller) ([]RoleBinding, error)

	GiveRole(ctx context.Context, c Caller, in RoleInput) (RoleBinding, error)
	ReleaseRole(ctx context.Context, c Caller, in RoleInput) error
	Reissue(ctx context.Context, c Caller, id string, in ReasonInput) (RequestView, error)
	Drop(ctx context.Context, c Caller, id string, in ReasonInput) (RequestView, error)
	Force(ctx context.Context, c Caller, id string, in ForceInput) (RequestView, error)
	// AcceptUnproven goes through the "done when" list: from a doer or an
	// unnamed session it is kept as a claim and closes nothing.
	AcceptUnproven(ctx context.Context, c Caller, id string, in ReasonInput) (RequestView, error)
	FromBacklog(ctx context.Context, c Caller, in BacklogInput) (v RequestView, created bool, err error)
}

// Condition is something that can be true or false, or have a value, that
// datawatch watches (a wake, a rule version). The episode or change number is
// saved in the same write as the request it makes.
type Condition struct {
	ID string
	// Holds is the condition now. A request is made each time it goes from
	// false to true.
	Holds bool
	// Value, when set, replaces Holds: a request is made each time it differs
	// from the saved one (A, B, A makes two after the first).
	Value string
	// Request builds the request for occurrence n. Its Key is ignored.
	Request func(n int) CreateInput
}

// RequestDaemon is what only datawatch calls. No surface offers any of it.
type RequestDaemon interface {
	// Tick ages requests, expires within the mass guard, sends the reminders
	// that are allowed and runs the check on reported requests.
	Tick(ctx context.Context) error
	// HolderEnded records that datawatch saw a session end.
	HolderEnded(ctx context.Context, sessionID string) error
	// Candidates is the delivery check's "requests" source.
	Candidates(ctx context.Context, now time.Time) ([]Candidate, error)
	// Observe makes a request for a new occurrence of a condition (job 5, 6).
	Observe(ctx context.Context, cond Condition) (v RequestView, created bool, err error)
	// Has reports whether an occurrence key has a request or a stub.
	Has(ctx context.Context, key string) (bool, error)
	// SourceGone lets the stubs of a source that no longer exists be removed.
	SourceGone(ctx context.Context, ref SourceRef) error
}

// ---- what C reads and changes (A implements) ---------------------------------

// RequestBook is the part of the store that roles, reminders and the check
// use. Every change is saved durably before the call returns; on
// ErrCannotRecord nothing changed and nothing may be typed.
type RequestBook interface {
	Requests(f RequestFilter) []Request
	Request(id string) (Request, bool)
	Content(id string) (RequestContent, error)
	// Change applies fn to a copy and saves it. An error from fn changes nothing.
	Change(id string, fn func(*Request) error) (Request, error)
	Bindings() []RoleBinding
	PutBinding(b RoleBinding) error
	DropBinding(role Role, scope Scope) error
	// Notices are the reminders decided since then.
	Notices(since time.Time) []RequestNotice
	AddNotice(n RequestNotice) error
}

// ---- what C offers A ----------------------------------------------------------

// RoleKeeper gives, releases and ends roles. Resolve is the one place that
// says who a role is (job 5 builds on it for the missing-supervisor choice).
type RoleKeeper interface {
	Resolve(b RequestBook, role Role, scope Scope) (RoleBinding, bool)
	// Give returns the binding and, when this session has not yet proven it can
	// take, the hello request to create.
	Give(ctx context.Context, b RequestBook, c Caller, in RoleInput) (RoleBinding, *CreateInput, error)
	Release(ctx context.Context, b RequestBook, c Caller, in RoleInput) error
	// Ended drops the bindings of an ended session. Request states are A's.
	Ended(ctx context.Context, b RequestBook, sessionID string) error
	// Delivered is told when a delivery counts (a hello proves the take).
	Delivered(ctx context.Context, b RequestBook, r Request) error
}

// Notifier sends the reminders and does the typed-once sends that are allowed
// now. It saves before it types.
type Notifier interface {
	Notify(ctx context.Context, b RequestBook, now time.Time) error
}

// Verifier is the only door to "done". It uses the "done when" lists; there is
// no second proof engine.
type Verifier interface {
	// Prepare makes and fixes the list before the first delivery. A
	// DeliveryOnly kind's list gets an empty ask: any ask sentence becomes a
	// person's confirm item, and the list could then never read "delivered".
	// Prepare refuses a list that already exists under the ID and was not made
	// by datawatch.
	Prepare(ctx context.Context, r Request, content RequestContent) error
	// Holder records who does the work, before any receipt is read. When it
	// replaces another holder it first tells the list the work was replaced, so
	// the earlier holder stays a doer and its old receipt and answers stop
	// counting.
	Holder(ctx context.Context, requestID, sessionID string) error
	// Started records the first notice, take or typed input.
	Started(ctx context.Context, requestID string, kind RequestKind) error
	// Check runs the list and reads it.
	Check(ctx context.Context, r Request) (Verdict, error)
	// Rework asks whether failed work may go back to the holder once more.
	Rework(ctx context.Context, r Request) (allowed bool, err error)
	// Waive records "accepted without proof" on the open items as this caller,
	// then reads the list. A doer's waiver is a claim and closes nothing.
	Waive(ctx context.Context, c Caller, r Request, in ReasonInput) (Verdict, error)
}

// ---- what datawatch supplies --------------------------------------------------

// SessionInfo is what datawatch knows of a session.
type SessionInfo struct {
	ID         string
	Family     string // backend family
	ProjectDir string
	Live       bool
}

// SessionBook reads session records.
type SessionBook interface {
	Info(sessionID string) (SessionInfo, bool)
}

// Holds says why a session gets no reminder and why its take-by clock stops: a
// pause, a usage back-off, a burn guard or budget hold, or a rate limit. The
// cause is one of the WhyHeld codes.
type Holds interface {
	Held(sessionID string, scope Scope) (cause string, held bool)
}

// ErrPromptRefused: the terminal guard refused to type (a prompt is open).
// Nothing was typed and the try does not count.
var ErrPromptRefused = errors.New("not typed: the session is at a prompt")

// Typer types into a session. A nil error means the terminal took the keys. It
// is never proof of delivery.
type Typer interface {
	// Notice types one content-free line.
	Notice(ctx context.Context, sessionID, line string) error
	// Request types a request's text. Only "request <id> typed" is recorded by
	// the session tracker and logs.
	Request(ctx context.Context, sessionID, requestID, text string) error
}

// HolderStarter starts the project's defined supervisor when a request waits
// and nobody holds the role. Job 4 wires none; job 5 supplies it under the
// operator's rule (never while paused or under a spending hold).
type HolderStarter interface {
	StartHolder(ctx context.Context, role Role, scope Scope) (started bool, err error)
}

// RequestSettings are read live.
type RequestSettings struct {
	TakeBy    time.Duration
	Due       time.Duration
	DueByKind map[RequestKind]time.Duration
	// ReminderInterval is the shortest time between two reminders to a session.
	ReminderInterval time.Duration
	// RemindersPerSessionHour and RemindersPerHostHour are the operator's two
	// limits. Zero means no typed reminders.
	RemindersPerSessionHour int
	RemindersPerHostHour    int
}

// MaxFetchedReminders is how many reminders a fetched, not accepted request
// gets before it is only listed.
const MaxFetchedReminders = 2

// RequestOptions configure the store.
type RequestOptions struct {
	Dir string // <data dir>/queue.d
	Key []byte // the delivery store key; nil on a non-secure install
	// AdoptPlain: secure mode was just turned on; plain records are sealed.
	AdoptPlain bool
	Host       string
	Now        func() time.Time
	Interval   time.Duration
	Settings   func() RequestSettings
	Roles      RoleKeeper
	Notifier   Notifier
	Verifier   Verifier
	Sessions   SessionBook
	Holds      Holds
	Starter    HolderStarter // may be nil
	// Record raises an alarm or a recorded failure on the delivery list.
	Record func(Candidate) error
	// RulesVersion names the rules version of a scope now (job 6). May be nil.
	RulesVersion func(Scope) string
	// Backlog says whether the delivery list holds this backlog entry, and its
	// episode, so that an entry becomes a request once. May be nil: any entry
	// key is taken, as episode 1.
	Backlog func(entryKey string) (episode int, ok bool)

	// writeFile replaces the durable write in tests (a full disk).
	writeFile func(path string, data []byte) error
}

// ---- names every surface uses --------------------------------------------------

// REST routes. {id} routes are POST, under RouteRequests + "/{id}/<verb>".
const (
	RouteRequests           = "/api/requests"               // GET list, POST create
	RouteRequestsMine       = "/api/requests/mine"          // GET
	RouteRequestRoles       = "/api/requests/roles"         // GET
	RouteRequestRoleGive    = "/api/requests/roles/give"    // POST
	RouteRequestRoleRelease = "/api/requests/roles/release" // POST
	RouteRequestFromBacklog = "/api/requests/from-backlog"  // POST
	VerbTake                = "take"
	VerbSeen                = "seen" // the worker's program only; no MCP tool
	VerbAccept              = "accept"
	VerbReport              = "report"
	VerbDecline             = "decline"
	VerbReissue             = "reissue"
	VerbDrop                = "drop"
	VerbForce               = "force"
	VerbAcceptUnproven      = "accept-unproven"
)

// MCP tool names. They are new names: the approved tool-name list used by one
// test changes only with the operator's approval of exactly these.
const (
	ToolRequestMine    = "request_mine"
	ToolRequestTake    = "request_take"
	ToolRequestAccept  = "request_accept"
	ToolRequestReport  = "request_report"
	ToolRequestDecline = "request_decline"
	ToolRequestCreate  = "request_create"
	ToolRequestList    = "request_list"   // list, one by id, roles
	ToolRequestRole    = "request_role"   // give, release
	ToolRequestManage  = "request_manage" // reissue, drop, force, accept-unproven, from-backlog
)

// RequestToolNames are the nine.
var RequestToolNames = []string{ToolRequestAccept, ToolRequestCreate, ToolRequestDecline, ToolRequestList,
	ToolRequestManage, ToolRequestMine, ToolRequestReport, ToolRequestRole, ToolRequestTake}

// KindRequest is a request on the delivery list. ID is the request ID.
const KindRequest Kind = "request"

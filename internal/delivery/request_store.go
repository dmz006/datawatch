// The request store: one file per request, <data dir>/queue.d/<id>.rec, with a
// readable index and, on a secure install, sealed content from the first
// write. It shares the delivery store key (storekey.go). Owner: part A.
//
// STUB: nothing here works yet and nothing is wired.
//
// What part A must keep (design v2 1.4):
//   - Secure record: {"v":1,"index":Request,"sealed":"..."}; the sealed part
//     holds the content and a copy of the index. A readable index that differs
//     from the sealed copy is a foreign write: red, never believed.
//     Non-secure record: {"v":1,"index":Request,"content":RequestContent}.
//   - sealedRecords (storekey.go) must count a sealed request record, so a
//     missing store.key with records present still refuses the start.
//   - A sealed manifest of each file's last write number and hash (I12). An
//     unknown, changed or missing file, or a plain one in a sealed store, is
//     red. A record that cannot be opened is red (I17), never "not there".
//   - Nothing under queue.d is written until every other sealed store opened.

package delivery

import (
	"context"
	"encoding/json"
	"time"
)

// Requests is the store and the service over it.
type Requests struct {
	opts RequestOptions
}

var (
	_ RequestService = (*Requests)(nil)
	_ RequestDaemon  = (*Requests)(nil)
	_ RequestBook    = (*Requests)(nil)
)

// OpenRequests opens the store. It writes nothing.
func OpenRequests(opts RequestOptions) (*Requests, error) {
	return &Requests{opts: opts}, ErrRequestsNotBuilt
}

// Create implements RequestService.
func (s *Requests) Create(context.Context, Caller, CreateInput) (RequestView, bool, error) {
	return RequestView{}, false, ErrRequestsNotBuilt
}

// Take implements RequestService.
func (s *Requests) Take(context.Context, Caller, string, TakeInput) (TakeReply, error) {
	return TakeReply{}, ErrRequestsNotBuilt
}

// Seen implements RequestService.
func (s *Requests) Seen(context.Context, Caller, string, SeenInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// Accept implements RequestService.
func (s *Requests) Accept(context.Context, Caller, string, AcceptInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// Report implements RequestService.
func (s *Requests) Report(context.Context, Caller, string, ReportInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// Decline implements RequestService.
func (s *Requests) Decline(context.Context, Caller, string, DeclineInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// Mine implements RequestService.
func (s *Requests) Mine(context.Context, Caller) (MineReply, error) {
	return MineReply{}, ErrRequestsNotBuilt
}

// Get implements RequestService.
func (s *Requests) Get(context.Context, Caller, string) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// List implements RequestService.
func (s *Requests) List(context.Context, Caller, RequestFilter) ([]RequestView, error) {
	return nil, ErrRequestsNotBuilt
}

// Roles implements RequestService.
func (s *Requests) Roles(context.Context, Caller) ([]RoleBinding, error) {
	return nil, ErrRequestsNotBuilt
}

// GiveRole implements RequestService.
func (s *Requests) GiveRole(context.Context, Caller, RoleInput) (RoleBinding, error) {
	return RoleBinding{}, ErrRequestsNotBuilt
}

// ReleaseRole implements RequestService.
func (s *Requests) ReleaseRole(context.Context, Caller, RoleInput) error { return ErrRequestsNotBuilt }

// Reissue implements RequestService.
func (s *Requests) Reissue(context.Context, Caller, string, ReasonInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// Drop implements RequestService.
func (s *Requests) Drop(context.Context, Caller, string, ReasonInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// Force implements RequestService.
func (s *Requests) Force(context.Context, Caller, string, ForceInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// AcceptUnproven implements RequestService.
func (s *Requests) AcceptUnproven(context.Context, Caller, string, ReasonInput) (RequestView, error) {
	return RequestView{}, ErrRequestsNotBuilt
}

// FromBacklog implements RequestService.
func (s *Requests) FromBacklog(context.Context, Caller, BacklogInput) (RequestView, bool, error) {
	return RequestView{}, false, ErrRequestsNotBuilt
}

// Tick implements RequestDaemon.
func (s *Requests) Tick(context.Context) error { return ErrRequestsNotBuilt }

// HolderEnded implements RequestDaemon.
func (s *Requests) HolderEnded(context.Context, string) error { return ErrRequestsNotBuilt }

// Candidates implements RequestDaemon.
func (s *Requests) Candidates(context.Context, time.Time) ([]Candidate, error) {
	return nil, ErrRequestsNotBuilt
}

// Observe implements RequestDaemon.
func (s *Requests) Observe(context.Context, Condition) (RequestView, bool, error) {
	return RequestView{}, false, ErrRequestsNotBuilt
}

// Has implements RequestDaemon.
func (s *Requests) Has(context.Context, string) (bool, error) { return false, ErrRequestsNotBuilt }

// SourceGone implements RequestDaemon.
func (s *Requests) SourceGone(context.Context, SourceRef) error { return ErrRequestsNotBuilt }

// Requests implements RequestBook.
func (s *Requests) Requests(RequestFilter) []Request { return nil }

// Request implements RequestBook.
func (s *Requests) Request(string) (Request, bool) { return Request{}, false }

// Content implements RequestBook.
func (s *Requests) Content(string) (RequestContent, error) {
	return RequestContent{}, ErrRequestsNotBuilt
}

// Change implements RequestBook.
func (s *Requests) Change(string, func(*Request) error) (Request, error) {
	return Request{}, ErrRequestsNotBuilt
}

// Bindings implements RequestBook.
func (s *Requests) Bindings() []RoleBinding { return nil }

// PutBinding implements RequestBook.
func (s *Requests) PutBinding(RoleBinding) error { return ErrRequestsNotBuilt }

// DropBinding implements RequestBook.
func (s *Requests) DropBinding(Role, Scope) error { return ErrRequestsNotBuilt }

// Notices implements RequestBook.
func (s *Requests) Notices(time.Time) []RequestNotice { return nil }

// AddNotice implements RequestBook.
func (s *Requests) AddNotice(RequestNotice) error { return ErrRequestsNotBuilt }

// The older work queue keeps its items in the same folder, one sealed record
// each (session.QueueRecords).

// LoadQueue returns every queue item.
func (s *Requests) LoadQueue() ([]json.RawMessage, error) { return nil, ErrRequestsNotBuilt }

// SaveQueueItem writes one queue item.
func (s *Requests) SaveQueueItem(string, []byte) error { return ErrRequestsNotBuilt }

// DeleteQueueItem removes one queue item.
func (s *Requests) DeleteQueueItem(string) error { return ErrRequestsNotBuilt }

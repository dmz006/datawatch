// "Done" for a request is its "done when" list reading done (internal/
// acceptance). There is no second proof engine here, and no request runs a
// command. Owner: part C. STUB.
//
// What the list needs from this side (audit/review-acceptance-a-code-report.md,
// "Job 4 must"): the observed caller on every change; the holder recorded
// before any receipt is read; SourceKnown for datawatch's own sources; the
// recipient named on work lists; the receipt's time with sub-second
// precision, with the list made before delivery; no occurrence except for a
// scheduled run; and something must call Evaluate.
//
// A list holds the request's words, so on a secure install its store is part of
// the request store: wired with the delivery store key, not the key tied to the
// config file's salt, and covered by the marker scan.

package delivery

import (
	"context"
	"time"

	"github.com/dmz006/datawatch/internal/acceptance"
)

type listsVerifier struct{ lists *acceptance.Service }

// NewListsVerifier is the Verifier over the "done when" lists. With lists off
// or missing it never closes anything.
func NewListsVerifier(lists *acceptance.Service) Verifier { return &listsVerifier{lists: lists} }

func (v *listsVerifier) Prepare(context.Context, Request, RequestContent) error {
	return ErrRequestsNotBuilt
}

func (v *listsVerifier) Holder(context.Context, string, string) error { return ErrRequestsNotBuilt }

func (v *listsVerifier) Started(context.Context, string, RequestKind) error {
	return ErrRequestsNotBuilt
}

func (v *listsVerifier) Check(context.Context, Request) (Verdict, error) {
	return Verdict{}, ErrRequestsNotBuilt
}

func (v *listsVerifier) Rework(context.Context, Request) (bool, error) {
	return false, ErrRequestsNotBuilt
}

func (v *listsVerifier) Waive(context.Context, Caller, Request, ReasonInput) (Verdict, error) {
	return Verdict{}, ErrRequestsNotBuilt
}

type receiptProbe struct{ book func() RequestBook }

// NewReceiptProbe answers the list's receipt items from the delivery records.
// It answers "fetched" only for a delivery that counts under the holder's
// worker policy. book may return nil until the store is open.
func NewReceiptProbe(book func() RequestBook) acceptance.Probe { return &receiptProbe{book: book} }

func (p *receiptProbe) Read(context.Context, acceptance.ProbeRequest) (acceptance.Reading, error) {
	return acceptance.Reading{}, nil // not answered: "no receipt yet"
}

type requestResolver struct{ book func() RequestBook }

// NewRequestResolver says which version of the work a request's file items are
// read at: the commit its project folder is on. For any other list it errors,
// so that item reads "cannot tell".
func NewRequestResolver(book func() RequestBook) acceptance.SubjectResolver {
	return &requestResolver{book: book}
}

func (r *requestResolver) Resolve(context.Context, string, string) (acceptance.Subject, error) {
	return acceptance.Subject{}, ErrRequestsNotBuilt
}

// RefusingRunner runs no check. Until check execution is isolated (job 5),
// datawatch is wired with it, and command items read "cannot tell".
type RefusingRunner struct{}

// Run implements acceptance.Runner.
func (RefusingRunner) Run(context.Context, acceptance.CheckSpec, acceptance.Subject, time.Duration) acceptance.RunOutcome {
	return acceptance.RunOutcome{ExitCode: -1, Reason: "datawatch runs no command checks yet: running them is not isolated"}
}

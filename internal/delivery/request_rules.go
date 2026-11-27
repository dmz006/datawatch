// The fixed table of request kinds. It is in Go, not in settings: which work
// may be sent again by itself is not a choice a record or a caller makes.
// Owner: part A. (Status rules are in rules.go; job 6 must not use that name.)

package delivery

import "time"

// KindRule is what datawatch knows about one kind.
type KindRule struct {
	Class Class
	// ReadOnly: fetched but not accepted may be sent again to a new holder.
	// Every other kind is sent again by itself only if it was never handed
	// over; otherwise it fails onto the operator's list.
	ReadOnly bool
	// DeliveryOnly: proven receipt closes it, shown as "delivered (not the
	// same as done)". Every other kind needs plain done.
	DeliveryOnly bool
	// ListSource is the "done when" source name the list is made with; it
	// picks the list's default items and how strict it is.
	ListSource string
	// MustDeliver: a role gets it only when its holder has proven it can take.
	// A request to one session (RoleSession) is that session's at once: its own
	// take is the proof.
	MustDeliver bool
	// Due is the kind's own default; zero takes the setting.
	Due time.Duration
}

// DeliveryOnly is set only where the written design closes on receipt: the
// hello (rule 12), and answers and rule bundles (design section 3.5,
// "received"). Whether a wake, a handover or a parent reply may close on
// receipt is not decided: they are held strict (they need plain done) until
// the operator decides, with job 5.
var kindRules = map[RequestKind]KindRule{
	ReqHello:       {Class: ClassNotice, ReadOnly: true, DeliveryOnly: true, ListSource: "message", Due: time.Hour},
	ReqAsk:         {Class: ClassAsk, ListSource: "unknown", MustDeliver: true},
	ReqBug:         {Class: ClassAsk, ListSource: "bug", MustDeliver: true},
	ReqWake:        {Class: ClassNotice, ReadOnly: true, ListSource: "unknown", MustDeliver: true},
	ReqHandover:    {Class: ClassNotice, ReadOnly: true, ListSource: "unknown", MustDeliver: true},
	ReqParentReply: {Class: ClassNotice, ListSource: "unknown", MustDeliver: true},
	ReqAnswer:      {Class: ClassNotice, DeliveryOnly: true, ListSource: "question", MustDeliver: true},
	ReqScheduleAsk: {Class: ClassAsk, ListSource: "unknown", MustDeliver: true},
	ReqExitHook:    {Class: ClassAsk, ListSource: "unknown", MustDeliver: true},
	ReqListGap:     {Class: ClassAsk, ListSource: "unknown", MustDeliver: true},
	ReqRules:       {Class: ClassNotice, ReadOnly: true, DeliveryOnly: true, ListSource: "message", MustDeliver: true},
}

// RuleFor returns the rule of a kind; ok is false for an unknown kind.
func RuleFor(k RequestKind) (KindRule, bool) {
	r, ok := kindRules[k]
	return r, ok
}

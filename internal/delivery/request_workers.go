// What counts as delivery for each kind of worker. This is THE PLUG-IN POINT
// for a fetch-and-confirm proof: a worker family counts only once its row here
// says it is proven. Owner: part C.
//
// A row never proves that one session can take: each session still proves that
// by getting its own hello delivered under its family's row. A row only says
// which signal from such a session is enough.
//
// To add a proven family: (1) the proof report, (2) its row here with Proven
// true and the signal the proof supports, (3) whatever makes the worker call
// RouteRequestsMine, take, seen and accept with its own session credential,
// (4) a failing test first. Nothing else changes.

package delivery

// DeliveryLevel is how far datawatch saw a request get. Only Delivered says
// that it counts.
type DeliveryLevel string

// The levels, weakest first.
const (
	LevelNone             DeliveryLevel = "none"
	LevelTypedUnconfirmed DeliveryLevel = "typed_unconfirmed"
	LevelFetched          DeliveryLevel = "fetched"       // taken with the session's credential
	LevelInModelCall      DeliveryLevel = "in_model_call" // and reported in a completed model call
)

// WorkerPolicy is the row of one backend family.
type WorkerPolicy struct {
	Family string
	// Proven: a recorded proof shows this family can fetch and confirm.
	Proven bool
	// Needs is the least level that counts as delivery.
	Needs DeliveryLevel
	// ReceiptCode: the content carries a code that the worker's receipt tool
	// repeats in its accept. It is extra evidence, never the delivery proof; a
	// wrong code is refused.
	ReceiptCode bool
	// PollsItself: the worker's program asks for waiting requests, so no
	// reminder has to be typed.
	PollsItself bool
}

var workerPolicies = map[string]WorkerPolicy{
	// The design counts the model's own take. NOT YET SHOWN with a real Claude
	// Code session and its own credential (design 3.7): that is the first gate
	// before job 4 is called ready. Each session still proves it with its hello.
	"claude-code": {Family: "claude-code", Proven: true, Needs: LevelFetched},
	// Proven by build/pi-fetch-proof-report.md (2026-11-27, pi 1.3.0, one local
	// Ollama OpenAI-style provider): a datawatch pi extension takes, injects,
	// and reports "seen" only after the exact text was in a model call that
	// completed (99 requests, 94 confirmed, 5 planted never to be; 0 false and
	// 0 missed against a separate proxy). Take alone must never count: in 11
	// forced cases no completed call held a fetched request. The model's own
	// receipt is extra only. The built extension passed a sandbox proof of its
	// own (build/d4-finish-report.md): follow-up delivery with the note, pi
	// 1.3.0, "seen" agreeing with a separate proxy.
	"pi": {Family: "pi", Proven: true, Needs: LevelInModelCall, ReceiptCode: true, PollsItself: true},
}

// PolicyFor returns the row of a family. An unknown family is not proven.
func PolicyFor(family string) WorkerPolicy {
	if p, ok := workerPolicies[family]; ok {
		return p
	}
	return WorkerPolicy{Family: family, Needs: LevelInModelCall}
}

// Level reads how far a delivery got.
func (p WorkerPolicy) Level(d DeliveryRecord) DeliveryLevel {
	switch {
	case d.FetchedAt != nil && d.InModelCallAt != nil:
		return LevelInModelCall
	case d.FetchedAt != nil:
		return LevelFetched
	case d.TypeClaimAt != nil || d.ContentTypedAt != nil || d.NoticeAt != nil:
		return LevelTypedUnconfirmed
	}
	return LevelNone
}

// Delivered reports whether a delivery counts. Typing never does.
func (p WorkerPolicy) Delivered(d DeliveryRecord) bool {
	if !p.Proven {
		return false
	}
	switch p.Level(d) {
	case LevelInModelCall:
		return true
	case LevelFetched:
		return p.Needs == LevelFetched
	}
	return false
}

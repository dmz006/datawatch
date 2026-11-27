// The state machine. Owner: part A.
//
// A take moves waiting -> delivered only when the delivery then counts under
// the holder's worker policy (claude-code: the take; pi: the later seen).
// Until then the request stays waiting, take-by keeps running, and if it was
// handed over the holder-ended and resend rules treat it as handed over.
//
// The moves part A builds (anything else is ErrRequestState):
//
//	waiting   -> delivered (holder take) | failed | expired (due) | cancelled
//	delivered -> accepted | failed (declined, holder gone) | expired (due) | cancelled
//	          -> waiting (holder gone or forced, and the kind is read-only)
//	accepted  -> reported | failed | cancelled
//	          never expired and never moved by a late take: past due it is
//	          marked overdue and listed
//	reported  -> done (datawatch's check only) | accepted (a check failed and
//	          rework is allowed) | failed (rework used up) | cancelled
//	          never expired and never sent again by itself
//	done, failed, expired, cancelled -> no move; reissue makes a new request
//
// One more move for a DeliveryOnly kind (hello, answer, rules): there is no work to report, so delivered -> done and accepted ->
// done, by datawatch's check only, once the delivery counts under the holder's
// worker policy and the list reads "delivered".
//
// A role is lost only when datawatch records that the holder ended, the holder
// releases it, or somebody reassigns it. When a holder ends: never handed over
// goes to the next holder; handed over and not accepted goes again only for a
// read-only kind; everything else fails onto the list. Reported stays reported.
// A request to one session (RoleSession) has no next holder: it fails.

package delivery

// Force is the one move outside this table: the operator takes a request off
// its holder, even an accepted one, back to waiting, and it is marked "may run
// twice". A reported one is never sent again.

// CanMove reports whether a request of kind rule may move between two states.
// handedOver is DeliveryRecord.MaybeHandedOver.
func CanMove(rule KindRule, from, to RequestState, handedOver bool) bool {
	if from.Closed() {
		return false
	}
	switch to {
	case StateWaiting:
		// Back from delivered only when the work may go again.
		return from == StateDelivered && (rule.ReadOnly || !handedOver)
	case StateDelivered:
		return from == StateWaiting
	case StateAccepted:
		return from == StateDelivered || from == StateReported
	case StateReported:
		return from == StateAccepted && !rule.DeliveryOnly
	case StateDone:
		if rule.DeliveryOnly {
			return from == StateDelivered || from == StateAccepted
		}
		return from == StateReported
	case StateFailed, StateCancelled:
		return true
	case StateExpired:
		return from == StateWaiting || from == StateDelivered
	}
	return false
}

// The operator's reminder limits: so many per session and per computer each
// hour (both are settings), none while paused, held for usage or rate limited,
// counted from saved records so a restart resets nothing. Owner: part C. STUB.

package delivery

import "time"

// ReminderAllowed reports whether one more reminder to session may be typed
// now, given the reminders saved in the last hour. When not, why is a reason
// code.
func ReminderAllowed(RequestSettings, []RequestNotice, string, time.Time) (ok bool, why string) {
	return false, WhyUndeliverable
}

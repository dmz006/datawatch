// The operator's reminder limits: so many per session and per computer each
// hour (both are settings), none while paused, held for usage or rate limited,
// counted from saved records so a restart resets nothing. Owner: part C.

package delivery

import "time"

// Why one more reminder is not typed.
const (
	whyRemindersOff     = "reminders_off"
	whyReminderInterval = "reminder_interval"
	whySessionLimit     = "session_reminder_limit"
	whyHostLimit        = "host_reminder_limit"
)

// ReminderAllowed reports whether one more reminder to session may be typed
// now, given the reminders saved in the last hour. When not, why is a reason
// code. A limit of zero means none. A saved time that is ahead of now (the
// clock was set back) still counts.
func ReminderAllowed(set RequestSettings, sent []RequestNotice, sessionID string, now time.Time) (ok bool, why string) {
	if set.RemindersPerSessionHour <= 0 || set.RemindersPerHostHour <= 0 {
		return false, whyRemindersOff
	}
	host, mine := 0, 0
	for _, n := range sent {
		age := now.Sub(n.At)
		if age >= time.Hour {
			continue
		}
		host++
		if n.SessionID != sessionID {
			continue
		}
		mine++
		if age < set.ReminderInterval {
			return false, whyReminderInterval
		}
	}
	switch {
	case mine >= set.RemindersPerSessionHour:
		return false, whySessionLimit
	case host >= set.RemindersPerHostHour:
		return false, whyHostLimit
	}
	return true, ""
}

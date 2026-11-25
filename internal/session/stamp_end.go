package session

import "time"

// StampEnd records the end time and exit note on a record whose terminal state
// the caller has just written itself, outside a running Manager's end paths.
// from is the state it left. The first end wins; the caller saves the record.
func StampEnd(sess *Session, from State, why EndReason) {
	stampEnd(sess, time.Now(), endNoteFor(from, sess.State, why))
}

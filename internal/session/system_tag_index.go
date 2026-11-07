package session

import "time"

// systemTagIndex builds, in one pass over the active and archived sessions,
// a map from Automaton ID (Session.PRDID) to the system tags of that
// Automaton's sessions. The active sessions are read from the live store; the
// archive is decoded once, with an extra function that collects tags and
// never keeps the session, so no archived record is retained in memory.
//
// Entries keep repeats and are in no particular order: the consumer
// (autonomous.Manager.InheritedTagsForPRD) de-duplicates and sorts. Sessions
// with an empty PRDID are skipped. There is no caching: each call reads the
// store again.
func (m *Manager) systemTagIndex() map[string][]string {
	idx := map[string][]string{}
	collect := func(sess *Session) {
		if sess == nil || sess.PRDID == "" {
			return
		}
		idx[sess.PRDID] = append(idx[sess.PRDID], sess.SystemTags...)
	}
	for _, sess := range m.store.List() {
		collect(sess)
	}
	m.ListArchivedSessions(SessionFilter{}, func(sess *Session) bool {
		collect(sess)
		return false // collect only; nothing is returned or retained
	}, "", time.Time{}, 0)
	return idx
}

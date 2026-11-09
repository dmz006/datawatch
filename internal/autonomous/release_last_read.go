// Last-release read — returns the most recent release record for a
// project (tag, pushed commit, release path, push outcome, CI outcome and
// CI time) for callers such as the comm channels.
//
// ReadLastRelease reads from the release record store installed with
// SetReleaseRecordStore. With no store installed, or no record for the
// project, it returns nil. This file only reads; it never records a push
// or changes a CI outcome.
package autonomous

import (
	"fmt"
	"sync"
)

var (
	releaseStoreMu sync.Mutex
	releaseStore   *ReleaseRecordStore
)

// SetReleaseRecordStore installs the store ReadLastRelease reads from.
// Passing nil clears it.
func SetReleaseRecordStore(s *ReleaseRecordStore) {
	releaseStoreMu.Lock()
	defer releaseStoreMu.Unlock()
	releaseStore = s
}

// ReadLastRelease returns a copy of the most recent release record for
// project, or nil when the project has no release. "Most recent" means
// the latest PushedAt.
func ReadLastRelease(project string) *ReleaseRecord {
	releaseStoreMu.Lock()
	s := releaseStore
	releaseStoreMu.Unlock()
	return lastReleaseIn(s, project)
}

// lastReleaseIn returns the newest record for project in s, or nil when s
// is nil or holds no record for project.
func lastReleaseIn(s *ReleaseRecordStore, project string) *ReleaseRecord {
	if s == nil {
		return nil
	}
	// List is sorted newest PushedAt first, so the first match wins.
	for _, rec := range s.List() {
		if rec.Project == project {
			return rec
		}
	}
	return nil
}

// LastReleaseSummary renders one line for comm channel use, for example:
// "datawatch v8.40.0 at abc123 (pushed), CI green at 2026-11-09 10:05 UTC".
// A nil record yields "no release recorded".
func LastReleaseSummary(rec *ReleaseRecord) string {
	if rec == nil {
		return "no release recorded"
	}
	ci := fmt.Sprintf("CI %s", rec.CIOutcome)
	if rec.CIWatchedAt != nil {
		ci += " at " + rec.CIWatchedAt.UTC().Format("2006-01-02 15:04") + " UTC"
	} else {
		ci += " (not yet observed)"
	}
	pushed := rec.PushOutcome
	if pushed == "" {
		pushed = "unknown push outcome"
	}
	return fmt.Sprintf("%s %s at %s (%s), %s", rec.Project, rec.Tag, rec.PushedCommit, pushed, ci)
}

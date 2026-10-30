// Package autonomous — the repo CI watch alert text.
//
// The alert text is the operator-facing line (and body) the action
// phase raises through the Coordinator's alert sink when a record has
// been unresolved (failing, hung or pending) since first_seen_at past
// alert_minutes, once per episode. The wording always names the
// Automaton watch — the internal record type must not leak into the
// text the operator reads.
package autonomous

import "fmt"

// repoCIWatchAlertTitle is the alert line for one unresolved record.
func repoCIWatchAlertTitle(rec *RepoCIWatchRecord) string {
	return fmt.Sprintf("CI watch: %s — %s %s@%s/%s run %s unresolved", rec.Repo, rec.RefKind, rec.RefName, rec.Workflow, rec.Status, rec.RunID)
}

// repoCIWatchAlertBody expands the title with the run's identity, the
// classification the watch has reached, the run URL and the first-seen
// stamp so the operator can take one action per line.
func repoCIWatchAlertBody(rec *RepoCIWatchRecord) string {
	return fmt.Sprintf(
		"Automaton is watching the CI of %s; run %s (workflow %s, %s@%s) has been %s with classification %s since %s.\nRun: %s\nThe watch cancels hung runs, reruns infra failures within the retry budget and deletes only runs a later green run superseded; it will not touch this run further until it is resolved, superseded or budget changes.",
		rec.Repo, rec.RunID, rec.Workflow, rec.RefKind, rec.RefName, rec.Status, rec.Classification, rec.FirstSeenAt.UTC().Format("2006-01-02 15:04:05 UTC"), rec.RunURL,
	)
}

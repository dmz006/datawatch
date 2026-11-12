// The decide step for a security-failure run: it turns the assessment of
// the failed log, the accepted-risks registry result, the advisory
// lookup result, the mode in effect, and whether an Automaton line
// carries the run's SHA into one report and one action. This file is the
// single home of SecurityFailureReport — every later step and every
// read surface uses that one type — and of the pure classification of
// recommendation and action.
//
// decideSecurityFailure is pure: no network, no filesystem, no time
// beyond stamping assessed_at. The caller that does consult the registry
// and the advisory lookup is DecideSecurityFailure in this same file,
// and it is the caller that enforces the one gate this function does not
// see: off mode never reaches decideSecurityFailure — the caller returns
// report_only before any lookup.
//
// There is no age or waiting clock anywhere on this path: an advisory
// published one hour ago gets exactly the same action as one published a
// year ago. published_at is carried verbatim from the lookup result and
// is empty when unknown; it is never invented.

package autonomous

import (
	"context"
	"time"
)

// The recommendation vocabulary. One of these, and only these,
// appears in a SecurityFailureReport:
//
//   - upgrade: a fixed version is known, whatever the registry says.
//   - unknown: no fixed version and the lookup or the registry failed.
//   - keep: no fixed version, the registry is readable and holds an
//     entry for the advisory.
//   - keep_no_entry: no fixed version and no entry: the operator must
//     add one through the registry, and the coordinator writes nothing.
const (
	recommendUpgrade     = "upgrade"
	recommendUnknown     = "unknown"
	recommendKeep        = "keep"
	recommendKeepNoEntry = "keep_no_entry"
)

// The action vocabulary. One of these, and only these, appears in a
// SecurityFailureReport and as the action DecideSecurityFailure returns:
//
//   - report_only: a not_dependency or unknown assessment; or a
//     dependency or cve assessment with no Automaton line carrying the
//     run's commit (the note says so).
//   - would_stop: shadow mode with a linked Automaton. Shadow can never
//     stop anything; it would only.
//   - stopped: on mode with a linked Automaton.
const (
	actionReportOnly = "report_only"
	actionWouldStop  = "would_stop"
	actionStopped    = "stopped"
)

// SecurityFailureReport is the one report type for the security-failure
// path. Every later step and every read surface (REST, PWA, logs, the
// coordinator) uses this type and nothing else.
//
// published_at is carried exactly as the lookup reported it and is
// empty when the lookup knew or said nothing; it is never invented.
// The registry_* fields are empty (registry_found false) when the
// registry could not be consulted or held no entry for the advisory;
// registry_error and lookup_error carry the reason in that case.
// note is empty except where the decision needs to say why it stopped
// at report_only (no Automaton line carries the commit) or why it
// consulted nothing (off mode) — it never restates a recommendation
// the fields already say.
type SecurityFailureReport struct {
	Dependency         string   `json:"dependency"`
	AdvisoryIDs        []string `json:"advisory_ids"`
	Workflow           string   `json:"workflow"`
	RunID              int64    `json:"run_id"`
	RunURL             string   `json:"run_url"`
	SHA                string   `json:"sha"`
	PublishedAt        string   `json:"published_at"`
	FixedVersion       string   `json:"fixed_version"`
	RegistryFound      bool     `json:"registry_found"`
	RegistryID         string   `json:"registry_id"`
	RegistryFirstAdded string   `json:"registry_first_added"`
	RegistryAdded      string   `json:"registry_added"`
	RegistryExpires    string   `json:"registry_expires"`
	RegistryError      string   `json:"registry_error"`
	LookupError        string   `json:"lookup_error"`
	Recommendation     string   `json:"recommendation"`
	Note               string   `json:"note"`
	Mode               string   `json:"mode"`
	Action             string   `json:"action"`
	AssessedAt         string   `json:"assessed_at"`
}

// SecurityFailureRunIdentity carries the run's identity for the report:
// where it is this function's job to carry the run, not to refetch it.
type SecurityFailureRunIdentity struct {
	Workflow string
	RunID    int64
	RunURL   string
	SHA      string
}

// SecurityFailureRegistryResult is one registry consultation's answer,
// exactly the shape LookupSecurityFailureRisk returns, flattened: found
// is false with a non-empty error when the registry could not be
// consulted (missing, unreadable, malformed); false with an empty error
// when it was consulted and holds no entry for the advisory.
type SecurityFailureRegistryResult struct {
	Found bool
	Entry SecurityFailureRiskEntry
	Error string
}

// DecideSecurityFailure is the caller path to the decide step, and the
// one caller that owns the off-mode gate.
//
// In off mode (SecurityFailureModeOff) it returns the report_only
// decision before consulting either the registry or the advisory
// lookup — off means nothing is consulted, nothing is written, and
// decideSecurityFailure is never reached. A caller that would reach
// decideSecurityFailure with off mode is a caller in error: off is the
// caller's job to refuse, and this function is deliberately written so
// that the off path returns before any lookup happens.
//
// In shadow or on mode it consults the accepted-risks registry and, when
// the assessment names at least one advisory identifier, the advisory
// lookup for the first one, then hands all of it to
// decideSecurityFailure.
func DecideSecurityFailure(ctx context.Context, repoPath string, assessment SecurityFailureAssessment, run SecurityFailureRunIdentity, mode string, lineCarriesRunSHA bool) (SecurityFailureReport, string) {
	if mode == SecurityFailureModeOff {
		report := baseSecurityFailureReport(run, assessment)
		report.Mode = SecurityFailureModeOff
		report.Recommendation = recommendUnknown
		report.Note = "security failure handling is off: neither the registry nor the advisory lookup was consulted"
		report.Action = actionReportOnly // the report carries its own action too
		return report, actionReportOnly
	}

	firstID := ""
	if len(assessment.AdvisoryIDs) > 0 {
		firstID = assessment.AdvisoryIDs[0]
	}

	found, entry, err := LookupSecurityFailureRisk(repoPath, firstID)
	registry := SecurityFailureRegistryResult{Found: found, Entry: entry}
	if err != nil {
		registry.Error = err.Error()
	}

	var lookup SecurityFailureAdvisoryResult
	if firstID != "" {
		lookup = LookupSecurityAdvisory(ctx, firstID)
	}

	return decideSecurityFailure(assessment, run, registry, lookup, mode, lineCarriesRunSHA)
}

// decideSecurityFailure is the pure decide step. It takes the
// assessment, the registry result, the lookup result, the mode, and
// whether an Automaton line carries the run's SHA, and returns the
// report and the action.
//
// SecurityFailureModeOff is never passed to this function: off mode
// returns before any lookup happens, and the one path that does that is
// the caller DecideSecurityFailure — that caller path is what the tests
// exercise for off mode, not this function. This function sees only
// shadow and on.
//
// Recommendation: a known fixed version (from the lookup, or stated in
// the log) gives upgrade whatever the registry says; no fixed version
// with the lookup or the registry failed gives unknown; no fixed
// version with a readable registry that holds an entry for the advisory
// gives keep; no fixed version and no entry gives keep_no_entry — the
// operator adds the entry through the registry, and the coordinator
// writes nothing.
//
// Action: a not_dependency or unknown assessment gives report_only; a
// dependency or cve assessment with no Automaton line carrying the
// commit gives report_only with a note saying so; shadow mode with a
// linked Automaton gives would_stop and never stopped; on mode with a
// linked Automaton gives stopped.
//
// There is no age and no waiting clock: published_at is carried
// verbatim (empty when unknown, never invented), and an advisory
// published one hour ago is acted on exactly like one published a year
// ago.
func decideSecurityFailure(assessment SecurityFailureAssessment, run SecurityFailureRunIdentity, registry SecurityFailureRegistryResult, lookup SecurityFailureAdvisoryResult, mode string, lineCarriesRunSHA bool) (SecurityFailureReport, string) {
	fixedVersion := lookup.FixedVersion
	if fixedVersion == "" {
		fixedVersion = assessment.FixedVersion
	}
	recommendation := securityFailureRecommendation(fixedVersion, registry, lookup)

	action := actionReportOnly
	note := ""
	switch assessment.Kind {
	case SecurityFailureDependency, SecurityFailureCVE:
		switch {
		case !lineCarriesRunSHA:
			// report_only: the note says why no stop was even possible.
			note = "no Automaton line carries this run's commit"
		case mode == SecurityFailureModeOn:
			action = actionStopped
		case mode == SecurityFailureModeShadow:
			// Shadow can only would_stop; it never stops.
			action = actionWouldStop
		default:
			// Defensively: a mode that is not shadow or on (e.g. off,
			// which the caller refuses before reaching this function)
			// cannot stop anything, so it stays report_only.
			note = "mode " + mode + " does not authorize a stop"
		}
	}

	report := baseSecurityFailureReport(run, assessment)
	report.PublishedAt = lookup.PublishedDate // verbatim; empty when unknown, never invented
	report.FixedVersion = fixedVersion
	report.RegistryFound = registry.Found
	if registry.Found {
		report.RegistryID = registry.Entry.ID
		report.RegistryFirstAdded = registry.Entry.FirstAdded
		report.RegistryAdded = registry.Entry.Added
		report.RegistryExpires = registry.Entry.Expires
	}
	report.RegistryError = registry.Error
	report.LookupError = lookup.ErrText
	report.Recommendation = recommendation
	report.Note = note
	report.Mode = mode
	report.Action = action
	return report, action
}

// securityFailureRecommendation applies the four-valued rule: a known
// fixed version wins whatever the registry says; otherwise a failed
// lookup or registry is unknown; otherwise a held entry is keep;
// otherwise there is no entry to keep with.
func securityFailureRecommendation(fixedVersion string, registry SecurityFailureRegistryResult, lookup SecurityFailureAdvisoryResult) string {
	if fixedVersion != "" {
		return recommendUpgrade
	}
	if registry.Error != "" || lookup.ErrText != "" {
		return recommendUnknown
	}
	if registry.Found {
		return recommendKeep
	}
	return recommendKeepNoEntry
}

// baseSecurityFailureReport fills the identity fields every report
// carries from the run and the assessment. AdvisoryIDs is copied so a
// report never shares a caller's slice.
func baseSecurityFailureReport(run SecurityFailureRunIdentity, assessment SecurityFailureAssessment) SecurityFailureReport {
	ids := make([]string, len(assessment.AdvisoryIDs))
	copy(ids, assessment.AdvisoryIDs)
	return SecurityFailureReport{
		Dependency:  assessment.Dependency,
		AdvisoryIDs: ids,
		Workflow:    run.Workflow,
		RunID:       run.RunID,
		RunURL:      run.RunURL,
		SHA:         run.SHA,
		AssessedAt:  time.Now().UTC().Format(time.RFC3339),
	}
}

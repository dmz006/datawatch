// Protected-path matching and the per-Automaton list of authorised
// protected paths.
//
// MatchProtectedPaths is pure and deterministic: no git, no LLM, no
// clock. A pattern takes one of three forms, matched case-sensitively
// against a repository-relative path after both go through
// normalizeDriftPath:
//
//  1. Exact file path — "internal/autonomous/protected_paths.go"
//     matches only that path.
//  2. Directory — a trailing slash ("docs/") or a trailing /** ("docs/**")
//     matches every path below that directory, at any depth.
//  3. Glob — a pattern using * ? [ ] is matched with path.Match against the
//     whole path, so a * never crosses a slash ("src/*.go" matches
//     "src/a.go" but not "src/sub/a.go").
//
// A changed path that is empty or escapes the repository (a ".."
// segment) never matches, and a pattern with a ".." segment matches
// nothing.
//
// SetAuthorizedProtectedPaths stores the operator's per-Automaton list
// of protected paths that the Automaton may change without a decision.

package autonomous

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	// maxAuthorizedProtectedPaths caps the entries of one authorisation list.
	maxAuthorizedProtectedPaths = 100
	// maxAuthorizedProtectedPathBytes caps the length of one entry.
	maxAuthorizedProtectedPathBytes = 256

	// protectedPathsAuthorisedKind is the Decision kind the setter records.
	protectedPathsAuthorisedKind = "protected_paths_authorised_set"
	// protectedPathsAuthorisedAudit is the audit action the setter emits.
	protectedPathsAuthorisedAudit = "autonomous_protected_paths_authorised_set"
)

// hasEscapingSegment reports whether p has a ".." path segment.
func hasEscapingSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// compiledProtectedPattern is one pattern ready to match.
type compiledProtectedPattern struct {
	value string // normalised path, directory without its trailing slash
	dir   bool
}

// compileProtectedPatterns normalises patterns, dropping empty ones and ones
// that escape the repository.
func compileProtectedPatterns(patterns []string) []compiledProtectedPattern {
	out := make([]compiledProtectedPattern, 0, len(patterns))
	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		dir := strings.HasSuffix(p, "/")
		if strings.HasSuffix(p, "/**") {
			p = strings.TrimSuffix(p, "**")
			dir = true
		}
		p = normalizeDriftPath(p)
		if p == "" || hasEscapingSegment(p) {
			continue
		}
		out = append(out, compiledProtectedPattern{value: p, dir: dir})
	}
	return out
}

// MatchProtectedPaths returns the sorted, de-duplicated changed paths that
// match any of the patterns. See the file header for the three pattern forms.
func MatchProtectedPaths(changed, patterns []string) []string {
	pats := compileProtectedPatterns(patterns)
	if len(pats) == 0 {
		return nil
	}
	var hits []string
	for _, raw := range changed {
		c := normalizeDriftPath(raw)
		if c == "" || hasEscapingSegment(c) {
			continue
		}
		for _, p := range pats {
			if p.matches(c) {
				hits = append(hits, c)
				break
			}
		}
	}
	return sortedUnion(hits)
}

func (p compiledProtectedPattern) matches(changed string) bool {
	if p.dir {
		return strings.HasPrefix(changed, p.value+"/")
	}
	if changed == p.value {
		return true
	}
	ok, err := path.Match(p.value, changed)
	return err == nil && ok
}

// SplitAuthorized splits the protected-path hits into those that still need
// an operator decision and those the Automaton's authorised list covers. A hit
// is allowed when MatchProtectedPaths([hit], authorized) is non-empty. Both
// results are sorted and de-duplicated; each is nil when empty.
func SplitAuthorized(hits, authorized []string) (needsDecision, allowed []string) {
	for _, h := range hits {
		n := normalizeDriftPath(h)
		if n == "" {
			continue
		}
		if len(MatchProtectedPaths([]string{n}, authorized)) > 0 {
			allowed = append(allowed, n)
		} else {
			needsDecision = append(needsDecision, n)
		}
	}
	return sortedUnion(needsDecision), sortedUnion(allowed)
}

// validateAuthorizedProtectedPaths rejects a list the setter must not store.
// The error names the offending entry.
func validateAuthorizedProtectedPaths(paths []string) error {
	if len(paths) > maxAuthorizedProtectedPaths {
		return fmt.Errorf("too many authorised paths: %d (at most %d)", len(paths), maxAuthorizedProtectedPaths)
	}
	for _, raw := range paths {
		e := strings.TrimSpace(raw)
		switch {
		case len(e) > maxAuthorizedProtectedPathBytes:
			return fmt.Errorf("authorised path %q is longer than %d bytes", truncateForError(e), maxAuthorizedProtectedPathBytes)
		case strings.HasPrefix(e, "/"):
			return fmt.Errorf("authorised path %q is absolute; use a repository-relative path", e)
		case hasEscapingSegment(e):
			return fmt.Errorf("authorised path %q contains a .. segment", e)
		}
	}
	return nil
}

func truncateForError(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// SetAuthorizedProtectedPaths sets the Automaton's list of protected paths
// that its tasks may change without an operator decision. Entries are trimmed,
// lose a leading ./, become a directory form for a trailing / or /**, are
// path.Clean-ed, de-duplicated and kept in order. An absolute entry, an entry
// with a .. segment, an entry over 256 bytes and a list over 100 entries are
// rejected with an error naming the entry. An empty list clears the
// authorisation. The change is saved, recorded as a protected_paths_authorised_set
// Decision and audited (autonomous_protected_paths_authorised_set). It returns
// the stored list.
func (m *Manager) SetAuthorizedProtectedPaths(prdID string, paths []string, actor string) ([]string, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("automaton %q not found", prdID)
	}
	if err := validateAuthorizedProtectedPaths(paths); err != nil {
		return nil, err
	}
	list := config.NormalizeProtectedPaths(paths)

	var note string
	if len(list) == 0 {
		note = "authorised protected paths cleared"
	} else {
		note = "authorised protected paths: " + strings.Join(list, ", ")
	}

	m.store.WithLock(func() {
		if len(list) == 0 {
			prd.AuthorizedProtectedPaths = nil
		} else {
			prd.AuthorizedProtectedPaths = list
		}
		now := time.Now()
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{
			At: now, Kind: protectedPathsAuthorisedKind, Actor: actor, Note: note,
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}

	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn != nil {
		auditFn(protectedPathsAuthorisedAudit, map[string]any{
			"prd_id": prdID,
			"actor":  actor,
			"paths":  append([]string(nil), list...),
		})
	}
	return append([]string(nil), list...), nil
}

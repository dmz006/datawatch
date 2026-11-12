package autonomous

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path"
	"regexp"
	"strings"
)

// This file is the confirmed-proposal rule: an implementation Automaton may be
// started by lane fill only when its proposal file carries a "Confirmed: yes"
// line. The planner stays pure. The coordinator reads the proposal files
// through LaneFillProposalReader while it builds the snapshot, outside any
// lock, and stores the outcome on the candidate; EvaluateLaneFillEligibility
// only maps that outcome to a refusal.

// The states a candidate's proposal check can end in.
const (
	// LaneFillProposalConfirmed: a named, readable file carries the line.
	LaneFillProposalConfirmed = "confirmed"
	// LaneFillProposalNotConfirmed: a named file is readable and none carries
	// the line.
	LaneFillProposalNotConfirmed = "not_confirmed"
	// LaneFillProposalMissing: the spec names no proposal file, or none of the
	// named files can be read.
	LaneFillProposalMissing = "missing"
	// LaneFillProposalExempt: not an implementation Automaton, so the rule
	// does not apply.
	LaneFillProposalExempt = "exempt"
	// LaneFillProposalUnknown: the check could not be made (no reader, or a
	// read failed for a reason other than the file not existing). It never
	// refuses.
	LaneFillProposalUnknown = "unknown"
)

// laneFillProposalMaxLines is how many lines from the top of a proposal file
// are searched for the confirmation line.
const laneFillProposalMaxLines = 200

// LaneFillProposalReader reads the head of a proposal file. rel is a
// repository-relative path. An error wrapping fs.ErrNotExist means the file
// is not there (or the reference is not allowed to be read); any other error
// means the read failed and the check is unknown.
type LaneFillProposalReader interface {
	ReadHead(rel string) ([]byte, error)
}

var (
	// proposalPathPattern finds the proposal files named in a spec: a token
	// ending in docs/plans/<name>.md. A directory prefix is matched too, so a
	// reference such as an absolute path or one with a parent-directory
	// element is seen and refused rather than half-matched.
	proposalPathPattern = regexp.MustCompile("(?:[^\\s\"'`()<>\\[\\]{},;:|]*/)?docs/plans/[A-Za-z0-9][A-Za-z0-9._-]*\\.md")
	// proposalConfirmedPattern matches a line that starts with "Confirmed: yes",
	// ignoring case and leading or wrapping markdown decoration (bold, italic,
	// quotes, a list marker).
	proposalConfirmedPattern = regexp.MustCompile("(?i)^[\\s>*_~\"'`-]*confirmed\\s*:[\\s*_~\"'`]*yes(?:[^\\pL\\pN]|$)")
)

// LaneFillProposalFiles returns the distinct proposal file references found in
// the spec text, in order of appearance. The spec is searched as data with a
// regular expression only.
func LaneFillProposalFiles(spec string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range proposalPathPattern.FindAllString(spec, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// laneFillProposalSafePath returns the cleaned repository-relative form of a
// reference, or false when the reference must not be read: absolute, holding
// a parent-directory element, or not under docs/plans after cleaning.
func laneFillProposalSafePath(ref string) (string, bool) {
	if ref == "" || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "\\") || strings.Contains(ref, "\\") {
		return "", false
	}
	for _, el := range strings.Split(ref, "/") {
		if el == ".." {
			return "", false
		}
	}
	clean := path.Clean(ref)
	if !strings.HasPrefix(clean, "docs/plans/") {
		return "", false
	}
	return clean, true
}

// LaneFillProposalConfirmedIn reports whether the head of a proposal file has
// a "Confirmed: yes" line among its first 200 lines.
func LaneFillProposalConfirmedIn(head []byte) bool {
	lines := strings.SplitN(string(head), "\n", laneFillProposalMaxLines+1)
	if len(lines) > laneFillProposalMaxLines {
		lines = lines[:laneFillProposalMaxLines]
	}
	for _, ln := range lines {
		if proposalConfirmedPattern.MatchString(strings.TrimRight(ln, "\r")) {
			return true
		}
	}
	return false
}

// laneFillImplementationType says the Automaton type is an implementation
// type: empty or software. Research, operational and personal Automata, and
// any other type, are exempt from the rule.
func laneFillImplementationType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "", "software":
		return true
	}
	return false
}

// CheckLaneFillProposal runs the proposal check for one Automaton and returns
// its state and the path the verdict is about (the first readable file for
// not_confirmed, the first named file for missing). warn, when not nil, is
// called for a read that failed for a reason other than the file not existing.
// A nil reader gives unknown: the rule does not apply until one is set.
func CheckLaneFillProposal(typ string, isTemplate bool, spec string, rd LaneFillProposalReader, warn func(ref string, err error)) (state, ref string) {
	if isTemplate || !laneFillImplementationType(typ) {
		return LaneFillProposalExempt, ""
	}
	if rd == nil {
		return LaneFillProposalUnknown, ""
	}
	files := LaneFillProposalFiles(spec)
	if len(files) == 0 {
		return LaneFillProposalMissing, ""
	}
	var readable, unknown bool
	var firstReadable string
	for _, f := range files {
		clean, ok := laneFillProposalSafePath(f)
		if !ok {
			continue // counts as not readable; nothing is read
		}
		head, err := rd.ReadHead(clean)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				unknown = true
				if warn != nil {
					warn(clean, err)
				}
			}
			continue
		}
		if LaneFillProposalConfirmedIn(head) {
			return LaneFillProposalConfirmed, clean
		}
		if !readable {
			firstReadable = clean
		}
		readable = true
	}
	switch {
	case unknown:
		return LaneFillProposalUnknown, ""
	case readable:
		return LaneFillProposalNotConfirmed, firstReadable
	}
	return LaneFillProposalMissing, files[0]
}

// laneFillProposalRefusal maps a candidate's proposal state to the refusal
// code and text, or two empty strings when it does not refuse.
func laneFillProposalRefusal(c LaneFillCandidate) (code, text string) {
	switch c.ProposalState {
	case LaneFillProposalNotConfirmed:
		return LaneFillReasonProposalNotConfirmed,
			fmt.Sprintf("Automaton %s: proposal %s does not carry a Confirmed: yes line", c.ID, c.ProposalPath)
	case LaneFillProposalMissing:
		return LaneFillReasonProposalMissing,
			fmt.Sprintf("Automaton %s: the spec names no readable proposal file", c.ID)
	}
	return "", ""
}

// SetLaneFillProposalReader sets how the proposal files of an Automaton are
// read. Until one is set the confirmed-proposal rule gives unknown and never
// refuses.
func (c *Coordinator) SetLaneFillProposalReader(rd LaneFillProposalReader) {
	if c == nil {
		return
	}
	c.lf.mu.Lock()
	c.lf.proposal = rd
	c.lf.mu.Unlock()
}

// laneFillProposalWarnOnce logs a failed proposal read the first time it is
// seen for a path; the rule fails open, so the log is the only trace.
func (c *Coordinator) laneFillProposalWarnOnce(ref string, err error) {
	c.lf.mu.Lock()
	if c.lf.proposalWarned == nil {
		c.lf.proposalWarned = map[string]bool{}
	}
	seen := c.lf.proposalWarned[ref]
	c.lf.proposalWarned[ref] = true
	c.lf.mu.Unlock()
	if !seen {
		log.Printf("[autonomous] lane fill: proposal %s could not be read, not refusing: %v", ref, err)
	}
}

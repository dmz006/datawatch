// Planning record for the rules/context files. The existing
// rulesContextSuffix (rules_context.go) points the planner at the
// project's rules and context files, but nothing recorded which files
// the planner was actually pointed at — so a plan could not be traced
// back to the rules it was made under: did AGENT.md exist? Which
// CONTEXT file did the project policy resolve to? This file closes
// that: recordRulesContext captures, at decompose time, one reference
// per configured file (name, kind, presence, content sha256, size) and
// attaches it to the PRD so the store and the PRD GET endpoints
// surface it. Missing files are stated plainly in the record's Notes,
// listed in Missing, and each also raises one operator-visible event on
// the PRD's decision timeline.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// rulesContextHashMaxBytes caps how much of a configured file's
// content the record will read to compute a hash: a file larger than
// this is recorded by name and size only, with the reason in the
// record's Notes. 4 MiB is far above any real rules/context doc and
// keeps decompose's startup cost bounded on a misconfigured giant file.
const rulesContextHashMaxBytes = 4 * 1024 * 1024

// RulesContextFileRef is the record's entry for one configured
// rules/context file: its name, which slot it came from ("rules" or
// "context"), whether it was present at capture time, the hex sha256 of
// its content (empty when the file is missing, when it is over the hash
// cap, or when hashing failed) and its size in bytes (0 when missing).
// Counts and names only — the record never copies file content.
type RulesContextFileRef struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Present bool   `json:"present"`
	SHA256  string `json:"sha256,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
}

// RulesContextRecord is the per-PRD record of which rules/context files
// the planner was pointed at at decompose time (stored on
// PRD.RulesContext, see recordRulesContext). DigestMode pins the digest
// sub-setting in effect when the record was captured; DigestInjected /
// DigestBytes carry the result of the digest-injection step that runs
// later in the same decompose (see MarkDigest and
// SetRulesContextDigest) and are false/0 while the prompt got only the
// name pointer.
type RulesContextRecord struct {
	CapturedAt     time.Time             `json:"captured_at"`
	Files          []RulesContextFileRef `json:"files"`
	Missing        []string              `json:"missing,omitempty"`
	Notes          []string              `json:"notes,omitempty"`
	DigestMode     string                `json:"digest_mode,omitempty"`
	DigestInjected bool                  `json:"digest_injected"`
	DigestBytes    int                   `json:"digest_bytes,omitempty"`
}

// MarkDigest sets the digest-injection half of the record after the
// digest step has run: DigestInjected says whether a digest ended up in
// the prompt, DigestBytes its size. Convenience for callers holding the
// record directly.
func (r *RulesContextRecord) MarkDigest(injected bool, digestBytes int) {
	r.DigestInjected = injected
	r.DigestBytes = digestBytes
}

// RulesContextRecordFor returns the record stored on the PRD (nil before
// anything has been recorded; nil-safe on a nil PRD).
func (m *Manager) RulesContextRecordFor(prd *PRD) *RulesContextRecord {
	if prd == nil {
		return nil
	}
	return prd.RulesContext
}

// SetRulesContextDigest records the digest-injection result (injected,
// size in bytes) on the PRD's rules-context record — capturing one first
// if Decompose has not recorded one yet — and persists it through the
// store's normal SavePRD path. Returns the stored record.
func (m *Manager) SetRulesContextDigest(prd *PRD, injected bool, digestBytes int) *RulesContextRecord {
	rec := m.RulesContextRecordFor(prd)
	if rec == nil {
		rec = m.recordRulesContext(prd)
	}
	rec.MarkDigest(injected, digestBytes)
	_ = m.store.SavePRD(prd)
	return rec
}

// recordRulesContext captures the resolved rules/context policy for the
// PRD's project directory — one reference per configured file with its
// presence, content sha256 and size, plus a human-readable note per
// missing file — attaches the record and one decision-timeline event
// per missing file to the PRD, and persists it through the store's
// normal SavePRD path (the same one the neighbouring planning records
// such as PlanCoverage ride on; the PRD is returned as JSON by the
// existing PRD GET endpoints, so the field is visible without extra
// plumbing).
//
// Behavior:
//   - A configured name that would escape the project directory (absolute
//     path or parent-directory walk) is never stat'ed, read or recorded;
//     the reason goes to Notes. (Config-side validation already rejects
//     such names, this is defense in depth.)
//   - A file larger than rulesContextHashMaxBytes is recorded by name
//     and size only — its content is not read — with a note saying why
//     the hash is empty.
//   - Every missing file lands in Missing and gets a note such as
//     "context file CONTEXT.md is configured but was not found in the
//     project directory". When the PRD has no project directory set
//     yet, that is itself the note.
//
// Counts and names only: file content is read exclusively to compute
// the hash and is never copied into the record.
func (m *Manager) recordRulesContext(prd *PRD) *RulesContextRecord {
	rec := &RulesContextRecord{CapturedAt: time.Now()}
	policy := m.resolvedRulesContext(prd)
	rec.DigestMode = policy.DigestMode
	var missingNotes []string
	if prd.ProjectDir == "" {
		rec.Notes = append(rec.Notes, "project directory is not set on this PRD yet, so no rules or context files were recorded")
	} else {
		rec.Files, rec.Missing, missingNotes = rulesContextRefsFor(prd.ProjectDir, policy, rec)
	}

	// Missing files are surfaced beyond the JSON field: each also raises
	// one operator-visible planning event on the PRD's decision timeline —
	// the same append-and-persist surface plan_review.go, file_drift.go
	// and the other operator-visible warnings use — so the absence shows
	// up in the PRD's event log even where the record itself is not read.
	for i, name := range rec.Missing {
		note := "rules/context file " + name + " is configured but was not found in the project directory"
		if i < len(missingNotes) {
			note = missingNotes[i]
		}
		prd.Decisions = append(prd.Decisions, Decision{
			At:    rec.CapturedAt,
			Kind:  "rules_context_missing",
			Actor: "autonomous",
			Note:  note,
		})
	}
	prd.RulesContext = rec
	if err := m.store.SavePRD(prd); err != nil {
		// The record is still attached to the in-memory PRD and returns
		// with it; the persist failure is logged rather than aborting
		// decompose over a planning record.
		log.Printf("[autonomous] prd=%s persist rules-context record: %v", prdIDOf(prd), err)
	}
	return rec
}

// rulesContextRefsFor builds the per-file references for the resolved
// policy: the rules file first, then the context files in configured
// order, deduplicated by name (a name configured for both slots is
// recorded once, under its first slot). Notes are appended to rec; the
// returned missingNotes align with the missing names one-for-one.
func rulesContextRefsFor(projectDir string, policy ResolvedRulesContext, rec *RulesContextRecord) (files []RulesContextFileRef, missing []string, missingNotes []string) {
	base := filepath.Clean(projectDir)
	seen := map[string]bool{}
	add := func(name, kind, kindLabel string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true

		// Reject names that would escape the project directory rather
		// than stat/hash them (absolute path or parent-directory walk).
		target := filepath.Join(base, name)
		if target == base || !strings.HasPrefix(target, base+string(os.PathSeparator)) {
			rec.Notes = append(rec.Notes, fmt.Sprintf("%s %q would resolve outside the project directory, so it was not recorded", kindLabel, name))
			return
		}

		st, err := os.Stat(target) // #nosec G304 -- see rules_context.go's fileExists for the trust model
		if err != nil || !st.Mode().IsRegular() {
			files = append(files, RulesContextFileRef{Name: name, Kind: kind, Present: false})
			missing = append(missing, name)
			note := fmt.Sprintf("%s %s is configured but was not found in the project directory", kindLabel, name)
			rec.Notes = append(rec.Notes, note)
			missingNotes = append(missingNotes, note)
			return
		}

		ref := RulesContextFileRef{Name: name, Kind: kind, Present: true, Bytes: int(st.Size())}
		switch {
		case st.Size() > rulesContextHashMaxBytes:
			// Over the cap: record the size only, never read the
			// content for a hash.
			rec.Notes = append(rec.Notes, fmt.Sprintf("%s %s is larger than the 4 MiB hash cap, so its content was not read; name and size are recorded without a hash", kindLabel, name))
		default:
			if sum, herr := hashFileWithSize(target, st.Size()); herr != nil {
				rec.Notes = append(rec.Notes, fmt.Sprintf("%s %s could not be hashed (%v); recorded by name and size only", kindLabel, name, herr))
			} else {
				ref.SHA256 = hex.EncodeToString(sum[:])
			}
		}
		files = append(files, ref)
	}

	add(policy.RulesFile, "rules", "rules file")
	for _, c := range policy.ContextFiles {
		add(c, "context", "context file")
	}
	return files, missing, missingNotes
}

// hashFileWithSize hashes a regular file's content with sha256. The
// caller has already stat'ed it and capped the size; the read is bounded
// to that size so a file that grows between stat and open cannot make
// it unbounded.
func hashFileWithSize(path string, size int64) (sum [sha256.Size]byte, err error) {
	f, err := os.Open(path) // #nosec G304 -- same trust model as fileExists in rules_context.go
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.CopyN(h, f, size); err != nil {
		return [sha256.Size]byte{}, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// prdIDOf is a nil-safe PRD.ID reader for log lines.
func prdIDOf(prd *PRD) string {
	if prd == nil {
		return "<nil>"
	}
	return prd.ID
}

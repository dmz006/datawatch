// Pattern-drift staleness guard for rate-limit detection. The effective
// rate-limit patterns are matched against backend output strings, so a
// config override or a backend upgrade that rewords its limit message
// silently stops detection. This file fingerprints the effective pattern
// set and records the fingerprint an operator last rechecked; the two
// differing is surfaced as patterns_stale. Nothing here re-derives patterns
// or adds detection heuristics.

package session

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// patternsVersionLen is the number of hex characters of the sha256 digest
// shown as patterns_version.
const patternsVersionLen = 12

// patternsRecheckedFile is the data-dir entry holding the fingerprint an
// operator last rechecked.
const patternsRecheckedFile = "rate_limit_patterns_rechecked"

// patternsRecheckMu serializes reads and writes of the rechecked entry.
var patternsRecheckMu sync.Mutex

// lengthPrefix writes a 4-byte big-endian length so concatenated entries
// cannot ambiguously re-parse ("ab","c" vs "a","bc").
func lengthPrefix(h io.Writer, s string) {
	h.Write([]byte{byte(len(s) >> 24), byte(len(s) >> 16), byte(len(s) >> 8), byte(len(s))})
}

// writePatterns digests a pattern set into h, case-sensitive as written,
// de-duplicated and sorted, so ordering and repeats never change the result
// while any added, removed or edited pattern does.
func writePatterns(h io.Writer, patterns []string) {
	uniq := make([]string, 0, len(patterns))
	seen := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		uniq = append(uniq, p)
	}
	sort.Strings(uniq)
	for _, p := range uniq {
		lengthPrefix(h, p)
		h.Write([]byte(p))
	}
}

// fingerprintPatterns returns a short stable digest of a pattern set.
func fingerprintPatterns(patterns []string) string {
	h := sha256.New()
	writePatterns(h, patterns)
	return hex.EncodeToString(h.Sum(nil))[:patternsVersionLen]
}

// fingerprintPatternsWithTool digests the pattern set together with the
// claude tool text, length-prefixing the text the same way entries are
// prefixed. When text is the unknown sentinel (or empty) the combined
// digest equals the pattern-only digest, so previously recorded recheck
// entries remain comparable.
func fingerprintPatternsWithTool(patterns []string, toolText string) string {
	h := sha256.New()
	writePatterns(h, patterns)
	if toolText != claudeCLIVersionUnknown && toolText != "" {
		lengthPrefix(h, toolText)
		h.Write([]byte(toolText))
	}
	return hex.EncodeToString(h.Sum(nil))[:patternsVersionLen]
}

// RateLimitPatternsVersion returns the combined fingerprint of the effective
// rate-limit detection patterns (config override, else the built-in set)
// and the cached claude CLI version text. With no claude tool the text is
// "unknown" and this is exactly the old pattern-only digest.
func (m *Manager) RateLimitPatternsVersion() string {
	return fingerprintPatternsWithTool(m.effectiveRateLimitPatterns(), m.ClaudeCLIVersion())
}

func (m *Manager) patternsRecheckedPath() string {
	return filepath.Join(m.dataDir, patternsRecheckedFile)
}

// recheckedEntry is the parsed data-dir recheck record: the pattern-only
// digest an operator last approved (line one) and the claude tool text at
// that time (line two; empty for old single-line entries, which counts as
// unknown). exists is false when no entry was readable.
type recheckedEntry struct {
	exists   bool
	version  string
	toolText string
}

// readRecheckedEntry parses the last rechecked entry; zero/false when none
// has been recorded (or the entry is unreadable). An old one-line entry
// yields an empty toolText, i.e. unknown.
func (m *Manager) readRecheckedEntry() recheckedEntry {
	b, err := os.ReadFile(m.patternsRecheckedPath())
	if err != nil {
		return recheckedEntry{}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 0 {
		return recheckedEntry{}
	}
	return recheckedEntry{
		exists:   true,
		version:  strings.TrimSpace(lines[0]),
		toolText: strings.TrimSpace(strings.Join(lines[1:], "\n")),
	}
}

// knownToolText reports whether a claude tool text carries actual version
// information; empty and the unknown sentinel are "absent" and never raise
// the stale flag on their own.
func knownToolText(s string) bool {
	return s != "" && s != claudeCLIVersionUnknown
}

// RateLimitGuardStatus reports the combined version (patterns plus claude
// tool text), the current claude tool text, and whether the last rechecked
// entry is out of date. Stale is true when no entry exists, when the stored
// pattern-only digest differs from the current one, or when both stored and
// current claude texts are known and differ. Either side's text being
// unknown (or empty) never raises stale by itself.
func (m *Manager) RateLimitGuardStatus() (version, claudeCLIVersion string, stale bool) {
	tool := m.ClaudeCLIVersion()
	version = fingerprintPatternsWithTool(m.effectiveRateLimitPatterns(), tool)
	patternsOnly := fingerprintPatterns(m.effectiveRateLimitPatterns())
	patternsRecheckMu.Lock()
	entry := m.readRecheckedEntry()
	patternsRecheckMu.Unlock()
	stale = !entry.exists ||
		entry.version != patternsOnly ||
		(knownToolText(entry.toolText) && knownToolText(tool) && entry.toolText != tool)
	return version, tool, stale
}

// RateLimitPatternsStatus reports the current fingerprint and whether it
// differs from the one an operator last rechecked. A daemon that has never
// been rechecked is stale. Kept as a thin wrapper over RateLimitGuardStatus
// for existing (version, stale) callers.
func (m *Manager) RateLimitPatternsStatus() (version string, stale bool) {
	version, _, stale = m.RateLimitGuardStatus()
	return version, stale
}

// RecheckRateLimitPatterns records the current pattern-only digest and the
// freshly read claude tool text as rechecked, so patterns_stale reads false
// until either side changes again. It forces a fresh claude version read
// first and returns the combined version string.
func (m *Manager) RecheckRateLimitPatterns() (string, error) {
	tool := m.RefreshClaudeCLIVersion()
	version := fingerprintPatternsWithTool(m.effectiveRateLimitPatterns(), tool)
	patternsOnly := fingerprintPatterns(m.effectiveRateLimitPatterns())
	patternsRecheckMu.Lock()
	defer patternsRecheckMu.Unlock()
	path := m.patternsRecheckedPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(patternsOnly+"\n"+tool+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return version, nil
}

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

// fingerprintPatterns returns a short stable digest of a pattern set. The
// set is compared case-sensitively as written, de-duplicated and sorted, so
// ordering and repeats never change the result while any added, removed or
// edited pattern does.
func fingerprintPatterns(patterns []string) string {
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
	h := sha256.New()
	for _, p := range uniq {
		// Length-prefix each entry so ["ab","c"] and ["a","bc"] differ.
		h.Write([]byte{byte(len(p) >> 24), byte(len(p) >> 16), byte(len(p) >> 8), byte(len(p))})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:patternsVersionLen]
}

// RateLimitPatternsVersion returns the fingerprint of the effective
// rate-limit detection patterns (config override, else the built-in set).
func (m *Manager) RateLimitPatternsVersion() string {
	return fingerprintPatterns(m.effectiveRateLimitPatterns())
}

func (m *Manager) patternsRecheckedPath() string {
	return filepath.Join(m.dataDir, patternsRecheckedFile)
}

// recheckedPatternsVersion reads the last rechecked fingerprint; empty when
// none has been recorded (or the entry is unreadable).
func (m *Manager) recheckedPatternsVersion() string {
	b, err := os.ReadFile(m.patternsRecheckedPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// RateLimitPatternsStatus reports the current fingerprint and whether it
// differs from the one an operator last rechecked. A daemon that has never
// been rechecked is stale.
func (m *Manager) RateLimitPatternsStatus() (version string, stale bool) {
	version = m.RateLimitPatternsVersion()
	patternsRecheckMu.Lock()
	defer patternsRecheckMu.Unlock()
	return version, m.recheckedPatternsVersion() != version
}

// RecheckRateLimitPatterns records the current fingerprint as rechecked, so
// patterns_stale reads false until the pattern set changes again. It returns
// the recorded version.
func (m *Manager) RecheckRateLimitPatterns() (string, error) {
	version := m.RateLimitPatternsVersion()
	patternsRecheckMu.Lock()
	defer patternsRecheckMu.Unlock()
	path := m.patternsRecheckedPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(version+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return version, nil
}

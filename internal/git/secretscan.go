// Secret scanning for about-to-be-pushed commits.
//
// ScanUnpushed lists the commits that are present on HEAD but on no ref of
// the named remote (the same range logic as UnpushedCommits:
// `git rev-list HEAD --not --remotes=<remote>`) and scans the lines those
// commits *add* for built-in secret patterns. The result is a list of
// SecretFinding values; a Finding deliberately holds only the detector
// name, the commit SHA, the file path, the line number and a short
// SHA-256 fingerprint of the matched material. It never holds, and String
// never prints, the matched secret itself, so findings are safe to log,
// store and show to operators.
//
// Detectors are selectable by name via ScanOptions.Detectors (empty means
// all built-ins); ScanOptions also carries the allow-list: path globs
// (matched with path.Match against the repository-relative path) and
// fingerprint hashes (matched against the 16-hex short fingerprint or the
// full 64-hex SHA-256 digest).

package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"path"
	"regexp"
	"strings"
)

// Built-in detector names. These are the names selectable by name in a
// configuration's detector list.
const (
	// DetectorPrivateKey matches PEM private key blocks.
	DetectorPrivateKey = "private_key"
	// DetectorAWSAccessKey matches AWS access key IDs.
	DetectorAWSAccessKey = "aws_access_key"
	// DetectorGitHubToken matches GitHub personal access / app tokens.
	DetectorGitHubToken = "github_token"
	// DetectorGenericToken matches high-entropy assignments to
	// key-ish names (api_key, token, secret, ...).
	DetectorGenericToken = "generic_token"
	// DetectorSlackToken matches Slack tokens.
	DetectorSlackToken = "slack_token"
)

// AllDetectors lists the built-in detector names; it is the effective
// detector list when a ScanOptions (or a configuration) names none.
var AllDetectors = []string{
	DetectorPrivateKey,
	DetectorAWSAccessKey,
	DetectorGitHubToken,
	DetectorGenericToken,
	DetectorSlackToken,
}

// secretFingerprintLen is the number of hex chars in the short
// fingerprint a Finding carries (8 bytes of SHA-256).
const secretFingerprintLen = 16

// secretDetector is one built-in detector. minEntropy > 0 means the
// matched material must also carry at least that much Shannon entropy
// (bits per character), which is what keeps generic assignments from
// matching obvious placeholders like "aaaaaaaaaaaaaaaa".
type secretDetector struct {
	name       string
	re         *regexp.Regexp
	minEntropy float64
}

var builtInDetectors = []secretDetector{
	{
		name: DetectorPrivateKey,
		re:   regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`),
	},
	{
		name: DetectorAWSAccessKey,
		re:   regexp.MustCompile(`\b(?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`),
	},
	{
		name: DetectorGitHubToken,
		re:   regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,255}\b`),
	},
	{
		name: DetectorGenericToken,
		re:   regexp.MustCompile(`(?i)\b(?:api[_-]?key|apikey|access[_-]?token|auth[_-]?token|client[_-]?secret|secret|token|password|passwd)\b\s*[:=]\s*["']?[A-Za-z0-9+/=_-]{16,}`),
		// High entropy required: the material after the assignment must be
		// hard to guess, not a fixed placeholder.
		minEntropy: 3.5,
	},
	{
		name: DetectorSlackToken,
		re:   regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,250}\b`),
	},
}

// detectorByIndex returns the built-in detectors, or the selected
// subset, in a stable order. Unknown names are an error rather than a
// silent no-op, so a typo in a detector list cannot quietly disable
// scanning.
func resolveDetectors(names []string) ([]secretDetector, error) {
	if len(names) == 0 {
		return builtInDetectors, nil
	}
	seen := make(map[string]bool, len(names))
	var out []secretDetector
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		var d *secretDetector
		for i := range builtInDetectors {
			if builtInDetectors[i].name == n {
				d = &builtInDetectors[i]
				break
			}
		}
		if d == nil {
			return nil, fmt.Errorf("unknown secret detector %q (known: %s)", n, strings.Join(AllDetectors, ", "))
		}
		seen[n] = true
		out = append(out, *d)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("secret detector list is empty (use at least one of: %s)", strings.Join(AllDetectors, ", "))
	}
	return out, nil
}

// Finding is a secret-scan result. Fingerprint is a short SHA-256
// digest (16 hex chars) of the matched material; it is stable enough to
// allow-list and dedupe by, and reveals nothing usable about the secret
// itself. No field of Finding holds the matched secret, and String
// never prints it.
type SecretFinding struct {
	Detector    string
	Commit      string
	Path        string
	Line        int
	Fingerprint string
}

// String renders a Finding for logs and operator-facing output. It
// contains only the Finding's non-secret fields — the matched secret is
// never part of it.
func (f SecretFinding) String() string {
	return fmt.Sprintf("secret scan: detector=%s commit=%s path=%s line=%d fingerprint=%s",
		f.Detector, f.Commit, f.Path, f.Line, f.Fingerprint)
}

// ScanOptions configures a ScanUnpushed run. Zero value = all built-in
// detectors, no allow-list.
type ScanOptions struct {
	// Detectors is the list of detector names to run (see AllDetectors).
	// Empty means all built-ins; an unknown name is an error.
	Detectors []string
	// PathPatterns is the allow-list of path globs (path.Match against
	// the repository-relative path); matches are skipped entirely.
	PathPatterns []string
	// Fingerprints is the allow-list of fingerprint hashes: the short
	// 16-hex fingerprint or a full 64-hex SHA-256 digest. Matching
	// findings are dropped.
	Fingerprints []string
}

// pathAllowed reports whether a repository path matches any allow-list
// glob.
func (o ScanOptions) pathAllowed(p string) bool {
	for _, pat := range o.PathPatterns {
		if pat == "" {
			continue
		}
		if ok, _ := path.Match(pat, p); ok {
			return true
		}
		// A bare directory name in the allow-list ("vendor") also
		// matches any path inside it, which is the usual intent.
		for _, seg := range strings.Split(p, "/") {
			if seg == pat {
				return true
			}
		}
	}
	return false
}

// fpAllowed reports whether a short fingerprint was allow-listed.
func (o ScanOptions) fpAllowed(short, full string) bool {
	for _, fp := range o.Fingerprints {
		fp = strings.ToLower(strings.TrimSpace(fp))
		if fp == "" {
			continue
		}
		if fp == short || fp == full {
			return true
		}
	}
	return false
}

// secretFingerprint digests matched material into a Finding-safe
// fingerprint: the first 8 bytes of its SHA-256 as hex (16 chars).
func secretFingerprint(secret string) (short, full string) {
	sum := sha256.Sum256([]byte(secret))
	full = hex.EncodeToString(sum[:])
	return full[:secretFingerprintLen], full
}

// shannonEntropy returns the Shannon entropy of s in bits per
// (byte) character.
func shannonEntropy(s string) float64 {
	n := len(s)
	if n == 0 {
		return 0
	}
	var freq [256]int
	for i := 0; i < n; i++ {
		freq[s[i]]++
	}
	h := 0.0
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

// unpushedCommitList lists, newest first, the SHAs on HEAD that are
// present on no ref of remote — the same range as UnpushedCommits,
// minus the count. An unconfigured remote contributes no refs, so
// every HEAD commit is listed.
func unpushedCommitList(ctx context.Context, dir, remote string) ([]string, error) {
	if remote == "" {
		remote = "origin"
	}
	if err := CheckRemoteName(remote); err != nil {
		return nil, err
	}
	out, err := runGit(ctx, dir, "rev-list", "HEAD", "--not", "--remotes="+remote)
	if err != nil {
		return nil, fmt.Errorf("list unpushed commits vs %q: %s", remote, tailReason(out))
	}
	var shas []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		sha := strings.TrimSpace(line)
		if sha == "" {
			continue
		}
		if !IsObjectID(sha) {
			return nil, fmt.Errorf("list unpushed commits vs %q: git printed a line that is not a commit id", remote)
		}
		shas = append(shas, sha)
	}
	return shas, nil
}

// hunkNewStart parses `@@ -a[,b] +c[,d] @@ ...` and returns c, the
// first new-file line number of the hunk.
func hunkNewStart(hunk string) int {
	f := strings.Fields(hunk)
	if len(f) < 2 {
		return 0
	}
	neg, pos := f[1], f[2]
	if !strings.HasPrefix(neg, "-") || !strings.HasPrefix(pos, "+") {
		return 0
	}
	pos = strings.TrimPrefix(pos, "+")
	if i := strings.IndexByte(pos, ','); i >= 0 {
		pos = pos[:i]
	}
	n, err := parsePositiveInt(pos)
	if err != nil {
		return 0
	}
	return n
}

func parsePositiveInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not an integer: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// ScanUnpushed scans the commits about to be pushed — the ones on HEAD
// and on no ref of remote — for secret material in the lines they add,
// and returns the findings. With opts omitted (or zero) all built-in
// detectors run with no allow-list.
//
// Shadow-vs-enforce policy (logging, recording, or refusing the push)
// is the caller's decision: this function only reports.
func ScanUnpushed(ctx context.Context, dir, remote string, opts ...ScanOptions) ([]SecretFinding, error) {
	var o ScanOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	dets, err := resolveDetectors(o.Detectors)
	if err != nil {
		return nil, err
	}
	shas, err := unpushedCommitList(ctx, dir, remote)
	if err != nil {
		return nil, err
	}
	m := &outgoingManifest{}
	for _, sha := range shas {
		raw, err := readGit(ctx, dir, "", "cat-file", "commit", sha)
		if err != nil {
			return nil, fmt.Errorf("read commit %s: %w", sha, err)
		}
		lines, err := commitAddedLines(ctx, dir, sha, commitParents(raw))
		if err != nil {
			return nil, fmt.Errorf("diff commit %s: %w", sha, err)
		}
		m.lines = append(m.lines, lines...)
	}
	return scanManifest(m, dets, o), nil
}

// matchLine returns one finding per detector that matches text.
func matchLine(text string, dets []secretDetector, o ScanOptions) []SecretFinding {
	var out []SecretFinding
	for _, d := range dets {
		m := d.re.FindString(text)
		if m == "" {
			continue
		}
		if d.minEntropy > 0 && shannonEntropy(m) < d.minEntropy {
			continue
		}
		short, full := secretFingerprint(m)
		if o.fpAllowed(short, full) {
			continue
		}
		out = append(out, SecretFinding{Detector: d.name, Fingerprint: short})
	}
	return out
}

// Where a finding outside file content is reported. A file or ref name is
// never put in a finding when it is what matched.
const (
	findingInCommit   = "(commit message or header)"
	findingInTag      = "(tag message or header)"
	findingInFileName = "(file name)"
	findingInRefName  = "(ref name)"
)

// scanManifest scans everything in the manifest: added lines, commit and tag
// objects, added file names and destination ref names.
func scanManifest(m *outgoingManifest, dets []secretDetector, o ScanOptions) []SecretFinding {
	var findings []SecretFinding
	add := func(text, commit, path string, line int) {
		for _, f := range matchLine(text, dets, o) {
			f.Commit, f.Path, f.Line = commit, path, line
			findings = append(findings, f)
		}
	}
	// shown keeps a secret that is part of a file name out of every finding.
	shown := func(path string) string {
		if len(matchLine(path, dets, ScanOptions{})) > 0 {
			return findingInFileName
		}
		return path
	}
	for _, l := range m.lines {
		if !o.pathAllowed(l.path) {
			add(l.text, l.commit, shown(l.path), l.line)
		}
	}
	for _, c := range m.commits {
		for i, line := range strings.Split(c.raw, "\n") {
			add(line, c.id, findingInCommit, i+1)
		}
		for i, name := range c.files {
			if !o.pathAllowed(name) {
				add(name, c.id, findingInFileName, i+1)
			}
		}
	}
	for _, t := range m.tags {
		for i, line := range strings.Split(t.raw, "\n") {
			add(line, t.id, findingInTag, i+1)
		}
	}
	for i, r := range m.refs {
		add(r.dst, r.newID, findingInRefName, i+1)
	}
	return findings
}

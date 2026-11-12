// Pre-push secret-scan wiring for PushToBranch and
// PushToDefaultBranch.
//
// The scanner itself (ScanUnpushed, the built-in detectors and the
// allow-list) lives in secretscan.go. This file decides what a push
// does with the findings per the daemon's secret-scan mode:
//
//   - off: no scan, the push proceeds exactly as before;
//   - shadow: the scan runs; findings are logged and returned on the
//     PushResult (always redacted — a SecretFinding never holds the
//     matched secret) so the caller records them for the operator;
//     the push proceeds;
//   - enforce: any finding refuses the push before it runs — the
//     result is Kind=secret_found with a redacted Reason (counts,
//     detector names, file:line, fingerprints — never the secret),
//     and the remote is left untouched.
//
// The mode names mirror the config package's git.secret_scan modes
// (config.GitSecretScanOff/Shadow/Enforce), defined here as plain
// string constants to avoid an import cycle with internal/config.

package git

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// The pre-push secret-scan modes (zero value / empty = off).
const (
	// ScanOff disables the pre-push secret scan.
	ScanOff = "off"
	// ScanShadow scans and reports (logs, findings on the PushResult,
	// caller records an alert) but never refuses the push.
	ScanShadow = "shadow"
	// ScanEnforce scans and refuses the push (Kind=secret_found) when
	// a finding is not allow-listed; the remote is untouched.
	ScanEnforce = "enforce"
)

// SecretScanPolicy is the pre-push secret scan for one push attempt.
// A zero policy (Mode == "") or Mode == ScanOff skips the scan
// entirely, which is what keeps callers without a daemon-configured
// policy (unit tests, one-offs) byte-for-byte on the old behaviour.
type SecretScanPolicy struct {
	// Mode is ScanOff, ScanShadow or ScanEnforce; empty reads as off.
	Mode string
	// Detectors is the detector list to run (names from AllDetectors);
	// empty means all built-ins, as in ScanOptions.
	Detectors []string
	// PathPatterns is the allow-list of path globs (matches skipped).
	PathPatterns []string
	// Fingerprints is the allow-list of fingerprint hashes (matching
	// findings dropped), short 16-hex or full 64-hex SHA-256.
	Fingerprints []string
}

// options maps the policy onto a ScanOptions for ScanUnpushed (the
// allow-list semantics are identical).
func (p SecretScanPolicy) options() ScanOptions {
	return ScanOptions{
		Detectors:    p.Detectors,
		PathPatterns: p.PathPatterns,
		Fingerprints: p.Fingerprints,
	}
}

// pushScanPolicy picks the first non-empty policy from a variadic
// tail; no usable policy reads as off.
func pushScanPolicy(opts []SecretScanPolicy) SecretScanPolicy {
	for _, o := range opts {
		if o.Mode != "" {
			return o
		}
	}
	return SecretScanPolicy{}
}

// RedactedFindingsSummary renders findings for logs, operator alerts
// and the push failure Reason: the count, then per finding the
// detector, file:line and the short fingerprint. It never contains the
// matched secret — findings themselves do not hold one (see
// SecretFinding), and this function only reuses their fields.
func RedactedFindingsSummary(findings []SecretFinding) string {
	if len(findings) == 0 {
		return ""
	}
	parts := make([]string, len(findings))
	for i, f := range findings {
		parts[i] = fmt.Sprintf("%s %s:%d (fingerprint %s)", f.Detector, f.Path, f.Line, f.Fingerprint)
	}
	return fmt.Sprintf("%d secret finding(s): %s", len(findings), strings.Join(parts, "; "))
}

// scanBeforePush runs the pre-push secret scan for a push to
// remote/<ref> and returns the redacted findings (empty when there was
// none, or nothing was found) plus a non-zero blocked PushResult only
// when enforce mode refuses the push. An internal scan error is logged
// and the push proceeds — a scanner malfunction must never wedge a
// legitimate push, and no finding is ever invented from an error.
func scanBeforePush(ctx context.Context, dir, remote, ref string, p SecretScanPolicy) ([]SecretFinding, PushResult) {
	if p.Mode == "" || p.Mode == ScanOff {
		return nil, PushResult{}
	}
	// The scan is about to execute (shadow or enforce); count it, including
	// the case where the scanner itself fails below (a run with zero
	// findings — a malfunction never wedges a legitimate push).
	RecordSecretScanRun()
	findings, err := ScanUnpushed(ctx, dir, remote, p.options())
	if err != nil {
		log.Printf("git: pre-push secret scan (%s) of %s failed; the push proceeds: %v", p.Mode, ref, err)
		return nil, PushResult{}
	}
	if len(findings) == 0 {
		return nil, PushResult{}
	}
	RecordSecretScanFindings(len(findings))
	summary := RedactedFindingsSummary(findings)
	if p.Mode == ScanEnforce {
		RecordSecretScanRefusal()
		log.Printf("git: push to %s/%s refused by the pre-push secret scan (enforce): %s", remote, ref, summary)
		return findings, PushResult{
			Remote:         remote,
			Ref:            ref,
			Kind:           PushFailSecret,
			Reason:         "secret scan (enforce mode): " + summary + " refuses the push to " + remote + "/" + ref,
			SecretFindings: findings,
		}
	}
	// Shadow (and any other mode value — the config write paths
	// refuse unknown ones): log, record via the caller, push proceeds.
	if p.Mode == ScanShadow {
		RecordSecretScanShadowReport()
	}
	log.Printf("git: push to %s/%s proceeds with shadow-mode secret findings: %s", remote, ref, summary)
	return findings, PushResult{}
}

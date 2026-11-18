// worker_tls.go — TLS settings derivation for the worker-side observer
// push client. The parent's own leaf-certificate fingerprint is the
// preferred trust signal (the same env var the parent injects for the
// bootstrap channel); an explicit skip-verification opt-out is only
// honoured when no pin is set, and both cases carry a visible warning
// or notice so the operator sees the trust posture of the push path.
package observerpeer

import (
	"net/url"
	"os"
	"strings"
)

// Environment variables read by WorkerTLSSettings.
const (
	// EnvParentCertFingerprint — hex SHA-256 of the parent's leaf
	// certificate. The parent injects this into every worker it spawns
	// (bootstrap channel), so the push channel can reuse the same
	// variable; a pin in it wins over the opt-out.
	EnvParentCertFingerprint = "DATAWATCH_PARENT_CERT_FINGERPRINT"
	// EnvPushInsecure — opt-out: skip TLS certificate verification on the
	// push client. Truthy values: 1, true, yes, on (case-insensitive).
	// Only applies when no pin is set.
	EnvPushInsecure = "DATAWATCH_OBSERVER_PUSH_INSECURE"
)

// WorkerTLSSettings derives the push client's TLS posture from the
// worker's environment:
//
//   - pin:        EnvParentCertFingerprint, trimmed; a non-empty pin
//     wins — the chain is not checked and the leaf SHA-256 must match
//     (validated later, by New, which fails closed on a malformed pin).
//   - insecure:   true only when EnvPushInsecure is truthy (1, true,
//     yes, on, any case) AND no pin is set.
//   - warnings:   a visible skip-verification warning when insecure is
//     on; for an https parent with neither a pin nor the opt-out, a
//     notice that verification uses the system trust store and that a
//     self-signed parent needs the pin or the opt-out. An http parent
//     produces no TLS notices.
//
// Warnings name the parent's host and never any token.
func WorkerTLSSettings(getenv func(string) string, parentURL string) (pin string, insecure bool, warnings []string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	pin = strings.TrimSpace(getenv(EnvParentCertFingerprint))
	host := parentHost(parentURL)

	if pin != "" {
		// Pin wins over the opt-out; nothing to warn about.
		return pin, false, nil
	}
	if truthyEnv(getenv(EnvPushInsecure)) {
		return pin, true, []string{insecurePushWarning(host)}
	}
	if usesTLS(parentURL) {
		return "", false, []string{systemStoreNotice(host)}
	}
	return "", false, nil
}

// truthyEnv reports whether v is an affirmative switch value.
func truthyEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// parentHost returns the host part of rawURL for warning texts. It
// never returns userinfo (credentials) — only the hostname — and falls
// back to the raw URL when it cannot be parsed.
func parentHost(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return strings.TrimSpace(rawURL)
	}
	if h := u.Hostname(); h != "" {
		return h
	}
	if u.Host != "" {
		return u.Host
	}
	return strings.TrimSpace(rawURL)
}

// usesTLS reports whether parentURL uses the https scheme (an empty or
// http scheme carries no TLS posture to announce).
func usesTLS(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && strings.EqualFold(u.Scheme, "https")
}

// insecurePushWarning is the operator-facing warning emitted when
// certificate verification is skipped for a given parent. It names the
// parent host and how to pin instead; it never contains a token.
func insecurePushWarning(host string) string {
	return "observer peer: TLS certificate verification is SKIPPED for parent " + host +
		". Anything on the network path can read and alter the pushed stats. " +
		"To pin the parent's own certificate instead, set " + EnvParentCertFingerprint +
		" (or PinnedSHA256 on the client) to the hex SHA-256 fingerprint of the " +
		"parent's leaf certificate."
}

// systemStoreNotice is the operator-facing notice for an https parent
// with neither a pin nor the opt-out.
func systemStoreNotice(host string) string {
	return "observer peer: TLS verification for parent " + host +
		" uses the system trust store. If the parent presents a self-signed " +
		"certificate that is not in that store, the push will fail until you set " +
		EnvParentCertFingerprint + " (hex SHA-256 of the parent's leaf certificate) " +
		"or opt out with " + EnvPushInsecure + "."
}

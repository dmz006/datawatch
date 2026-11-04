// Exported redaction entry point over the built-in secret detectors, so other
// packages (for example internal/trace) reuse the exact detectors that
// git.secret_scan runs instead of keeping a second, drifting pattern list.

package git

import (
	"regexp"
)

// SecretRedactMarker replaces every secret match RedactSecrets removes.
const SecretRedactMarker = "[REDACTED]"

// privateKeyBlock matches a whole PEM private key block, or from the BEGIN
// line to the end of the text when the block is truncated.
var privateKeyBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|\z)`)

// RedactSecrets replaces every match of the built-in secret detectors in s
// with SecretRedactMarker and returns the result and the number of
// redactions. All detectors run, with the same patterns and entropy floors as
// ScanUnpushed; none is weakened. A PEM private key block is removed whole,
// not just its header line.
func RedactSecrets(s string) (string, int) {
	count := 0
	for _, d := range builtInDetectors {
		if d.name == DetectorPrivateKey {
			s = privateKeyBlock.ReplaceAllStringFunc(s, func(string) string {
				count++
				return SecretRedactMarker
			})
			continue
		}
		s = d.re.ReplaceAllStringFunc(s, func(m string) string {
			if d.minEntropy > 0 && shannonEntropy(m) < d.minEntropy {
				return m
			}
			count++
			return SecretRedactMarker
		})
	}
	return s, count
}

// ContainsSecret reports whether s matches any built-in detector.
func ContainsSecret(s string) bool {
	_, n := RedactSecrets(s)
	return n > 0
}

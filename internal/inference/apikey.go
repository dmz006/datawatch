// Call-time API key resolution for LLM entries whose APIKeyRef names a
// secret in the centralized secrets store.
//
// APIKeyRef may be:
//   - empty: no key needed, resolve to "" with no error;
//   - a literal key (no ${secret: token): returned unchanged — the
//     plaintext path stays for backends not yet moved to the secrets store;
//   - exactly one ${secret:name} reference: resolved via store.Get(name);
//   - anything else containing ${secret: (surrounding text, multiple refs):
//     refused.
//
// No error message ever contains the secret value or the literal key —
// only the LLM entry name and the secret name.

package inference

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/secrets"
)

// ResolveAPIKey returns the API key for lllm, resolving a ${secret:name}
// reference through store when present. See the package comment for the
// accepted shapes. Errors name the LLM entry and (where applicable) the
// secret; they never include the secret value or any literal key.
func ResolveAPIKey(llm *LLM, store secrets.Store) (string, error) {
	if llm == nil {
		return "", fmt.Errorf("resolve api key: nil llm entry")
	}
	name := llm.Name
	ref := llm.APIKeyRef

	// (1) Empty: no key.
	if ref == "" {
		return "", nil
	}

	// (2) Literal: no ${secret: token at all, returned unchanged.
	if !strings.Contains(ref, "${secret:") {
		return ref, nil
	}

	// (4) Ref-like but malformed: surrounding text or more than one
	// reference. Refused before any store access.
	if !secrets.IsRef(ref) {
		return "", fmt.Errorf(`llm %q: api key must be exactly one ${secret:name} reference`, name)
	}

	secretName := strings.TrimSuffix(strings.TrimPrefix(ref, "${secret:"), "}")

	// (5) No store, or lookup failed.
	if store == nil {
		return "", fmt.Errorf("llm %q: api key secret %q not found", name, secretName)
	}
	sec, err := store.Get(secretName)
	if err != nil {
		return "", fmt.Errorf("llm %q: api key secret %q not found", name, secretName)
	}
	if sec.Value == "" {
		return "", fmt.Errorf("llm %q: api key secret %q is empty", name, secretName)
	}

	// (6) Defense in depth: the resolved value must not itself contain a
	// ${secret: token. The error names only the entry and secret, never
	// the value.
	if strings.Contains(sec.Value, "${secret:") {
		return "", fmt.Errorf(`llm %q: api key secret %q resolves to a value containing a ${secret: reference`, name, secretName)
	}
	return sec.Value, nil
}

package packs

import (
	"fmt"
	"regexp"
)

// InputDecl is the declaration of a pack input.
type InputDecl = Input

var secretRefRe = regexp.MustCompile(`^\$\{secret:[A-Za-z0-9_.\-]+\}$`)

// CheckSecretInput refuses any value for a secret input that is not a vault
// reference of the form ${secret:NAME}. Non-secret inputs accept any value.
func CheckSecretInput(input InputDecl, value string) error {
	if !input.Secret {
		return nil
	}
	if !secretRefRe.MatchString(value) {
		return fmt.Errorf("input %q is secret: value must be a vault reference of the form ${secret:NAME}", input.Name)
	}
	return nil
}

// RedactInputs returns a copy of resolved that is safe to show: secret inputs
// keep their vault reference and every other input shows its value. A secret
// input whose value is not a vault reference is masked.
func RedactInputs(resolved map[string]string, decls []InputDecl) map[string]string {
	secret := map[string]bool{}
	for _, d := range decls {
		if d.Secret {
			secret[d.Name] = true
		}
	}
	out := make(map[string]string, len(resolved))
	for k, v := range resolved {
		if secret[k] && !secretRefRe.MatchString(v) {
			out[k] = "***"
			continue
		}
		out[k] = v
	}
	return out
}

// Env-var-backed secret provider — reads from the process
// environment. Useful for K8s when the secret is already projected
// from a Secret resource into the pod's env, or when the operator
// wants to source credentials from `direnv` / `vault env exec`.

package secrets

import (
	"fmt"
	"os"
	"strings"
)

// EnvVarProvider reads from the process environment. Put silently
// succeeds without persisting (env vars are immutable from inside
// the process; we'd be lying if we said writes worked across
// restarts).
type EnvVarProvider struct {
	// Prefix is prepended to every key on read so multiple
	// independent secret namespaces can coexist in one env. Empty
	// = no prefix (operator names env vars exactly as the keys).
	// Convention: "DATAWATCH_SECRET_" gives you DATAWATCH_SECRET_FOO
	// for key "FOO".
	Prefix string
}

// NewEnvVarProvider returns an EnvVarProvider with no prefix.
// Callers can mutate Prefix before first use.
func NewEnvVarProvider() *EnvVarProvider { return &EnvVarProvider{} }

// Kind implements Provider.
func (*EnvVarProvider) Kind() string { return "env" }

// Get returns the env var Prefix+key. Returns ErrNotFound when
// the var is unset OR set to the empty string (treated identically
// for hardening — empty secret is never a valid secret).
func (p *EnvVarProvider) Get(key string) (string, error) {
	full := p.Prefix + key
	v := os.Getenv(full)
	if v == "" {
		return "", fmt.Errorf("%w (env %q empty/unset)", ErrNotFound, full)
	}
	return v, nil
}

// Put exports value as Prefix+key in the process environment.
// Persists only for the lifetime of the current process — operator
// must use a different provider (File, Vault, K8sSecret) for
// cross-restart durability. Documented in the package head.
func (p *EnvVarProvider) Put(key, value string) error {
	if key == "" {
		return fmt.Errorf("secrets env: key required")
	}
	// Refuse path-like keys to keep operator mental model simple
	// (env keys aren't paths).
	if strings.ContainsAny(key, "/\\") {
		return fmt.Errorf("secrets env: rejected key %q (slashes not allowed)", key)
	}
	return os.Setenv(p.Prefix+key, value)
}

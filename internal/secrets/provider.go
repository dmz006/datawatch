// Package secrets — F10 sprint 8 (S8.1) pluggable secret providers.
//
// Datawatch reads + writes a handful of secrets at runtime: the
// operator's git PAT (via gh / glab CLI), per-spawn git tokens
// (broker-minted), the parent's TLS key, optional bearer tokens for
// remote-server proxy auth, and the F10 spawn bootstrap tokens. Each
// of these has the same shape — "give me this named secret" / "store
// this secret under this name" — but operators want different
// backends for production hardening.
//
// The Provider interface gives us one swap point. Concrete impls:
//
//   File     — reads/writes a 0600 file under a base directory.
//              Default for single-host deployments + dev. Always
//              available.
//   EnvVar   — reads from the process environment; Put is a no-op
//              (env vars are immutable from inside the process so
//              writes silently succeed without persisting; doc'd).
//              Useful for K8s when the secret is already projected
//              from a Secret resource.
//   K8sSecret — stub. Future impl shells out to kubectl
//              create/patch secret.
//   Vault    — stub. Future impl uses HashiCorp Vault's API +
//              namespace-scoped tokens.
//   CSI      — stub. Future impl reads from a CSI-mounted secret
//              store path (vault-csi, secrets-store-csi).
//
// ClusterProfile.CredsRef.Provider already accepts these names
// (S2 schema); this package wires runtime resolution.

package secrets

import (
	"errors"
	"fmt"
)

// ErrNotImplemented is returned by stub providers (K8sSecret /
// Vault / CSI) for any call. Surface verbatim so operators know
// exactly which backend needs implementation work.
var ErrNotImplemented = errors.New("secrets provider: not implemented")

// ErrNotFound signals a missing key for a configured provider —
// distinct from ErrNotImplemented (the provider exists but the
// requested secret doesn't).
var ErrNotFound = errors.New("secrets provider: not found")

// Provider is the narrow surface every backend implements.
type Provider interface {
	// Kind returns the provider name as it appears in
	// ClusterProfile.CredsRef.Provider ("file" | "env" | "k8s-secret"
	// | "vault" | "csi").
	Kind() string

	// Get returns the secret value at key, or ErrNotFound when the
	// key isn't set, or ErrNotImplemented for stub providers.
	Get(key string) (string, error)

	// Put stores value at key. Some providers (EnvVar) accept this
	// as a no-op; documented per impl. Returns ErrNotImplemented
	// for stub providers.
	Put(key, value string) error
}

// Resolve returns the Provider matching name. Unknown kinds get a
// stub provider that echoes the requested kind in errors so
// operators see exactly which backend is missing.
//
// File needs a baseDir argument supplied by the operator config;
// callers that just want EnvVar pass "" for baseDir.
func Resolve(kind, baseDir string) Provider {
	switch kind {
	case "", "file":
		return NewFileProvider(baseDir)
	case "env", "envvar":
		return NewEnvVarProvider()
	case "k8s-secret":
		return &stubProvider{kind: "k8s-secret"}
	case "vault":
		return &stubProvider{kind: "vault"}
	case "csi":
		return &stubProvider{kind: "csi"}
	default:
		return &stubProvider{kind: kind}
	}
}

// stubProvider returns ErrNotImplemented for every call. Kind() is
// retained so log messages show the operator-requested backend name.
type stubProvider struct{ kind string }

func (s *stubProvider) Kind() string { return s.kind }
func (s *stubProvider) Get(_ string) (string, error) {
	return "", fmt.Errorf("%w (provider %q is a stub — see docs/agents.md S8.1)", ErrNotImplemented, s.kind)
}
func (s *stubProvider) Put(_, _ string) error {
	return fmt.Errorf("%w (provider %q is a stub — see docs/agents.md S8.1)", ErrNotImplemented, s.kind)
}

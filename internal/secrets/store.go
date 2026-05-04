// BL242 — centralized secrets manager interface.
// The BuiltinStore (AES-256-GCM encrypted JSON) is the Phase 1 backend.
// KeePass (Phase 2) and 1Password (Phase 3) will implement Store.

package secrets

import (
	"errors"
	"time"
)

// ErrSecretNotFound is returned when a named secret does not exist in the store.
var ErrSecretNotFound = errors.New("secret not found")

// Secret is a named encrypted secret in the centralized store.
// Value is only populated on an explicit Get; List omits it.
type Secret struct {
	Name        string    `json:"name"`
	Value       string    `json:"value,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Scopes      []string  `json:"scopes,omitempty"`
	Description string    `json:"description,omitempty"`
	Backend     string    `json:"backend"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store is the centralized secrets manager interface.
type Store interface {
	List() ([]Secret, error)
	Get(name string) (Secret, error)
	Set(name, value string, tags []string, description string, scopes []string) error
	Delete(name string) error
	Exists(name string) (bool, error)
}

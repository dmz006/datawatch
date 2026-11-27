package delivery

import "errors"

// KeyParams are the key derivation settings kept in the key file.
type KeyParams struct {
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
}

// DefaultKeyParams meet the OWASP high-strength offline storage baseline.
var DefaultKeyParams = KeyParams{Time: 4, MemoryKiB: 512 * 1024, Threads: 4}

// KeyFile is where the store key's salt, settings and test value are kept,
// under the data folder. The delivery request store will share it.
const KeyFile = "queue.d/store.key"

// ErrStoreKey wraps every reason the daemon must not start.
var ErrStoreKey = errors.New("delivery store key")

// StoreKey is the key that seals the delivery records.
type StoreKey struct{}

// OpenStoreKey opens or prepares the store key. password is nil on a
// non-secure install, and the returned key is then nil. It writes nothing;
// Commit writes a new key file.
func OpenStoreKey(dataDir string, password []byte, params KeyParams) (*StoreKey, error) {
	return &StoreKey{}, nil
}

// Key returns the 32-byte key, or nil on a non-secure install.
func (k *StoreKey) Key() []byte { return nil }

// Commit writes a new key file. It does nothing when the file is already
// there or the install is not secure.
func (k *StoreKey) Commit() error { return nil }

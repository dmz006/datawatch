// The delivery store key. It is derived from the secure-mode password and a
// salt of its own, so a settings change that gives the config a new salt
// leaves the delivery records readable. It fails closed: when it cannot be
// used, datawatch does not start, and nothing is ever written plain.

package delivery

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"

	"github.com/dmz006/datawatch/internal/secfile"
)

// KeyParams are the key derivation settings kept in the key file.
type KeyParams struct {
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
}

// DefaultKeyParams meet the OWASP high-strength offline storage baseline.
var DefaultKeyParams = KeyParams{Time: 4, MemoryKiB: 512 * 1024, Threads: 4}

// maxKeyParams bound what a key file may ask for, so a damaged one cannot
// exhaust memory.
var maxKeyParams = KeyParams{Time: 20, MemoryKiB: 2 * 1024 * 1024, Threads: 64}

// KeyFile is where the store key's salt, settings and test value are kept,
// under the data folder. The delivery request store will share it.
const KeyFile = "queue.d/store.key"

// adoptMarker is there from the moment a new key is written until the records
// that were plain have been sealed with it. It holds nothing.
const adoptMarker = HeartbeatDir + "/.adopt-plain"

// ErrStoreKey wraps every reason the daemon must not start.
var ErrStoreKey = errors.New("delivery store key")

const (
	keyFileVersion = 1
	keyKDF         = "argon2id"
	keySaltLen     = 16
	keyTestValue   = "datawatch delivery store key test value"
)

type keyFile struct {
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	KeyParams
	Salt []byte `json:"salt"`
	// Test is keyTestValue sealed with the key: a wrong password fails it.
	Test string `json:"test"`
}

// StoreKey is the key that seals the delivery records.
type StoreKey struct {
	path   string
	marker string
	key    []byte
	// pending is a new key file that Commit has yet to write.
	pending []byte
}

// sealedRecords reports whether any sealed delivery record is under dataDir.
func sealedRecords(dataDir string) (bool, error) {
	for _, dir := range []string{HeartbeatDir, filepath.Dir(filepath.FromSlash(KeyFile))} {
		files, err := filepath.Glob(filepath.Join(dataDir, dir, "*.rec"))
		if err != nil {
			return false, err
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return false, err
			}
			// A request record is sealed inside a readable index.
			if secfile.IsEncrypted(data) || secfile.IsEncrypted(bodyOf(data)) {
				return true, nil
			}
		}
	}
	return false, nil
}

// OpenStoreKey opens or prepares the store key. password is nil on a
// non-secure install, and the returned key is then nil. It writes nothing;
// Commit writes a new key file.
func OpenStoreKey(dataDir string, password []byte, params KeyParams) (*StoreKey, error) {
	refuse := func(format string, a ...any) (*StoreKey, error) {
		return nil, fmt.Errorf("%w: %s", ErrStoreKey, fmt.Sprintf(format, a...))
	}
	path := filepath.Join(dataDir, filepath.FromSlash(KeyFile))
	marker := filepath.Join(dataDir, filepath.FromSlash(adoptMarker))
	leave := "To leave secure mode on purpose, stop datawatch and remove that file and the delivery folder beside queue.d; the delivery list and its history are then lost"
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return refuse("cannot read %s: %v", path, err)
	}
	if err == nil {
		if password == nil {
			return refuse("%s says the delivery records are sealed, but datawatch runs without secure mode. Start with --secure and the same password; datawatch never writes them plain. %s", path, leave)
		}
		var kf keyFile
		if err := json.Unmarshal(data, &kf); err != nil {
			return refuse("%s is damaged: %v. Restore it. %s", path, err, leave)
		}
		if kf.Version != keyFileVersion || kf.KDF != keyKDF || len(kf.Salt) != keySaltLen ||
			kf.Time < 1 || kf.Time > maxKeyParams.Time || kf.Threads < 1 || kf.Threads > maxKeyParams.Threads ||
			kf.MemoryKiB < 8*uint32(kf.Threads) || kf.MemoryKiB > maxKeyParams.MemoryKiB {
			return refuse("%s has settings this version cannot use", path)
		}
		key := deriveStoreKey(password, kf.Salt, kf.KeyParams)
		if got, err := secfile.Decrypt([]byte(kf.Test), key); err != nil || string(got) != keyTestValue {
			return refuse("the password does not open %s (wrong password?)", path)
		}
		return &StoreKey{path: path, marker: marker, key: key}, nil
	}

	sealed, err := sealedRecords(dataDir)
	if err != nil {
		return refuse("cannot look for sealed records: %v", err)
	}
	if sealed {
		return refuse("%s is missing but sealed delivery records exist. It is never recreated, because a new key could not open them. Restore the file", path)
	}
	if password == nil {
		return &StoreKey{path: path, marker: marker}, nil
	}
	kf := keyFile{Version: keyFileVersion, KDF: keyKDF, KeyParams: params, Salt: make([]byte, keySaltLen)}
	if _, err := rand.Read(kf.Salt); err != nil {
		return refuse("make a salt: %v", err)
	}
	key := deriveStoreKey(password, kf.Salt, params)
	test, err := secfile.Encrypt([]byte(keyTestValue), key)
	if err != nil {
		return refuse("seal the test value: %v", err)
	}
	kf.Test = string(test)
	pending, err := json.Marshal(kf)
	if err != nil {
		return refuse("encode the key file: %v", err)
	}
	return &StoreKey{path: path, marker: marker, key: key, pending: pending}, nil
}

func deriveStoreKey(password, salt []byte, p KeyParams) []byte {
	return argon2.IDKey(password, salt, p.Time, p.MemoryKiB, p.Threads, 32)
}

// AdoptPlain reports whether plain records may still be read and sealed: the
// key is new, or an earlier start wrote it and stopped before it had sealed
// them.
func (k *StoreKey) AdoptPlain() bool {
	if k == nil || k.key == nil {
		return false
	}
	return k.pending != nil || exists(k.marker)
}

// SealDone ends the time in which plain records are adopted.
func (k *StoreKey) SealDone() error {
	if k == nil || k.key == nil {
		return nil
	}
	if err := os.Remove(k.marker); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncFolder(filepath.Dir(k.marker))
}

// Key returns the 32-byte key, or nil on a non-secure install.
func (k *StoreKey) Key() []byte {
	if k == nil {
		return nil
	}
	return k.key
}

// IsNew reports whether the key was made by this open and is not on disk yet.
func (k *StoreKey) IsNew() bool { return k != nil && k.pending != nil }

// Commit writes a new key file, once: an existing one is never replaced. It
// does nothing when the file is already there or the install is not secure.
func (k *StoreKey) Commit() error {
	if k == nil || k.pending == nil {
		return nil
	}
	dir := filepath.Dir(k.path)
	for _, d := range []string{dir, filepath.Dir(k.marker)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("%w: %v", ErrStoreKey, err)
		}
	}
	// Before the key: a stop after the key must find this.
	if err := secfile.WriteFileDurable(k.marker, nil, 0o600, nil); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreKey, err)
	}
	tmp := k.path + ".new"
	if err := secfile.WriteFileDurable(tmp, k.pending, 0o600, nil); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreKey, err)
	}
	defer os.Remove(tmp) //nolint:errcheck // the link below is the real file
	// A link fails when the name is taken, so a key is never overwritten.
	if err := os.Link(tmp, k.path); err != nil {
		return fmt.Errorf("%w: write %s: %v", ErrStoreKey, k.path, err)
	}
	if err := syncFolder(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreKey, err)
	}
	k.pending = nil
	return nil
}

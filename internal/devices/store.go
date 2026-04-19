// Package devices — mobile push-token registration for datawatch-app.
//
// Closes #1. The mobile client (dmz006/datawatch-app) records an FCM
// registration token (or ntfy topic URL) at login and calls the
// server to register it. The server uses the stored record to push
// minimal wake-events (input-needed, rate-limited, completed, error)
// to the specific device.
//
// Design constraints (from issue #1):
//   * Persist durably — survives daemon restart
//   * One bearer token MAY register multiple devices (phone + tablet)
//   * Delete + list scoped to the bearer identity (for v1 this is the
//     session token; a future ADR may promote to per-user accounts)
//   * Push payload is deliberately minimal — no session content over
//     Google FCM infrastructure. Full content fetched via the
//     bearer-authenticated REST API after being woken.

package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Kind enumerates the push-delivery channels the mobile app supports.
type Kind string

const (
	KindFCM  Kind = "fcm"
	KindNTFY Kind = "ntfy"
)

// Valid reports whether the kind string is a known value.
func (k Kind) Valid() bool { return k == KindFCM || k == KindNTFY }

// Platform enumerates the client OS families.
type Platform string

const (
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
)

// Valid reports whether the platform string is a known value.
func (p Platform) Valid() bool { return p == PlatformAndroid || p == PlatformIOS }

// Device is one registered push target.
type Device struct {
	ID          string    `json:"device_id"`
	Token       string    `json:"device_token"` // FCM token or ntfy topic URL
	Kind        Kind      `json:"kind"`
	AppVersion  string    `json:"app_version,omitempty"`
	Platform    Platform  `json:"platform,omitempty"`
	ProfileHint string    `json:"profile_hint,omitempty"` // user-provided label
	RegisteredAt time.Time `json:"registered_at"`
}

// Store persists devices to a JSON file. Safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	path    string
	devices []*Device
}

// ErrNotFound is returned by Delete/Get when the requested device ID
// isn't registered.
var ErrNotFound = errors.New("device not found")

// NewStore opens (or creates) the device store at path.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("devices: mkdir: %w", err)
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("devices: read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &s.devices)
}

func (s *Store) persist() error {
	data, err := json.MarshalIndent(s.devices, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Register stores a new device or updates an existing one (keyed by
// token — re-registering the same token refreshes app_version /
// platform / profile_hint without creating a duplicate record).
func (s *Store) Register(d Device) (Device, error) {
	if d.Token == "" {
		return Device{}, fmt.Errorf("devices: device_token required")
	}
	if !d.Kind.Valid() {
		return Device{}, fmt.Errorf("devices: kind %q: must be fcm or ntfy", d.Kind)
	}
	if d.Platform != "" && !d.Platform.Valid() {
		return Device{}, fmt.Errorf("devices: platform %q: must be android or ios", d.Platform)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	for i, existing := range s.devices {
		if existing.Token == d.Token {
			// Refresh metadata; keep stable ID + registration time.
			d.ID = existing.ID
			d.RegisteredAt = existing.RegisteredAt
			s.devices[i] = &d
			if err := s.persist(); err != nil {
				return Device{}, err
			}
			return d, nil
		}
	}

	d.ID = uuid.NewString()
	d.RegisteredAt = now
	s.devices = append(s.devices, &d)
	if err := s.persist(); err != nil {
		return Device{}, err
	}
	return d, nil
}

// Delete removes the device with the given ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.devices {
		if d.ID == id {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			return s.persist()
		}
	}
	return ErrNotFound
}

// Get returns the device with the given ID, or ErrNotFound.
func (s *Store) Get(id string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.ID == id {
			return *d, nil
		}
	}
	return Device{}, ErrNotFound
}

// List returns a snapshot of every registered device.
func (s *Store) List() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, len(s.devices))
	for i, d := range s.devices {
		out[i] = *d
	}
	return out
}

// ListByKind returns only devices registered with the given kind.
// Used by the push dispatcher to route FCM events to FCM devices +
// ntfy events to ntfy devices.
func (s *Store) ListByKind(k Kind) []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Device
	for _, d := range s.devices {
		if d.Kind == k {
			out = append(out, *d)
		}
	}
	return out
}

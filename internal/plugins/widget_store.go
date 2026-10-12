package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// MaxWidgetPayloadBytes bounds the size of a single stored widget Data
// payload. Exported so the HTTP push handler and the pull scheduler can
// reject oversized payloads before they reach the store.
const MaxWidgetPayloadBytes = 64 * 1024

// DefaultWidgetSnapshotInterval is how often Run persists a dirty store.
// Push widgets refresh themselves and pull widgets only need a recent
// value after a restart, so snapshotting on every Set would over-persist;
// a periodic snapshot (plus a final one on shutdown) is enough.
const DefaultWidgetSnapshotInterval = 60 * time.Second

// Widget value sources.
const (
	WidgetSourcePush = "push"
	WidgetSourcePull = "pull"
)

// WidgetValue is the latest data a plugin produced for one manifest widget.
type WidgetValue struct {
	Plugin    string          `json:"plugin"`
	WidgetID  string          `json:"widget_id"`
	Data      json.RawMessage `json:"data"`
	UpdatedAt time.Time       `json:"updated_at"`
	Source    string          `json:"source"`          // "push" | "pull"
	Error     string          `json:"error,omitempty"` // last pull error; the previous Data is kept
}

// WidgetStore holds the latest WidgetValue per plugin widget, with an
// optional JSON snapshot at <dataDir>/plugins/widgets.json. It follows the
// observer peer-registry persistence convention: tmp file + rename at 0o600.
type WidgetStore struct {
	mu     sync.Mutex
	values map[string]*WidgetValue
	path   string
	dirty  bool
}

func widgetKey(plugin, widgetID string) string { return plugin + "/" + widgetID }

// NewWidgetStore creates a store backed by <dataDir>/plugins/widgets.json,
// loading the existing snapshot when present. An empty dataDir gives a
// memory-only store. A missing or empty snapshot is treated as empty; a
// corrupt snapshot is returned as an error (matching the peer registry) so
// operators see the corruption instead of silently losing values.
func NewWidgetStore(dataDir string) (*WidgetStore, error) {
	s := &WidgetStore{values: map[string]*WidgetValue{}}
	if dataDir == "" {
		return s, nil
	}
	dir := filepath.Join(dataDir, "plugins")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir plugins dir: %w", err)
	}
	s.path = filepath.Join(dir, "widgets.json")
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("read widgets.json: %w", err)
	}
	if len(data) == 0 {
		return s, nil
	}
	var stored []WidgetValue
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parse widgets.json: %w", err)
	}
	for i := range stored {
		v := stored[i]
		s.values[widgetKey(v.Plugin, v.WidgetID)] = &v
	}
	return s, nil
}

// Set stores v, replacing any previous value for the same widget. UpdatedAt
// defaults to now. Data must be valid JSON no larger than
// MaxWidgetPayloadBytes.
func (s *WidgetStore) Set(v WidgetValue) error {
	if v.Plugin == "" || v.WidgetID == "" {
		return errors.New("widget value needs plugin and widget_id")
	}
	if len(v.Data) > MaxWidgetPayloadBytes {
		return fmt.Errorf("widget %s payload is %d bytes, over the %d byte limit",
			widgetKey(v.Plugin, v.WidgetID), len(v.Data), MaxWidgetPayloadBytes)
	}
	if len(v.Data) > 0 && !json.Valid(v.Data) {
		return fmt.Errorf("widget %s payload is not valid JSON", widgetKey(v.Plugin, v.WidgetID))
	}
	if v.UpdatedAt.IsZero() {
		v.UpdatedAt = time.Now()
	}
	v.Data = append(json.RawMessage(nil), v.Data...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[widgetKey(v.Plugin, v.WidgetID)] = &v
	s.dirty = true
	return nil
}

// SetError records a pull error for a widget, keeping its last Data so the
// UI can keep showing the previous value alongside the error.
func (s *WidgetStore) SetError(plugin, widgetID, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := widgetKey(plugin, widgetID)
	v, ok := s.values[k]
	if !ok {
		v = &WidgetValue{Plugin: plugin, WidgetID: widgetID, Source: WidgetSourcePull}
		s.values[k] = v
	}
	v.Error = errMsg
	v.UpdatedAt = time.Now()
	s.dirty = true
}

// Get returns a copy of the stored value for a widget.
func (s *WidgetStore) Get(plugin, widgetID string) (WidgetValue, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[widgetKey(plugin, widgetID)]
	if !ok {
		return WidgetValue{}, false
	}
	return copyWidgetValue(v), true
}

// List returns copies of the stored values for plugin (all plugins when
// plugin is empty), sorted by plugin then widget id.
func (s *WidgetStore) List(plugin string) []WidgetValue {
	s.mu.Lock()
	out := make([]WidgetValue, 0, len(s.values))
	for _, v := range s.values {
		if plugin == "" || v.Plugin == plugin {
			out = append(out, copyWidgetValue(v))
		}
	}
	s.mu.Unlock()
	sortWidgetValues(out)
	return out
}

// DeletePlugin removes every stored value for plugin.
func (s *WidgetStore) DeletePlugin(plugin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.values {
		if v.Plugin == plugin {
			delete(s.values, k)
			s.dirty = true
		}
	}
}

// Snapshot writes the store to disk when it has changed since the last
// snapshot. It is a no-op for a memory-only store.
func (s *WidgetStore) Snapshot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" || !s.dirty {
		return nil
	}
	out := make([]WidgetValue, 0, len(s.values))
	for _, v := range s.values {
		out = append(out, *v)
	}
	sortWidgetValues(out)
	// Compact (not indented) so each Data payload round-trips byte-for-byte.
	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	s.dirty = false
	return nil
}

// Run snapshots the store every interval until ctx is done, then takes one
// final snapshot. A non-positive interval uses DefaultWidgetSnapshotInterval.
// A failed snapshot leaves the store dirty, so it is retried on the next tick.
func (s *WidgetStore) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultWidgetSnapshotInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = s.Snapshot()
			return
		case <-t.C:
			_ = s.Snapshot()
		}
	}
}

func copyWidgetValue(v *WidgetValue) WidgetValue {
	c := *v
	c.Data = append(json.RawMessage(nil), v.Data...)
	return c
}

func sortWidgetValues(vs []WidgetValue) {
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Plugin != vs[j].Plugin {
			return vs[i].Plugin < vs[j].Plugin
		}
		return vs[i].WidgetID < vs[j].WidgetID
	})
}

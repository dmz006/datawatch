package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// TagSweepMarker is the done record for the daemon-start tag sweep: the
// tags.placement settings hash the last full sweep ran against (and when
// it wrote the record). It is what makes the startup pass run once per
// settings change instead of on every restart: a marker holding the same
// hash means the whole existing history is already tagged under the
// current block, so the pass skips; a different hash (or no marker) means
// it sweeps again. Stored as a small JSON record under the data directory's
// autonomous store (tag_sweep_marker.json), next to the JSONL records the
// sweep re-derives.
type TagSweepMarker struct {
	// SettingsHash is the PlacementSettingsHash of the tags.placement block
	// the last sweep ran under.
	SettingsHash string `json:"settings_hash"`
	// UpdatedAt is when the marker was last written.
	UpdatedAt time.Time `json:"updated_at"`
}

const tagSweepMarkerFile = "tag_sweep_marker.json"

// PlacementSettingsHash returns a stable hash of the tags.placement block:
// the deterministic JSON encoding of the whole PlacementConfig (every axis's
// enabled/template, the master switch and the custom axes) reduced to a
// hex sha256. Two blocks that would derive the same tags for every fact
// hash identically; any settings edit changes the hash. An unencodable
// value (which cannot happen for this struct) hashes to "".
func PlacementSettingsHash(pc config.PlacementConfig) string {
	b, err := json.Marshal(pc)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// GetTagSweepMarker reads the done record; a missing or unparseable record
// reports ok=false (the sweep then runs, which is the safe direction — a
// repeat sweep is a no-op on objects already consistent).
func (s *Store) GetTagSweepMarker() (TagSweepMarker, bool) {
	data, err := os.ReadFile(filepath.Join(s.dir, tagSweepMarkerFile))
	if err != nil {
		return TagSweepMarker{}, false
	}
	var m TagSweepMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return TagSweepMarker{}, false
	}
	return m, true
}

// SetTagSweepMarker writes the done record (stamping UpdatedAt when unset),
// atomically: temp file in the same directory plus rename.
func (s *Store) SetTagSweepMarker(m TagSweepMarker) error {
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = time.Now().UTC()
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	dest := filepath.Join(s.dir, tagSweepMarkerFile)
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// SweepTagsOnce is the guarded one-pass entry for the daemon start: it runs
// the full SweepTags(false) pass over the whole existing history when the
// tags.placement settings hash the stored marker holds differs from the
// given hash (or no marker exists), then records the hash it ran under —
// so a restart with unchanged settings does not repeat the pass and a
// settings change does. When the marker already holds the same hash it
// examines and writes nothing and reports swept=false. The pass's per-kind
// tally is returned either way (zero when skipped); an error is returned
// only when the sweep itself ran and recording the marker failed (the tags
// were still re-derived).
func (m *Manager) SweepTagsOnce(settingsHash string) (swept bool, res SweepResult, err error) {
	if m.store == nil {
		return true, m.SweepTags(false), nil
	}
	if stored, ok := m.store.GetTagSweepMarker(); ok && stored.SettingsHash == settingsHash {
		return false, res, nil
	}
	res = m.SweepTags(false)
	if err := m.store.SetTagSweepMarker(TagSweepMarker{SettingsHash: settingsHash}); err != nil {
		return true, res, err
	}
	return true, res, nil
}

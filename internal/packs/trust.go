package packs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/skills"
)

// trustFileName is the JSON file, inside the install root, that holds the
// trust records. It sits beside the per-pack name folders.
const trustFileName = "trust.json"

// errNotAccepted is the refusal Runnable returns for a pack that has no trust
// record for its exact digest. The message is shown to the operator verbatim.
var errNotAccepted = errors.New("pack not accepted: accept it first")

// TrustRecord is the operator's acceptance of one pack name and version at one
// content digest. A later change to the pack files changes the digest, so an
// old record no longer trusts the changed content.
type TrustRecord struct {
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	Digest     string    `json:"digest"`
	AcceptedBy string    `json:"accepted_by"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// TrustStore is the set of trust records kept in one install root. Load it with
// LoadTrustStore, record acceptances with Accept, and check packs with Runnable.
type TrustStore struct {
	path    string
	records []TrustRecord
}

// trustFile is the on-disk shape of trust.json.
type trustFile struct {
	Records []TrustRecord `json:"records"`
}

// LoadTrustStore reads the trust records from root/trust.json. A missing file
// is an empty store: nothing outside a first-party source is trusted until it
// is accepted. A file that exists but does not parse is an error, so a damaged
// store never silently trusts a pack.
func LoadTrustStore(root string) (*TrustStore, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("pack trust: %w", err)
	}
	s := &TrustStore{path: filepath.Join(absRoot, trustFileName)}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pack trust: %w", err)
	}
	var f trustFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("pack trust: %s: %w", s.path, err)
	}
	s.records = f.Records
	return s, nil
}

// Accept records that acceptedBy accepted pack at the time at, replacing any
// earlier record for the same name and version, and writes the store to disk.
// The record binds to pack.Digest, so accepting a pack whose digest is empty is
// refused: there would be nothing to bind the acceptance to.
func (s *TrustStore) Accept(pack *Pack, acceptedBy string, at time.Time) (TrustRecord, error) {
	if pack == nil {
		return TrustRecord{}, fmt.Errorf("pack trust: no pack to accept")
	}
	recs, err := s.acceptAll([]*Pack{pack}, acceptedBy, at)
	if err != nil {
		return TrustRecord{}, err
	}
	return recs[0], nil
}

// acceptAll records every pack in packs as accepted by acceptedBy at time at.
// It checks every pack before it writes anything and then writes trust.json
// once, so a refused pack leaves the store and the file unchanged. Each new
// record replaces an earlier record with the same name and version.
func (s *TrustStore) acceptAll(packs []*Pack, acceptedBy string, at time.Time) ([]TrustRecord, error) {
	acceptedBy = strings.TrimSpace(acceptedBy)
	if acceptedBy == "" {
		return nil, fmt.Errorf("pack trust: accepted_by is required")
	}
	recs := make([]TrustRecord, 0, len(packs))
	replaced := make(map[string]bool, len(packs))
	for _, pack := range packs {
		if pack == nil {
			return nil, fmt.Errorf("pack trust: no pack to accept")
		}
		if pack.Digest == "" {
			return nil, fmt.Errorf("pack trust: pack %s %s has no digest to accept", pack.Name, pack.Version)
		}
		recs = append(recs, TrustRecord{
			Name:       pack.Name,
			Version:    pack.Version,
			Digest:     pack.Digest,
			AcceptedBy: acceptedBy,
			AcceptedAt: at.UTC(),
		})
		replaced[recordKey(pack.Name, pack.Version)] = true
	}
	next := make([]TrustRecord, 0, len(s.records)+len(recs))
	for _, r := range s.records {
		if !replaced[recordKey(r.Name, r.Version)] {
			next = append(next, r)
		}
	}
	next = append(next, recs...)
	if err := writeTrustFile(s.path, next); err != nil {
		return nil, err
	}
	s.records = next
	return recs, nil
}

// recordKey identifies a name and version pair for replacement. The NUL byte
// cannot appear in a pack name or version, so distinct pairs never collide.
func recordKey(name, version string) string {
	return name + "\x00" + version
}

// Record returns the trust record for name and version, if one exists.
func (s *TrustStore) Record(name, version string) (TrustRecord, bool) {
	for _, r := range s.records {
		if r.Name == name && r.Version == version {
			return r, true
		}
	}
	return TrustRecord{}, false
}

// Runnable reports whether pack may run. A pack from a first-party source runs
// without a record. Any other pack, including one whose Source is empty, runs
// only when a record exists for its name, version and digest; otherwise the
// error is "pack not accepted: accept it first".
func (s *TrustStore) Runnable(pack *Pack) error {
	if pack == nil {
		return fmt.Errorf("pack trust: no pack to check")
	}
	if skills.IsFirstPartyPackSource(pack.Source) {
		return nil
	}
	rec, ok := s.Record(pack.Name, pack.Version)
	if !ok || pack.Digest == "" || rec.Digest != pack.Digest {
		return errNotAccepted
	}
	return nil
}

// writeTrustFile writes records to path atomically: it writes a temporary file
// in the same folder and renames it over path.
func writeTrustFile(path string, records []TrustRecord) error {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Name != records[j].Name {
			return records[i].Name < records[j].Name
		}
		return records[i].Version < records[j].Version
	})
	data, err := json.MarshalIndent(trustFile{Records: records}, "", "  ")
	if err != nil {
		return fmt.Errorf("pack trust: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("pack trust: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".trust-*.json")
	if err != nil {
		return fmt.Errorf("pack trust: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("pack trust: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("pack trust: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("pack trust: %w", err)
	}
	return nil
}

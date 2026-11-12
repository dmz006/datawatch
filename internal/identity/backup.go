package identity

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultBackupsToKeep is the number of identity backups retained when no
// provider is wired or the provider returns a value below 1.
const DefaultBackupsToKeep = 10

const (
	backupDirName    = "identity-backups"
	backupTimeLayout = "20060102T150405.000Z"
	latestBackup     = "latest"
)

// Sentinel errors returned by the guard and the restore path.
var (
	// ErrEmptyOverwrite is returned by Set when an empty identity would
	// replace one that has operator content. Use Reset to clear on purpose.
	ErrEmptyOverwrite = errors.New("refusing to overwrite a non-empty identity with an empty one")
	// ErrBackupNotFound is returned by Restore for a name that is invalid
	// or does not match an existing backup.
	ErrBackupNotFound = errors.New("identity backup not found")
	// ErrBackupEmpty is returned by Restore for a backup that holds no
	// operator content.
	ErrBackupEmpty = errors.New("identity backup has no operator content")
)

// Backup describes one saved copy of the identity file.
type Backup struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	SizeBytes  int64     `json:"size_bytes"`
	Role       string    `json:"role"`
	FieldCount int       `json:"field_count"`
}

// backupNameRE matches identity-<UTC ms timestamp>[_<nnn>].yaml. The
// suffix sorts after the bare name, so plain string order is chronological.
var backupNameRE = regexp.MustCompile(`^identity-(\d{8}T\d{6}\.\d{3}Z)(_\d{3})?\.yaml$`)

// SetBackupsToKeep wires the retention provider. It is read on every
// write, so a config change applies on the next write with no restart.
func (m *Manager) SetBackupsToKeep(fn func() int) {
	m.mu.Lock()
	m.keep = fn
	m.mu.Unlock()
}

// BackupsToKeep returns the effective retention limit. A missing provider
// or a value below 1 reads as DefaultBackupsToKeep, so protection can
// never be switched off.
func (m *Manager) BackupsToKeep() int {
	m.mu.RLock()
	fn := m.keep
	m.mu.RUnlock()
	if fn == nil {
		return DefaultBackupsToKeep
	}
	if n := fn(); n >= 1 {
		return n
	}
	return DefaultBackupsToKeep
}

func (m *Manager) backupDir() string {
	return filepath.Join(filepath.Dir(m.path), backupDirName)
}

// nonEmptyFields names the identity fields that have content.
func nonEmptyFields(id Identity) []string {
	var out []string
	if id.Role != "" {
		out = append(out, "role")
	}
	if len(id.NorthStarGoals) > 0 {
		out = append(out, "north_star_goals")
	}
	if len(id.CurrentProjects) > 0 {
		out = append(out, "current_projects")
	}
	if len(id.Values) > 0 {
		out = append(out, "values")
	}
	if id.CurrentFocus != "" {
		out = append(out, "current_focus")
	}
	if id.ContextNotes != "" {
		out = append(out, "context_notes")
	}
	return out
}

// diskState is the identity file as found at the start of a write cycle.
type diskState struct {
	raw    []byte
	exists bool
	parsed bool // exists and parsed as YAML
	id     Identity
}

// readDisk reads and parses the identity file. Only a read error other
// than not-exist is returned.
func (m *Manager) readDisk() (diskState, error) {
	b, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return diskState{}, nil
	}
	if err != nil {
		return diskState{}, fmt.Errorf("identity read: %w", err)
	}
	st := diskState{raw: b, exists: true}
	if len(bytes.TrimSpace(b)) == 0 {
		st.parsed = true // blank counts as an empty identity, not a corrupt one
	} else if yaml.Unmarshal(b, &st.id) == nil {
		st.parsed = true
	} else {
		st.id = Identity{}
	}
	return st, nil
}

// base is the identity a write builds on: the on-disk one when the file
// exists and parses, otherwise the cache.
func (m *Manager) base(st diskState) Identity {
	if st.parsed {
		return st.id
	}
	return m.Get()
}

// stripUpdatedAt drops the updated_at line so two saves of the same
// identity compare equal.
func stripUpdatedAt(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	out := lines[:0]
	for _, l := range lines {
		if bytes.HasPrefix(l, []byte("updated_at:")) {
			continue
		}
		out = append(out, l)
	}
	return bytes.Join(out, []byte("\n"))
}

// listBackupNames returns matching backup file names, oldest first.
func (m *Manager) listBackupNames() ([]string, error) {
	ents, err := os.ReadDir(m.backupDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("identity backups: %w", err)
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && backupNameRE.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// backupBeforeWrite copies the existing file into the backup directory
// when it is worth keeping, then prunes to the retention limit. Callers
// hold wmu. An error means nothing may be written.
func (m *Manager) backupBeforeWrite(st diskState) error {
	if !st.exists || (st.parsed && st.id.IsEmpty()) {
		return nil
	}
	dir := m.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("identity backup dir: %w", err)
	}
	names, err := m.listBackupNames()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		newest, err := os.ReadFile(filepath.Join(dir, names[len(names)-1]))
		if err == nil && bytes.Equal(stripUpdatedAt(newest), stripUpdatedAt(st.raw)) {
			m.prune(names)
			return nil
		}
	}
	newest := ""
	if len(names) > 0 {
		newest = names[len(names)-1]
	}
	name, err := writeBackupFile(dir, time.Now().UTC().Format(backupTimeLayout), newest, st.raw)
	if err != nil {
		return err
	}
	m.prune(append(names, name))
	return nil
}

// writeBackupFile writes b under a fresh, unique name built from stamp and
// returns it. The name always sorts after newest (the newest existing
// backup name, or ""): within the same millisecond, or if the clock has
// gone backwards, the stamp of newest is reused with the next suffix.
func writeBackupFile(dir, stamp, newest string, b []byte) (string, error) {
	seq := 0
	if m := backupNameRE.FindStringSubmatch(newest); m != nil && m[1] >= stamp {
		stamp = m[1]
		seq = 1
		if m[2] != "" {
			n, _ := strconv.Atoi(m[2][1:])
			seq = n + 1
		}
	}
	for ; seq < 1000; seq++ {
		name := "identity-" + stamp + ".yaml"
		if seq > 0 {
			name = fmt.Sprintf("identity-%s_%03d.yaml", stamp, seq)
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("identity backup create: %w", err)
		}
		_, werr := f.Write(b)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(filepath.Join(dir, name))
			return "", fmt.Errorf("identity backup write: %w", werr)
		}
		return name, nil
	}
	return "", fmt.Errorf("identity backup: no free name for %s", stamp)
}

// prune removes the oldest backups beyond the retention limit. names is
// oldest first. A failed removal only leaves an extra backup behind, so
// it never blocks the write.
func (m *Manager) prune(names []string) {
	keep := m.BackupsToKeep()
	for len(names) > keep {
		_ = os.Remove(filepath.Join(m.backupDir(), names[0]))
		names = names[1:]
	}
}

// writeAtomic writes id via a same-directory temp file, fsync and rename,
// so a crash never leaves a truncated identity file.
func (m *Manager) writeAtomic(id Identity) error {
	b, err := yaml.Marshal(&id)
	if err != nil {
		return fmt.Errorf("identity marshal: %w", err)
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("identity mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".identity-*.tmp")
	if err != nil {
		return fmt.Errorf("identity write: %w", err)
	}
	tmpName := tmp.Name()
	werr := tmp.Chmod(0o600)
	if werr == nil {
		_, werr = tmp.Write(b)
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmpName, m.path)
	}
	if werr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("identity write: %w", werr)
	}
	return nil
}

// commit backs up the file described by st, then writes next and updates
// the cache. Callers hold wmu.
func (m *Manager) commit(st diskState, next Identity) error {
	if err := m.backupBeforeWrite(st); err != nil {
		return err
	}
	if err := m.writeAtomic(next); err != nil {
		return err
	}
	m.mu.Lock()
	m.cur = next
	m.mu.Unlock()
	return nil
}

// Reset is the explicit clear: it backs up the current file, writes an
// empty identity and is never refused. An identity that is already empty
// is left untouched and makes no backup.
func (m *Manager) Reset() (Identity, error) {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	st, err := m.readDisk()
	if err != nil {
		return Identity{}, err
	}
	if cur := m.base(st); cur.IsEmpty() && (st.parsed || !st.exists) {
		return cur, nil
	}
	next := Identity{UpdatedAt: time.Now().UTC()}
	if err := m.commit(st, next); err != nil {
		return Identity{}, err
	}
	return next, nil
}

// ListBackups returns the saved backups, newest first. A missing backup
// directory yields an empty slice. Files not matching the name pattern
// are ignored.
func (m *Manager) ListBackups() ([]Backup, error) {
	names, err := m.listBackupNames()
	if err != nil {
		return nil, err
	}
	out := make([]Backup, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		b, ok := m.describeBackup(names[i])
		if ok {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *Manager) describeBackup(name string) (Backup, bool) {
	p := filepath.Join(m.backupDir(), name)
	raw, err := os.ReadFile(p)
	if err != nil {
		return Backup{}, false // pruned or removed since the directory read
	}
	b := Backup{Name: name, SizeBytes: int64(len(raw))}
	if t, err := time.Parse(backupTimeLayout, backupNameRE.FindStringSubmatch(name)[1]); err == nil {
		b.CreatedAt = t
	}
	var id Identity
	if yaml.Unmarshal(raw, &id) == nil {
		b.Role = id.Role
		b.FieldCount = len(nonEmptyFields(id))
	}
	return b, true
}

// Restore replaces the identity with a saved backup, given by file name
// or "latest" (the newest backup that has operator content). The current
// file is backed up first under the usual rules, so a restore can itself
// be undone.
func (m *Manager) Restore(name string) (Identity, error) {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	names, err := m.listBackupNames()
	if err != nil {
		return Identity{}, err
	}
	if name == latestBackup {
		name = ""
		for i := len(names) - 1; i >= 0 && name == ""; i-- {
			if b, ok := m.describeBackup(names[i]); ok && b.FieldCount > 0 {
				name = names[i]
			}
		}
		if name == "" {
			return Identity{}, fmt.Errorf("%w: no backup with operator content", ErrBackupNotFound)
		}
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || !backupNameRE.MatchString(name) {
		return Identity{}, fmt.Errorf("%w: %q", ErrBackupNotFound, name)
	}
	raw, err := os.ReadFile(filepath.Join(m.backupDir(), name))
	if os.IsNotExist(err) {
		return Identity{}, fmt.Errorf("%w: %q", ErrBackupNotFound, name)
	}
	if err != nil {
		return Identity{}, fmt.Errorf("identity backup read: %w", err)
	}
	var next Identity
	if err := yaml.Unmarshal(raw, &next); err != nil {
		return Identity{}, fmt.Errorf("identity backup %q is not valid yaml: %w", name, err)
	}
	if next.IsEmpty() {
		return Identity{}, fmt.Errorf("%w: %q", ErrBackupEmpty, name)
	}
	st, err := m.readDisk()
	if err != nil {
		return Identity{}, err
	}
	next.UpdatedAt = time.Now().UTC()
	if err := m.commit(st, next); err != nil {
		return Identity{}, err
	}
	return next, nil
}

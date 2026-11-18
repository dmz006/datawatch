// Archive migration: the dry-run report, the verified backup and the run that
// archives every eligible finished Automaton, one at a time.
//
// PlanArchive writes nothing. RunArchive copies each touched record (the
// record file and its side files) to a backup directory first and verifies
// every copy by size and SHA-256; when one copy does not verify it stops
// before any record changes. It then archives one Automaton at a time, so an
// interrupt leaves each record at its old or new generation, and it skips an
// Automaton that is in use instead of forcing it. A second run finds nothing
// left to do.

package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const archiveBackupsDirName = "archive-backups"

// ArchiveSkip names an Automaton that was not archived and why.
type ArchiveSkip struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

// ArchivePlan is the dry-run report: which Automata would be archived and the
// bytes before and after. Nothing is written to produce it.
type ArchivePlan struct {
	DryRun        bool            `json:"dry_run"`
	RetentionDays int             `json:"retention_days"`
	Now           time.Time       `json:"now"`
	Eligible      []ArchiveResult `json:"eligible"`
	// Skipped lists finished Automata that were not eligible because they are
	// in use; Automata that are simply not finished or too new are only counted.
	Skipped        []ArchiveSkip `json:"skipped,omitempty"`
	NotFinished    int           `json:"not_finished"`
	BytesBefore    int64         `json:"bytes_before"`
	BytesAfter     int64         `json:"bytes_after"`
	ArchiveBytes   int64         `json:"archive_bytes"`
	AlreadyArchive int           `json:"already_archived"`
}

// ArchiveReport is the outcome of RunArchive.
type ArchiveReport struct {
	ArchivePlan
	Archived []ArchiveResult `json:"archived"`
	Failed   []ArchiveSkip   `json:"failed,omitempty"`
	// BackupDir is where the verified backup of the touched records sits.
	BackupDir   string `json:"backup_dir,omitempty"`
	BackupFiles int    `json:"backup_files"`
	BackupBytes int64  `json:"backup_bytes"`
}

func retentionDays(retention time.Duration) int { return int(retention / (24 * time.Hour)) }

// PlanArchive reports which Automata an archive run would handle, with the
// bytes before and after. It reads the store and writes nothing.
func (s *Store) PlanArchive(now time.Time, retention time.Duration) ArchivePlan {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.planArchiveLocked(now, retention)
}

func (s *Store) planArchiveLocked(now time.Time, retention time.Duration) ArchivePlan {
	plan := ArchivePlan{DryRun: true, RetentionDays: retentionDays(retention), Now: now.UTC(), Eligible: []ArchiveResult{}}
	ids := make([]string, 0, len(s.prds))
	for id := range s.prds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := s.prds[id]
		if p.Archive != nil {
			plan.AlreadyArchive++
			continue
		}
		ok, why, inUse := s.archiveEligibility(p, now, retention)
		if !ok {
			if inUse {
				plan.Skipped = append(plan.Skipped, ArchiveSkip{ID: id, Title: p.Title, Reason: why})
			} else {
				plan.NotFinished++
			}
			continue
		}
		res := ArchiveResult{ID: id, Title: p.Title, Status: string(p.Status), BytesBefore: s.recordSize(p)}
		data, _, slim, err := archiveSplit(p, now)
		if err != nil {
			plan.Skipped = append(plan.Skipped, ArchiveSkip{ID: id, Title: p.Title, Reason: err.Error()})
			continue
		}
		res.ArchiveBytes = int64(len(data))
		res.BytesAfter = s.recordSize(slim)
		plan.Eligible = append(plan.Eligible, res)
		plan.BytesBefore += res.BytesBefore
		plan.BytesAfter += res.BytesAfter
		plan.ArchiveBytes += res.ArchiveBytes
	}
	return plan
}

// fileDigest returns the size and hex SHA-256 of a file.
func fileDigest(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

type backupEntry struct {
	Path   string `json:"path"` // relative to the records directory
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// backupRecordFiles copies one record (the record file and its side files) to
// backupDir/prds and verifies every copy by size and SHA-256 against the
// source. It returns the entries and the bytes copied.
func (s *Store) backupRecordFiles(id, backupDir string) ([]backupEntry, int64, error) {
	name := recordName(id)
	src := s.recordsDir()
	dst := filepath.Join(backupDir, prdRecordsDirName)
	var files []string
	files = append(files, name+".json")
	if entries, err := os.ReadDir(filepath.Join(src, name+".files")); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, filepath.Join(name+".files", e.Name()))
			}
		}
	}
	var out []backupEntry
	var total int64
	for _, rel := range files {
		from := filepath.Join(src, rel)
		to := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return nil, 0, err
		}
		wantSize, wantSum, err := fileDigest(from)
		if err != nil {
			return nil, 0, fmt.Errorf("backup %s: %w", rel, err)
		}
		// A copy of a file that is already in the backup (an earlier pass over
		// the same record) is kept as is, never overwritten.
		to = uniqueName(to)
		if err := copyFileExcl(from, to); err != nil {
			return nil, 0, fmt.Errorf("backup %s: %w", rel, err)
		}
		if h := s.archiveBackupHook; h != nil {
			h(to)
		}
		gotSize, gotSum, err := fileDigest(to)
		if err != nil {
			return nil, 0, fmt.Errorf("verify backup %s: %w", rel, err)
		}
		if gotSize != wantSize || gotSum != wantSum {
			return nil, 0, fmt.Errorf("verify backup %s: copy has size %d sha256 %s, source has size %d sha256 %s",
				rel, gotSize, gotSum, wantSize, wantSum)
		}
		out = append(out, backupEntry{Path: rel, Size: wantSize, SHA256: wantSum})
		total += wantSize
	}
	return out, total, nil
}

// RunArchive archives every eligible finished Automaton. It takes a verified
// backup of the touched records first and stops without changing anything when
// a copy does not verify. Each Automaton is archived on its own, verified
// before its live record is rewritten, and an Automaton that is in use is
// skipped. A second run archives nothing more.
func (s *Store) RunArchive(now time.Time, retention time.Duration) (ArchiveReport, error) {
	return s.runArchive(now, retention, 0)
}

// ArchiveQuietWindow is how long Automaton writes must have been quiet before
// the background pass archives anything, and between two of its steps.
const ArchiveQuietWindow = 30 * time.Second

// writeHotLocked reports whether an Automaton write went through in the last
// quiet window or one is waiting to be written. Caller holds s.mu.
func (s *Store) writeHotLocked(quiet time.Duration) bool {
	return len(s.rec.dirty) > 0 || len(s.rec.deleted) > 0 || s.rec.allDirty ||
		(!s.rec.lastFlush.IsZero() && time.Since(s.rec.lastFlush) < quiet)
}

// runArchive is RunArchive; with quiet > 0 it stops (leaving the rest for the
// next pass) as soon as an Automaton write is hot, and does not start while one
// is.
func (s *Store) runArchive(now time.Time, retention, quiet time.Duration) (ArchiveReport, error) {
	s.mu.Lock()
	if quiet > 0 && s.writeHotLocked(quiet) {
		s.mu.Unlock()
		return ArchiveReport{ArchivePlan: ArchivePlan{RetentionDays: retentionDays(retention), Now: now.UTC(), Eligible: []ArchiveResult{}}, Archived: []ArchiveResult{}}, nil
	}
	plan := s.planArchiveLocked(now, retention)
	s.mu.Unlock()
	rep := ArchiveReport{ArchivePlan: plan, Archived: []ArchiveResult{}}
	rep.DryRun = false
	if len(plan.Eligible) == 0 {
		return rep, nil
	}

	// 1. Verified backup of every record that will change.
	stamp := now.UTC().Format("20060102T150405Z")
	backupDir := uniqueName(filepath.Join(s.dir, archiveBackupsDirName, stamp))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return rep, fmt.Errorf("backup: %w", err)
	}
	var manifest []backupEntry
	for _, it := range plan.Eligible {
		entries, n, err := s.backupRecordFiles(it.ID, backupDir)
		if err != nil {
			// Nothing was changed yet: the backup copies are all that exist.
			return rep, fmt.Errorf("archive aborted before any change: %w", err)
		}
		manifest = append(manifest, entries...)
		rep.BackupBytes += n
	}
	rep.BackupDir = backupDir
	rep.BackupFiles = len(manifest)
	mf, err := json.MarshalIndent(manifest, "", " ")
	if err == nil {
		err = atomicWriteFile(filepath.Join(backupDir, "MANIFEST.json"), mf)
	}
	if err != nil {
		return rep, fmt.Errorf("archive aborted before any change: manifest: %w", err)
	}

	// 2. Archive one Automaton at a time.
	have := map[string]string{}
	for _, e := range manifest {
		have[e.Path] = e.SHA256
	}
	for _, it := range plan.Eligible {
		// A record that changed after its backup was taken gets a fresh,
		// verified backup before it is touched.
		if _, sum, err := fileDigest(filepath.Join(s.recordsDir(), recordName(it.ID)+".json")); err == nil && sum != have[recordName(it.ID)+".json"] {
			if _, _, err := s.backupRecordFiles(it.ID, backupDir); err != nil {
				rep.Failed = append(rep.Failed, ArchiveSkip{ID: it.ID, Title: it.Title, Reason: "backup: " + err.Error()})
				continue
			}
		}
		s.mu.Lock()
		if quiet > 0 && s.writeHotLocked(quiet) {
			s.mu.Unlock()
			break // an Automaton write is hot: the rest waits for the next pass
		}
		res, err := s.archiveAutomatonLocked(it.ID, now, retention)
		s.mu.Unlock()
		if err != nil {
			rep.Failed = append(rep.Failed, ArchiveSkip{ID: it.ID, Title: it.Title, Reason: err.Error()})
			continue
		}
		rep.Archived = append(rep.Archived, res)
	}
	return rep, nil
}

package autonomous

// Decision records from a folder of files. The operator points
// autonomous.decisions.folder at a directory; every file in it that matches
// autonomous.decisions.folder_glob describes one decision in a small
// front-matter block, and the daemon keeps one decision record (and so one
// queue question) per file:
//
//   - a new file creates a record with source "file" and the file name as
//     source_ref;
//   - a file whose content hash differs from its open record updates that
//     record, and through it the same open question;
//   - an unchanged file does nothing (no store write, no question churn);
//   - a file that no longer exists withdraws its open record when
//     autonomous.decisions.folder_withdraw_on_delete is true;
//   - a file whose record is already answered (or withdrawn) is never
//     re-opened; an edit to it is reported as a folder note;
//   - a file that cannot be read, parsed or validated is skipped, never stops
//     the scan, and is kept in FolderStatus with a plain message.
//
// The scan runs on the daemon poll (DecisionRecordTick); it starts no
// goroutine and watches nothing through the file system.
//
// The file format never depends on this repository's documents. The file
// starts with a block between two delimiter lines (front_matter_delimiter,
// default "---"). Each line of the block is "key<separator> value"
// (front_matter_separator, default ":"); blank lines and lines starting with
// "#" are ignored; a value continues on the following lines that start with
// two spaces (the pieces are joined with a single space). The canonical keys
// are label, title, situation, deciding, option (repeatable, "id | label |
// tradeoff"), recommendation ("option_id | reason"), default, doc, automaton
// and registry_row; front_matter_keys renames any of them. The parts of an
// option or recommendation line are split on front_matter_field_separator
// (default "|"). Unknown keys are ignored, and the text after the closing
// delimiter is ignored. A record whose folder is changed in the
// configuration is judged against the new folder, so records whose files are
// not there are withdrawn when withdrawal on delete is on.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Limits of the folder scan.
const (
	// decisionFileMaxBytes is the largest decision file that is read.
	decisionFileMaxBytes = 64 * 1024
	// decisionFolderMaxFiles is the most files one scan reads.
	decisionFolderMaxFiles = 200
)

// The canonical front-matter keys.
const (
	DecisionFileKeyLabel          = "label"
	DecisionFileKeyTitle          = "title"
	DecisionFileKeySituation      = "situation"
	DecisionFileKeyDeciding       = "deciding"
	DecisionFileKeyOption         = "option"
	DecisionFileKeyRecommendation = "recommendation"
	DecisionFileKeyDefault        = "default"
	DecisionFileKeyDoc            = "doc"
	DecisionFileKeyAutomaton      = "automaton"
	DecisionFileKeyRegistryRow    = "registry_row"
)

// decisionFileKeys lists the canonical keys in the order they are documented.
var decisionFileKeys = []string{
	DecisionFileKeyLabel, DecisionFileKeyTitle, DecisionFileKeySituation, DecisionFileKeyDeciding,
	DecisionFileKeyOption, DecisionFileKeyRecommendation, DecisionFileKeyDefault,
	DecisionFileKeyDoc, DecisionFileKeyAutomaton, DecisionFileKeyRegistryRow,
}

// ErrDecisionFileInvalid wraps every problem found while reading a decision
// file; the message says what to fix.
var ErrDecisionFileInvalid = errors.New("invalid decision file")

// DecisionFolderProblem is one file (or the folder itself, when File is
// empty) that the last scan could not use, with a plain message.
type DecisionFolderProblem struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

// DecisionFolderStatus is what the folder watch reports. Errors holds the
// files that were skipped; Notes holds files that were read fine but could
// not change their record (for example an edit to an answered decision).
type DecisionFolderStatus struct {
	Enabled   bool                    `json:"enabled"`
	Path      string                  `json:"path"`
	FilesSeen int                     `json:"files_seen"`
	Errors    []DecisionFolderProblem `json:"errors"`
	Notes     []DecisionFolderProblem `json:"notes"`
	LastScan  *time.Time              `json:"last_scan,omitempty"`
}

// decisionFolderState is the scan state the service keeps between polls.
type decisionFolderState struct {
	scanMu sync.Mutex // one scan at a time
	mu     sync.Mutex // guards last
	last   DecisionFolderStatus
}

// ParseDecisionFile reads the front-matter block of a decision file into a
// record: the content fields only, validated and with the default label
// filled in. The key names, delimiter and separators come from cfg. Every
// problem comes back in one error that wraps ErrDecisionFileInvalid (or, for
// a block that parsed but is not a valid record, ErrInvalidDecisionRecord as
// well), so the operator sees all of them at once.
func ParseDecisionFile(content []byte, cfg config.AutonomousDecisionsConfig) (DecisionRecord, error) {
	if len(content) > decisionFileMaxBytes {
		return DecisionRecord{}, fmt.Errorf("%w: the file is %d bytes; the most that is read is %d", ErrDecisionFileInvalid, len(content), decisionFileMaxBytes)
	}
	delim := cfg.EffectiveFrontMatterDelimiter()
	sep := cfg.EffectiveFrontMatterSeparator()
	fieldSep := cfg.EffectiveFrontMatterFieldSeparator()

	fileName, err := decisionFileKeyNames(cfg.EffectiveFrontMatterKeys())
	if err != nil {
		return DecisionRecord{}, err
	}
	byFileName := make(map[string]string, len(fileName))
	for canon, name := range fileName {
		byFileName[strings.ToLower(name)] = canon
	}

	text := strings.TrimPrefix(string(content), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != delim {
		return DecisionRecord{}, fmt.Errorf("%w: the file must start with a line holding only %q, which opens the front matter block", ErrDecisionFileInvalid, delim)
	}
	start := i + 1

	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	type entry struct {
		canon string
		value string
		line  int
	}
	var entries []entry
	closed := false
	for n := start; n < len(lines); n++ {
		raw := lines[n]
		if strings.TrimSpace(raw) == delim {
			closed = true
			break
		}
		if strings.HasPrefix(raw, "  ") && strings.TrimSpace(raw) != "" {
			if len(entries) == 0 {
				add("line %d continues a value, but no key came before it", n+1)
				continue
			}
			last := &entries[len(entries)-1]
			if last.canon == "" { // continuation of an unknown key: ignored with it
				continue
			}
			last.value = strings.TrimSpace(last.value + " " + strings.TrimSpace(raw))
			continue
		}
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		k, v, ok := strings.Cut(t, sep)
		if !ok {
			add("line %d has no %q between a key and its value", n+1, sep)
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		canon := byFileName[key] // empty for a key this format does not know
		entries = append(entries, entry{canon: canon, value: strings.TrimSpace(v), line: n + 1})
	}
	if !closed {
		add("the front matter block is not closed; add a line holding only %q after the last key", delim)
	}

	var rec DecisionRecord
	seen := map[string]int{}
	for _, e := range entries {
		if e.canon == "" {
			continue
		}
		if e.canon != DecisionFileKeyOption {
			seen[e.canon]++
			if seen[e.canon] > 1 {
				add("line %d: %q appears more than once", e.line, fileName[e.canon])
				continue
			}
		}
		switch e.canon {
		case DecisionFileKeyLabel:
			rec.Label = e.value
		case DecisionFileKeyTitle:
			rec.Title = e.value
		case DecisionFileKeySituation:
			rec.Situation = e.value
		case DecisionFileKeyDeciding:
			rec.Deciding = e.value
		case DecisionFileKeyDefault:
			rec.Default = e.value
		case DecisionFileKeyDoc:
			rec.Links.Doc = e.value
		case DecisionFileKeyAutomaton:
			rec.Links.Automaton = e.value
		case DecisionFileKeyRegistryRow:
			rec.Links.RegistryRow = e.value
		case DecisionFileKeyOption:
			parts := strings.SplitN(e.value, fieldSep, 3)
			if len(parts) < 2 {
				add("line %d: %q needs the form id %s label %s tradeoff", e.line, fileName[e.canon], fieldSep, fieldSep)
				continue
			}
			opt := DecisionOption{ID: strings.TrimSpace(parts[0]), Label: strings.TrimSpace(parts[1])}
			if len(parts) == 3 {
				opt.Tradeoff = strings.TrimSpace(parts[2])
			}
			rec.Options = append(rec.Options, opt)
		case DecisionFileKeyRecommendation:
			parts := strings.SplitN(e.value, fieldSep, 2)
			if len(parts) < 2 {
				add("line %d: %q needs the form option id %s reason", e.line, fileName[e.canon], fieldSep)
				continue
			}
			rec.Recommendation = &DecisionRecommendation{
				OptionID: strings.TrimSpace(parts[0]),
				Reason:   strings.TrimSpace(parts[1]),
			}
		}
	}

	required := []string{DecisionFileKeyTitle, DecisionFileKeySituation, DecisionFileKeyDeciding, DecisionFileKeyDefault}
	for _, canon := range required {
		if seen[canon] == 0 {
			add("the required field %q is missing", fileName[canon])
		}
	}
	if len(rec.Options) == 0 {
		add("the required field %q is missing; give at least %d lines of it", fileName[DecisionFileKeyOption], decisionOptionsMin)
	}
	if len(problems) > 0 {
		return DecisionRecord{}, fmt.Errorf("%w: %s", ErrDecisionFileInvalid, strings.Join(problems, "; "))
	}

	if err := ValidateNewDecisionRecord(&rec); err != nil {
		return DecisionRecord{}, fmt.Errorf("%w: %w", ErrDecisionFileInvalid, err)
	}
	rec.ContentSHA256 = rec.ContentHash()
	return rec, nil
}

// decisionFileKeyNames returns the name a file uses for each canonical key:
// the configured rename, else the canonical name. Two keys that end up with
// the same name cannot be told apart, so that is an error.
func decisionFileKeyNames(renames map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(decisionFileKeys))
	owner := make(map[string]string, len(decisionFileKeys))
	for _, canon := range decisionFileKeys {
		name := strings.TrimSpace(renames[canon])
		if name == "" {
			name = canon
		}
		low := strings.ToLower(name)
		if prev, dup := owner[low]; dup {
			return nil, fmt.Errorf("%w: front_matter_keys gives %q and %q the same name %q", ErrDecisionFileInvalid, prev, canon, name)
		}
		owner[low] = canon
		out[canon] = name
	}
	return out, nil
}

// FolderStatus reports the folder watch: whether it is on, the folder, and
// what the last scan found. Before the first scan the counts are empty.
func (s *DecisionRecords) FolderStatus() DecisionFolderStatus {
	folder := strings.TrimSpace(s.decisionsConfig().EffectiveFolder())
	enabled := folder != "" && s.available() == nil
	s.folder.mu.Lock()
	last := s.folder.last
	s.folder.mu.Unlock()
	out := DecisionFolderStatus{Enabled: enabled, Path: folder, Errors: []DecisionFolderProblem{}, Notes: []DecisionFolderProblem{}}
	if last.Path == folder && last.LastScan != nil {
		out.FilesSeen = last.FilesSeen
		out.Errors = append(out.Errors, last.Errors...)
		out.Notes = append(out.Notes, last.Notes...)
		out.LastScan = cloneTime(last.LastScan)
	}
	return out
}

// ScanFolder runs one scan of the decision folder and returns the status it
// left. With no folder configured, or while a switch the decision records
// need is off, it touches nothing.
func (s *DecisionRecords) ScanFolder() DecisionFolderStatus {
	cfg := s.decisionsConfig()
	folder := strings.TrimSpace(cfg.EffectiveFolder())
	if folder == "" || s.available() != nil {
		return s.FolderStatus()
	}
	s.folder.scanMu.Lock()
	defer s.folder.scanMu.Unlock()

	st := DecisionFolderStatus{Enabled: true, Path: folder, Errors: []DecisionFolderProblem{}, Notes: []DecisionFolderProblem{}}
	fail := func(file, format string, args ...any) {
		st.Errors = append(st.Errors, DecisionFolderProblem{File: file, Message: fmt.Sprintf(format, args...)})
	}
	note := func(file, format string, args ...any) {
		st.Notes = append(st.Notes, DecisionFolderProblem{File: file, Message: fmt.Sprintf(format, args...)})
	}
	finish := func() DecisionFolderStatus {
		at := s.now()
		st.LastScan = &at
		s.folder.mu.Lock()
		prev := s.folder.last
		s.folder.last = st
		s.folder.mu.Unlock()
		logNewFolderProblems(prev, st)
		return s.FolderStatus()
	}

	glob := cfg.EffectiveFolderGlob()
	if _, err := filepath.Match(glob, "x"); err != nil {
		fail("", "folder_glob %q is not a valid pattern", glob)
		return finish()
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		// Nothing was read, so nothing is withdrawn: an unreadable or
		// missing folder must not look like every file being deleted.
		fail("", "the decision folder cannot be read: %s", plainFileError(err))
		return finish()
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if ok, _ := filepath.Match(glob, name); !ok {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 {
			fail(name, "the file is a symbolic link, so it was skipped")
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		if st.FilesSeen >= decisionFolderMaxFiles {
			fail("", "more than %d files match; only the first %d (by name) are read", decisionFolderMaxFiles, decisionFolderMaxFiles)
			break
		}
		st.FilesSeen++
		s.scanOneFile(cfg, folder, name, fail, note)
	}

	s.withdrawDeletedFiles(cfg, folder, fail)
	return finish()
}

// scanOneFile reads one decision file and applies it to its record.
func (s *DecisionRecords) scanOneFile(cfg config.AutonomousDecisionsConfig, folder, name string,
	fail, note func(file, format string, args ...any)) {
	data, err := readDecisionFile(filepath.Join(folder, name))
	if err != nil {
		fail(name, "%s", plainFileError(err))
		return
	}
	rec, err := ParseDecisionFile(data, cfg)
	if err != nil {
		fail(name, "%s", plainDecisionMessage(err))
		return
	}
	hash := rec.ContentHash()

	existing, found := s.records.GetBySource(DecisionSourceFile, name)
	if !found {
		if _, _, err := s.Create(rec, DecisionSourceFile, name); err != nil {
			fail(name, "%s", plainDecisionMessage(err))
		}
		return
	}
	switch existing.State {
	case DecisionRecordOpen:
		if existing.ContentSHA256 == hash {
			return
		}
		if _, _, err := s.Update(existing.ID, rec); err != nil {
			if errors.Is(err, ErrDecisionRecordClosed) {
				note(name, "the file changed, but its decision was already decided or withdrawn, so it was not changed")
				return
			}
			fail(name, "%s", plainDecisionMessage(err))
		}
	case DecisionRecordAnswered:
		if existing.ContentSHA256 != hash {
			note(name, "the file was edited after its decision was answered; the answered decision is not re-opened (use a new file for a new decision)")
		}
	case DecisionRecordWithdrawn:
		note(name, "its decision was withdrawn and is not raised again; use a new file name to raise it again")
	}
}

// withdrawDeletedFiles withdraws the open records whose file is gone.
func (s *DecisionRecords) withdrawDeletedFiles(cfg config.AutonomousDecisionsConfig, folder string,
	fail func(file, format string, args ...any)) {
	if !cfg.EffectiveFolderWithdrawOnDelete() {
		return
	}
	for _, rec := range s.records.List(DecisionRecordFilter{State: DecisionRecordOpen, Limit: maxDecisionListLimit}) {
		if rec.Source != DecisionSourceFile || rec.SourceRef == "" {
			continue
		}
		if filepath.Base(rec.SourceRef) != rec.SourceRef {
			continue // not a name this scan wrote
		}
		if _, err := os.Lstat(filepath.Join(folder, rec.SourceRef)); !errors.Is(err, fs.ErrNotExist) {
			continue // still there, or unreadable for another reason: not deleted
		}
		if _, err := s.Withdraw(rec.ID, decisionSyncActor, "its file "+rec.SourceRef+" was deleted"); err != nil && !errors.Is(err, ErrDecisionRecordClosed) {
			fail(rec.SourceRef, "the file was deleted but its decision could not be withdrawn: %s", plainDecisionMessage(err))
		}
	}
}

// readDecisionFile reads a regular file of at most decisionFileMaxBytes. It
// refuses a symbolic link, also one swapped in after the directory listing.
func readDecisionFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errDecisionFileNotRegular
	}
	f, err := os.Open(path) // #nosec G304 -- the name came from listing the configured decision folder
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errDecisionFileNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, decisionFileMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > decisionFileMaxBytes {
		return nil, errDecisionFileTooLarge
	}
	return data, nil
}

var (
	errDecisionFileNotRegular = errors.New("the file is not a regular file, so it was skipped")
	errDecisionFileTooLarge   = fmt.Errorf("the file is larger than %d KiB, so it was skipped", decisionFileMaxBytes/1024)
)

// plainFileError words a file system error without the folder path in it.
func plainFileError(err error) string {
	switch {
	case errors.Is(err, errDecisionFileNotRegular), errors.Is(err, errDecisionFileTooLarge):
		return err.Error()
	case errors.Is(err, fs.ErrNotExist):
		return "the file or folder does not exist"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// plainDecisionMessage words a decision error for the status: the sentinel
// prefixes are dropped, so the message reads as what to fix.
func plainDecisionMessage(err error) string {
	msg := err.Error()
	for _, p := range []string{ErrDecisionFileInvalid.Error() + ": ", ErrInvalidDecisionRecord.Error() + ": "} {
		msg = strings.ReplaceAll(msg, p, "")
	}
	return msg
}

// logNewFolderProblems logs each problem and note that the previous scan did
// not already have, so a file that stays broken is logged once, not on every
// poll.
func logNewFolderProblems(prev, cur DecisionFolderStatus) {
	had := make(map[string]bool, len(prev.Errors)+len(prev.Notes))
	for _, p := range prev.Errors {
		had[p.File+"\x00"+p.Message] = true
	}
	for _, p := range prev.Notes {
		had[p.File+"\x00"+p.Message] = true
	}
	for _, p := range append(append([]DecisionFolderProblem(nil), cur.Errors...), cur.Notes...) {
		if !had[p.File+"\x00"+p.Message] {
			if p.File == "" {
				log.Printf("[autonomous] decision folder: %s", p.Message)
			} else {
				log.Printf("[autonomous] decision folder: %s: %s", p.File, p.Message)
			}
		}
	}
}

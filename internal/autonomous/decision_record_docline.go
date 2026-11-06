package autonomous

// The status line rewrite of a decision record's linked document.
//
// When an answered record links a document (links.doc), the answer can be
// written back to one line of that document, the status line. Which line,
// what it is rewritten to and whether the file is touched at all are all
// configuration (autonomous.decisions.doc_status_mode, doc_status_pattern,
// doc_status_template, doc_root).
//
// Modes:
//
//	off     do nothing and record nothing
//	report  (the default) write nothing to the document; store the change that
//	        would be made on the record so the operator sees it
//	on      apply the change: a temp file in the same directory, renamed over
//	        the document, only when the content still hashes to what was read
//
// The daemon never runs git. In mode on the edit sits in the working tree
// where git diff shows it. Only the one status line changes; every other byte
// of the file, including the line endings, stays identical.
//
// Whatever the outcome, it is stored on the record with RecordDocChange.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// decisionDocMaxReadBytes is the most a document may hold to be rewritten.
const decisionDocMaxReadBytes = 1 << 20 // 1 MiB

// docLineBeforeWrite is a test seam: it runs after the document was read and
// before the content hash is checked again for the write.
var docLineBeforeWrite func()

// docProblem is a plain-language reason the document was not changed. It is
// stored on the record for the operator to read.
type docProblem string

func (p docProblem) Error() string { return string(p) }

// docStatusPatterns caches compiled status patterns so each one is compiled
// once.
var docStatusPatterns sync.Map // string -> *regexp.Regexp

func compileDocStatusPattern(p string) (*regexp.Regexp, error) {
	if v, ok := docStatusPatterns.Load(p); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, err
	}
	docStatusPatterns.Store(p, re)
	return re, nil
}

// rewriteDocStatusLine computes, and in mode on applies, the change to the
// status line of the document the record links to, then stores the outcome on
// the record. It does nothing when the mode is off or the record has no
// document link.
func (s *DecisionRecords) rewriteDocStatusLine(rec DecisionRecord, _ Question, a Answer) {
	if rec.Links.Doc == "" {
		return
	}
	cfg := s.decisionsConfig()
	mode := cfg.EffectiveDocStatusMode()
	if mode == config.DecisionDocStatusModeOff {
		return
	}
	if mode != config.DecisionDocStatusModeOn {
		mode = config.DecisionDocStatusModeReport // an unknown mode never writes
	}
	at := s.now().UTC()
	change := computeDocStatusChange(docStatusInput{
		Mode:     mode,
		Root:     cfg.EffectiveDocRoot(),
		Doc:      rec.Links.Doc,
		Pattern:  cfg.EffectiveDocStatusPattern(),
		Template: cfg.EffectiveDocStatusTemplate(),
		Rec:      rec,
		Answer:   a,
		Now:      at,
	})
	change.At = &at
	if _, err := s.records.RecordDocChange(rec.ID, change); err != nil {
		log.Printf("[autonomous] decision record %s: store document change: %v", rec.ID, err)
	}
}

// docStatusInput is everything the status line change is computed from.
type docStatusInput struct {
	Mode     string
	Root     string
	Doc      string
	Pattern  string
	Template string
	Rec      DecisionRecord
	Answer   Answer
	Now      time.Time
}

// computeDocStatusChange does the whole job and returns the result to store.
// In mode on it also writes the file when exactly one line matched and the
// line really changes.
func computeDocStatusChange(in docStatusInput) DecisionDocChange {
	out := DecisionDocChange{Mode: in.Mode, Path: in.Doc}
	fail := func(format string, args ...any) DecisionDocChange {
		out.Status = DecisionDocError
		out.Diff = fmt.Sprintf(format, args...)
		return out
	}

	re, err := compileDocStatusPattern(in.Pattern)
	if err != nil {
		return fail("The status line pattern is not a valid regular expression: %v", err)
	}
	full, err := resolveDecisionDoc(in.Root, in.Doc)
	if err != nil {
		return fail("%v", err)
	}
	data, info, err := readDecisionDoc(full)
	if err != nil {
		return fail("%v", err)
	}
	if docLineBeforeWrite != nil {
		docLineBeforeWrite()
	}
	sum := sha256.Sum256(data)

	lines := splitKeepingEnds(data)
	var hits []int
	for i, l := range lines {
		if re.MatchString(lineBody(l)) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		out.Status = DecisionDocNoMatch
		out.Diff = "No line in the document matches the status line pattern, so nothing was changed."
		return out
	case 1:
	default:
		nums := make([]string, len(hits))
		for i, h := range hits {
			nums[i] = strconv.Itoa(h + 1)
		}
		out.Status = DecisionDocAmbiguous
		out.Diff = "More than one line matches the status line pattern (lines " + strings.Join(nums, ", ") +
			"), so nothing was changed."
		return out
	}

	idx := hits[0]
	old := lines[idx]
	oldBody := lineBody(old)
	newBody := renderDocStatusLine(in)
	out.Line = idx + 1
	out.Before = oldBody
	out.After = newBody
	if newBody == oldBody {
		out.Status = DecisionDocUnchanged
		return out
	}
	out.Diff = docLineDiff(in.Doc, out.Line, oldBody, newBody)
	if len(out.Before) > decisionDocFieldMaxBytes || len(out.After) > decisionDocFieldMaxBytes ||
		len(out.Diff) > decisionDocFieldMaxBytes {
		out.Before, out.After = clipDocField(out.Before), clipDocField(out.After)
		out.Diff = "The status line is too long to store, so nothing was changed."
		out.Status = DecisionDocError
		return out
	}

	if in.Mode != config.DecisionDocStatusModeOn {
		out.Status = DecisionDocReported
		return out
	}

	newLine := newBody + old[len(oldBody):] // keep the line's own ending
	var buf bytes.Buffer
	buf.Grow(len(data) + len(newLine))
	for i, l := range lines {
		if i == idx {
			buf.WriteString(newLine)
		} else {
			buf.WriteString(l)
		}
	}
	if err := writeDecisionDocIfUnchanged(full, sum, buf.Bytes(), info.Mode().Perm()); err != nil {
		out.Status = DecisionDocError
		out.Diff = err.Error()
		return out
	}
	out.Status = DecisionDocApplied
	return out
}

func clipDocField(s string) string {
	if len(s) > decisionDocFieldMaxBytes {
		return s[:decisionDocFieldMaxBytes]
	}
	return s
}

// resolveDecisionDoc turns the repository-relative document path into the
// real path of a file inside the document root. An absolute path, a '..'
// segment, a path that leaves the root, or a symlink that leaves the root is
// refused with a plain message.
func resolveDecisionDoc(root, doc string) (string, error) {
	if doc == "" {
		return "", docProblem("The record has no document path.")
	}
	if filepath.IsAbs(doc) || strings.HasPrefix(doc, "/") || strings.HasPrefix(doc, "\\") {
		return "", docProblem("The document path must be relative to the document folder, not absolute.")
	}
	if strings.ContainsRune(doc, 0) {
		return "", docProblem("The document path contains a null byte.")
	}
	if err := checkDecisionDocPath(doc); err != nil {
		return "", docProblem("The document path is not allowed: it must stay inside the document folder.")
	}
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", docProblem("The document folder could not be found: the working directory is unknown.")
		}
		root = wd
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", docProblem("The document folder is not a usable path.")
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", docProblem("The document folder does not exist or cannot be read.")
	}
	real, err := filepath.EvalSymlinks(filepath.Join(realRoot, filepath.FromSlash(doc)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", docProblem("The document does not exist inside the document folder.")
		}
		return "", docProblem("The document path could not be resolved.")
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", docProblem("The document path leaves the document folder, so it was refused.")
	}
	return real, nil
}

// readDecisionDoc reads a regular file of at most 1 MiB.
func readDecisionDoc(path string) ([]byte, os.FileInfo, error) {
	f, err := os.Open(path) // #nosec G304 -- resolveDecisionDoc confined the path to the document root
	if err != nil {
		return nil, nil, docProblem("The document could not be opened.")
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, docProblem("The document could not be read.")
	}
	if !info.Mode().IsRegular() {
		return nil, nil, docProblem("The document path is not a regular file.")
	}
	data, err := io.ReadAll(io.LimitReader(f, decisionDocMaxReadBytes+1))
	if err != nil {
		return nil, nil, docProblem("The document could not be read.")
	}
	if len(data) > decisionDocMaxReadBytes {
		return nil, nil, docProblem("The document is larger than 1 MiB, so it was not read.")
	}
	return data, info, nil
}

// writeDecisionDocIfUnchanged replaces the file with content, atomically,
// only when the file still hashes to want. It keeps the permission bits.
func writeDecisionDocIfUnchanged(path string, want [sha256.Size]byte, content []byte, perm os.FileMode) error {
	now, _, err := readDecisionDoc(path)
	if err != nil {
		return docProblem("The document could not be read again before writing, so nothing was changed.")
	}
	if sha256.Sum256(now) != want {
		return docProblem("The document changed while the change was being prepared, so nothing was written.")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return docProblem("The document folder is not writable, so nothing was changed.")
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return docProblem("The new document could not be written, so nothing was changed.")
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return docProblem("The new document could not be written, so nothing was changed.")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return docProblem("The new document could not be written, so nothing was changed.")
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return docProblem("The new document could not be written, so nothing was changed.")
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return docProblem("The new document could not be moved into place, so nothing was changed.")
	}
	return nil
}

// splitKeepingEnds splits data into lines, each with its own line ending
// (\n, \r\n, or none for a last line without one), so joining them gives the
// data back byte for byte.
func splitKeepingEnds(data []byte) []string {
	var out []string
	s := string(data)
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// lineBody is the line without its line ending.
func lineBody(l string) string {
	l = strings.TrimSuffix(l, "\n")
	return strings.TrimSuffix(l, "\r")
}

// renderDocStatusLine fills the template. Every value is made one line, and
// the template is filled in a single pass so a value that looks like a
// placeholder is not expanded again.
func renderDocStatusLine(in docStatusInput) string {
	label := ""
	for _, o := range in.Rec.Options {
		if o.ID == in.Answer.ChosenOptionID {
			label = o.Label
			break
		}
	}
	when := in.Now
	if in.Answer.CreatedAt != nil {
		when = *in.Answer.CreatedAt
	}
	r := strings.NewReplacer(
		"{choice}", oneLine(in.Answer.ChosenOptionID),
		"{choice_label}", oneLine(label),
		"{quote}", oneLine(in.Answer.Quote),
		"{actor}", oneLine(in.Answer.Actor),
		"{date}", when.UTC().Format("2006-01-02"),
		"{decision_id}", oneLine(in.Rec.ID),
	)
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(r.Replace(in.Template))
}

// oneLine collapses every whitespace run, newlines included, to one space and
// trims the ends.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// docLineDiff is a small unified-style diff of one changed line.
func docLineDiff(path string, line int, before, after string) string {
	return "--- " + path + "\n+++ " + path + "\n@@ -" + strconv.Itoa(line) + " +" + strconv.Itoa(line) + " @@\n-" +
		before + "\n+" + after + "\n"
}

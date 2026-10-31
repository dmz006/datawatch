package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const folderFileDefault = `---
# a comment, then a blank line

label: storage
title: Pick the storage engine
situation: The service keeps its state in memory and loses it on every restart,
  so the operator has to re-enter the same settings each time it comes back.
deciding: Which storage engine the service uses from now on.
option: sqlite | Use SQLite | simple, one writer at a time
option: files | Keep append-only files | no new dependency, slow queries
recommendation: sqlite | queries stay fast
default: hold
doc: docs/plans/storage.md
automaton: ee660001
registry_row: row-12
---
Everything after the closing line is ignored.
option: ignored | Ignored | not read
`

const folderFilePlus = `+++
label = storage
heading = Pick the storage engine
background = The service keeps its state in memory and loses it on every restart,
  so the operator has to re-enter the same settings each time it comes back.
question = Which storage engine the service uses from now on.
choice = sqlite ; Use SQLite ; simple, one writer at a time
choice = files ; Keep append-only files ; no new dependency, slow queries
advice = sqlite ; queries stay fast
fallback = hold
document = docs/plans/storage.md
automaton = ee660001
registry_row = row-12
+++
Body text is not read.
`

func plusConfig() config.AutonomousDecisionsConfig {
	delim, sep, fieldSep := "+++", "=", ";"
	return config.AutonomousDecisionsConfig{
		FrontMatterDelimiter:      &delim,
		FrontMatterSeparator:      &sep,
		FrontMatterFieldSeparator: &fieldSep,
		FrontMatterKeys: map[string]string{
			"title":          "heading",
			"situation":      "background",
			"deciding":       "question",
			"option":         "choice",
			"recommendation": "advice",
			"default":        "fallback",
			"doc":            "document",
		},
	}
}

// folderRig is a sync rig with the decision folder set to a temp directory.
func folderRig(t *testing.T) (*decisionSyncRig, string) {
	t.Helper()
	r := newDecisionSyncRig(t)
	dir := t.TempDir()
	r.dcfg.Folder = dir
	return r, dir
}

func writeFolderFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func fileRecords(r *decisionSyncRig) []DecisionRecord {
	var out []DecisionRecord
	for _, rec := range r.records.List(DecisionRecordFilter{Limit: maxDecisionListLimit}) {
		if rec.Source == DecisionSourceFile {
			out = append(out, rec)
		}
	}
	return out
}

func folderErrorFor(st DecisionFolderStatus, file string) (string, bool) {
	for _, e := range st.Errors {
		if e.File == file {
			return e.Message, true
		}
	}
	return "", false
}

func TestDecisionFolder_ParsesFullDefaultFile(t *testing.T) {
	rec, err := ParseDecisionFile([]byte(folderFileDefault), config.AutonomousDecisionsConfig{})
	if err != nil {
		t.Fatalf("ParseDecisionFile: %v", err)
	}
	if rec.Label != "storage" || rec.Title != "Pick the storage engine" {
		t.Fatalf("label/title = %q/%q", rec.Label, rec.Title)
	}
	wantSituation := "The service keeps its state in memory and loses it on every restart, " +
		"so the operator has to re-enter the same settings each time it comes back."
	if rec.Situation != wantSituation {
		t.Fatalf("situation = %q, want the continued lines joined: %q", rec.Situation, wantSituation)
	}
	if rec.Deciding != "Which storage engine the service uses from now on." {
		t.Fatalf("deciding = %q", rec.Deciding)
	}
	wantOpts := []DecisionOption{
		{ID: "sqlite", Label: "Use SQLite", Tradeoff: "simple, one writer at a time"},
		{ID: "files", Label: "Keep append-only files", Tradeoff: "no new dependency, slow queries"},
	}
	if !reflect.DeepEqual(rec.Options, wantOpts) {
		t.Fatalf("options = %+v, want %+v", rec.Options, wantOpts)
	}
	if rec.Recommendation == nil || rec.Recommendation.OptionID != "sqlite" || rec.Recommendation.Reason != "queries stay fast" {
		t.Fatalf("recommendation = %+v", rec.Recommendation)
	}
	if rec.Default != "hold" {
		t.Fatalf("default = %q", rec.Default)
	}
	if rec.Links != (DecisionLinks{Doc: "docs/plans/storage.md", Automaton: "ee660001", RegistryRow: "row-12"}) {
		t.Fatalf("links = %+v", rec.Links)
	}
	if rec.ContentSHA256 == "" || rec.ContentSHA256 != rec.ContentHash() {
		t.Fatalf("content hash = %q, want the hash of the content", rec.ContentSHA256)
	}
}

func TestDecisionFolder_MissingRequiredFieldIsNamedError(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"title", strings.Replace(folderFileDefault, "title: Pick the storage engine\n", "", 1), `"title"`},
		{"deciding", strings.Replace(folderFileDefault, "deciding: Which storage engine the service uses from now on.\n", "", 1), `"deciding"`},
		{"default", strings.Replace(folderFileDefault, "default: hold\n", "", 1), `"default"`},
		{"options", strings.NewReplacer(
			"option: sqlite | Use SQLite | simple, one writer at a time\n", "",
			"option: files | Keep append-only files | no new dependency, slow queries\n", "",
			"recommendation: sqlite | queries stay fast\n", "").Replace(folderFileDefault), `"option"`},
		{"unclosed block", "---\ntitle: x\n", "not closed"},
		{"no opening line", "title: x\n", "must start with"},
		{"line without separator", "---\ntitle x\n---\n", "no \":\""},
		{"bad option line", strings.Replace(folderFileDefault, "option: files | Keep append-only files | no new dependency, slow queries", "option: files", 1), "id | label | tradeoff"},
		{"repeated key", strings.Replace(folderFileDefault, "default: hold\n", "default: hold\ndefault: sqlite\n", 1), "more than once"},
	}
	for _, c := range cases {
		_, err := ParseDecisionFile([]byte(c.content), config.AutonomousDecisionsConfig{})
		if err == nil {
			t.Errorf("%s: parsed without error", c.name)
			continue
		}
		if !errors.Is(err, ErrDecisionFileInvalid) {
			t.Errorf("%s: err = %v, want ErrDecisionFileInvalid", c.name, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %q, want it to mention %q", c.name, err, c.want)
		}
	}
	// A block that parses but is not a valid record keeps the record rules.
	_, err := ParseDecisionFile([]byte(strings.Replace(folderFileDefault, "default: hold", "default: nowhere", 1)), config.AutonomousDecisionsConfig{})
	if !errors.Is(err, ErrInvalidDecisionRecord) || !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("unknown default: err = %v, want ErrInvalidDecisionRecord naming it", err)
	}
}

func TestDecisionFolder_DifferentFrontMatterPattern(t *testing.T) {
	want, err := ParseDecisionFile([]byte(folderFileDefault), config.AutonomousDecisionsConfig{})
	if err != nil {
		t.Fatalf("default-format file: %v", err)
	}
	got, err := ParseDecisionFile([]byte(folderFilePlus), plusConfig())
	if err != nil {
		t.Fatalf("plus-format file: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plus-format record\n %+v\nwant the default-format record\n %+v", got, want)
	}
	// The same file read with the default pattern is not a decision file.
	if _, err := ParseDecisionFile([]byte(folderFilePlus), config.AutonomousDecisionsConfig{}); err == nil {
		t.Fatal("a +++ file parsed under the default delimiter")
	}
	// Two keys renamed to one name cannot be told apart.
	bad := plusConfig()
	bad.FrontMatterKeys["situation"] = "heading"
	if _, err := ParseDecisionFile([]byte(folderFilePlus), bad); !errors.Is(err, ErrDecisionFileInvalid) {
		t.Fatalf("clashing key names: err = %v, want ErrDecisionFileInvalid", err)
	}
}

func TestDecisionFolder_NewFileCreatesOneRecordAndOneQuestion(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	writeFolderFile(t, dir, "notes.txt", "not matched by *.md")

	st := r.svc.ScanFolder()
	if !st.Enabled || st.Path != dir || st.FilesSeen != 1 || len(st.Errors) != 0 || st.LastScan == nil {
		t.Fatalf("status = %+v, want enabled, 1 file seen, no errors, a scan time", st)
	}
	recs := fileRecords(r)
	if len(recs) != 1 {
		t.Fatalf("%d file records, want 1", len(recs))
	}
	rec := recs[0]
	if rec.Source != DecisionSourceFile || rec.SourceRef != "storage.md" || rec.State != DecisionRecordOpen || rec.QuestionID == "" {
		t.Fatalf("record = %+v, want open, source file, ref storage.md, with a question", rec)
	}
	if n := len(r.openQuestions(t, rec.ID)); n != 1 {
		t.Fatalf("%d questions for the record, want 1", n)
	}
	if got := r.svc.FolderStatus(); got.FilesSeen != 1 || !got.Enabled {
		t.Fatalf("FolderStatus() = %+v", got)
	}
}

func TestDecisionFolder_UnchangedFileChangesNothing(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	r.svc.ScanFolder()
	before := fileRecords(r)[0]
	qBefore, _ := r.questions.Get(before.QuestionID)

	r.advance(time.Hour)
	for i := 0; i < 3; i++ {
		if st := r.svc.ScanFolder(); len(st.Errors) != 0 {
			t.Fatalf("scan %d: errors %+v", i, st.Errors)
		}
	}
	after, _ := r.records.Get(before.ID)
	qAfter, _ := r.questions.Get(before.QuestionID)
	if !after.UpdatedAt.Equal(*before.UpdatedAt) || after.ContentSHA256 != before.ContentSHA256 {
		t.Fatalf("record rewritten by an unchanged file: before %+v after %+v", before, after)
	}
	if !reflect.DeepEqual(qBefore, qAfter) {
		t.Fatalf("question changed by an unchanged file:\n before %+v\n after  %+v", qBefore, qAfter)
	}
	if n := len(fileRecords(r)); n != 1 {
		t.Fatalf("%d records after repeated scans, want 1", n)
	}
}

func TestDecisionFolder_EditedFileUpdatesSameQuestion(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	r.svc.ScanFolder()
	before := fileRecords(r)[0]

	r.advance(time.Minute)
	writeFolderFile(t, dir, "storage.md", strings.Replace(folderFileDefault, "Pick the storage engine", "Choose the storage engine", 1))
	if st := r.svc.ScanFolder(); len(st.Errors) != 0 {
		t.Fatalf("errors %+v", st.Errors)
	}
	recs := fileRecords(r)
	if len(recs) != 1 || recs[0].ID != before.ID {
		t.Fatalf("records = %+v, want the same single record", recs)
	}
	if recs[0].Title != "Choose the storage engine" || recs[0].ContentSHA256 == before.ContentSHA256 {
		t.Fatalf("record not updated: %+v", recs[0])
	}
	qs := r.openQuestions(t, before.ID)
	if len(qs) != 1 || qs[0].ID != before.QuestionID {
		t.Fatalf("questions = %+v, want the same single question %s", qs, before.QuestionID)
	}
	if !strings.HasPrefix(qs[0].Text, "Choose the storage engine\n\n") || qs[0].State != QuestionOpen {
		t.Fatalf("question text = %q, state %s; want the edited title on the open question", qs[0].Text, qs[0].State)
	}
}

func TestDecisionFolder_DeletedFileWithdrawsRecordAndQuestion(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	r.svc.ScanFolder()
	rec := fileRecords(r)[0]

	if err := os.Remove(filepath.Join(dir, "storage.md")); err != nil {
		t.Fatal(err)
	}
	if st := r.svc.ScanFolder(); len(st.Errors) != 0 {
		t.Fatalf("errors %+v", st.Errors)
	}
	got, _ := r.records.Get(rec.ID)
	q, _ := r.questions.Get(rec.QuestionID)
	if got.State != DecisionRecordWithdrawn || q.State != QuestionWithdrawn {
		t.Fatalf("record %s, question %s; want both withdrawn", got.State, q.State)
	}
	// A later scan neither errors nor resurrects it.
	if st := r.svc.ScanFolder(); len(st.Errors) != 0 {
		t.Fatalf("second scan errors %+v", st.Errors)
	}
	if got, _ := r.records.Get(rec.ID); got.State != DecisionRecordWithdrawn {
		t.Fatalf("record is %s after another scan", got.State)
	}
}

func TestDecisionFolder_DeletedFileKeptWhenWithdrawOnDeleteIsOff(t *testing.T) {
	r, dir := folderRig(t)
	off := false
	r.dcfg.FolderWithdrawOnDelete = &off
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	r.svc.ScanFolder()
	rec := fileRecords(r)[0]

	if err := os.Remove(filepath.Join(dir, "storage.md")); err != nil {
		t.Fatal(err)
	}
	r.svc.ScanFolder()
	got, _ := r.records.Get(rec.ID)
	q, _ := r.questions.Get(rec.QuestionID)
	if got.State != DecisionRecordOpen || q.State != QuestionOpen {
		t.Fatalf("record %s, question %s; want both still open", got.State, q.State)
	}
}

func TestDecisionFolder_BadFileIsSkippedAndReportedAndGoodFileStillRuns(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "a-broken.md", "---\ntitle: Only a title\n---\n")
	writeFolderFile(t, dir, "b-good.md", folderFileDefault)
	writeFolderFile(t, dir, "c-not-a-decision.md", "just some prose, no block\n")

	st := r.svc.ScanFolder()
	if st.FilesSeen != 3 {
		t.Fatalf("files seen = %d, want 3", st.FilesSeen)
	}
	recs := fileRecords(r)
	if len(recs) != 1 || recs[0].SourceRef != "b-good.md" {
		t.Fatalf("records = %+v, want only b-good.md", recs)
	}
	msg, ok := folderErrorFor(st, "a-broken.md")
	if !ok || !strings.Contains(msg, `"situation"`) || strings.Contains(msg, dir) {
		t.Fatalf("a-broken.md error = %q (found %v), want a plain message naming the missing field and no path", msg, ok)
	}
	if _, ok := folderErrorFor(st, "c-not-a-decision.md"); !ok {
		t.Fatalf("c-not-a-decision.md missing from errors %+v", st.Errors)
	}
	if _, ok := folderErrorFor(st, "b-good.md"); ok {
		t.Fatalf("the good file was reported as an error: %+v", st.Errors)
	}
	// Fixing the file clears its error on the next scan.
	writeFolderFile(t, dir, "a-broken.md", strings.Replace(folderFileDefault, "Pick the storage engine", "Pick another engine", 1))
	st = r.svc.ScanFolder()
	if _, ok := folderErrorFor(st, "a-broken.md"); ok {
		t.Fatalf("a-broken.md still reported after the fix: %+v", st.Errors)
	}
	if n := len(fileRecords(r)); n != 2 {
		t.Fatalf("%d records after the fix, want 2", n)
	}
}

func TestDecisionFolder_SymlinkAndOversizedFilesAreSkipped(t *testing.T) {
	r, dir := folderRig(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte(folderFileDefault), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	big := folderFileDefault + "# " + strings.Repeat("x", decisionFileMaxBytes) + "\n"
	writeFolderFile(t, dir, "big.md", big)
	// Exactly at the cap is still read.
	pad := decisionFileMaxBytes - len(folderFileDefault) - len("# \n")
	atCap := folderFileDefault + "# " + strings.Repeat("y", pad) + "\n"
	if len(atCap) != decisionFileMaxBytes {
		t.Fatalf("test file is %d bytes, want %d", len(atCap), decisionFileMaxBytes)
	}
	writeFolderFile(t, dir, "cap.md", atCap)
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFolderFile(t, filepath.Join(dir, "sub.md"), "nested.md", folderFileDefault)

	st := r.svc.ScanFolder()
	recs := fileRecords(r)
	if len(recs) != 1 || recs[0].SourceRef != "cap.md" {
		t.Fatalf("records = %+v, want only cap.md (the link, the oversized file and the folder are skipped)", recs)
	}
	for _, name := range []string{"link.md", "big.md"} {
		if _, ok := folderErrorFor(st, name); !ok {
			t.Errorf("%s not reported in errors %+v", name, st.Errors)
		}
	}
	if st.FilesSeen != 2 { // cap.md and big.md; the link is never opened
		t.Fatalf("files seen = %d, want 2", st.FilesSeen)
	}
}

func TestDecisionFolder_EmptyFolderMeansNoScan(t *testing.T) {
	r := newDecisionSyncRig(t)
	dir := t.TempDir()
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	// The folder key is empty: the file sits elsewhere and nothing is read.
	st := r.svc.ScanFolder()
	if st.Enabled || st.Path != "" || st.FilesSeen != 0 || st.LastScan != nil || len(st.Errors) != 0 {
		t.Fatalf("status = %+v, want an idle watch", st)
	}
	if n := len(fileRecords(r)); n != 0 {
		t.Fatalf("%d records from an unset folder", n)
	}
	m := &Manager{}
	m.SetDecisionRecords(r.svc)
	m.DecisionRecordTick(context.Background())
	if n := len(fileRecords(r)); n != 0 {
		t.Fatalf("%d records after a tick with the folder unset", n)
	}
	// A switch that is off stops the scan too.
	r.dcfg.Folder = dir
	r.qcfg.Enabled = false
	if st := r.svc.ScanFolder(); st.Enabled || len(fileRecords(r)) != 0 {
		t.Fatalf("scan ran while questions are off: %+v", st)
	}
}

func TestDecisionFolder_AnsweredRecordIsNeverReopened(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	r.svc.ScanFolder()
	rec := fileRecords(r)[0]
	if _, err := r.questions.Answer(rec.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "SQLite"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r.svc.Reconcile() // applies the answer to the record
	if got, _ := r.records.Get(rec.ID); got.State != DecisionRecordAnswered {
		t.Fatalf("record is %s, want answered", got.State)
	}

	writeFolderFile(t, dir, "storage.md", strings.Replace(folderFileDefault, "Pick the storage engine", "Pick again", 1))
	st := r.svc.ScanFolder()
	got, _ := r.records.Get(rec.ID)
	if got.State != DecisionRecordAnswered || got.Title != "Pick the storage engine" {
		t.Fatalf("answered record changed: %+v", got)
	}
	if len(st.Notes) != 1 || st.Notes[0].File != "storage.md" || !strings.Contains(st.Notes[0].Message, "answered") {
		t.Fatalf("notes = %+v, want one note about the edit to the answered decision", st.Notes)
	}
	if n := len(fileRecords(r)); n != 1 {
		t.Fatalf("%d records, want 1 (no new record for the edit)", n)
	}
	// Deleting the file of an answered decision leaves it alone.
	if err := os.Remove(filepath.Join(dir, "storage.md")); err != nil {
		t.Fatal(err)
	}
	r.svc.ScanFolder()
	if got, _ := r.records.Get(rec.ID); got.State != DecisionRecordAnswered {
		t.Fatalf("answered record is %s after its file was deleted", got.State)
	}
}

func TestDecisionFolder_UnreadableFolderWithdrawsNothing(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	r.svc.ScanFolder()
	rec := fileRecords(r)[0]

	r.dcfg.Folder = filepath.Join(dir, "gone")
	st := r.svc.ScanFolder()
	if msg, ok := folderErrorFor(st, ""); !ok || strings.Contains(msg, dir) {
		t.Fatalf("folder error = %q (found %v), want a plain message without the path", msg, ok)
	}
	if got, _ := r.records.Get(rec.ID); got.State != DecisionRecordOpen {
		t.Fatalf("record is %s after the folder became unreadable, want open", got.State)
	}
}

func TestDecisionFolder_TickScansTheFolder(t *testing.T) {
	r, dir := folderRig(t)
	writeFolderFile(t, dir, "storage.md", folderFileDefault)
	m := &Manager{}
	m.SetDecisionRecords(r.svc)
	m.DecisionRecordTick(context.Background())
	recs := fileRecords(r)
	if len(recs) != 1 || recs[0].QuestionID == "" {
		t.Fatalf("records = %+v, want one linked record from the tick", recs)
	}
}

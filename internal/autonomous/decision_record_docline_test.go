package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const docLineRel = "docs/plans/storage.md"

// docLineRig is a decision rig whose document root is a temp directory that
// holds one document.
type docLineRig struct {
	*decisionSyncRig
	root string
	path string
}

func newDocLineRig(t *testing.T, mode, content string) *docLineRig {
	t.Helper()
	r, _ := newAnswerRig(t, true)
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(docLineRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	r.dcfg.DocRoot = root
	r.dcfg.DocStatusMode = &mode
	return &docLineRig{decisionSyncRig: r, root: root, path: path}
}

// answer creates a record that links the document and answers its question.
func (d *docLineRig) answer(t *testing.T, quote string) DecisionRecord {
	t.Helper()
	v := openedRecord(t, d.decisionSyncRig)
	if _, err := d.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: quote}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	rec, ok := d.records.Get(v.ID)
	if !ok {
		t.Fatal("record vanished")
	}
	return rec
}

func (d *docLineRig) content(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(d.path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const docLineBody = "# Storage\n\nSome text.\nConfirmed: pending\n\nMore text.\n"

func TestDecisionDocLine_ReportModeComputesAndLeavesFileIdentical(t *testing.T) {
	d := newDocLineRig(t, "report", docLineBody)
	rec := d.answer(t, "SQLite, please")
	if got := d.content(t); got != docLineBody {
		t.Fatalf("report mode changed the file: %q", got)
	}
	c := rec.DocChange
	if c == nil {
		t.Fatal("no doc change recorded")
	}
	if c.Status != DecisionDocReported || c.Mode != "report" || c.Path != docLineRel || c.Line != 4 {
		t.Fatalf("change = %+v, want reported/report/%s/line 4", *c, docLineRel)
	}
	if c.Before != "Confirmed: pending" {
		t.Fatalf("before = %q", c.Before)
	}
	date := rec.Answer.AnsweredAt.UTC().Format("2006-01-02")
	want := `Confirmed: sqlite - "SQLite, please" (alice, ` + date + `)`
	if c.After != want {
		t.Fatalf("after = %q, want %q", c.After, want)
	}
	if !strings.Contains(c.Diff, "-Confirmed: pending\n+"+want) || c.At == nil {
		t.Fatalf("diff = %q, at = %v", c.Diff, c.At)
	}
}

func TestDecisionDocLine_OnModeRewritesExactlyOneLine(t *testing.T) {
	d := newDocLineRig(t, "on", docLineBody)
	rec := d.answer(t, "SQLite, please")
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocApplied || c.Mode != "on" {
		t.Fatalf("change = %+v, want applied in mode on", c)
	}
	want := strings.Replace(docLineBody, "Confirmed: pending", c.After, 1)
	if got := d.content(t); got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	info, err := os.Stat(d.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("permission bits = %v, want 0640", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(d.path))
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the document (no temp file left)", len(entries))
	}
}

func TestDecisionDocLine_OffModeRecordsNothing(t *testing.T) {
	d := newDocLineRig(t, "off", docLineBody)
	rec := d.answer(t, "yes")
	if rec.DocChange != nil {
		t.Fatalf("off mode recorded %+v", *rec.DocChange)
	}
	if got := d.content(t); got != docLineBody {
		t.Fatalf("off mode changed the file: %q", got)
	}
}

func TestDecisionDocLine_NoMatch(t *testing.T) {
	const body = "# Storage\n\nNo status here.\n"
	d := newDocLineRig(t, "on", body)
	rec := d.answer(t, "yes")
	if rec.DocChange == nil || rec.DocChange.Status != DecisionDocNoMatch {
		t.Fatalf("change = %+v, want no_match", rec.DocChange)
	}
	if got := d.content(t); got != body {
		t.Fatalf("file changed: %q", got)
	}
}

func TestDecisionDocLine_AmbiguousWritesNothing(t *testing.T) {
	const body = "Confirmed: one\ntext\nConfirmed: two\n"
	d := newDocLineRig(t, "on", body)
	rec := d.answer(t, "yes")
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocAmbiguous {
		t.Fatalf("change = %+v, want ambiguous", c)
	}
	if !strings.Contains(c.Diff, "1, 3") {
		t.Fatalf("message %q does not name the matching lines", c.Diff)
	}
	if got := d.content(t); got != body {
		t.Fatalf("file changed: %q", got)
	}
}

func TestDecisionDocLine_NewLineEqualsOldIsUnchanged(t *testing.T) {
	d := newDocLineRig(t, "on", docLineBody)
	tmpl := "Confirmed: pending"
	d.dcfg.DocStatusTemplate = &tmpl
	rec := d.answer(t, "yes")
	if rec.DocChange == nil || rec.DocChange.Status != DecisionDocUnchanged || rec.DocChange.Line != 4 {
		t.Fatalf("change = %+v, want unchanged on line 4", rec.DocChange)
	}
	if got := d.content(t); got != docLineBody {
		t.Fatalf("file changed: %q", got)
	}
}

func TestDecisionDocLine_MultiLineQuoteIsCollapsed(t *testing.T) {
	d := newDocLineRig(t, "on", docLineBody)
	rec := d.answer(t, multiLineQuote)
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocApplied {
		t.Fatalf("change = %+v, want applied", c)
	}
	if !strings.Contains(c.After, `"Go with SQLite. Keep the files as a backup; do not drop them."`) {
		t.Fatalf("after = %q, want the quote on one line with single spaces", c.After)
	}
	if strings.ContainsAny(c.After, "\r\n") || strings.Count(d.content(t), "\n") != strings.Count(docLineBody, "\n") {
		t.Fatalf("the rewrite changed the line count: %q", d.content(t))
	}
}

func TestDecisionDocLine_RefusesPathsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("Confirmed: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{"../x.md", "a/../../x.md", secret, "link.md", "dir/secret.md", "/etc/passwd"} {
		c := computeDocStatusChange(docStatusInput{
			Mode: "on", Root: root, Doc: doc, Pattern: config.DefaultDecisionDocStatusPattern,
			Template: config.DefaultDecisionDocStatusTemplate, Answer: Answer{ChosenOptionID: "a"}, Now: time.Now(),
		})
		if c.Status != DecisionDocError || c.Diff == "" {
			t.Fatalf("path %q: change = %+v, want status error with a message", doc, c)
		}
	}
	if b, _ := os.ReadFile(secret); string(b) != "Confirmed: x\n" {
		t.Fatalf("the file outside the root was changed: %q", b)
	}
}

func TestDecisionDocLine_CustomPatternAndTemplateAreHonoured(t *testing.T) {
	const body = "Confirmed: not this one\nStatus: open\n"
	d := newDocLineRig(t, "on", body)
	pat, tmpl := `^Status:.*$`, "Status: {choice_label} [{choice}] by {actor} on {date} ({decision_id})"
	d.dcfg.DocStatusPattern, d.dcfg.DocStatusTemplate = &pat, &tmpl
	rec := d.answer(t, "go")
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocApplied || c.Line != 2 {
		t.Fatalf("change = %+v, want applied on line 2", c)
	}
	date := rec.Answer.AnsweredAt.UTC().Format("2006-01-02")
	want := "Status: Use SQLite [sqlite] by alice on " + date + " (" + rec.ID + ")"
	if c.After != want {
		t.Fatalf("after = %q, want %q", c.After, want)
	}
	if got := d.content(t); got != "Confirmed: not this one\n"+want+"\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestDecisionDocLine_InvalidPatternIsAnErrorNotAPanic(t *testing.T) {
	d := newDocLineRig(t, "on", docLineBody)
	bad := `([`
	d.dcfg.DocStatusPattern = &bad
	rec := d.answer(t, "go")
	if rec.DocChange == nil || rec.DocChange.Status != DecisionDocError {
		t.Fatalf("change = %+v, want error", rec.DocChange)
	}
	if got := d.content(t); got != docLineBody {
		t.Fatalf("file changed: %q", got)
	}
}

func TestDecisionDocLine_FileChangedBeforeWriteIsNotOverwritten(t *testing.T) {
	d := newDocLineRig(t, "on", docLineBody)
	other := docLineBody + "Added by someone else.\n"
	docLineBeforeWrite = func() { _ = os.WriteFile(d.path, []byte(other), 0o640) }
	t.Cleanup(func() { docLineBeforeWrite = nil })
	rec := d.answer(t, "go")
	if rec.DocChange == nil || rec.DocChange.Status != DecisionDocError {
		t.Fatalf("change = %+v, want error", rec.DocChange)
	}
	if got := d.content(t); got != other {
		t.Fatalf("file = %q, want the other writer's content kept", got)
	}
}

func TestDecisionDocLine_CRLFEndingsArePreserved(t *testing.T) {
	const body = "# Storage\r\nConfirmed: pending\r\nlast line without ending"
	d := newDocLineRig(t, "on", body)
	rec := d.answer(t, "go")
	c := rec.DocChange
	if c == nil || c.Status != DecisionDocApplied {
		t.Fatalf("change = %+v, want applied", c)
	}
	if c.Before != "Confirmed: pending" {
		t.Fatalf("before = %q, want no carriage return", c.Before)
	}
	want := "# Storage\r\n" + c.After + "\r\nlast line without ending"
	if got := d.content(t); got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
}

func TestDecisionDocLine_RecordWithoutDocLinkRecordsNothing(t *testing.T) {
	d := newDocLineRig(t, "on", docLineBody)
	rec := clearRecord()
	rec.Links.Doc = ""
	v, _, err := d.svc.Create(rec, DecisionSourceREST, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.questions.Answer(v.QuestionID, Answer{Actor: "alice", Surface: "pwa", ChosenOptionID: "sqlite", Quote: "go"}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.records.Get(v.ID)
	if got.DocChange != nil {
		t.Fatalf("recorded %+v for a record without a document", *got.DocChange)
	}
	if d.content(t) != docLineBody {
		t.Fatal("file changed")
	}
}

func TestDecisionDocLine_DateIsUTC(t *testing.T) {
	zone := time.FixedZone("late", -5*3600)
	at := time.Date(2026, 10, 30, 22, 30, 0, 0, zone) // 03:30 on the 31st in UTC
	got := renderDocStatusLine(docStatusInput{
		Template: "{date}", Answer: Answer{CreatedAt: &at}, Now: time.Now(),
	})
	if got != "2026-10-31" {
		t.Fatalf("date = %q, want 2026-10-31", got)
	}
}

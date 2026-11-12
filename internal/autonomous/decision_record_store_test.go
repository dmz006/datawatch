package autonomous

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// newDecisionStoreTest builds a store on a temp file with a controllable
// clock and a deterministic id sequence.
func newDecisionStoreTest(t *testing.T) (string, *DecisionRecordStore, func(time.Duration)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	s, err := NewDecisionRecordStore(path)
	if err != nil {
		t.Fatalf("NewDecisionRecordStore: %v", err)
	}
	wireDecisionStoreTest(s)
	ts := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return ts }
	return path, s, func(d time.Duration) { ts = ts.Add(d) }
}

// wireDecisionStoreTest gives a store a deterministic id sequence that
// continues past any ids it replayed.
func wireDecisionStoreTest(s *DecisionRecordStore) {
	n := len(s.order)
	s.newID = func() string { n++; return fmt.Sprintf("dr-%020x", n) }
}

func mustCreateDecision(t *testing.T, s *DecisionRecordStore, mutate func(*DecisionRecord)) DecisionRecord {
	t.Helper()
	in := validDecisionRecord()
	if mutate != nil {
		mutate(&in)
	}
	got, err := s.Create(in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return got
}

func TestDecisionRecordStore_CreateThenGet(t *testing.T) {
	_, s, _ := newDecisionStoreTest(t)
	in := validDecisionRecord()
	in.ID, in.State, in.QuestionID, in.ContentSHA256 = "dr-forged", DecisionRecordAnswered, "q-forged", "forged"
	in.Answer = &DecisionAnswer{AnswerID: "a-forged"}
	in.Source = DecisionSourceMCP
	got, err := s.Create(in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID != "dr-00000000000000000001" {
		t.Errorf("id = %q, want the store's id, not the caller's", got.ID)
	}
	if got.State != DecisionRecordOpen || got.QuestionID != "" || got.Answer != nil {
		t.Errorf("bookkeeping taken from the caller: %+v", got)
	}
	if got.Label != DecisionDefaultLabel {
		t.Errorf("label = %q, want default", got.Label)
	}
	if got.ContentSHA256 != got.ContentHash() || got.ContentSHA256 == "forged" {
		t.Errorf("content hash not computed by the store: %q", got.ContentSHA256)
	}
	if got.CreatedAt == nil || got.UpdatedAt == nil || got.Source != DecisionSourceMCP {
		t.Errorf("timestamps or source missing: %+v", got)
	}
	back, ok := s.Get(got.ID)
	if !ok || !reflect.DeepEqual(back, got) {
		t.Fatalf("Get = %+v (%v), want %+v", back, ok, got)
	}
	if _, ok := s.Get("dr-missing"); ok {
		t.Fatal("Get found a record that does not exist")
	}
	// The returned view is a copy: changing it leaves the store alone.
	back.Options[0].Label = "tampered"
	back.Recommendation.Reason = "tampered"
	again, _ := s.Get(got.ID)
	if again.Options[0].Label == "tampered" || again.Recommendation.Reason == "tampered" {
		t.Fatal("Get shares memory with the store")
	}
	// An invalid record is refused and nothing is stored.
	if _, err := s.Create(DecisionRecord{}); !errors.Is(err, ErrInvalidDecisionRecord) {
		t.Fatalf("empty record: err = %v", err)
	}
	if n := len(s.List(DecisionRecordFilter{})); n != 1 {
		t.Fatalf("store holds %d records, want 1", n)
	}
}

func TestDecisionRecordStore_ReplayGivesSameView(t *testing.T) {
	path, s, advance := newDecisionStoreTest(t)
	a := mustCreateDecision(t, s, func(r *DecisionRecord) { r.Source, r.SourceRef = DecisionSourceFile, "a.md" })
	advance(time.Minute)
	b := mustCreateDecision(t, s, nil)
	c := mustCreateDecision(t, s, nil)
	advance(time.Minute)
	if _, err := s.SetQuestionID(a.ID, "q-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordAnswer(a.ID, DecisionAnswer{AnswerID: "a-1", ChosenOptionID: "sqlite", Quote: "go with sqlite", Actor: "dmz", Surface: "pwa"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordDocChange(a.ID, DecisionDocChange{Mode: "report", Path: "docs/a.md", Line: 3, Before: "x", After: "y", Diff: "-x\n+y", Status: DecisionDocReported}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordFollowUp(a.ID, DecisionFollowUp{Requested: true, Kind: "start_template", Target: "t1", Status: "started", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(b.ID, func() DecisionRecord { r := validDecisionRecord(); r.Title = "A new title"; return r }()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(c.ID, "dmz", "not needed"); err != nil {
		t.Fatal(err)
	}

	re, err := NewDecisionRecordStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	want, got := s.List(DecisionRecordFilter{}), re.List(DecisionRecordFilter{})
	if len(want) != 3 || !reflect.DeepEqual(want, got) {
		t.Fatalf("replayed view differs:\nwant %+v\n got %+v", want, got)
	}
	if r, ok := re.GetByQuestionID("q-1"); !ok || r.ID != a.ID {
		t.Errorf("GetByQuestionID after replay = %v, %v", r.ID, ok)
	}
	if r, ok := re.GetBySource(DecisionSourceFile, "a.md"); !ok || r.ID != a.ID {
		t.Errorf("GetBySource after replay = %v, %v", r.ID, ok)
	}
	// A garbled line in the middle is an error naming the line.
	data, _ := os.ReadFile(path)
	bad := strings.Replace(string(data), "\n", "\nnot json\n", 1)
	badPath := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(badPath, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDecisionRecordStore(badPath); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("garbled middle line: err = %v, want one naming line 2", err)
	}
}

func TestDecisionRecordStore_UpdateRefreshesHash(t *testing.T) {
	_, s, advance := newDecisionStoreTest(t)
	rec := mustCreateDecision(t, s, func(r *DecisionRecord) { r.Source, r.SourceRef = DecisionSourceCLI, "ref-1" })
	advance(time.Hour)
	in := validDecisionRecord()
	in.Title = "A better title"
	in.Options = append(in.Options, DecisionOption{ID: "third", Label: "Third way"})
	in.Source, in.SourceRef, in.QuestionID = "rest", "other", "q-ignored"
	got, err := s.Update(rec.ID, in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Title != "A better title" || len(got.Options) != 3 {
		t.Errorf("content not replaced: %+v", got)
	}
	if got.ContentSHA256 == rec.ContentSHA256 || got.ContentSHA256 != got.ContentHash() {
		t.Errorf("hash not refreshed: old %s new %s", rec.ContentSHA256, got.ContentSHA256)
	}
	if !got.UpdatedAt.After(*rec.UpdatedAt) || !got.CreatedAt.Equal(*rec.CreatedAt) {
		t.Errorf("updated_at %v created_at %v (was %v / %v)", got.UpdatedAt, got.CreatedAt, rec.UpdatedAt, rec.CreatedAt)
	}
	if got.Source != DecisionSourceCLI || got.SourceRef != "ref-1" || got.QuestionID != "" {
		t.Errorf("update changed bookkeeping: %+v", got)
	}
	if _, err := s.Update(rec.ID, DecisionRecord{}); !errors.Is(err, ErrInvalidDecisionRecord) {
		t.Errorf("invalid update: err = %v", err)
	}
	if _, err := s.Update("dr-missing", validDecisionRecord()); !errors.Is(err, ErrDecisionRecordNotFound) {
		t.Errorf("missing record: err = %v", err)
	}
}

func TestDecisionRecordStore_UpdateClosedRecordRefused(t *testing.T) {
	path, s, _ := newDecisionStoreTest(t)
	answered := mustCreateDecision(t, s, nil)
	withdrawn := mustCreateDecision(t, s, nil)
	if _, err := s.RecordAnswer(answered.ID, DecisionAnswer{AnswerID: "a-1", ChosenOptionID: "files"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(withdrawn.ID, "dmz", ""); err != nil {
		t.Fatal(err)
	}
	size := func() int64 { fi, _ := os.Stat(path); return fi.Size() }
	before := size()
	for _, id := range []string{answered.ID, withdrawn.ID} {
		in := validDecisionRecord()
		in.Title = "Changed"
		if _, err := s.Update(id, in); !errors.Is(err, ErrDecisionRecordClosed) {
			t.Errorf("update of %s: err = %v, want ErrDecisionRecordClosed", id, err)
		}
	}
	if _, err := s.Withdraw(answered.ID, "dmz", ""); !errors.Is(err, ErrDecisionRecordClosed) {
		t.Errorf("withdraw of an answered record: err = %v", err)
	}
	if _, err := s.RecordAnswer(withdrawn.ID, DecisionAnswer{AnswerID: "a-2", ChosenOptionID: "files"}); !errors.Is(err, ErrDecisionRecordClosed) {
		t.Errorf("answer of a withdrawn record: err = %v", err)
	}
	if size() != before {
		t.Error("a refused change still wrote to the file")
	}
	if r, _ := s.Get(answered.ID); r.Title != answered.Title {
		t.Error("a refused update changed the record")
	}
	// Withdrawing twice is not an error.
	if _, err := s.Withdraw(withdrawn.ID, "dmz", "again"); err != nil {
		t.Errorf("second withdraw: %v", err)
	}
}

func TestDecisionRecordStore_DuplicateSourceRefRefused(t *testing.T) {
	_, s, _ := newDecisionStoreTest(t)
	first := mustCreateDecision(t, s, func(r *DecisionRecord) { r.Source, r.SourceRef = DecisionSourceFile, "choose.md" })
	in := validDecisionRecord()
	in.Source, in.SourceRef = DecisionSourceFile, "choose.md"
	if _, err := s.Create(in); !errors.Is(err, ErrDecisionRecordDuplicate) {
		t.Fatalf("duplicate: err = %v, want ErrDecisionRecordDuplicate", err)
	}
	// Same ref from another source, a different ref, and no source at all are fine.
	for _, c := range []struct{ src, ref string }{{DecisionSourceMCP, "choose.md"}, {DecisionSourceFile, "other.md"}, {"", ""}, {"", ""}} {
		in.Source, in.SourceRef = c.src, c.ref
		if _, err := s.Create(in); err != nil {
			t.Errorf("create %+v: %v", c, err)
		}
	}
	// Still refused once the first is withdrawn: the source is spoken for.
	if _, err := s.Withdraw(first.ID, "dmz", ""); err != nil {
		t.Fatal(err)
	}
	in.Source, in.SourceRef = DecisionSourceFile, "choose.md"
	if _, err := s.Create(in); !errors.Is(err, ErrDecisionRecordDuplicate) {
		t.Fatalf("duplicate after withdraw: err = %v", err)
	}
	if got, ok := s.GetBySource(DecisionSourceFile, "choose.md"); !ok || got.ID != first.ID {
		t.Fatalf("GetBySource = %v, %v", got.ID, ok)
	}
	if _, ok := s.GetBySource("", ""); ok {
		t.Fatal("empty source matched")
	}
	if _, ok := s.GetBySource(DecisionSourceFile, "nope.md"); ok {
		t.Fatal("unknown source ref matched")
	}
}

func TestDecisionRecordStore_SetQuestionIDOnce(t *testing.T) {
	path, s, _ := newDecisionStoreTest(t)
	a := mustCreateDecision(t, s, nil)
	b := mustCreateDecision(t, s, nil)
	got, err := s.SetQuestionID(a.ID, "q-1")
	if err != nil || got.QuestionID != "q-1" {
		t.Fatalf("SetQuestionID = %+v, %v", got, err)
	}
	fi, _ := os.Stat(path)
	if _, err := s.SetQuestionID(a.ID, "q-1"); err != nil {
		t.Fatalf("same id again: %v", err)
	}
	if fi2, _ := os.Stat(path); fi2.Size() != fi.Size() {
		t.Error("setting the same question id again wrote a line")
	}
	if _, err := s.SetQuestionID(a.ID, "q-2"); !errors.Is(err, ErrDecisionQuestionIDSet) {
		t.Fatalf("second different id: err = %v, want ErrDecisionQuestionIDSet", err)
	}
	if _, err := s.SetQuestionID(b.ID, "q-1"); !errors.Is(err, ErrDecisionQuestionIDSet) {
		t.Fatalf("question id of another record: err = %v", err)
	}
	if _, err := s.SetQuestionID(b.ID, " "); !errors.Is(err, ErrInvalidDecisionRecord) {
		t.Fatalf("blank question id: err = %v", err)
	}
	if _, err := s.SetQuestionID("dr-missing", "q-9"); !errors.Is(err, ErrDecisionRecordNotFound) {
		t.Fatalf("missing record: err = %v", err)
	}
	if r, ok := s.GetByQuestionID("q-1"); !ok || r.ID != a.ID {
		t.Fatalf("GetByQuestionID = %v, %v", r.ID, ok)
	}
	if _, ok := s.GetByQuestionID("q-2"); ok {
		t.Fatal("refused question id was indexed")
	}
	if _, ok := s.GetByQuestionID(""); ok {
		t.Fatal("empty question id matched")
	}
}

func TestDecisionRecordStore_AnswerDocChangeAndFollowUp(t *testing.T) {
	_, s, advance := newDecisionStoreTest(t)
	rec := mustCreateDecision(t, s, nil)
	advance(time.Minute)
	ans := DecisionAnswer{AnswerID: "a-1", ChosenOptionID: "sqlite", Quote: "yes, sqlite", Actor: "dmz", Surface: "signal"}
	got, err := s.RecordAnswer(rec.ID, ans)
	if err != nil {
		t.Fatalf("RecordAnswer: %v", err)
	}
	if got.State != DecisionRecordAnswered || got.Answer == nil || got.Answer.AnswerID != "a-1" || got.Answer.AnsweredAt == nil {
		t.Fatalf("answer not stored: %+v", got)
	}
	// Idempotent on the same answer id: no change, even with other text.
	ans.Quote = "different"
	again, err := s.RecordAnswer(rec.ID, ans)
	if err != nil || again.Answer.Quote != "yes, sqlite" || !again.UpdatedAt.Equal(*got.UpdatedAt) {
		t.Fatalf("repeat answer changed the record: %+v, %v", again.Answer, err)
	}
	// A new answer id supersedes.
	ans.AnswerID, ans.ChosenOptionID = "a-2", "files"
	sup, err := s.RecordAnswer(rec.ID, ans)
	if err != nil || sup.Answer.AnswerID != "a-2" || sup.Answer.ChosenOptionID != "files" {
		t.Fatalf("superseding answer: %+v, %v", sup.Answer, err)
	}
	for name, a := range map[string]DecisionAnswer{
		"no answer id": {ChosenOptionID: "x"},
		"empty answer": {AnswerID: "a-3"},
		"bad utf8":     {AnswerID: "a-3", Quote: "\xff\xfe"},
		"huge quote":   {AnswerID: "a-3", Quote: strings.Repeat("q", answerQuoteMaxBytes+1)},
	} {
		if _, err := s.RecordAnswer(rec.ID, a); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := s.RecordAnswer("dr-missing", ans); !errors.Is(err, ErrDecisionRecordNotFound) {
		t.Errorf("missing record: err = %v", err)
	}

	dc, err := s.RecordDocChange(rec.ID, DecisionDocChange{Mode: "report", Path: "docs/plan.md", Line: 7, Status: DecisionDocReported, Diff: "-a\n+b"})
	if err != nil || dc.DocChange == nil || dc.DocChange.Status != DecisionDocReported || dc.DocChange.At == nil {
		t.Fatalf("RecordDocChange = %+v, %v", dc.DocChange, err)
	}
	for _, bad := range []DecisionDocChange{
		{Status: "maybe"},
		{Status: DecisionDocApplied, Path: "../x.md"},
		{Status: DecisionDocApplied, Diff: strings.Repeat("d", decisionDocFieldMaxBytes+1)},
	} {
		if _, err := s.RecordDocChange(rec.ID, bad); !errors.Is(err, ErrInvalidDecisionRecord) {
			t.Errorf("doc change %+v: err = %v", bad, err)
		}
	}
	if _, err := s.RecordDocChange("dr-missing", DecisionDocChange{Status: DecisionDocError}); !errors.Is(err, ErrDecisionRecordNotFound) {
		t.Errorf("doc change on a missing record: err = %v", err)
	}

	fu, err := s.RecordFollowUp(rec.ID, DecisionFollowUp{Requested: true, Kind: "open_automaton", Target: "ee660001", Status: "done"})
	if err != nil || fu.FollowUp == nil || !fu.FollowUp.Requested || fu.FollowUp.At == nil {
		t.Fatalf("RecordFollowUp = %+v, %v", fu.FollowUp, err)
	}
	if _, err := s.RecordFollowUp(rec.ID, DecisionFollowUp{Kind: strings.Repeat("k", decisionShortFieldMax+1)}); !errors.Is(err, ErrInvalidDecisionRecord) {
		t.Errorf("oversized follow up: err = %v", err)
	}
	if _, err := s.RecordFollowUp("dr-missing", DecisionFollowUp{}); !errors.Is(err, ErrDecisionRecordNotFound) {
		t.Errorf("follow up on a missing record: err = %v", err)
	}
}

func TestDecisionRecordStore_ListFiltersAndOrder(t *testing.T) {
	_, s, advance := newDecisionStoreTest(t)
	var ids []string
	for i := 0; i < 5; i++ {
		i := i
		r := mustCreateDecision(t, s, func(r *DecisionRecord) {
			r.Links.Automaton = []string{"alpha", "beta"}[i%2]
		})
		ids = append(ids, r.ID)
		advance(time.Second)
	}
	if _, err := s.RecordAnswer(ids[1], DecisionAnswer{AnswerID: "a-1", ChosenOptionID: "files"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ids[2], "dmz", ""); err != nil {
		t.Fatal(err)
	}
	idsOf := func(f DecisionRecordFilter) []string {
		var out []string
		for _, r := range s.List(f) {
			out = append(out, r.ID)
		}
		return out
	}
	rev := func(in ...string) []string {
		out := make([]string, len(in))
		for i, v := range in {
			out[len(in)-1-i] = v
		}
		return out
	}
	if got, want := idsOf(DecisionRecordFilter{}), rev(ids...); !reflect.DeepEqual(got, want) {
		t.Errorf("all = %v, want newest first %v", got, want)
	}
	if got, want := idsOf(DecisionRecordFilter{State: DecisionRecordOpen}), rev(ids[0], ids[3], ids[4]); !reflect.DeepEqual(got, want) {
		t.Errorf("open = %v, want %v", got, want)
	}
	if got, want := idsOf(DecisionRecordFilter{State: DecisionRecordAnswered}), []string{ids[1]}; !reflect.DeepEqual(got, want) {
		t.Errorf("answered = %v, want %v", got, want)
	}
	if got, want := idsOf(DecisionRecordFilter{State: DecisionRecordWithdrawn}), []string{ids[2]}; !reflect.DeepEqual(got, want) {
		t.Errorf("withdrawn = %v, want %v", got, want)
	}
	if got, want := idsOf(DecisionRecordFilter{AutomatonID: "beta"}), rev(ids[1], ids[3]); !reflect.DeepEqual(got, want) {
		t.Errorf("automaton beta = %v, want %v", got, want)
	}
	if got, want := idsOf(DecisionRecordFilter{AutomatonID: "alpha", State: DecisionRecordOpen}), rev(ids[0], ids[4]); !reflect.DeepEqual(got, want) {
		t.Errorf("alpha open = %v, want %v", got, want)
	}
	if got, want := idsOf(DecisionRecordFilter{Limit: 2}), rev(ids...)[:2]; !reflect.DeepEqual(got, want) {
		t.Errorf("limit 2 = %v, want %v", got, want)
	}
	if got := idsOf(DecisionRecordFilter{AutomatonID: "nobody"}); len(got) != 0 {
		t.Errorf("unknown automaton = %v, want none", got)
	}
}

func TestDecisionRecordStore_ListLimitDefaultsAndMax(t *testing.T) {
	_, s, _ := newDecisionStoreTest(t)
	in := validDecisionRecord()
	for i := 0; i < maxDecisionListLimit+5; i++ {
		// Skip the file append cost: build the view through the same apply
		// path the store uses on replay.
		r := in.clone()
		r.ID = fmt.Sprintf("dr-%020x", i+1)
		r.State = DecisionRecordOpen
		s.applyLine(decisionStoreLine{Type: decisionLineCreated, Record: &r})
	}
	if n := len(s.List(DecisionRecordFilter{})); n != defaultDecisionListLimit {
		t.Errorf("default limit gave %d, want %d", n, defaultDecisionListLimit)
	}
	if n := len(s.List(DecisionRecordFilter{Limit: -3})); n != defaultDecisionListLimit {
		t.Errorf("negative limit gave %d, want %d", n, defaultDecisionListLimit)
	}
	if n := len(s.List(DecisionRecordFilter{Limit: 5000})); n != maxDecisionListLimit {
		t.Errorf("limit 5000 gave %d, want the max %d", n, maxDecisionListLimit)
	}
	for _, huge := range []int{1 << 30, int(^uint(0) >> 1)} {
		if n := len(s.List(DecisionRecordFilter{Limit: huge})); n != maxDecisionListLimit {
			t.Errorf("limit %d gave %d, want the max %d", huge, n, maxDecisionListLimit)
		}
	}
	if n := len(s.List(DecisionRecordFilter{Limit: 7})); n != 7 {
		t.Errorf("limit 7 gave %d", n)
	}
}

func TestDecisionRecordStore_TornFinalLineRepaired(t *testing.T) {
	path, s, _ := newDecisionStoreTest(t)
	a := mustCreateDecision(t, s, nil)

	// A crash in the middle of a write leaves half a line with no newline.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	torn := `{"type":"decision_created","record":{"id":"dr-torn","title":"half`
	if _, err := f.WriteString(torn); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	re, err := NewDecisionRecordStore(path)
	if err != nil {
		t.Fatalf("reopen with a torn last line: %v", err)
	}
	if got := re.List(DecisionRecordFilter{}); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("view after torn line = %+v, want only %s", got, a.ID)
	}
	wireDecisionStoreTest(re)
	re.newID = func() string { return "dr-00000000000000000002" }
	b, err := re.Create(validDecisionRecord())
	if err != nil {
		t.Fatalf("append after a torn line: %v", err)
	}
	// The file keeps the fragment (it is never rewritten), followed by a
	// marker, and it opens cleanly again with both records.
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), torn+"\n") || !strings.Contains(string(data), `"crash_residue"`) {
		t.Errorf("file does not hold the fragment and its marker:\n%s", data)
	}
	re2, err := NewDecisionRecordStore(path)
	if err != nil {
		t.Fatalf("second reopen: %v", err)
	}
	if got := re2.List(DecisionRecordFilter{}); len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Fatalf("view after repair = %+v", got)
	}
	// An unterminated but valid last line only needs its newline.
	data, _ = os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.TrimSuffix(string(data), "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	re3, err := NewDecisionRecordStore(path)
	if err != nil {
		t.Fatalf("reopen with a valid unterminated line: %v", err)
	}
	if n := len(re3.List(DecisionRecordFilter{})); n != 2 {
		t.Fatalf("valid unterminated line: %d records, want 2", n)
	}
	if _, err := re3.RecordFollowUp(a.ID, DecisionFollowUp{Requested: false}); err != nil {
		t.Fatalf("append after a valid unterminated line: %v", err)
	}
	if _, err := NewDecisionRecordStore(path); err != nil {
		t.Fatalf("reopen after that append: %v", err)
	}
}

func TestDecisionRecordStore_FailedWriteLeavesViewUntouched(t *testing.T) {
	_, s, _ := newDecisionStoreTest(t)
	rec := mustCreateDecision(t, s, nil)
	s.writeFn = func(f *os.File, b []byte) (int, error) { return 0, errors.New("disk full") }
	if _, err := s.Create(validDecisionRecord()); err == nil {
		t.Fatal("Create succeeded on a failed write")
	}
	if n := len(s.List(DecisionRecordFilter{})); n != 1 {
		t.Errorf("failed create left %d records, want 1", n)
	}
	if _, err := s.Withdraw(rec.ID, "dmz", ""); err == nil {
		t.Fatal("Withdraw succeeded on a failed write")
	}
	if got, _ := s.Get(rec.ID); got.State != DecisionRecordOpen {
		t.Errorf("failed withdraw changed the state to %s", got.State)
	}
}

func TestDecisionRecordStore_DefaultsAndMissingFile(t *testing.T) {
	s, err := NewDecisionRecordStore(filepath.Join(t.TempDir(), "nothing-yet.jsonl"))
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if got := s.List(DecisionRecordFilter{}); len(got) != 0 {
		t.Fatalf("new store holds %v", got)
	}
	// With the real clock and id source a record still gets a well-formed id.
	rec, err := s.Create(validDecisionRecord())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(rec.ID, "dr-") || len(rec.ID) != 23 {
		t.Errorf("id = %q", rec.ID)
	}
	// A directory in place of the file is an error, not an empty store.
	if _, err := NewDecisionRecordStore(t.TempDir()); err == nil {
		t.Error("opening a directory succeeded")
	}
}

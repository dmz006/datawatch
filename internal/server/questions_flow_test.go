package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// TestQuestionsFlow_EndToEnd drives the full operator-question flow — store,
// REST handlers, the enable switch, persistence, and the audit log with its
// CEF mirror — through the HTTP surface only. The sections, in order:
//
//  1. POST /api/questions creates an open question;
//  2. lists filter by state, asker, text, context and limit;
//  3. an answer quote holding a newline, pipe, backslashes, tab and UTF-8 is
//     stored byte-exact (hash included) in memory, in the store file and on
//     the wire;
//  4. a second non-superseding answer gets a 409;
//  5. a superseding answer gets a fresh id and the question stays answered;
//  6. a second question is withdrawn, after which answers to it are refused;
//  7. a secret-looking answer is refused with 422 and its token is never
//     echoed to the client, the files, or the audit trail;
//  8. with autonomous.questions.enabled off every surface answers 503 and
//     names the switch; flipping it on resumes without a restart;
//  9. re-opening the store on the same file yields the identical view;
//  10. the audit log holds one entry per action with the documented details,
//     and the CEF mirror line for each carries the right signature, an
//     escaped quote and no secret;
//  11. a federated peer without the capability gets 403 and the store and
//     audit trail stay untouched.
func TestQuestionsFlow_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	// The daemon wiring: one append-only audit log with the CEF mirror
	// enabled, and the store's audit callback pointed at it, exactly like
	// cmd/datawatch/main.go does.
	al, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { al.Close() })
	if err := al.EnableCEFMirror(); err != nil {
		t.Fatalf("EnableCEFMirror: %v", err)
	}

	storePath := filepath.Join(dir, "questions.jsonl")
	store, err := autonomous.NewQuestionStore(storePath)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	store.SetAuditFn(func(action string, details map[string]any) {
		actor, _ := details["actor"].(string)
		if actor == "" {
			actor = "operator"
		}
		subject, _ := details["question_id"].(string)
		if werr := al.Write(audit.Entry{Actor: actor, Action: action, Subject: subject, Details: details}); werr != nil {
			t.Errorf("audit write (%s): %v", action, werr)
		}
	})

	cfg := questionRESTTestConfig(true)
	s := &Server{cfg: &cfg, cfgPath: filepath.Join(dir, "config.yaml"), questionStore: store}

	// --- 1. raise -------------------------------------------------------------
	asker := "automation:orchestrator-task-7"
	body := validQuestionBody()
	body["asked_by"] = asker
	rr := doQ(t, s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("raise: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	q1 := decodeQuestion(t, rr)
	if q1.ID == "" {
		t.Fatalf("201 without a question id (body=%q)", rr.Body.String())
	}
	if q1.State != autonomous.QuestionOpen {
		t.Fatalf("raised state=%q, want open", q1.State)
	}
	if q1.AskedBy != asker || q1.Default != "yes" || len(q1.Options) != 2 {
		t.Fatalf("raised record wrong: %+v", q1)
	}

	// --- 2. list filters -----------------------------------------------------
	filters := map[string]string{
		"state":   "state=open",
		"asker":   "asker=" + asker,
		"text":    "text=backoff",
		"context": "context=none",
		"limit":   "limit=1",
	}
	for name, args := range filters {
		lrr := doQ(t, s, http.MethodGet, "/api/questions?"+args, nil)
		if lrr.Code != http.StatusOK {
			t.Errorf("filter %s=%s: want 200, got %d body=%s", name, args, lrr.Code, lrr.Body.String())
			continue
		}
		var listEnv struct {
			Count     int                   `json:"count"`
			Questions []autonomous.Question `json:"questions"`
		}
		if err := json.Unmarshal(lrr.Body.Bytes(), &listEnv); err != nil {
			t.Errorf("filter %s=%s: decode: %v", name, args, err)
			continue
		}
		if listEnv.Count != 1 || len(listEnv.Questions) != 1 || listEnv.Questions[0].ID != q1.ID {
			t.Errorf("filter %s=%s: got %d question(s), want exactly %q", name, args, listEnv.Count, q1.ID)
		}
	}

	// --- 3. answer with a hostile quote -------------------------------------
	quote := "  pick A, not B | a" + "\t" + "b" + "\r\n" + `\back\slash — café 日本語   `
	hsum := sha256.Sum256([]byte(quote))
	wantHash := hex.EncodeToString(hsum[:])

	rr = doAnswer(t, s, q1.ID, map[string]any{"chosen_option_id": "yes", "quote": quote, "actor": "operator", "surface": "rest"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("answer: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	a1, q1After := decodeAnswerEnv(t, rr)
	if a1.ID == "" || a1.QuestionID != q1.ID {
		t.Fatalf("answer ids wrong: %+v", a1)
	}
	if a1.Quote != quote {
		t.Fatalf("wire quote not byte-identical to what was sent")
	}
	if a1.QuoteSHA256 != wantHash {
		t.Fatalf("quote hash %s, want %s", a1.QuoteSHA256, wantHash)
	}
	if a1.ChosenOptionID != "yes" {
		t.Fatalf("chosen option %q, want yes", a1.ChosenOptionID)
	}
	if q1After.State != autonomous.QuestionAnswered {
		t.Fatalf("state after answer=%q, want answered", q1After.State)
	}
	// the store file keeps the exact quote bytes, not a rendered form
	answerLineStoredByteExact(t, storePath, a1.ID, quote, wantHash)

	// --- 4. a second plain answer conflicts ----------------------------------
	rr = doAnswer(t, s, q1.ID, map[string]any{"chosen_option_id": "no", "quote": "changed my mind", "actor": "operator", "surface": "rest"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate answer: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}

	// --- 5. supersede ---------------------------------------------------------
	rr = doAnswer(t, s, q1.ID, map[string]any{"quote": quote, "supersedes": a1.ID, "actor": "operator", "surface": "rest"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("supersede: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	a2, q1After := decodeAnswerEnv(t, rr)
	if a2.ID == "" || a2.ID == a1.ID {
		t.Fatalf("superseding answer must carry a fresh id (got %q, first was %q)", a2.ID, a1.ID)
	}
	if a2.Supersedes != a1.ID {
		t.Fatalf("supersede chain: %q, want %q", a2.Supersedes, a1.ID)
	}
	if q1After.State != autonomous.QuestionAnswered {
		t.Fatalf("state after supersede=%q, want answered", q1After.State)
	}
	views, okV := store.Answers(q1.ID)
	if !okV || len(views) != 2 {
		t.Fatalf("answer log: want 2 entries, got %d (ok=%v)", len(views), okV)
	}
	if !views[0].Superseded || views[0].SupersededBy != a2.ID {
		t.Fatalf("first answer view: superseded=%v superseded_by=%q, want true/%q",
			views[0].Superseded, views[0].SupersededBy, a2.ID)
	}
	if views[1].Superseded || views[1].SupersededBy != "" {
		t.Fatalf("second answer view wrong: %+v", views[1])
	}

	// --- 6. second question, withdraw, then refusals -------------------------
	q2 := raiseQ(t, s)
	rr = doWithdraw(t, s, q2.ID, "no longer relevant")
	if rr.Code != http.StatusOK {
		t.Fatalf("withdraw: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	wq := decodeQuestion(t, rr)
	if wq.ID != q2.ID || wq.State != autonomous.QuestionWithdrawn {
		t.Fatalf("withdrawn record wrong: %+v", wq)
	}
	rr = doAnswer(t, s, q2.ID, map[string]any{"quote": "too late", "actor": "operator", "surface": "rest"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("answer after withdraw: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}

	// --- 7. a secret-looking answer is refused and never echoed --------------
	secret := "ghp_" + strings.Repeat("Xk7", 10)
	rr = doAnswer(t, s, q1.ID, map[string]any{"quote": secret, "actor": "operator", "surface": "rest"})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("secret answer: want 422, got %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), secret) || strings.Contains(rr.Body.String(), "ghp_") {
		t.Fatalf("refusal response echoes the secret: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "secret_detected") {
		t.Fatalf("refusal does not name the reason: %s", rr.Body.String())
	}
	// the question keeps exactly the answer it already had
	cur, okCur := store.CurrentAnswer(q1.ID)
	if !okCur || cur.ID != a2.ID || cur.Quote != quote {
		t.Fatalf("current answer moved after a refusal: %+v (ok=%v)", cur, okCur)
	}

	// --- 8. the enable switch --------------------------------------------------
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": false})
	if s.cfg.Autonomous.Questions.Enabled {
		t.Fatalf("switch still on after PUT off")
	}
	offSurfaces := []struct {
		name string
		run  func() *httptest.ResponseRecorder
	}{
		{"list", func() *httptest.ResponseRecorder { return doQ(t, s, http.MethodGet, "/api/questions", nil) }},
		{"raise", func() *httptest.ResponseRecorder {
			return doQ(t, s, http.MethodPost, "/api/questions", validQuestionBody())
		}},
		{"get", func() *httptest.ResponseRecorder { return doQ(t, s, http.MethodGet, "/api/questions/"+q1.ID, nil) }},
		{"answer", func() *httptest.ResponseRecorder { return doAnswer(t, s, q1.ID, map[string]any{"quote": "while off"}) }},
		{"withdraw", func() *httptest.ResponseRecorder { return doWithdraw(t, s, q2.ID, "while off") }},
		{"answers", func() *httptest.ResponseRecorder { return doAnswers(t, s, q1.ID) }},
	}
	for _, c := range offSurfaces {
		orrr := c.run()
		if orrr.Code != http.StatusServiceUnavailable {
			t.Errorf("switch off: %s want 503, got %d body=%s", c.name, orrr.Code, orrr.Body.String())
			continue
		}
		if !strings.Contains(orrr.Body.String(), "autonomous.questions.enabled") {
			t.Errorf("switch off: %s 503 does not name the switch: %s", c.name, orrr.Body.String())
		}
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": true})
	if !s.cfg.Autonomous.Questions.Enabled {
		t.Fatal("switch did not come back on")
	}
	rr = doQ(t, s, http.MethodGet, "/api/questions", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("switch on: list want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// --- 9. re-open the store on the same file --------------------------------
	// Equality by normalized JSON: two time.Time values with the same
	// instant but different location (local vs parsed-from-file) are not
	// DeepEqual, but their RFC3339 rendering is identical.
	sameViaJSON := func(a, b any) bool {
		aj, aerr := json.Marshal(a)
		bj, berr := json.Marshal(b)
		if aerr != nil || berr != nil {
			return false
		}
		return bytes.Equal(aj, bj)
	}
	listBefore := store.List(autonomous.QuestionFilter{})
	ansBefore, okA := store.Answers(q1.ID)
	if !okA || len(ansBefore) != 2 {
		t.Fatalf("answers before reopen: want 2, got %d (ok=%v)", len(ansBefore), okA)
	}
	curBefore, okC := store.CurrentAnswer(q1.ID)
	if !okC {
		t.Fatal("no current answer before reopen")
	}

	re, err := autonomous.NewQuestionStore(storePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if !sameViaJSON(re.List(autonomous.QuestionFilter{}), listBefore) {
		t.Fatalf("question list changed across a reopen: %v vs %v", re.List(autonomous.QuestionFilter{}), listBefore)
	}
	ansRe, okA := re.Answers(q1.ID)
	if !okA || !sameViaJSON(ansRe, ansBefore) {
		t.Fatalf("answer log changed across a reopen")
	}
	curRe, okC := re.CurrentAnswer(q1.ID)
	if !okC || !sameViaJSON(curRe, curBefore) {
		t.Fatalf("current answer changed across a reopen")
	}
	gotAnswered, okG := re.Get(q1.ID)
	gotWithdrawn, okW := re.Get(q2.ID)
	if !okG || !okW {
		t.Fatal("reopen lost a question")
	}
	if gotAnswered.State != autonomous.QuestionAnswered || gotWithdrawn.State != autonomous.QuestionWithdrawn {
		t.Fatalf("states across reopen: %q / %q", gotAnswered.State, gotWithdrawn.State)
	}

	// --- 10. audit entries and the CEF mirror ----------------------------------
	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	gotActions := make([]string, 0, len(entries))
	for _, en := range entries {
		gotActions = append(gotActions, en.Action)
	}
	// Read is newest-first, so the expected order is the reverse of
	// append order.
	if want := "question_answer_refused, question_withdrawn, question_raised, answer_superseded, question_answered, question_raised"; strings.Join(gotActions, ", ") != want {
		t.Fatalf("audit action order: got %v, want %s", gotActions, want)
	}
	assert := func(label string, en audit.Entry, action, subject string, keys []string, wantVals map[string]any) {
		if en.Action != action {
			t.Errorf("%s: action=%q, want %q", label, en.Action, action)
			return
		}
		if en.Subject != subject {
			t.Errorf("%s: subject=%q, want %q", label, en.Subject, subject)
		}
		if en.Actor != "operator" {
			t.Errorf("%s: actor=%q, want operator", label, en.Actor)
		}
		if en.Timestamp.IsZero() {
			t.Errorf("%s: zero timestamp", label)
		}
		for _, k := range keys {
			if _, have := en.Details[k]; !have {
				t.Errorf("%s: details missing key %q (have %v)", label, k, en.Details)
			}
		}
		for k, v := range wantVals {
			got, have := en.Details[k]
			if !have {
				t.Errorf("%s: details missing %q", label, k)
				continue
			}
			if fmt.Sprint(got) != fmt.Sprint(v) {
				t.Errorf("%s: details[%s]=%v, want %v", label, k, got, v)
			}
		}
	}
	assert("raised q1", entries[5], "question_raised", q1.ID,
		[]string{"question_id", "label", "asked_by", "asker_kind", "context", "risk", "option_count", "default", "text"},
		map[string]any{"question_id": q1.ID, "label": "decision", "asked_by": asker,
			"context": "none", "risk": "low", "default": "yes",
			"text": "Should retry backoff be capped at five minutes?"})
	assert("answered q1", entries[4], "question_answered", q1.ID,
		[]string{"question_id", "answer_id", "chosen_option_id", "quote", "quote_sha256", "actor", "surface"},
		map[string]any{"answer_id": a1.ID, "chosen_option_id": "yes", "quote": quote,
			"quote_sha256": wantHash, "actor": "operator", "surface": "rest"})
	assert("superseded q1", entries[3], "answer_superseded", q1.ID,
		[]string{"question_id", "answer_id", "supersedes", "quote", "quote_sha256", "actor", "surface"},
		map[string]any{"answer_id": a2.ID, "supersedes": a1.ID, "quote": quote,
			"quote_sha256": wantHash, "actor": "operator", "surface": "rest"})
	assert("raised q2", entries[2], "question_raised", q2.ID,
		[]string{"question_id", "label", "asked_by", "asker_kind", "context", "risk", "option_count", "default", "text"},
		map[string]any{"question_id": q2.ID, "asked_by": "operator"})
	assert("withdrawn q2", entries[1], "question_withdrawn", q2.ID,
		[]string{"question_id", "actor", "reason"},
		map[string]any{"actor": "operator", "reason": "no longer relevant"})
	assert("refused q1", entries[0], "question_answer_refused", q1.ID,
		[]string{"question_id", "actor", "surface", "reason", "pattern"},
		map[string]any{"reason": "secret_detected", "pattern": "github token",
			"actor": "operator", "surface": "rest"})
	// the refusal detail must carry the rule, never any text
	for _, k := range []string{"quote", "text", "answer"} {
		if _, have := entries[0].Details[k]; have {
			t.Errorf("refusal detail echoes %q: %v", k, entries[0].Details)
		}
	}

	// the secret token must not appear anywhere the flow touches
	logPath := filepath.Join(dir, "audit.log")
	cefPath := logPath + ".cef"
	for _, p := range []string{storePath, logPath, cefPath} {
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("read %s: %v", p, rerr)
		}
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("secret token leaked into %s", p)
		}
	}

	// CEF mirror: one line per audit entry, same order, right signature.
	cefRaw, rerr := os.ReadFile(cefPath)
	if rerr != nil {
		t.Fatalf("read CEF mirror: %v", rerr)
	}
	cefLines := strings.Split(strings.TrimRight(string(cefRaw), "\n"), "\n")
	if len(cefLines) != len(entries) {
		t.Fatalf("CEF mirror lines=%d, audit lines=%d", len(cefLines), len(entries))
	}
	wantCEF := []struct{ id, name, sev string }{
		{"50", "QuestionRaised", "3"},
		{"51", "QuestionAnswered", "3"},
		{"54", "AnswerSuperseded", "4"},
		{"50", "QuestionRaised", "3"},
		{"52", "QuestionWithdrawn", "3"},
		{"55", "QuestionAnswerRefused", "6"},
	}
	for i, line := range cefLines {
		if !strings.HasPrefix(line, "CEF:0|datawatch|datawatch|") {
			t.Errorf("cef line %d: bad header: %s", i, line)
		}
		if strings.ContainsAny(line, "\r") {
			t.Errorf("cef line %d carries a raw carriage return: %s", i, line)
		}
		fields := strings.SplitN(line, "|", 7)
		if len(fields) != 7 {
			t.Errorf("cef line %d: less than 7 columns: %s", i, line)
			continue
		}
		sev, _, _ := strings.Cut(fields[6], "|")
		if fields[4] != wantCEF[i].id || fields[5] != wantCEF[i].name || sev != wantCEF[i].sev {
			t.Errorf("cef line %d: %s | %s | %s, want %s | %s | %s",
				i, fields[4], fields[5], sev, wantCEF[i].id, wantCEF[i].name, wantCEF[i].sev)
		}
	}
	// the answered line: the quote is escaped, not mangled
	answeredLine := cefLines[1]
	if !strings.Contains(answeredLine, "deviceCustomString1=question_answered") {
		t.Errorf("answered CEF line missing the action cs2: %s", answeredLine)
	}
	if !strings.Contains(answeredLine, `\r\n`) {
		t.Errorf("answered CEF line: the quote's CRLF is not escaped to CR LF: %s", answeredLine)
	}
	if !strings.Contains(answeredLine, `\\back\\slash`) {
		t.Errorf("answered CEF line: the quote's backslashes are not escaped: %s", answeredLine)
	}
	if !strings.Contains(answeredLine, "pick A, not B | a\tb") {
		t.Errorf("answered CEF line: the quote's pipes/tabs should stay verbatim: %s", answeredLine)
	}
	if !strings.Contains(answeredLine, "café 日本語") {
		t.Errorf("answered CEF line: the quote's UTF-8 was mangled: %s", answeredLine)
	}
	if !strings.Contains(cefLines[2], "deviceCustomString1=answer_superseded") {
		t.Errorf("superseded CEF line missing the action cs2: %s", cefLines[2])
	}
	if !strings.Contains(cefLines[5], "deviceCustomString1=question_answer_refused") {
		t.Errorf("refused CEF line missing the action cs2: %s", cefLines[5])
	}
	if strings.Contains(cefLines[5], "ghp_") {
		t.Fatalf("refused CEF line leaks the token: %s", cefLines[5])
	}
	if !strings.Contains(cefLines[5], "github token") {
		t.Errorf("refused CEF line should name the matched rule: %s", cefLines[5])
	}

	// --- 11. a capability-less federated peer -----------------------------------
	fed403 := func(label string, rec *httptest.ResponseRecorder) {
		if rec.Code != http.StatusForbidden {
			t.Errorf("peer %s: want 403, got %d body=%s", label, rec.Code, rec.Body.String())
		}
	}
	peerDo := func(method, path string, b any) *httptest.ResponseRecorder {
		var r io.Reader
		if b != nil {
			marshalled, merr := json.Marshal(b)
			if merr != nil {
				t.Fatalf("marshal peer body: %v", merr)
			}
			r = bytes.NewReader(marshalled)
		}
		rec := httptest.NewRecorder()
		s.handleQuestions(rec, asPeer(httptest.NewRequest(method, path, r)))
		return rec
	}
	fed403("list", peerDo(http.MethodGet, "/api/questions", nil))
	fed403("raise", peerDo(http.MethodPost, "/api/questions", validQuestionBody()))
	fed403("get", peerDo(http.MethodGet, "/api/questions/"+q1.ID, nil))
	fed403("answer", doAnswer(t, s, q1.ID, map[string]any{"quote": "x", "actor": "peer"}, "smoke:read"))
	fed403("withdraw", doWithdraw(t, s, q2.ID, "peer", "smoke:write"))
	fed403("answers", doAnswers(t, s, q1.ID, "smoke:read"))

	// none of those refusals may have touched the store or the audit trail
	if n := len(re.List(autonomous.QuestionFilter{})); n != 2 {
		t.Errorf("store changed under 403s: %d questions", n)
	}
	if cur, okAfter := store.CurrentAnswer(q1.ID); !okAfter || cur.ID != a2.ID {
		t.Errorf("current answer moved under 403s")
	}
	after, aerr := al.Read(audit.QueryFilter{})
	if aerr != nil {
		t.Fatalf("re-read audit log: %v", aerr)
	}
	if len(after) != 6 {
		t.Errorf("audit trail grew under 403s: %d entries", len(after))
	}
}

// answerLineStoredByteExact finds the answer line for id in the store file and
// checks that the quote and its hash survive the file byte-for-byte.
func answerLineStoredByteExact(t *testing.T, path, id, quote, wantHash string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, `"type":"answer_recorded"`) || !strings.Contains(line, id) {
			continue
		}
		var rec struct {
			Type   string            `json:"type"`
			Answer autonomous.Answer `json:"answer"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode answer line: %v (line=%q)", err, line)
		}
		if rec.Answer.Quote != quote {
			t.Fatalf("store file quote not byte-identical: %q != %q", rec.Answer.Quote, quote)
		}
		if rec.Answer.QuoteSHA256 != wantHash {
			t.Fatalf("store file hash %s != %s", rec.Answer.QuoteSHA256, wantHash)
		}
		return
	}
	t.Fatalf("no answer line for %q in %s", id, path)
}

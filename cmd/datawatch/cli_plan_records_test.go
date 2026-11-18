// CLI tests for `datawatch plan-record`, against the httptest fake daemon the
// decision record tests use (decRecordDaemon): it records every request and
// serves canned replies.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const planRecordOneBody = `{"record":{"title":"Move plans into datawatch","id":"mv-plans","uid":"u-1","kind":"registry_row","project":"datawatch","state":"planned","body":"Body text here.","legacy_number":396,"legacy_source":"docs/plans/README.md row 396","aliases":["Old name"],"links":[{"kind":"doc","ref":"docs/x.md"}],"needs_review":true,"review_reason":"suffixed","created_at":"2026-11-10T10:00:00Z","updated_at":"2026-11-10T11:00:00Z"}}`

const planRecordListBody = `{"records":[
 {"title":"First thing","id":"first-thing","project":"datawatch","state":"open","legacy_number":7},
 {"title":"Second thing","id":"second-thing","project":"datawatch","state":"shipped","needs_review":true}
],"total":5,"limit":2,"offset":0}`

const planRecordAmbiguousBody = `{"error":"\"Fix\" matches more than one record; say which one (use its id)","candidates":[{"title":"Fix the queue","id":"fix-queue","project":"datawatch","state":"open","kind":"finding"},{"title":"Fix the store","id":"fix-store","project":"other","state":"open","kind":"finding"}]}`

func runPlanRecordCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newPlanRecordCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"plan-record"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestPlanRecordCLI_CreateSendsContentAndShowsTitleFirst(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/plan-records/registry-rows": {code: 201, body: planRecordOneBody},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "create", "--kind", "registry-row", "--project", "datawatch",
		"--title", " Move plans into datawatch ", "--body", "Body text here.", "--state", "planned",
		"--legacy-number", "396", "--link", "doc=docs/x.md", "--alias", "Old name", "--needs-review")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if len(d.requests) != 1 {
		t.Fatalf("requests = %d", len(d.requests))
	}
	body := d.bodyJSON(t, 0)
	for k, want := range map[string]any{"project": "datawatch", "title": "Move plans into datawatch", "body": "Body text here.",
		"state": "planned", "legacy_number": float64(396), "needs_review": true} {
		if body[k] != want {
			t.Errorf("body[%s] = %v, want %v", k, body[k], want)
		}
	}
	if l, ok := body["links"].([]any); !ok || len(l) != 1 {
		t.Errorf("links = %v", body["links"])
	}
	if a, ok := body["aliases"].([]any); !ok || len(a) != 1 {
		t.Errorf("aliases = %v", body["aliases"])
	}
	if !strings.HasPrefix(out, "created: Move plans into datawatch  [mv-plans]") {
		t.Errorf("title must come first, id second; got %q", out)
	}
}

func TestPlanRecordCLI_CreateFindingUsesFindingsRoute(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/plan-records/findings": {code: 201, body: planRecordOneBody},
	}}
	withDecRecordServer(t, d)
	if _, err := runPlanRecordCmd(t, "create", "--kind", "finding", "--project", "datawatch", "--title", "T"); err != nil {
		t.Fatal(err)
	}
	if len(d.requests) != 1 || d.requests[0].path != "/api/plan-records/findings" {
		t.Errorf("requests = %+v", d.requests)
	}
}

func TestPlanRecordCLI_ArgumentValidationNeverCallsTheDaemon(t *testing.T) {
	d := &decRecordDaemon{}
	withDecRecordServer(t, d)
	cases := map[string][]string{
		"no kind":         {"create", "--project", "p", "--title", "t"},
		"bad kind":        {"create", "--kind", "story", "--project", "p", "--title", "t"},
		"no project":      {"create", "--kind", "finding", "--title", "t"},
		"no title":        {"create", "--kind", "finding", "--project", "p"},
		"bad link":        {"create", "--kind", "finding", "--project", "p", "--title", "t", "--link", "nokind"},
		"body and file":   {"create", "--kind", "finding", "--project", "p", "--title", "t", "--body", "a", "--body-file", "b"},
		"update nothing":  {"update", "some-id", "--kind", "finding"},
		"list bad kind":   {"list", "--kind", "zzz"},
		"search bad kind": {"search", "words", "--kind", "zzz"},
		"import bad src":  {"import", "--source", "everything"},
		"import bad pol":  {"import", "--policy", "overwrite"},
		"list neg limit":  {"list", "--limit", "-1"},
	}
	for name, args := range cases {
		if _, err := runPlanRecordCmd(t, args...); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(d.requests) != 0 {
		t.Errorf("a refused command reached the daemon: %+v", d.requests)
	}
}

func TestPlanRecordCLI_GetRendersTitleThenIDAndDetails(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/registry-rows/row 396": {body: planRecordOneBody},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "get", "row 396", "--kind", "registry-row", "--project", "datawatch")
	if err != nil {
		t.Fatalf("get: %v\n%s", err, out)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "Move plans into datawatch" || !strings.HasPrefix(lines[1], "id:") || !strings.Contains(lines[1], "mv-plans") {
		t.Errorf("title then id expected, got %q / %q", lines[0], lines[1])
	}
	for _, want := range []string{"old row:", "396", "older titles:", "Old name", "needs review: suffixed", "Body text here.", "doc docs/x.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if d.requests[0].query != "project=datawatch" {
		t.Errorf("query = %q", d.requests[0].query)
	}
}

func TestPlanRecordCLI_GetWithoutKindFindsTheFinding(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/registry-rows/fix-queue": {code: 404, body: `{"error":"no plan record matches that reference"}`},
		"GET /api/plan-records/findings/fix-queue":      {body: strings.ReplaceAll(planRecordOneBody, "registry_row", "finding")},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "get", "fix-queue")
	if err != nil {
		t.Fatalf("get: %v\n%s", err, out)
	}
	if len(d.requests) != 2 {
		t.Errorf("requests = %d, want 2 (one per kind)", len(d.requests))
	}
}

func TestPlanRecordCLI_GetNotFoundInEitherKindFails(t *testing.T) {
	nf := decRecordReply{code: 404, body: `{"error":"no plan record matches that reference"}`}
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/registry-rows/zzz": nf, "GET /api/plan-records/findings/zzz": nf,
	}}
	withDecRecordServer(t, d)
	if _, err := runPlanRecordCmd(t, "get", "zzz"); err == nil || !strings.Contains(err.Error(), "no plan record matches") {
		t.Errorf("want a not-found error, got %v", err)
	}
}

func TestPlanRecordCLI_AmbiguousTitlePrintsCandidatesAndFails(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/findings/Fix": {code: 409, body: planRecordAmbiguousBody},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "get", "Fix", "--kind", "finding")
	if err == nil {
		t.Fatal("an ambiguous reference must exit non-zero")
	}
	for _, want := range []string{"Candidates:", "Fix the queue  [fix-queue]", "Fix the store  [fix-store]", "pass its id"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Update must not write when the reference is ambiguous.
	d.requests = nil
	d.routes["GET /api/plan-records/findings/Fix"] = decRecordReply{code: 409, body: planRecordAmbiguousBody}
	if _, err := runPlanRecordCmd(t, "update", "Fix", "--kind", "finding", "--state", "fixed"); err == nil {
		t.Fatal("update of an ambiguous reference must fail")
	}
	for _, r := range d.requests {
		if r.method == "PUT" {
			t.Errorf("an ambiguous update sent a PUT: %+v", r)
		}
	}
}

func TestPlanRecordCLI_SameTitleInBothKindsIsAmbiguous(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/registry-rows/Shared": {body: planRecordOneBody},
		"GET /api/plan-records/findings/Shared":      {body: strings.ReplaceAll(planRecordOneBody, "mv-plans", "other-id")},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "get", "Shared")
	if err == nil || !strings.Contains(out, "say which with --kind") {
		t.Errorf("want an ambiguity error naming --kind; err=%v out=%q", err, out)
	}
}

func TestPlanRecordCLI_UpdateSendsOnlyChangedFields(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/registry-rows/mv-plans": {body: planRecordOneBody},
		"PUT /api/plan-records/registry-rows/mv-plans": {body: planRecordOneBody},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "update", "mv-plans", "--kind", "registry-row", "--state", "shipped", "--add-alias", "Older", "--review-reason", "")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	var put *decRecordRequest
	for i := range d.requests {
		if d.requests[i].method == "PUT" {
			put = &d.requests[i]
		}
	}
	if put == nil {
		t.Fatal("no PUT sent")
	}
	body := d.bodyJSON(t, len(d.requests)-1)
	if body["state"] != "shipped" {
		t.Errorf("body = %v", body)
	}
	if _, has := body["title"]; has {
		t.Errorf("title sent although not given: %v", body)
	}
	if v, has := body["review_reason"]; !has || v != "" {
		t.Errorf("empty --review-reason must be sent to clear it: %v", body)
	}
	if a, ok := body["add_aliases"].([]any); !ok || len(a) != 1 {
		t.Errorf("add_aliases = %v", body["add_aliases"])
	}
	if !strings.HasPrefix(out, "updated: Move plans into datawatch  [mv-plans]") {
		t.Errorf("output = %q", out)
	}
}

func TestPlanRecordCLI_BodyFileIsReadAndSent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte("from a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/plan-records/findings": {code: 201, body: planRecordOneBody},
	}}
	withDecRecordServer(t, d)
	if _, err := runPlanRecordCmd(t, "create", "--kind", "finding", "--project", "datawatch", "--title", "T", "--body-file", p); err != nil {
		t.Fatal(err)
	}
	if got := d.bodyJSON(t, 0)["body"]; got != "from a file\n" {
		t.Errorf("body = %q", got)
	}
}

func TestPlanRecordCLI_ListPinnedKindFiltersAndShowsMore(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/registry-rows": {body: planRecordListBody},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "list", "--kind", "registry-row", "--project", "datawatch", "--state", "open", "--limit", "2")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	q := d.requests[0].query
	for _, want := range []string{"project=datawatch", "state=open", "limit=2"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q lacks %s", q, want)
		}
	}
	if !strings.Contains(out, "First thing  [first-thing]  datawatch  open  row 7") ||
		!strings.Contains(out, "Second thing  [second-thing]  datawatch  shipped  needs review") ||
		!strings.Contains(out, "2 of 5 shown") {
		t.Errorf("output = %q", out)
	}
}

func TestPlanRecordCLI_ListWithoutKindCoversBothKinds(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/registry-rows": {body: planRecordListBody},
		"GET /api/plan-records/findings":      {body: `{"records":[],"total":0}`},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.requests) != 2 || !strings.Contains(out, "== registry-rows ==") || !strings.Contains(out, "== findings ==") || !strings.Contains(out, "No plan records.") {
		t.Errorf("requests=%d out=%q", len(d.requests), out)
	}
	out, err = runPlanRecordCmd(t, "list", "--json")
	if err != nil || !strings.Contains(out, `"registry-rows"`) || !strings.Contains(out, `"findings"`) {
		t.Errorf("combined json = %q err=%v", out, err)
	}
}

func TestPlanRecordCLI_SearchSendsQueryAndRendersMatch(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/search": {body: `{"hits":[{"record":{"title":"Boot resume","id":"boot-resume","project":"datawatch","state":"open"},"score":9,"matched":"title"}],"total":1}`},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "search", "boot", "resume", "--kind", "finding", "--project", "datawatch")
	if err != nil {
		t.Fatal(err)
	}
	q := d.requests[0].query
	for _, want := range []string{"q=boot+resume", "kind=finding", "project=datawatch"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q lacks %s", q, want)
		}
	}
	if !strings.Contains(out, "Boot resume  [boot-resume]  datawatch  open  (matched title)") {
		t.Errorf("output = %q", out)
	}
}

const planRecordImportBody = `{"note":"Preview only: nothing was written.","result":{"project":"datawatch","dry_run":true,"policy":"suffix","counts":{"create":3,"unchanged":1,"changed":0,"conflicts":1,"skipped":0},"conflicts":[{"source":"docs/plans/README.md row 12","title":"Same title","reason":"title","with":"same-title","action":"suffix","new_title":"Same title (row 12)"}],"unparseable":[{"source":"README row 99","reason":"no title"}],"files":["docs/plans/README.md"]}}`

func TestPlanRecordCLI_ImportPreviewsByDefault(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/plan-records/import": {body: planRecordImportBody},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "import", "--project", "datawatch", "--source", "registry", "--policy", "skip")
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if d.requests[0].query != "dry_run=true" {
		t.Errorf("query = %q, want dry_run=true by default", d.requests[0].query)
	}
	body := d.bodyJSON(t, 0)
	if body["project"] != "datawatch" || body["source"] != "registry" || body["conflict_policy"] != "skip" {
		t.Errorf("body = %v", body)
	}
	for _, want := range []string{"Preview for project datawatch", "create 3, unchanged 1", "conflict (title): Same title — suffix → Same title (row 12) (with same-title)",
		"not imported: README row 99", "--dry-run=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestPlanRecordCLI_ImportWritesOnlyWhenAsked(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/plan-records/import": {body: strings.ReplaceAll(planRecordImportBody, `"dry_run":true`, `"dry_run":false`)},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "import", "--dry-run=false")
	if err != nil {
		t.Fatal(err)
	}
	if d.requests[0].query != "dry_run=false" {
		t.Errorf("query = %q", d.requests[0].query)
	}
	if !strings.Contains(out, "Import for project datawatch") || strings.Contains(out, "To write these records") {
		t.Errorf("output = %q", out)
	}
}

func TestPlanRecordCLI_JSONFlagPrintsRawBody(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/plan-records/findings/x": {body: planRecordOneBody},
	}}
	withDecRecordServer(t, d)
	out, err := runPlanRecordCmd(t, "get", "x", "--kind", "finding", "--json")
	if err != nil || !strings.Contains(out, `"uid": "u-1"`) {
		t.Errorf("out=%q err=%v", out, err)
	}
}

func TestPlanRecordCLI_HelpNamesEverySubcommand(t *testing.T) {
	out, err := runPlanRecordCmd(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"create", "get", "update", "list", "search", "import", "--kind", "--dry-run=false", "title prefix", "exits non-zero"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	for _, banned := range []string{"PRD", "BL4", "GH#"} {
		if strings.Contains(out, banned) {
			t.Errorf("help mentions %q", banned)
		}
	}
}

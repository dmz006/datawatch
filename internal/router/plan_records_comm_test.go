// plan_records_comm_test.go — the plan record verbs over the comm channel:
// list, get, create, update, search, import, ambiguous references, argument
// validation, the usage line and help text, and the verbs list against the
// handler.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// planCommReq is one request the fake daemon saw.
type planCommReq struct{ method, path, query, body string }

// planCommDaemon serves canned replies by "METHOD /path" and records requests.
func planCommDaemon(t *testing.T, routes map[string]struct {
	code int
	body string
}) (*[]planCommReq, int) {
	t.Helper()
	var mu sync.Mutex
	var seen []planCommReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, planCommReq{r.Method, r.URL.Path, r.URL.RawQuery, string(raw)})
		mu.Unlock()
		rep, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no plan record matches that reference"}`))
			return
		}
		if rep.code != 0 {
			w.WriteHeader(rep.code)
		}
		_, _ = w.Write([]byte(rep.body))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	return &seen, port
}

type planCommRoute = struct {
	code int
	body string
}

const planCommRec = `{"record":{"title":"Move plans into datawatch","id":"mv-plans","uid":"u1","kind":"registry_row","project":"datawatch","state":"planned","legacy_number":396}}`

func TestPlanRecordComm_ListShowsTitleFirstForBothKinds(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plan-records/registry-rows": {0, `{"records":[{"title":"Move plans into datawatch","id":"mv-plans","project":"datawatch","state":"planned","legacy_number":396},{"title":"Second","id":"second","project":"datawatch","state":"open","needs_review":true}]}`},
		"GET /api/plan-records/findings":      {0, `{"records":[]}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan-records project=datawatch"), "\n")
	if len(*seen) != 2 {
		t.Fatalf("requests = %d, want one per kind", len(*seen))
	}
	for _, rq := range *seen {
		if !strings.Contains(rq.query, "limit=10") || !strings.Contains(rq.query, "project=datawatch") {
			t.Errorf("query %q lacks limit=10 and project", rq.query)
		}
	}
	for _, want := range []string{"== registry-rows ==", "Move plans into datawatch [mv-plans] datawatch planned row 396",
		"Second [second] datawatch open needs-review", "== findings ==", "No plan records to show."} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	// The same through the explicit list verb and with one kind.
	*seen = nil
	r.HandleTestMessage("autonomous plan-record list finding state=open")
	if len(*seen) != 1 || (*seen)[0].path != "/api/plan-records/findings" || !strings.Contains((*seen)[0].query, "state=open") {
		t.Errorf("requests = %+v", *seen)
	}
}

func TestPlanRecordComm_ListCutsLongTitleAndCapsAtTen(t *testing.T) {
	long := strings.Repeat("word ", 30)
	var recs []string
	for i := 0; i < 14; i++ {
		recs = append(recs, `{"title":"`+long+`","id":"id-`+strconv.Itoa(i)+`","project":"p","state":"open"}`)
	}
	_, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plan-records/registry-rows": {0, `{"records":[` + strings.Join(recs, ",") + `]}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan-records registry-row"), "\n")
	if strings.Contains(got, strings.TrimSpace(long)) || !strings.Contains(got, "\u2026") {
		t.Errorf("long titles must be cut with an ellipsis:\n%s", got)
	}
	if strings.Contains(got, "id-10") || !strings.Contains(got, "id-9") {
		t.Errorf("a reply holds ten lines at most:\n%s", got)
	}
}

func TestPlanRecordComm_GetFindsTheKindAndPutsTitleThenID(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plan-records/findings/row 396": {0, planCommRec},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan-record get row 396"), "\n")
	if len(*seen) != 2 || (*seen)[0].path != "/api/plan-records/registry-rows/row 396" {
		t.Errorf("requests = %+v, want registry rows asked first, then findings", *seen)
	}
	i := strings.Index(got, "Move plans into datawatch\nid mv-plans")
	if i < 0 {
		t.Errorf("title then id expected:\n%s", got)
	}
}

func TestPlanRecordComm_AmbiguousTitleRepliesWithCandidates(t *testing.T) {
	amb := `{"error":"\"Fix\" matches more than one record; say which one (use its id)","candidates":[{"title":"Fix the queue","id":"fix-queue","project":"datawatch","state":"open","kind":"finding"},{"title":"Fix the store","id":"fix-store","project":"other","state":"open","kind":"finding"}]}`
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plan-records/registry-rows/Fix": {http.StatusConflict, amb},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan-record get Fix"), "\n")
	for _, want := range []string{"matches more than one record", "Fix the queue [fix-queue]", "Fix the store [fix-store]", "send its id"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	// Update refuses too and never sends a PUT.
	*seen = nil
	got = strings.Join(r.HandleTestMessage(`autonomous plan-record update Fix {"state":"fixed"}`), "\n")
	if !strings.Contains(got, "Fix the queue [fix-queue]") {
		t.Errorf("update reply lacks the candidates:\n%s", got)
	}
	for _, rq := range *seen {
		if rq.method == http.MethodPut {
			t.Errorf("an ambiguous update sent a PUT: %+v", rq)
		}
	}
}

func TestPlanRecordComm_GetNotFoundAndBothKindsAmbiguous(t *testing.T) {
	_, port := planCommDaemon(t, map[string]planCommRoute{})
	r := setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous plan-record get zzz"), " "); !strings.Contains(got, `no plan record matches "zzz"`) {
		t.Errorf("reply = %q", got)
	}
	_, port = planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plan-records/registry-rows/Shared": {0, planCommRec},
		"GET /api/plan-records/findings/Shared":      {0, planCommRec},
	})
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous plan-record get Shared"), " "); !strings.Contains(got, "matches a registry row and a finding") {
		t.Errorf("reply = %q", got)
	}
}

func TestPlanRecordComm_CreateRoutesByKindAndStripsIt(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plan-records/findings": {http.StatusCreated, planCommRec},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage(`autonomous plan-record create {"kind":"finding","project":"datawatch","title":"A finding","body":"text"}`), "\n")
	if len(*seen) != 1 || (*seen)[0].method != http.MethodPost {
		t.Fatalf("requests = %+v", *seen)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte((*seen)[0].body), &sent); err != nil {
		t.Fatal(err)
	}
	if _, has := sent["kind"]; has || sent["title"] != "A finding" || sent["project"] != "datawatch" {
		t.Errorf("body = %v; kind must come from the route", sent)
	}
	if !strings.Contains(got, "Move plans into datawatch [mv-plans] datawatch planned row 396") {
		t.Errorf("reply = %q", got)
	}
}

func TestPlanRecordComm_UpdateResolvesThenPuts(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plan-records/registry-rows/Move plans": {0, planCommRec},
		"PUT /api/plan-records/registry-rows/Move plans": {0, planCommRec},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage(`autonomous plan-record update Move plans {"kind":"registry-row","project":"datawatch","state":"shipped"}`), "\n")
	var put *planCommReq
	for i := range *seen {
		if (*seen)[i].method == http.MethodPut {
			put = &(*seen)[i]
		}
	}
	if put == nil {
		t.Fatalf("no PUT: %+v", *seen)
	}
	if put.body != `{"state":"shipped"}` || put.query != "project=datawatch" {
		t.Errorf("PUT body=%q query=%q; kind and project must not ride in the body", put.body, put.query)
	}
	if !strings.Contains(got, "[mv-plans]") {
		t.Errorf("reply = %q", got)
	}
}

func TestPlanRecordComm_SearchRendersHits(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plan-records/search": {0, `{"hits":[{"record":{"title":"Boot resume","id":"boot-resume","project":"datawatch","state":"open"},"matched":"title"}]}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan-record search boot resume"), "\n")
	if !strings.Contains((*seen)[0].query, "q=boot+resume") || !strings.Contains((*seen)[0].query, "limit=10") {
		t.Errorf("query = %q", (*seen)[0].query)
	}
	if !strings.Contains(got, "Boot resume [boot-resume] datawatch open (matched title)") {
		t.Errorf("reply = %q", got)
	}
	_, port = planCommDaemon(t, map[string]planCommRoute{"GET /api/plan-records/search": {0, `{"hits":[]}`}})
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous plan-record search nothing"), " "); !strings.Contains(got, "No plan records match.") {
		t.Errorf("empty reply = %q", got)
	}
}

const planCommImport = `{"note":"x","result":{"project":"datawatch","dry_run":true,"policy":"suffix","counts":{"create":3,"unchanged":0,"changed":0,"conflicts":1,"skipped":0},"conflicts":[{"title":"Same","reason":"title","action":"suffix","new_title":"Same (row 12)"}]}}`

func TestPlanRecordComm_ImportDryRunPreviewsAndPlainWrites(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plan-records/import": {0, planCommImport},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan-record import dry-run project=datawatch source=registry policy=skip"), "\n")
	if (*seen)[0].query != "dry_run=true" {
		t.Errorf("query = %q", (*seen)[0].query)
	}
	var sent map[string]string
	_ = json.Unmarshal([]byte((*seen)[0].body), &sent)
	if sent["project"] != "datawatch" || sent["source"] != "registry" || sent["conflict_policy"] != "skip" {
		t.Errorf("body = %q", (*seen)[0].body)
	}
	for _, want := range []string{"Preview for datawatch (policy suffix): create 3", "conflict (title): Same — suffix → Same (row 12)", "Nothing was written"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	r.HandleTestMessage("autonomous plan-record import")
	if (*seen)[1].query != "dry_run=false" {
		t.Errorf("a plain import must write: query = %q", (*seen)[1].query)
	}
}

func TestPlanRecordComm_BadVerbAndMissingArgsShowUsageAndSendNothing(t *testing.T) {
	for _, msg := range []string{
		"autonomous plan-record",
		"autonomous plan-record frobnicate",
		"autonomous plan-record get",
		"autonomous plan-record create",
		"autonomous plan-record create not json",
		`autonomous plan-record create {"project":"datawatch","title":"no kind"}`,
		`autonomous plan-record create {"kind":"story","title":"bad kind"}`,
		"autonomous plan-record update Fix",
		`autonomous plan-record update {"state":"fixed"}`,
		`autonomous plan-record update Fix {}`,
		"autonomous plan-record search",
		"autonomous plan-record import source=everything",
		"autonomous plan-record import policy=overwrite",
		"autonomous plan-record import bogus",
		"autonomous plan-records widget",
		"autonomous plan-records project=",
	} {
		seen, port := planCommDaemon(t, map[string]planCommRoute{})
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "plan-record") || !strings.Contains(got, "failed") {
			t.Errorf("%q: expected a failure reply, got %q", msg, got)
		}
		if len(*seen) != 0 {
			t.Errorf("%q: a local refusal must not reach the daemon, got %+v", msg, *seen)
		}
	}
	// A bad verb shows the usage line.
	_, port := planCommDaemon(t, map[string]planCommRoute{})
	r := setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous plan-record"), " "); !strings.Contains(got, "usage: autonomous plan-records") {
		t.Errorf("expected the usage, got %q", got)
	}
}

func TestPlanRecordComm_HelpAndVerbsListMatchHandler(t *testing.T) {
	r := &Router{}
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	for _, want := range []string{"plan-records [registry-row|finding]", "plan-record get <ref>", "plan-record create <json>",
		"plan-record update <ref> <json>", "plan-record search <text>", "plan-record import [dry-run]"} {
		if !strings.Contains(help, want) {
			t.Errorf("autonomous help lacks %q", want)
		}
	}
	for _, banned := range []string{"BL4", "GH#"} {
		if strings.Contains(planRecordsUsage, banned) {
			t.Errorf("usage mentions %q", banned)
		}
	}
	verbs := PlanRecordsCommVerbs()
	for _, want := range []string{"plan-records", "plan-record"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("PlanRecordsCommVerbs() = %v, must contain %q", verbs, want)
		}
	}
	for _, verb := range verbs {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
		if got := strings.Join(r.HandleTestMessage("autonomous "+verb), " "); strings.Contains(got, "unknown verb") {
			t.Errorf("verb %q reached the unknown-verb help: %q", verb, got)
		}
	}
}

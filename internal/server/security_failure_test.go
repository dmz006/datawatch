package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// securityFailureFake is the double for the security failure read route:
// a fixed set of stored records and a count of reads, so the tests write
// to the store only through the fake and can prove a refused request
// never reaches it.
type securityFailureFake struct {
	fakeOrchAutonomous
	records []*autonomous.RepoCIWatchRecord
	calls   int
}

func (f *securityFailureFake) ListRepoCIWatchRecords() []*autonomous.RepoCIWatchRecord {
	f.calls++
	return f.records
}

func securityFailureGET(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSecurityFailures(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/security-failures"+query, nil))
	return rec
}

func securityFailureBase() time.Time { return time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) }

// securityFailureFullReport has every SecurityFailureReport field set
// to a distinct, non-zero value.
func securityFailureFullReport() *autonomous.SecurityFailureReport {
	return &autonomous.SecurityFailureReport{
		Dependency:         "golang.org/x/net",
		AdvisoryIDs:        []string{"GHSA-aaaa-bbbb-cccc", "CVE-2026-0001"},
		Workflow:           "security.yml",
		RunID:              9001,
		RunURL:             "https://github.com/dmz006/datawatch/actions/runs/9001",
		SHA:                "abc123",
		PublishedAt:        "2026-10-27T08:00:00Z",
		FixedVersion:       "0.40.1",
		RegistryFound:      true,
		RegistryID:         "GHSA-aaaa-bbbb-cccc",
		RegistryFirstAdded: "2026-10-01",
		RegistryAdded:      "2026-10-10",
		RegistryExpires:    "2026-12-10",
		RegistryError:      "registry warning",
		LookupError:        "lookup warning",
		Recommendation:     "upgrade",
		Note:               "a note",
		Mode:               "on",
		Action:             "stopped",
		AssessedAt:         "2026-10-29T09:00:00Z",
	}
}

// securityFailureFixture is the store the route reads: two records with
// a report, one with none, across two repositories. The refresh times
// differ on purpose — the route returns newest first whatever order the
// store yields them.
func securityFailureFixture() []*autonomous.RepoCIWatchRecord {
	base := securityFailureBase()
	other := securityFailureFullReport()
	other.Workflow, other.RunID, other.Action, other.Mode = "codeql.yml", 9002, "report_only", "shadow"
	return []*autonomous.RepoCIWatchRecord{
		{
			Repo: "dmz006/datawatch", RefKind: "main", RefName: "main",
			SHA: "abc123", Workflow: "security.yml", RunID: "9001",
			RunURL: "https://github.com/dmz006/datawatch/actions/runs/9001",
			Status: "failing", Classification: "security",
			FirstSeenAt: base, UpdatedAt: base,
			Security: securityFailureFullReport(),
		},
		{
			Repo: "dmz006/datawatch", RefKind: "main", RefName: "main",
			SHA: "def456", Workflow: "ci.yml", RunID: "9003",
			Status: "failing", Classification: "code_or_test",
			FirstSeenAt: base, UpdatedAt: base.Add(5 * time.Minute),
		},
		{
			Repo: "dmz006/other", RefKind: "tag", RefName: "v1.2.0",
			SHA: "fed789", Workflow: "codeql.yml", RunID: "9002",
			RunURL: "https://github.com/dmz006/other/actions/runs/9002",
			Status: "superseded", Classification: "security",
			FirstSeenAt: base, UpdatedAt: base.Add(time.Minute),
			Security: other,
		},
	}
}

func securityFailureEntries(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var got []map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	return got
}

func TestSecurityFailureRouteEmptyList(t *testing.T) {
	s := &Server{autonomousMgr: &securityFailureFake{}}
	rec := securityFailureGET(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s, want an empty list []", got)
	}

	// Records without a report are not security failure reports.
	s = &Server{autonomousMgr: &securityFailureFake{records: securityFailureFixture()[1:2]}}
	if got := strings.TrimSpace(securityFailureGET(t, s, "").Body.String()); got != "[]" {
		t.Errorf("records without a report: body = %s, want []", got)
	}
}

func TestSecurityFailureRouteReturnsStoredReportWithEveryField(t *testing.T) {
	s := &Server{autonomousMgr: &securityFailureFake{records: securityFailureFixture()[:1]}}
	rec := securityFailureGET(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	entries := securityFailureEntries(t, rec.Body.Bytes())
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %s", len(entries), rec.Body)
	}
	e := entries[0]
	for key, want := range map[string]string{
		"repo": "dmz006/datawatch", "ref_kind": "main", "ref_name": "main",
		"workflow": "security.yml", "run_id": "9001",
		"run_url": "https://github.com/dmz006/datawatch/actions/runs/9001",
		"status":  "failing",
	} {
		if e[key] != want {
			t.Errorf("%s = %v, want %q", key, e[key], want)
		}
	}

	// The report must come back whole: decode it into the report type and
	// compare with what the fake stored, then check every json key of the
	// type is present in the body.
	raw, _ := json.Marshal(e["report"])
	var got autonomous.SecurityFailureReport
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if !reflect.DeepEqual(got, *securityFailureFullReport()) {
		t.Errorf("report = %+v\nwant   %+v", got, *securityFailureFullReport())
	}
	report, _ := e["report"].(map[string]any)
	typ := reflect.TypeOf(autonomous.SecurityFailureReport{})
	for i := 0; i < typ.NumField(); i++ {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if _, ok := report[key]; !ok {
			t.Errorf("report lacks field %q", key)
		}
	}
}

func TestSecurityFailureRouteFiltersApply(t *testing.T) {
	s := &Server{autonomousMgr: &securityFailureFake{records: securityFailureFixture()}}

	runIDs := func(query string) string {
		rec := securityFailureGET(t, s, query)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d: %s", query, rec.Code, rec.Body.String())
		}
		var ids []string
		for _, e := range securityFailureEntries(t, rec.Body.Bytes()) {
			ids = append(ids, e["run_id"].(string))
		}
		return strings.Join(ids, ",")
	}

	// Newest first: the superseded run was refreshed a minute after the
	// failing one.
	if got := runIDs(""); got != "9002,9001" {
		t.Errorf("no filter: run ids = %q, want 9002,9001", got)
	}
	if got := runIDs("?repo=dmz006/other"); got != "9002" {
		t.Errorf("repo filter: run ids = %q, want 9002", got)
	}
	if got := runIDs("?status=failing"); got != "9001" {
		t.Errorf("status filter: run ids = %q, want 9001", got)
	}
	if got := runIDs("?repo=dmz006/datawatch&status=failing"); got != "9001" {
		t.Errorf("both filters: run ids = %q, want 9001", got)
	}
	if got := runIDs("?repo=dmz006/other&status=failing"); got != "" {
		t.Errorf("filters that match nothing: run ids = %q, want none", got)
	}
	if body := securityFailureGET(t, s, "?repo=nobody/nothing").Body.String(); strings.TrimSpace(body) != "[]" {
		t.Errorf("a repo that matches nothing: body = %s, want []", body)
	}

	rec := securityFailureGET(t, s, "?status=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown status: want 400, got %d", rec.Code)
	}
	for _, name := range autonomous.RepoCIWatchStatuses {
		if !strings.Contains(rec.Body.String(), name) {
			t.Errorf("400 body does not name %q: %s", name, rec.Body)
		}
	}
}

func TestSecurityFailureRouteRejectsNonGET(t *testing.T) {
	fake := &securityFailureFake{records: securityFailureFixture()}
	s := &Server{autonomousMgr: fake}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleSecurityFailures(rec, httptest.NewRequest(method, "/api/autonomous/security-failures", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	if fake.calls != 0 {
		t.Errorf("a refused request read the store %d times", fake.calls)
	}

	// The exported handler used by the surface tests refuses them too.
	rec := httptest.NewRecorder()
	SecurityFailureHandler(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/security-failures", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("SecurityFailureHandler POST: want 405, got %d", rec.Code)
	}
}

func TestSecurityFailureRouteUnavailableWithoutAutonomous(t *testing.T) {
	rec := securityFailureGET(t, &Server{}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no manager: want 503, got %d", rec.Code)
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

func TestAuditQuery_RESTFilters(t *testing.T) {
	s := bl90Server(t)
	l, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close() //nolint:errcheck
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for i, e := range []audit.Entry{
		{Actor: "operator", Action: "start", SessionID: "s1"},
		{Actor: "daemon", Action: audit.ActionCapacityWait, Subject: "task-1"},
		{Actor: "daemon", Action: audit.ActionBudgetCeilingWait, Subject: "claude"},
		{Actor: "daemon", Action: audit.ActionBudgetWindowResumed, Subject: "claude"},
		{Actor: "daemon", Action: audit.ActionCapacityAdmitted, Subject: "task-1"},
	} {
		e.Timestamp = base.Add(time.Duration(i) * time.Minute)
		if err := l.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	s.SetAuditLog(l)

	query := func(q url.Values) (int, []audit.Entry, string) {
		req := httptest.NewRequest(http.MethodGet, "/api/audit?"+q.Encode(), nil)
		rr := httptest.NewRecorder()
		s.handleAudit(rr, req)
		var out struct {
			Count   int           `json:"count"`
			Entries []audit.Entry `json:"entries"`
		}
		if rr.Code == http.StatusOK {
			if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.Count != len(out.Entries) {
				t.Errorf("count %d != %d entries", out.Count, len(out.Entries))
			}
		}
		return rr.Code, out.Entries, rr.Body.String()
	}
	actions := func(es []audit.Entry) string {
		var a []string
		for _, e := range es {
			a = append(a, e.Action)
		}
		return strings.Join(a, ",")
	}

	cases := []struct {
		name string
		q    url.Values
		want string
	}{
		{"default newest first", url.Values{}, strings.Join([]string{audit.ActionCapacityAdmitted, audit.ActionBudgetWindowResumed, audit.ActionBudgetCeilingWait, audit.ActionCapacityWait, "start"}, ",")},
		{"exact action", url.Values{"action": {audit.ActionCapacityWait}}, audit.ActionCapacityWait},
		{"prefix action", url.Values{"action": {"autonomous_budget_*"}}, audit.ActionBudgetWindowResumed + "," + audit.ActionBudgetCeilingWait},
		{"subject", url.Values{"subject": {"task-1"}}, audit.ActionCapacityAdmitted + "," + audit.ActionCapacityWait},
		{"subject and prefix", url.Values{"subject": {"claude"}, "action": {"autonomous_*"}}, audit.ActionBudgetWindowResumed + "," + audit.ActionBudgetCeilingWait},
		{"actor", url.Values{"actor": {"operator"}}, "start"},
		{"session_id", url.Values{"session_id": {"s1"}}, "start"},
		{"since", url.Values{"since": {base.Add(3 * time.Minute).Format(time.RFC3339)}}, audit.ActionCapacityAdmitted + "," + audit.ActionBudgetWindowResumed},
		{"until", url.Values{"until": {base.Add(2 * time.Minute).Format(time.RFC3339)}}, audit.ActionCapacityWait + ",start"},
		{"limit", url.Values{"limit": {"1"}}, audit.ActionCapacityAdmitted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, es, body := query(tc.q)
			if code != http.StatusOK {
				t.Fatalf("status %d: %s", code, body)
			}
			if got := actions(es); got != tc.want {
				t.Errorf("actions = %q, want %q", got, tc.want)
			}
		})
	}

	// More than the default limit exists: the default cap is 100.
	for i := 0; i < 120; i++ {
		_ = l.Write(audit.Entry{Actor: "bulk", Action: "bulk"})
	}
	if _, es, _ := query(url.Values{}); len(es) != audit.DefaultQueryLimit {
		t.Errorf("default limit returned %d entries, want %d", len(es), audit.DefaultQueryLimit)
	}

	// An unparseable since or until is rejected, never silently ignored.
	for _, name := range []string{"since", "until"} {
		code, _, body := query(url.Values{name: {"not-a-time"}})
		if code != http.StatusBadRequest {
			t.Errorf("%s=not-a-time: status %d, want 400", name, code)
		}
		if !strings.Contains(body, "invalid "+name) {
			t.Errorf("%s error body = %q", name, body)
		}
	}
}

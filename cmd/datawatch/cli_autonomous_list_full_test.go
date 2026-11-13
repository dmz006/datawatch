package main

import (
	"net/url"
	"strings"
	"testing"
)

// TestAutonomousListFull_DefaultHasNoFullFlag: the default list is the summary
// list, so the query carries no full key and no paging.
func TestAutonomousListFull_DefaultHasNoFullFlag(t *testing.T) {
	q, err := prdListQueryOpts("", "", false, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 0 {
		t.Errorf("default query = %q; want empty", q.Encode())
	}
}

// TestAutonomousListFull_FullFlagSetsFullTrue: --full asks for whole records.
func TestAutonomousListFull_FullFlagSetsFullTrue(t *testing.T) {
	q, err := prdListQueryOpts("", "", true, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Get("full"); got != "true" {
		t.Errorf("full = %q; want true (query %q)", got, q.Encode())
	}
}

// TestAutonomousListFull_PagingAndFilters: --limit, --cursor and the filters
// compose; a negative limit is refused before any request.
func TestAutonomousListFull_PagingAndFilters(t *testing.T) {
	q, err := prdListQueryOpts("running,shipped", "false", true, 25, "abc")
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{"full": {"true"}, "limit": {"25"}, "cursor": {"abc"}, "status": {"running,shipped"}, "shipped": {"false"}}
	if q.Encode() != want.Encode() {
		t.Errorf("query = %q; want %q", q.Encode(), want.Encode())
	}
	if _, err := prdListQueryOpts("", "", false, -1, ""); err == nil {
		t.Error("negative limit: expected an error")
	}
}

// TestAutonomousListFull_TasksPath: the tasks command builds the paged URL.
func TestAutonomousListFull_TasksPath(t *testing.T) {
	cases := []struct {
		name                  string
		id                    string
		limit                 int
		cursor, story, status string
		want                  string
	}{
		{"bare", "cc29", 0, "", "", "", "/api/autonomous/prds/cc29/tasks"},
		{"limit and cursor", "cc29", 10, "Zm9v", "", "", "/api/autonomous/prds/cc29/tasks?cursor=Zm9v&limit=10"},
		{"filters", "cc29", 0, "", "s1", "failed,blocked", "/api/autonomous/prds/cc29/tasks?status=failed%2Cblocked&story=s1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := prdTasksPath(tc.id, tc.limit, tc.cursor, tc.story, tc.status)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("path = %q; want %q", got, tc.want)
			}
		})
	}
	if _, err := prdTasksPath(" ", 0, "", "", ""); err == nil {
		t.Error("empty id: expected an error")
	}
	if _, err := prdTasksPath("x", -2, "", "", ""); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("negative limit error = %v", err)
	}
}

// TestAutonomousListFull_CommandsRegistered: the list flags and the tasks
// command exist.
func TestAutonomousListFull_CommandsRegistered(t *testing.T) {
	c := newAutonomousPRDListCmd()
	for _, f := range []string{"full", "limit", "cursor", "status", "shipped"} {
		if c.Flags().Lookup(f) == nil {
			t.Errorf("prd-list missing --%s", f)
		}
	}
	tc := newAutonomousPRDTasksCmd()
	for _, f := range []string{"limit", "cursor", "story", "status"} {
		if tc.Flags().Lookup(f) == nil {
			t.Errorf("tasks missing --%s", f)
		}
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/dmz006/datawatch/internal/audit"
)

func TestAuditQuery_CLIFilters(t *testing.T) {
	// The flags are exactly the shared filter set, with '-' for '_'.
	var flags []string
	newAuditCmd().Flags().VisitAll(func(f *pflag.Flag) {
		flags = append(flags, strings.ReplaceAll(f.Name, "-", "_"))
	})
	want := slices.Clone(audit.QueryParamNames)
	sort.Strings(flags)
	sort.Strings(want)
	if !slices.Equal(flags, want) {
		t.Errorf("flags = %v, want %v", flags, want)
	}

	var gotPath string
	var gotQuery url.Values
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		_, _ = w.Write([]byte(`{"count":0,"entries":[]}`))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()

	t.Run("every flag is forwarded under its filter name", func(t *testing.T) {
		c := newAuditCmd()
		c.SetArgs([]string{
			"--actor", "daemon", "--action", "autonomous_budget_*", "--subject", "claude",
			"--session-id", "s1", "--since", "2026-10-22T12:00:00+02:00", "--until", "2026-10-22T13:00:00Z", "--limit", "5",
		})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if gotPath != "/api/audit" {
			t.Errorf("path = %q", gotPath)
		}
		for k, v := range map[string]string{
			"actor": "daemon", "action": "autonomous_budget_*", "subject": "claude",
			"session_id": "s1", "since": "2026-10-22T12:00:00+02:00", "until": "2026-10-22T13:00:00Z", "limit": "5",
		} {
			if gotQuery.Get(k) != v {
				t.Errorf("forwarded %s = %q, want %q", k, gotQuery.Get(k), v)
			}
		}
	})

	t.Run("defaults send only the default limit", func(t *testing.T) {
		c := newAuditCmd()
		c.SetArgs(nil)
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if len(gotQuery) != 1 || gotQuery.Get("limit") != "100" {
			t.Errorf("query = %v, want only limit=100", gotQuery)
		}
	})

	t.Run("invalid since or until is rejected before any request", func(t *testing.T) {
		before := hits
		for _, name := range []string{"since", "until"} {
			c := newAuditCmd()
			c.SetArgs([]string{"--" + name, "yesterday"})
			err := runRunE(t, c)
			if err == nil || !strings.Contains(err.Error(), "invalid "+name) {
				t.Errorf("--%s yesterday: err = %v", name, err)
			}
		}
		if hits != before {
			t.Errorf("a rejected query still reached the daemon")
		}
	})
}

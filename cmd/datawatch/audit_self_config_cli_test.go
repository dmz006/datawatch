// BL412 — CLI `audit self-config`: the operator read view.

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditSelfConfig_CLI(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"count":0,"entries":[],"path":"/x.jsonl"}`))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()

	t.Run("limit is forwarded", func(t *testing.T) {
		c := newAuditCmd()
		c.SetArgs([]string{"self-config", "--limit", "5"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if gotPath != "/api/audit/self-config" {
			t.Errorf("path = %q", gotPath)
		}
		if gotQuery != "limit=5" {
			t.Errorf("query = %q, want limit=5", gotQuery)
		}
	})

	t.Run("default limit is sent", func(t *testing.T) {
		c := newAuditCmd()
		c.SetArgs([]string{"self-config"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if gotQuery != "limit=100" {
			t.Errorf("query = %q, want limit=100", gotQuery)
		}
	})
}

// The parent command still exposes the full `audit` filter set plus the new
// self-config subcommand.
func TestAuditSelfConfig_CLIRegistration(t *testing.T) {
	c := newAuditCmd()
	sub, _, err := c.Find([]string{"self-config"})
	if err != nil || sub == nil {
		t.Fatalf("Find self-config: %v", err)
	}
	if sub.Name() != "self-config" {
		t.Fatalf("sub = %q", sub.Name())
	}
	f := sub.Flags().Lookup("limit")
	if f == nil {
		t.Fatal("self-config lacks --limit")
	}
	if f.DefValue != "100" {
		t.Errorf("--limit default = %q, want 100", f.DefValue)
	}
	if sub.Short == "" || !strings.Contains(sub.Short, "self-config audit log") {
		t.Errorf("self-config help does not name the self-config audit log: %q", sub.Short)
	}
	// The help is operator-facing and must not leak internal tracker IDs.
	if strings.Contains(sub.Short, "BL412") || strings.Contains(sub.Long, "BL412") {
		t.Errorf("self-config help leaks an internal tracker id: %q / %q", sub.Short, sub.Long)
	}
}

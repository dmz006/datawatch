package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func priorityFlagCmd(args ...string) *cobra.Command {
	c := &cobra.Command{Use: "new"}
	c.Flags().String("priority", "", "")
	_ = c.ParseFlags(args)
	return c
}

// The CLI sends the same integer the operator typed on both the create and
// the update request, in the shape the REST handlers parse.
func TestSessionPriorityParityCLI(t *testing.T) {
	p, err := sessionPriorityFlag(priorityFlagCmd("--priority", "7"))
	if err != nil || p == nil || *p != 7 {
		t.Fatalf("flag = %v, %v; want 7", p, err)
	}
	body, err := sessionNewStartBodyPriority("task", "", "", "", "", "", false, "", false, false, nil, false, p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if got, err := session.ParsePriority(m["priority"]); err != nil || got != 7 {
		t.Fatalf("start body priority = %v (%v), want 7", m["priority"], err)
	}

	var seen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &seen)
		if r.URL.Path != "/api/sessions/set_priority" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	cfg := &config.Config{}
	cfg.Server.Port = port
	if err := runSessionPriority(cfg, "ab12", 9); err != nil {
		t.Fatalf("runSessionPriority: %v", err)
	}
	if got, err := session.ParsePriority(seen["priority"]); err != nil || got != 9 || seen["id"] != "ab12" {
		t.Fatalf("update body = %v (%v), want id ab12 priority 9", seen, err)
	}
}

func TestSessionPriorityCLIOmittedAndInvalid(t *testing.T) {
	if p, err := sessionPriorityFlag(priorityFlagCmd()); p != nil || err != nil {
		t.Fatalf("omitted flag = %v, %v; want nil, nil", p, err)
	}
	body, _ := sessionNewStartBodyPriority("task", "", "", "", "", "", false, "", false, false, nil, false, nil)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if _, ok := m["priority"]; ok {
		t.Fatalf("body without a priority carries one: %s", body)
	}
	for _, bad := range []string{"1.5", "abc", ""} {
		if _, err := sessionPriorityFlag(priorityFlagCmd("--priority", bad)); err != session.ErrPriorityNotInteger {
			t.Errorf("--priority %q error = %v", bad, err)
		}
	}
}

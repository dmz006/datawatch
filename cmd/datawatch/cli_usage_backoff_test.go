package main

import (
	"reflect"
	"testing"
	"time"
)

func TestUsageBackoffCLI_HasSubs(t *testing.T) {
	c := newUsageBackoffCmd()
	if c.Name() != "usage-backoff" {
		t.Fatalf("name = %q", c.Name())
	}
	want := map[string]bool{"status": false, "set": false, "clear": false}
	for _, sub := range c.Commands() {
		want[sub.Name()] = true
	}
	for sub, ok := range want {
		if !ok {
			t.Errorf("usage-backoff missing subcommand: %s", sub)
		}
	}
	for _, sub := range c.Commands() {
		if sub.Name() == "set" && sub.Flag("reason") == nil {
			t.Error("set is missing --reason")
		}
	}
}

func TestUsageBackoffCLI_SetArgParsing(t *testing.T) {
	c := newUsageBackoffCmd()
	set, _, err := c.Find([]string{"set"})
	if err != nil || set.Name() != "set" {
		t.Fatalf("find set: %v %v", set, err)
	}
	for _, args := range [][]string{{}, {"claude-code"}, {"a", "5", "extra"}} {
		if err := set.Args(set, args); err == nil {
			t.Errorf("set accepted %d args %v, want exactly 2", len(args), args)
		}
	}
	if err := set.Args(set, []string{"claude-code", "5"}); err != nil {
		t.Errorf("set rejected valid args: %v", err)
	}
	clr, _, _ := c.Find([]string{"clear"})
	if err := clr.Args(clr, []string{"a", "b"}); err == nil {
		t.Error("clear accepted two args")
	}
	if err := clr.Args(clr, nil); err != nil {
		t.Errorf("clear rejected no args: %v", err)
	}
}

func TestUsageBackoffSetBody(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	got, err := usageBackoffSetBody(" claude-code ", "45", " weekly cap ", now)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"backend": "claude-code", "minutes": 45, "reason": "weekly cap"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("minutes body = %v, want %v", got, want)
	}

	got, err = usageBackoffSetBody("pi", "2026-10-23T06:00:00Z", "", now)
	if err != nil {
		t.Fatal(err)
	}
	want = map[string]any{"backend": "pi", "until": "2026-10-23T06:00:00Z"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("until body = %v, want %v", got, want)
	}

	for _, c := range []struct{ backend, spec string }{
		{"", "5"}, {"  ", "5"}, {"pi", "0"}, {"pi", "-3"}, {"pi", "soon"}, {"pi", "2026-10-21T00:00:00Z"}, {"pi", "5.5"},
	} {
		if _, err := usageBackoffSetBody(c.backend, c.spec, "", now); err == nil {
			t.Errorf("usageBackoffSetBody(%q,%q) accepted, want error", c.backend, c.spec)
		}
	}
}

func TestUsageBackoffClearBody(t *testing.T) {
	cases := []struct {
		args []string
		want map[string]any
	}{
		{nil, map[string]any{}},
		{[]string{"all"}, map[string]any{}},
		{[]string{"ALL"}, map[string]any{}},
		{[]string{" "}, map[string]any{}},
		{[]string{"claude-code"}, map[string]any{"backend": "claude-code"}},
	}
	for _, c := range cases {
		if got := usageBackoffClearBody(c.args); !reflect.DeepEqual(got, c.want) {
			t.Errorf("usageBackoffClearBody(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

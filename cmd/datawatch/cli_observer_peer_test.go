// BL172 (S11) — CLI parity smoke tests for `datawatch observer peer ...`.

package main

import (
	"strings"
	"testing"
)

func TestObserverPeerCmd_HasAllSubs(t *testing.T) {
	c := newObserverPeerCmd()
	if c.Name() != "peer" {
		t.Errorf("name = %q want peer", c.Name())
	}
	want := map[string]bool{"list": false, "get": false, "stats": false, "register": false, "delete": false}
	for _, sub := range c.Commands() {
		want[sub.Name()] = true
	}
	for sub, ok := range want {
		if !ok {
			t.Errorf("missing subcommand: %s", sub)
		}
	}
}

func TestObserverPeerCmd_RegisterArgRange(t *testing.T) {
	c := newObserverPeerCmd()
	var reg *struct{}
	_ = reg
	for _, sub := range c.Commands() {
		if sub.Name() != "register" {
			continue
		}
		// Args validator should accept 1, 2, or 3 positional args.
		if err := sub.Args(sub, []string{"name"}); err != nil {
			t.Errorf("register should accept 1 arg: %v", err)
		}
		if err := sub.Args(sub, []string{"name", "C"}); err != nil {
			t.Errorf("register should accept 2 args: %v", err)
		}
		if err := sub.Args(sub, []string{"name", "C", "v0.1"}); err != nil {
			t.Errorf("register should accept 3 args: %v", err)
		}
		if err := sub.Args(sub, []string{}); err == nil {
			t.Error("register should reject 0 args")
		}
		if err := sub.Args(sub, []string{"a", "b", "c", "d"}); err == nil {
			t.Error("register should reject 4 args")
		}
		return
	}
	t.Error("register subcommand not found")
}

func TestObserverPeerCmd_GetRequiresName(t *testing.T) {
	c := newObserverPeerCmd()
	for _, sub := range c.Commands() {
		switch sub.Name() {
		case "get", "stats", "delete":
			if err := sub.Args(sub, []string{}); err == nil {
				t.Errorf("%s should require a name arg", sub.Name())
			}
			if err := sub.Args(sub, []string{"x"}); err != nil {
				t.Errorf("%s should accept 1 arg: %v", sub.Name(), err)
			}
		}
	}
}

func TestObserverPeerCmd_AppearsUnderObserver(t *testing.T) {
	root := newObserverCmd()
	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "peer" {
			found = true
		}
	}
	if !found {
		t.Errorf("`observer peer` subcommand must be registered under newObserverCmd")
	}
}

func TestObserverPeerCmd_LongHelpMentionsBothPeerKinds(t *testing.T) {
	c := newObserverPeerCmd()
	long := strings.ToLower(c.Long)
	if !strings.Contains(long, "standalone") || !strings.Contains(long, "cluster") {
		t.Errorf("Long help should mention both standalone-daemon and cluster-container peers; got: %q", c.Long)
	}
}

// S13 — agent CLI alias.

func TestObserverAgentCmd_HasListAndStats(t *testing.T) {
	c := newObserverAgentCmd()
	if c.Name() != "agent" {
		t.Errorf("name = %q want agent", c.Name())
	}
	want := map[string]bool{"list": false, "stats": false}
	for _, sub := range c.Commands() {
		want[sub.Name()] = true
	}
	for sub, ok := range want {
		if !ok {
			t.Errorf("missing subcommand: %s", sub)
		}
	}
}

func TestObserverAgentCmd_AppearsUnderObserver(t *testing.T) {
	root := newObserverCmd()
	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "agent" {
			found = true
		}
	}
	if !found {
		t.Errorf("`observer agent` must be registered under newObserverCmd")
	}
}

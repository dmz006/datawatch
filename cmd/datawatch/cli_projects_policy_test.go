package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestProjectsPolicySubcommandTree(t *testing.T) {
	projects := newProjectsCmd()
	if projects == nil {
		t.Fatal("newProjectsCmd returned nil")
	}
	var policy *cobra.Command
	for _, sub := range projects.Commands() {
		if sub.Name() == "policy" {
			policy = sub
		}
	}
	if policy == nil {
		verbs := "projects "
		for _, sub := range projects.Commands() {
			verbs += sub.Name() + " "
		}
		t.Fatalf("`projects policy` subcommand not found; verbs: %q", strings.TrimSpace(verbs))
	}
	want := map[string]bool{"show": false, "set": false, "unset": false}
	for _, sub := range policy.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for verb, found := range want {
		if !found {
			t.Errorf("`projects policy %s` subcommand not registered", verb)
		}
	}
	// set takes <name> <key> <value>.
	var set *cobra.Command
	for _, sub := range policy.Commands() {
		if sub.Name() == "set" {
			set = sub
		}
	}
	if set != nil && set.Args != nil {
		if err := set.Args(set, []string{"acme", "plan_review_mode", "block"}); err != nil {
			t.Errorf("set Args rejected valid args: %v", err)
		}
		if err := set.Args(set, []string{"acme"}); err == nil {
			t.Errorf("set Args accepted missing key/value args — expected a count error")
		}
	}
	// show flags.
	var show *cobra.Command
	for _, sub := range policy.Commands() {
		if sub.Name() == "show" {
			show = sub
		}
	}
	if show != nil {
		for _, flag := range []string{"effective", "automaton"} {
			if show.Flags().Lookup(flag) == nil {
				t.Errorf("show is missing the --%s flag", flag)
			}
		}
	}
}

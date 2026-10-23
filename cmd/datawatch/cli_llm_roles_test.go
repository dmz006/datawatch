package main

import (
	"reflect"
	"testing"
)

func TestRoleParityCLIFlag(t *testing.T) {
	t.Run("repeatable --role on add", func(t *testing.T) {
		seen := llmDaemon(t)
		c := newLLMAddCmd(false)
		c.SetArgs([]string{"a", "--kind", "ollama", "--role", "planning", "--role", "Review"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		got := seen()[0].body["roles"]
		if !reflect.DeepEqual(got, []any{"planning", "Review"}) {
			t.Errorf("roles = %v, want the same list MCP and REST carry", got)
		}
	})
	t.Run("update without --role keeps roles", func(t *testing.T) {
		seen := llmDaemon(t)
		c := newLLMAddCmd(true)
		c.SetArgs([]string{"a", "--model", "m"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if _, has := seen()[0].body["roles"]; has {
			t.Errorf("roles sent without --role: %v", seen()[0].body)
		}
	})
	t.Run("update with empty --role clears roles", func(t *testing.T) {
		seen := llmDaemon(t)
		c := newLLMAddCmd(true)
		c.SetArgs([]string{"a", "--role", ""})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		got, has := seen()[0].body["roles"]
		if !has || !reflect.DeepEqual(got, []any{}) {
			t.Errorf("roles = %v (present=%v), want []", got, has)
		}
	})
}

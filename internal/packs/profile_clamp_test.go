package packs

import (
	"reflect"
	"testing"
)

func TestClampToProfileFullAllowKeepsEverything(t *testing.T) {
	task := PackTask{
		Title:      "edit",
		Tools:      []string{"Read", "Edit"},
		FileScopes: []string{"src/**", "docs/**"},
	}
	allow := WorkerAllowList{
		Tools:      []string{"Read", "Edit", "Bash"},
		FileScopes: []string{"src/**", "docs/**", "cmd/**"},
	}
	got, dropped := ClampToProfile(task, allow)
	if !reflect.DeepEqual(got.Tools, []string{"Read", "Edit"}) {
		t.Errorf("tools = %v, want [Read Edit]", got.Tools)
	}
	if !reflect.DeepEqual(got.FileScopes, []string{"src/**", "docs/**"}) {
		t.Errorf("file scopes = %v, want [src/** docs/**]", got.FileScopes)
	}
	if dropped != nil {
		t.Errorf("dropped = %v, want nil", dropped)
	}
	if got.Title != "edit" {
		t.Errorf("title changed to %q", got.Title)
	}
}

func TestClampToProfilePartialAllowDropsOnlyDisallowed(t *testing.T) {
	task := PackTask{
		Tools:      []string{"Read", "Bash", "Edit", "WebFetch"},
		FileScopes: []string{"src/**", "secrets/**", "docs/**"},
	}
	allow := WorkerAllowList{
		Tools:      []string{"Edit", "Read"},
		FileScopes: []string{"docs/**", "src/**"},
	}
	got, dropped := ClampToProfile(task, allow)
	if !reflect.DeepEqual(got.Tools, []string{"Read", "Edit"}) {
		t.Errorf("tools = %v, want [Read Edit] in task order", got.Tools)
	}
	if !reflect.DeepEqual(got.FileScopes, []string{"src/**", "docs/**"}) {
		t.Errorf("file scopes = %v, want [src/** docs/**]", got.FileScopes)
	}
	want := []string{"Bash", "WebFetch", "secrets/**"}
	if !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
}

func TestClampToProfileEmptyAllowDropsEverything(t *testing.T) {
	task := PackTask{
		Tools:      []string{"Read", "Edit"},
		FileScopes: []string{"src/**"},
	}
	got, dropped := ClampToProfile(task, WorkerAllowList{})
	if len(got.Tools) != 0 {
		t.Errorf("tools = %v, want empty", got.Tools)
	}
	if len(got.FileScopes) != 0 {
		t.Errorf("file scopes = %v, want empty", got.FileScopes)
	}
	want := []string{"Read", "Edit", "src/**"}
	if !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
}

func TestClampToProfileNeverAddsEntries(t *testing.T) {
	task := PackTask{Tools: []string{"Read"}}
	allow := WorkerAllowList{Tools: []string{"Read", "Edit", "Bash"}, FileScopes: []string{"src/**"}}
	got, dropped := ClampToProfile(task, allow)
	if !reflect.DeepEqual(got.Tools, []string{"Read"}) {
		t.Errorf("tools = %v, want [Read]", got.Tools)
	}
	if got.FileScopes != nil {
		t.Errorf("file scopes = %v, want nil (task named none)", got.FileScopes)
	}
	if dropped != nil {
		t.Errorf("dropped = %v, want nil", dropped)
	}
}

func TestClampToProfileDoesNotAliasInput(t *testing.T) {
	task := PackTask{Tools: []string{"Read", "Edit"}}
	got, _ := ClampToProfile(task, WorkerAllowList{Tools: []string{"Read", "Edit"}})
	got.Tools[0] = "Mutated"
	if task.Tools[0] != "Read" {
		t.Errorf("input task tools modified through result: %v", task.Tools)
	}
}

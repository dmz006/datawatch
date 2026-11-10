package packs

import (
	"reflect"
	"testing"
	"time"
)

func dvPack(version string, tasks ...Task) *Pack {
	return &Pack{Name: "p", Version: version, Digest: "d-" + version, Tasks: tasks}
}

func TestDiffVersions(t *testing.T) {
	old := dvPack("1.0.0", Task{Title: "a", VerificationScript: "v.sh", Tools: []string{"git"}, FileScopes: []string{"src/"}})
	cand := dvPack("1.1.0",
		Task{Title: "a", VerificationScript: "v.sh", Tools: []string{"git", "curl"}, FileScopes: []string{"src/", "docs/"}},
		Task{Title: "b", VerificationScript: "w.sh"})
	d := DiffVersions(old, cand)
	if !reflect.DeepEqual(d.NewTasks, []string{"b"}) || !reflect.DeepEqual(d.NewScripts, []string{"w.sh"}) ||
		!reflect.DeepEqual(d.NewTools, []string{"curl"}) || !reflect.DeepEqual(d.NewScopes, []string{"docs/"}) {
		t.Fatalf("unexpected diff %+v", d)
	}
	if !DiffVersions(old, old).Empty() {
		t.Fatal("identical packs must diff empty")
	}
	if got := DiffVersions(nil, old); len(got.NewTasks) != 1 {
		t.Fatalf("nil accepted: %+v", got)
	}
}

func TestAutoAcceptUpdate(t *testing.T) {
	store, err := LoadTrustStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := dvPack("1.0.0", Task{Title: "a", Tools: []string{"git"}})
	safe := dvPack("1.1.0", Task{Title: "a", Tools: []string{"git"}}, Task{Title: "b"})
	risky := dvPack("1.2.0", Task{Title: "a", Tools: []string{"git", "curl"}})
	at := time.Date(2026, 11, 10, 1, 2, 3, 0, time.UTC)

	if _, ok, _ := AutoAcceptUpdate(store, old, safe, false, "src", at); ok {
		t.Fatal("flag off must not auto-accept")
	}
	if _, ok, _ := AutoAcceptUpdate(store, old, risky, true, "src", at); ok {
		t.Fatal("new tool must not auto-accept")
	}
	rec, ok, err := AutoAcceptUpdate(store, old, safe, true, "src", at)
	if err != nil || !ok {
		t.Fatalf("want auto-accept, ok=%v err=%v", ok, err)
	}
	if rec.AcceptedBy != "auto:src" || !rec.AcceptedAt.Equal(at) {
		t.Fatalf("bad record %+v", rec)
	}
}

package skills

import (
	"path/filepath"
	"testing"
)

func TestCommunityDefaultRegistry_IsFirstParty(t *testing.T) {
	if !CommunityDefaultRegistry.FirstParty {
		t.Fatal("CommunityDefaultRegistry must be FirstParty")
	}
	if PAIDefaultRegistry.FirstParty {
		t.Fatal("PAIDefaultRegistry must not be FirstParty")
	}
}

func TestIsFirstPartyPackSource(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{CommunityDefaultRegistry.Name, true},
		{PAIDefaultRegistry.Name, false},
		{"", false},
		{"some-user-registry", false},
		{"Community", false},
	}
	for _, c := range cases {
		if got := IsFirstPartyPackSource(c.name); got != c.want {
			t.Errorf("IsFirstPartyPackSource(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAddCommunityDefault_PersistsFirstPartyFlag(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.AddCommunityDefault(); err != nil {
		t.Fatalf("AddCommunityDefault: %v", err)
	}
	reg, ok := m.Store.GetRegistry(CommunityDefaultRegistry.Name)
	if !ok {
		t.Fatal("community registry not seeded")
	}
	if !reg.FirstParty {
		t.Error("seeded community registry lost FirstParty flag")
	}
	// Idempotent: a second call must not error or duplicate.
	if err := m.AddCommunityDefault(); err != nil {
		t.Fatalf("second AddCommunityDefault: %v", err)
	}
	// Flag survives a reload from disk.
	store, err := NewStore(filepath.Join(m.SyncedRoot, "..", "skills.json"))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	reg, ok = store.GetRegistry(CommunityDefaultRegistry.Name)
	if !ok || !reg.FirstParty {
		t.Error("FirstParty flag not persisted to skills.json")
	}
}

// BL99 — closets / drawers chain tests.

package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func chainStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSaveClosetWithDrawer_HappyPath(t *testing.T) {
	s := chainStore(t)
	res, err := SaveClosetWithDrawer(context.Background(), s, nil,
		"/proj",
		"FULL VERBATIM: a long detailed log full of bytes and stuff. "+
			strings.Repeat("x", 500),
		"summary: short version",
		"session", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if res.DrawerID == 0 || res.ClosetID == 0 || res.DrawerID == res.ClosetID {
		t.Errorf("expected two distinct IDs, got %+v", res)
	}

	// Drawer points back from the closet.
	d, err := s.Drawer(res.ClosetID)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Fatal("Drawer returned nil for a closet with a chain")
	}
	if !strings.Contains(d.Content, "FULL VERBATIM") {
		t.Errorf("drawer content not the verbatim: %q", d.Content[:30])
	}
}

func TestSaveClosetWithDrawer_RequiresVerbatim(t *testing.T) {
	s := chainStore(t)
	_, err := SaveClosetWithDrawer(context.Background(), s, nil, "/p", "  ", "ok", "session", "")
	if err == nil {
		t.Error("expected error for empty verbatim")
	}
}

func TestSaveClosetWithDrawer_RequiresSummary(t *testing.T) {
	s := chainStore(t)
	_, err := SaveClosetWithDrawer(context.Background(), s, nil, "/p", "ok", "  ", "session", "")
	if err == nil {
		t.Error("expected error for empty summary")
	}
}

func TestDrawer_StandaloneCloset_ReturnsNil(t *testing.T) {
	s := chainStore(t)
	id, err := s.SaveCloset("/p", "just a summary", "session", "sess", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Drawer(id)
	if err != nil {
		t.Fatal(err)
	}
	if d != nil {
		t.Errorf("expected nil drawer for standalone closet, got %+v", d)
	}
}

func TestDrawer_RequiresPositiveID(t *testing.T) {
	s := chainStore(t)
	if _, err := s.Drawer(0); err == nil {
		t.Error("expected error for zero closet_id")
	}
}

func TestSaveCloset_LinksDrawer(t *testing.T) {
	s := chainStore(t)
	drawerID, err := s.Save("/p", "verbatim", "", "session-verbatim", "sess", nil)
	if err != nil {
		t.Fatal(err)
	}
	closetID, err := s.SaveCloset("/p", "summary text", "session", "sess", nil, drawerID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Drawer(closetID)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.ID != drawerID {
		t.Errorf("drawer link broken: got %+v want id=%d", d, drawerID)
	}
}

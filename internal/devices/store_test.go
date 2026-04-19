// Issue #1 — device store tests.

package devices

import (
	"errors"
	"path/filepath"
	"testing"
)

func storeFixture(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestRegister_HappyPath(t *testing.T) {
	s, _ := storeFixture(t)
	got, err := s.Register(Device{
		Token: "fcm-abc123", Kind: KindFCM,
		AppVersion: "0.2.0", Platform: PlatformAndroid,
		ProfileHint: "pixel-8",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Error("device_id not assigned")
	}
	if got.RegisteredAt.IsZero() {
		t.Error("RegisteredAt not set")
	}
}

func TestRegister_RequiresToken(t *testing.T) {
	s, _ := storeFixture(t)
	if _, err := s.Register(Device{Kind: KindFCM}); err == nil {
		t.Error("expected error for empty token")
	}
}

func TestRegister_RejectsUnknownKind(t *testing.T) {
	s, _ := storeFixture(t)
	if _, err := s.Register(Device{Token: "x", Kind: Kind("invalid")}); err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestRegister_RejectsUnknownPlatform(t *testing.T) {
	s, _ := storeFixture(t)
	if _, err := s.Register(Device{
		Token: "x", Kind: KindFCM, Platform: Platform("plan9"),
	}); err == nil {
		t.Error("expected error for unknown platform")
	}
}

func TestRegister_SameTokenRefreshes(t *testing.T) {
	s, _ := storeFixture(t)
	first, _ := s.Register(Device{Token: "abc", Kind: KindFCM, AppVersion: "0.1.0"})
	second, _ := s.Register(Device{Token: "abc", Kind: KindFCM, AppVersion: "0.2.0"})
	if first.ID != second.ID {
		t.Errorf("ID should be stable: %s vs %s", first.ID, second.ID)
	}
	if second.AppVersion != "0.2.0" {
		t.Errorf("AppVersion not refreshed: got %q", second.AppVersion)
	}
	if len(s.List()) != 1 {
		t.Errorf("duplicate created: list=%d want 1", len(s.List()))
	}
}

func TestDelete_HappyPath(t *testing.T) {
	s, _ := storeFixture(t)
	d, _ := s.Register(Device{Token: "t1", Kind: KindFCM})
	if err := s.Delete(d.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Errorf("list should be empty: %v", s.List())
	}
}

func TestDelete_NotFound(t *testing.T) {
	s, _ := storeFixture(t)
	if err := s.Delete("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestListByKind_Filters(t *testing.T) {
	s, _ := storeFixture(t)
	_, _ = s.Register(Device{Token: "fcm1", Kind: KindFCM})
	_, _ = s.Register(Device{Token: "fcm2", Kind: KindFCM})
	_, _ = s.Register(Device{Token: "ntfy1", Kind: KindNTFY})
	if got := s.ListByKind(KindFCM); len(got) != 2 {
		t.Errorf("fcm len=%d want 2", len(got))
	}
	if got := s.ListByKind(KindNTFY); len(got) != 1 {
		t.Errorf("ntfy len=%d want 1", len(got))
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	s, path := storeFixture(t)
	d, _ := s.Register(Device{Token: "persist", Kind: KindFCM})
	// Re-open the file in a new Store.
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "persist" {
		t.Errorf("round-trip broken: %+v", got)
	}
}

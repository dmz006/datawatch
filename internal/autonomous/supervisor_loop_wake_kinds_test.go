package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// TestWakeSettingsDefaultKindsAreRegistered: every default wake kind is one a
// subscription may filter on, and the loop settings refuse a kind the
// registry does not know.
func TestWakeSettingsDefaultKindsAreRegistered(t *testing.T) {
	if supervisorloop.EventKindChecker() == nil {
		t.Fatal("the registry check is not installed")
	}
	for _, k := range supervisorloop.DefaultWakeKinds() {
		if !IsKnownEventKind(k) {
			t.Errorf("default wake kind %q is not a registered event kind", k)
		}
	}
	s := supervisorloop.DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(supervisorloop.KeyWakeKinds, []string{"no.such_kind"}); err == nil {
		t.Fatal("an unregistered kind was accepted")
	}
	if err := s.Set(supervisorloop.KeyWakeKinds, []string{KindTaskLooping}); err == nil {
		t.Fatal("a kind a subscription cannot filter on was accepted")
	}
}

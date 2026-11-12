package autonomous

import "testing"

func TestIsDaemonMarkerPath(t *testing.T) {
	cases := map[string]bool{
		"DATAWATCH_COMPLETE":        true,
		"./DATAWATCH_COMPLETE":      true,
		"CHECKPOINT.md":             true,
		treeLockRelPath:             true,
		"sub/DATAWATCH_COMPLETE":    false,
		"sub/../DATAWATCH_COMPLETE": true,
		"main.go":                   false,
		"sub/CHECKPOINT.md":         false,
		"":                          false,
		"DATAWATCH_COMPLETE.bak":    false,
	}
	for p, want := range cases {
		if got := isDaemonMarkerPath(p); got != want {
			t.Errorf("isDaemonMarkerPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestDaemonMarkerFilesIncludeCompletionMarker(t *testing.T) {
	found := false
	for _, m := range daemonMarkerFiles() {
		if m == completionMarkerFile {
			found = true
		}
		if !isDaemonMarkerPath(m) {
			t.Errorf("listed marker %q not recognised", m)
		}
	}
	if !found {
		t.Fatal("completion marker missing from daemonMarkerFiles")
	}
}

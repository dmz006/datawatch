package session

import (
	"strings"
	"testing"
)

func TestSetPaneEnvSkipsNamesAShellCannotExport(t *testing.T) {
	m := &Manager{}
	fake := m.WithFakeTmux()
	if err := fake.NewSessionWithSize("cs-a", 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPaneEnv("cs-a", map[string]string{
		"GOOD_1": "v", "_ALSO": "v", "1BAD": "v", "BAD NAME": "v", "X;touch /tmp/x": "v", "A=$(id)": "v", "": "v",
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(m.takePaneEnvNames("cs-a"), ","); got != "GOOD_1,_ALSO" {
		t.Fatalf("names = %q", got)
	}
	if got := m.takePaneEnvNames("cs-a"); len(got) != 0 {
		t.Fatalf("names were not forgotten: %v", got)
	}
}

func TestPaneEnvCommandsTypeNamesOnlyInShortLines(t *testing.T) {
	var names []string
	for i := 0; i < 200; i++ {
		names = append(names, "VARIABLE_NUMBER_"+strings.Repeat("N", i%20+1))
	}
	lines := paneEnvCommands("/usr/bin/tmux", "cs-a", names)
	if len(lines) < 2 {
		t.Fatalf("expected several lines, got %d", len(lines))
	}
	typed := 0
	for _, l := range lines {
		if len(l) > paneEnvLineMax {
			t.Errorf("line of %d bytes", len(l))
		}
		if !strings.HasPrefix(l, "set +x +v; ") || !strings.Contains(l, "show-environment -s -t 'cs-a'") {
			t.Errorf("unexpected line: %s", l)
		}
		typed += strings.Count(l, "VARIABLE_NUMBER_")
	}
	if typed != len(names) {
		t.Errorf("typed %d names, want %d", typed, len(names))
	}
	if got := paneEnvCommands("tmux", "cs-a", nil); len(got) != 0 {
		t.Errorf("no names should type nothing, got %v", got)
	}
}

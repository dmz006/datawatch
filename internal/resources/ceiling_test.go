package resources

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func mustParse(t *testing.T, s string) Override {
	t.Helper()
	o, err := ParseOverride(s)
	if err != nil {
		t.Fatalf("ParseOverride(%q): %v", s, err)
	}
	return o
}

func TestResolveCeiling(t *testing.T) {
	cfg := config.DefaultResourceLimits()
	const gb = 1024
	tests := []struct {
		name     string
		totalMB  int
		taskKind string
		project  string
		wantMB   int
		wantSrc  Source
	}{
		{"122GB host gives the cap", 122 * gb, "", "", cfg.MemoryCapMB, SourceDefault},
		{"2GB host gives the floor", 2 * gb, "", "", cfg.MemoryFloorMB, SourceDefault},
		{"mid host is total times share", 10 * gb, "", "", 10 * gb * cfg.MemorySharePercent / 100, SourceDefault},
		{"project override", 122 * gb, "", "4096", 4096, SourceOverride},
		{"task-kind override", 122 * gb, "8192", "", 8192, SourceOverride},
		{"task-kind beats project", 122 * gb, "8192", "4096", 8192, SourceOverride},
		{"project beats global", 2 * gb, "", "30000", 30000, SourceOverride},
		{"none at task kind", 122 * gb, "none", "4096", 0, SourceNone},
		{"none at project", 122 * gb, "", "none", 0, SourceNone},
		{"task-kind number beats project none", 122 * gb, "1000", "none", 1000, SourceOverride},
		{"task-kind none beats project number", 122 * gb, "none", "1000", 0, SourceNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ov := PickOverride(mustParse(t, tc.taskKind), mustParse(t, tc.project))
			got := ResolveCeiling(tc.totalMB, cfg, ov)
			if got.MB != tc.wantMB || got.Source != tc.wantSrc {
				t.Fatalf("got %+v, want MB=%d Source=%s", got, tc.wantMB, tc.wantSrc)
			}
			if got.NoCeiling() != (tc.wantSrc == SourceNone) {
				t.Fatalf("NoCeiling = %v", got.NoCeiling())
			}
		})
	}
}

func TestResolveCeilingUsesConfigValues(t *testing.T) {
	cfg := config.ResourceLimitsConfig{MemorySharePercent: 25, MemoryFloorMB: 100, MemoryCapMB: 500}
	for _, tc := range []struct{ total, want int }{{4000, 500}, {200, 100}, {1000, 250}} {
		if got := ResolveCeiling(tc.total, cfg, Override{}); got.MB != tc.want {
			t.Errorf("total %d: got %d, want %d", tc.total, got.MB, tc.want)
		}
	}
}

func TestParseOverrideInvalid(t *testing.T) {
	for _, s := range []string{"abc", "0", "-5", "1.5"} {
		if _, err := ParseOverride(s); err == nil {
			t.Errorf("ParseOverride(%q): want error", s)
		}
	}
	if o := mustParse(t, "NONE"); !o.None {
		t.Errorf("NONE should parse as none")
	}
}

func TestHostTotalMBFrom(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(good, []byte("MemFree: 1 kB\nMemTotal:       2097152 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if mb, err := hostTotalMBFrom(good); err != nil || mb != 2048 {
		t.Fatalf("got %d, %v", mb, err)
	}
	if _, err := hostTotalMBFrom(filepath.Join(dir, "missing")); !errors.Is(err, ErrMemTotalUnavailable) {
		t.Fatalf("missing file: got %v", err)
	}
	bad := filepath.Join(dir, "bad")
	_ = os.WriteFile(bad, []byte("MemFree: 1 kB\n"), 0o600)
	if _, err := hostTotalMBFrom(bad); !errors.Is(err, ErrMemTotalUnavailable) {
		t.Fatalf("no MemTotal: got %v", err)
	}
}

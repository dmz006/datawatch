// Tests for the cleanup read text (cleanup_text.go): the human size
// wording the disk alert uses for its folder report, and the status
// render's biggest-folders block.
package autonomous

import (
	"strings"
	"testing"
)

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{5 * 1024, "5.0 KiB"},
		{30 * 1024, "30 KiB"},
		{3 * 1024 * 1024, "3.0 MiB"},
		{12 * 1024 * 1024, "12 MiB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.5 GiB"},
		{10 * 1024 * 1024 * 1024, "10 GiB"},
		{2 * 1024 * 1024 * 1024 * 1024, "2.0 TiB"},
		{-5, "0 B"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Fatalf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderCleanupStatusBiggestTempFolders(t *testing.T) {
	tmpDir := t.TempDir()
	folders := []TempFolderInfo{
		{Path: tmpDir + "/big", Size: 3 * 1024 * 1024, Held: true, Liveness: true},
		{Path: tmpDir + "/small", Size: 16, Held: false, Liveness: true},
		{Path: tmpDir + "/unknown", Size: 64, Held: false, Liveness: false},
	}
	st := CleanupStatus{
		DiskAlertPercent:   85,
		TempDisk:           &CleanupDiskCheck{Path: tmpDir, Percent: ptr(99), Alert: true, Threshold: tempDiskAlertPercent},
		DiskAlert:          true,
		BiggestTempFolders: folders,
	}
	got := RenderCleanupStatus(st)
	for _, w := range []string{
		"biggest folders in the temp folder",
		tmpDir + "/big",
		"about 3.0 MiB",
		"held by a running process",
		tmpDir + "/small",
		"about 16 B",
		"no running process holds it",
		"could not tell whether a running process holds it",
		"treat it as held",
	} {
		if !strings.Contains(got, w) {
			t.Fatalf("render missing %q:\n%s", w, got)
		}
	}

	// No folder report: no block at all.
	st.BiggestTempFolders = nil
	if got := RenderCleanupStatus(st); strings.Contains(got, "biggest folders in the temp folder") {
		t.Fatalf("render shows an empty folder block:\n%s", got)
	}
}

func TestRenderCleanupDiskTextThreshold(t *testing.T) {
	c := &CleanupDiskCheck{Path: "/data", Percent: ptr(82), Alert: true, Threshold: tempDiskAlertPercent}
	if got := cleanupDiskText(c); !strings.Contains(got, "at 82% (alert)") || !strings.Contains(got, "threshold 80%") {
		t.Fatalf("disk text = %q, want usage, alert and threshold", got)
	}
	// No reading, threshold still stated.
	c = &CleanupDiskCheck{Path: "/data", Threshold: tempDiskAlertPercent}
	if got := cleanupDiskText(c); !strings.Contains(got, "no reading") || !strings.Contains(got, "threshold 80%") {
		t.Fatalf("no-reading disk text = %q, want the threshold stated", got)
	}
}

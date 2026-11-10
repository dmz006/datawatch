package mergepipeline

// The release readiness read of the merge pipeline: the body of
// GET /api/autonomous/release-readiness and the text the chat verb and the
// CLI print for it. Both surfaces render through RenderReadinessText, so the
// counts, the missing items and the ready flag read the same everywhere.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ReadinessSizes are the release sizes the readiness read accepts.
var ReadinessSizes = []string{"patch", "minor", "major"}

// ValidReadinessSize reports whether size is patch, minor or major.
func ValidReadinessSize(size string) bool {
	for _, s := range ReadinessSizes {
		if size == s {
			return true
		}
	}
	return false
}

// ReadinessReport is the readiness body of the daemon's release readiness read.
// It mirrors the JSON the REST endpoint writes; only the fields the text names
// are kept.
type ReadinessReport struct {
	Size      string `json:"size"`
	Project   string `json:"project"`
	Readiness struct {
		Size            string `json:"size"`
		Records         int    `json:"records"`
		Conflicts       int    `json:"conflicts"`
		PWAChanges      int    `json:"pwa_changes"`
		BlockingDefects int    `json:"blocking_defects"`
		Required        struct {
			Records    int `json:"records"`
			Conflicts  int `json:"conflicts"`
			PWAChanges int `json:"pwa_changes"`
		} `json:"required"`
		Missing []string `json:"missing"`
		Ready   bool     `json:"ready"`
	} `json:"readiness"`
}

// ParseReadiness reads a release readiness body. It refuses text that is not
// the expected object, naming what was wrong.
func ParseReadiness(body []byte) (ReadinessReport, error) {
	var r ReadinessReport
	if err := json.Unmarshal(body, &r); err != nil {
		return ReadinessReport{}, fmt.Errorf("the daemon's release readiness reply could not be read: %w", err)
	}
	return r, nil
}

// RenderReadinessText renders the counts against the bar, the blocking
// defects, one line per missing item and the ready flag. Zero counts are
// printed, so a size that was never compared reads as zero, not as absent.
func RenderReadinessText(r ReadinessReport) string {
	rd := r.Readiness
	var b strings.Builder
	scope := "all projects"
	if r.Project != "" {
		scope = "project " + r.Project
	}
	fmt.Fprintf(&b, "release readiness: %s (%s)\n", r.Size, scope)
	fmt.Fprintf(&b, "records (plan equal or better): %d of %d\n", rd.Records, rd.Required.Records)
	fmt.Fprintf(&b, "conflicts: %d of %d\n", rd.Conflicts, rd.Required.Conflicts)
	fmt.Fprintf(&b, "PWA changes: %d of %d\n", rd.PWAChanges, rd.Required.PWAChanges)
	fmt.Fprintf(&b, "blocking defects: %d\n", rd.BlockingDefects)
	if len(rd.Missing) == 0 {
		b.WriteString("missing: none\n")
	}
	for _, m := range rd.Missing {
		fmt.Fprintf(&b, "missing: %s\n", m)
	}
	fmt.Fprintf(&b, "ready: %t\n", rd.Ready)
	return b.String()
}

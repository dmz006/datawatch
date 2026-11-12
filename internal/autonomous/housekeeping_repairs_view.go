// Read view of the housekeeping repairs: per group, the mode in effect, what
// a report-mode pass would do, what an on-mode pass did, and the cases that
// failed or were skipped as unsafe. Read-only: building the view runs no
// repair, raises no alert and changes no mode (modes change only through the
// existing config set paths).
package autonomous

import (
	"io"
	"strconv"

	"github.com/dmz006/datawatch/internal/config"
)

// HousekeepingRepairGroupView is one repair group in the read view.
type HousekeepingRepairGroupView struct {
	Group         string         `json:"group"`
	Mode          string         `json:"mode"`
	Would         []RepairRecord `json:"would"`
	Did           []RepairRecord `json:"did"`
	Failed        []RepairRecord `json:"failed"`
	SkippedUnsafe []RepairRecord `json:"skipped_unsafe"`
}

// HousekeepingRepairsView is the body of GET /api/autonomous/housekeeping-repairs.
type HousekeepingRepairsView struct {
	GraceSeconds int                           `json:"grace_seconds"`
	Groups       []HousekeepingRepairGroupView `json:"groups"`
}

// View builds the read view from the bounded record, newest first within each
// list. A nil Repairer reads as every group in report mode with no records.
func (r *Repairer) View() HousekeepingRepairsView {
	var set config.HousekeepingRepairsConfig
	var recs []RepairRecord
	if r != nil {
		set = r.settings()
		recs = r.Records()
	} else {
		set = config.HousekeepingRepairsConfig{}.Effective()
	}
	v := HousekeepingRepairsView{GraceSeconds: set.GraceSecondsOrDefault()}
	for _, g := range config.HousekeepingRepairGroups {
		gv := HousekeepingRepairGroupView{
			Group: g, Mode: set.ModeFor(g),
			Would: []RepairRecord{}, Did: []RepairRecord{}, Failed: []RepairRecord{}, SkippedUnsafe: []RepairRecord{},
		}
		for i := len(recs) - 1; i >= 0; i-- {
			rec := recs[i]
			if rec.Group != g {
				continue
			}
			switch rec.Kind {
			case RepairWould:
				gv.Would = append(gv.Would, rec)
			case RepairDid:
				gv.Did = append(gv.Did, rec)
			case RepairFailed:
				gv.Failed = append(gv.Failed, rec)
			case RepairSkippedUnsafe:
				gv.SkippedUnsafe = append(gv.SkippedUnsafe, rec)
			}
		}
		v.Groups = append(v.Groups, gv)
	}
	return v
}

// RenderHousekeepingRepairs writes the view as plain text, one block per
// group, shared by the CLI and the chat verb.
func RenderHousekeepingRepairs(b io.StringWriter, v *HousekeepingRepairsView) {
	w := func(s string) { _, _ = b.WriteString(s) }
	w("Housekeeping repairs (grace " + strconv.Itoa(v.GraceSeconds) + "s)\n")
	for _, g := range v.Groups {
		w(g.Group + ": mode " + g.Mode + " — would do " + strconv.Itoa(len(g.Would)) + ", did " + strconv.Itoa(len(g.Did)) +
			", failed " + strconv.Itoa(len(g.Failed)) + ", skipped as unsafe " + strconv.Itoa(len(g.SkippedUnsafe)) + "\n")
		for _, sec := range []struct {
			label string
			recs  []RepairRecord
		}{{"would do", g.Would}, {"did", g.Did}, {"failed", g.Failed}, {"skipped", g.SkippedUnsafe}} {
			for _, rec := range sec.recs {
				w("  " + sec.label + ": " + rec.Subject + " — found: " + rec.Found + "; " + rec.Done + "; undo: " + rec.Undo + "\n")
			}
		}
	}
}

// SetHousekeepingRepairer installs the Repairer the read view reads from.
func (m *Manager) SetHousekeepingRepairer(r *Repairer) {
	m.mu.Lock()
	m.housekeepingRepairer = r
	m.mu.Unlock()
}

// HousekeepingRepairsView is the read view of the repairs now in force.
func (m *Manager) HousekeepingRepairsView() HousekeepingRepairsView {
	m.mu.Lock()
	r := m.housekeepingRepairer
	m.mu.Unlock()
	return r.View()
}

// HousekeepingRepairsView serves GET /api/autonomous/housekeeping-repairs.
func (a *API) HousekeepingRepairsView() HousekeepingRepairsView {
	return a.M.HousekeepingRepairsView()
}

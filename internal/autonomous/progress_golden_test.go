package autonomous

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The golden test runs the progress engine on a fixed fixture and compares its
// numbers with the output of scripts/v10-status.py on the same fixture. The
// oracle file holds the script's committed output; when python3 is available
// the script runs again so the file cannot drift silently. A mismatch is a bug
// in the engine or the fixture; no tolerance below is widened to hide one.

const (
	goldenDir          = "testdata/progress"
	goldenScript       = "../../scripts/v10-status.py"
	goldenNow          = "2026-10-20T12:00:00Z"
	goldenSince        = "2026-10-10"
	goldenLanes        = 5
	goldenGapHours     = 3
	goldenExactEpsilon = 1e-9
)

// goldenOracleGroup is one group block of the script's --json output.
type goldenOracleGroup struct {
	PRDs      int     `json:"prds"`
	Done      int     `json:"done"`
	Running   int     `json:"running"`
	Waiting   int     `json:"waiting"`
	Blocked   int     `json:"blocked"`
	Tasks     int     `json:"tasks"`
	TasksDone int     `json:"tasks_done"`
	WorkH     float64 `json:"work_h"`
	PRDWallH  int     `json:"prd_wall_h"`
	TaskH     int     `json:"task_h"`
}

// goldenOracle is the script's --json output. Unknown keys fail the decode, so
// a new key in the script has to be handled here.
type goldenOracle struct {
	Since               string                       `json:"since"`
	ElapsedDays         float64                      `json:"elapsed_days"`
	Groups              map[string]goldenOracleGroup `json:"groups"`
	CompletedPRDs       int                          `json:"completed_prds"`
	RunningPRDs         int                          `json:"running_prds"`
	ExistingPRDsLeft    int                          `json:"existing_prds_left"`
	ExistingTasksLeft   int                          `json:"existing_tasks_left"`
	NewPRDsNotYetFiled  int                          `json:"new_prds_not_yet_filed"`
	RemainingPRDs       int                          `json:"remaining_prds"`
	RemainingTasksEst   int                          `json:"remaining_tasks_est"`
	ThroughputPerDay    float64                      `json:"throughput_per_day"`
	AvgPRDWallH         *float64                     `json:"avg_prd_wall_h"`
	AvgTasksPerPRD      float64                      `json:"avg_tasks_per_prd"`
	EtaDaysByThroughput *float64                     `json:"eta_days_by_throughput"`
	EtaDaysByLaneMath   *float64                     `json:"eta_days_by_lane_math"`
}

// goldenDiff collects every mismatch so one failure message lists them all.
type goldenDiff struct{ lines []string }

func (d *goldenDiff) addf(format string, args ...any) {
	d.lines = append(d.lines, fmt.Sprintf(format, args...))
}

func (d *goldenDiff) int(name string, want, got int) {
	if want != got {
		d.addf("%s: oracle %d, engine %d", name, want, got)
	}
}

// exact compares to 1e-9.
func (d *goldenDiff) exact(name string, want, got float64) {
	if math.Abs(want-got) > goldenExactEpsilon {
		d.addf("%s: oracle %v, engine %v", name, want, got)
	}
}

// rounded accepts a value within half a unit of the script's rounding to the
// given number of places.
func (d *goldenDiff) rounded(name string, places int, want, got float64) {
	half := 0.5 * math.Pow(10, -float64(places))
	if math.Abs(want-got) > half+goldenExactEpsilon {
		d.addf("%s: oracle %v, engine %v (allowed +-%v)", name, want, got, half)
	}
}

// roundedPtr is rounded for a value that may be absent on either side.
func (d *goldenDiff) roundedPtr(name string, places int, want, got *float64) {
	switch {
	case want == nil && got == nil:
	case want == nil:
		d.addf("%s: oracle null, engine %v", name, *got)
	case got == nil:
		d.addf("%s: oracle %v, engine null", name, *want)
	default:
		d.rounded(name, places, *want, *got)
	}
}

// exactPtr is exact for a value that may be absent on either side.
func (d *goldenDiff) exactPtr(name string, want, got *float64) {
	switch {
	case want == nil && got == nil:
	case want == nil:
		d.addf("%s: oracle null, engine %v", name, *got)
	case got == nil:
		d.addf("%s: oracle %v, engine null", name, *want)
	default:
		d.exact(name, *want, *got)
	}
}

func goldenRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// goldenLoadPRDs decodes prds.json, which holds either the bare list or the
// {"prds": [...]} object the list command prints.
func goldenLoadPRDs(t *testing.T) []*PRD {
	t.Helper()
	raw := goldenRead(t, "prds.json")
	var list []*PRD
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var wrapped struct {
		PRDs []*PRD `json:"prds"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		t.Fatalf("decode prds.json: %v", err)
	}
	return wrapped.PRDs
}

func goldenLoadOracle(t *testing.T) goldenOracle {
	t.Helper()
	var o goldenOracle
	dec := json.NewDecoder(bytes.NewReader(goldenRead(t, "oracle.json")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		t.Fatalf("decode oracle.json: %v", err)
	}
	return o
}

func goldenCompute(t *testing.T) ProgressMetrics {
	t.Helper()
	now, ok := ParseProgressTime(goldenNow)
	if !ok {
		t.Fatalf("parse now %q", goldenNow)
	}
	since, ok := ParseProgressTime(goldenSince)
	if !ok {
		t.Fatalf("parse since %q", goldenSince)
	}
	windows, err := ParseWindowsTSV(bytes.NewReader(goldenRead(t, "windows.tsv")))
	if err != nil {
		t.Fatalf("parse windows.tsv: %v", err)
	}
	return ComputeProgress(ProgressInput{
		PRDs:    goldenLoadPRDs(t),
		Backlog: ParseBacklog(bytes.NewReader(goldenRead(t, "golden-backlog.md"))),
		Windows: windows,
		Options: ProgressOptions{Since: since, Now: now, Lanes: goldenLanes, GapHours: goldenGapHours},
	})
}

func TestProgressGolden(t *testing.T) {
	want := goldenLoadOracle(t)

	t.Run("engine matches oracle", func(t *testing.T) {
		m := goldenCompute(t)
		var d goldenDiff

		if got := m.Since.Format("2006-01-02"); got != want.Since {
			d.addf("since: oracle %s, engine %s", want.Since, got)
		}
		for _, kind := range []string{PRDKindFeature, PRDKindBug} {
			w, ok := want.Groups[kind]
			if !ok {
				d.addf("groups.%s: missing from oracle", kind)
				continue
			}
			g := m.Groups[kind]
			p := "groups." + kind + "."
			d.int(p+"prds", w.PRDs, g.PRDs)
			d.int(p+"done", w.Done, g.Done)
			d.int(p+"running", w.Running, g.Running)
			d.int(p+"waiting", w.Waiting, g.Waiting)
			d.int(p+"blocked", w.Blocked, g.Blocked)
			d.int(p+"tasks", w.Tasks, g.Tasks)
			d.int(p+"tasks_done", w.TasksDone, g.TasksDone)
			d.exact(p+"work_h", w.WorkH, g.WorkHours)
		}
		if other := m.Groups[PRDKindOther]; other.PRDs != 0 {
			d.addf("groups.other.prds: oracle has no such group, engine %d", other.PRDs)
		}

		d.int("completed_prds", want.CompletedPRDs, m.CompletedPRDs)
		d.int("running_prds", want.RunningPRDs, m.RunningPRDs)
		d.int("existing_prds_left", want.ExistingPRDsLeft, m.Remaining.Known.PRDs)
		d.int("existing_tasks_left", want.ExistingTasksLeft, m.Remaining.Known.Tasks)
		d.int("new_prds_not_yet_filed", want.NewPRDsNotYetFiled, m.Remaining.Estimated.NewPRDs)
		d.int("remaining_prds", want.RemainingPRDs, m.Remaining.Combined.PRDs)
		d.int("remaining_tasks_est", want.RemainingTasksEst, int(math.Round(m.Remaining.Combined.Tasks)))

		d.rounded("elapsed_days", 2, want.ElapsedDays, m.ActiveDays)
		d.rounded("throughput_per_day", 1, want.ThroughputPerDay, m.Throughput.PRDsPerDay3d)
		d.rounded("avg_tasks_per_prd", 1, want.AvgTasksPerPRD, m.Remaining.Estimated.TasksPerPRD.Mean)
		d.roundedPtr("eta_days_by_throughput", 1, want.EtaDaysByThroughput, m.Completion.ByPRDRateDays)
		d.roundedPtr("eta_days_by_lane_math", 1, want.EtaDaysByLaneMath, m.Completion.ByWorkerHoursDays)
		d.exactPtr("avg_prd_wall_h", want.AvgPRDWallH, m.Totals.MedianPRDWallHours)

		if len(d.lines) > 0 {
			t.Fatalf("engine and oracle disagree on %d field(s):\n  %s", len(d.lines), strings.Join(d.lines, "\n  "))
		}
	})

	t.Run("script reproduces oracle", func(t *testing.T) {
		py, err := exec.LookPath("python3")
		if err != nil {
			t.Log("python3 is not on PATH; skipping the check that the script still prints oracle.json")
			return
		}
		cmd := exec.Command(py, "-I", goldenScript, "--json", // #nosec G204 -- fixed interpreter and script path
			"--prds-file", filepath.Join(goldenDir, "prds.json"),
			"--registry", filepath.Join(goldenDir, "golden-backlog.md"),
			"--windows", filepath.Join(goldenDir, "windows.tsv"),
			"--now", goldenNow,
			"--lanes", fmt.Sprint(goldenLanes),
			"--gap-hours", fmt.Sprint(goldenGapHours))
		cmd.Env = goldenEnvWithoutTmux(os.Environ())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("run %s: %v\n%s", goldenScript, err, stderr.String())
		}
		var got, wantRaw any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("script output is not JSON: %v\n%s", err, out)
		}
		if err := json.Unmarshal(goldenRead(t, "oracle.json"), &wantRaw); err != nil {
			t.Fatalf("decode oracle.json: %v", err)
		}
		if !reflect.DeepEqual(got, wantRaw) {
			t.Fatalf("oracle.json differs from the script output on the same fixture;\nscript printed:\n%s", out)
		}
	})
}

// goldenEnvWithoutTmux drops TMUX and TMUX_TMPDIR so the script never touches a
// multiplexer socket.
func goldenEnvWithoutTmux(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

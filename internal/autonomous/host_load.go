// Host load check: compares the load average with a multiple of the core
// count and, once the breach has lasted the configured number of minutes,
// raises one alert per episode naming the busiest process groups and whether
// each belongs to a worktree or scratch folder of a running or ended session.
//
// Modes: off (does nothing), alert (default; reports only) and reap (also ends
// processes of ended sessions, only those proven by the orphan reaper's
// evidence rule). The clock, load source, core count and session list are
// injected so the check is testable without a real host.
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HostLoadMode selects what the check does on a sustained breach.
type HostLoadMode string

const (
	HostLoadModeOff   HostLoadMode = "off"
	HostLoadModeAlert HostLoadMode = "alert"
	HostLoadModeReap  HostLoadMode = "reap"
)

const (
	// HostLoadDefaultMultiple is the default load-to-cores multiple.
	HostLoadDefaultMultiple = 1.5
	// HostLoadDefaultMinutes is the default number of minutes a breach must last.
	HostLoadDefaultMinutes = 10
	hostLoadTopGroups      = 5
)

// HostLoadConfig is the operator-tunable part of the check.
type HostLoadConfig struct {
	Mode     HostLoadMode
	Multiple float64
	Minutes  int
}

// EffectiveMode returns the mode, defaulting an empty or unknown value to alert.
func (c HostLoadConfig) EffectiveMode() HostLoadMode {
	switch c.Mode {
	case HostLoadModeOff, HostLoadModeAlert, HostLoadModeReap:
		return c.Mode
	}
	return HostLoadModeAlert
}

// EffectiveMultiple returns the multiple, defaulting a non-positive value to 1.5.
func (c HostLoadConfig) EffectiveMultiple() float64 {
	if c.Multiple <= 0 {
		return HostLoadDefaultMultiple
	}
	return c.Multiple
}

// EffectiveMinutes returns the minutes, defaulting a non-positive value to 10.
func (c HostLoadConfig) EffectiveMinutes() int {
	if c.Minutes <= 0 {
		return HostLoadDefaultMinutes
	}
	return c.Minutes
}

// HostLoadSession names one session's folders. Running is false for an ended session.
type HostLoadSession struct {
	ID           string
	Running      bool
	WorktreeDirs []string
	ScratchDirs  []string
}

// HostLoadGroup is one process group in an alert.
type HostLoadGroup struct {
	PGID     int
	Procs    int
	CPUTicks uint64
	Command  string // short command of the group leader or first member
	Owner    string // "worktree", "scratch" or "" when no session folder matches
	Session  string
	Running  bool
}

// HostLoadResult reports one Check.
type HostLoadResult struct {
	Breached bool
	Alerted  bool
	Load     float64
	Limit    float64
	Groups   []HostLoadGroup
	Reaped   OrphanReapResult
}

// HostLoadStatus is the check's read-only snapshot for the monitoring
// surfaces: the system stats host_load field (GET /api/stats and the WS
// stats broadcast), the autonomous_status read (GET /api/autonomous/status,
// which the autonomous_status MCP tool returns verbatim) and the
// datawatch_host_load_* Prometheus gauges. It is the effective mode, the
// last successful load reading against its threshold (multiple x cores),
// whether that reading is over the threshold (a breach streak in progress)
// and how many leftover processes the reaper has ended since the check
// started (reap mode; the alert fires on the same tick as the reap). Field
// values are snapshots at read time, not persisted state.
type HostLoadStatus struct {
	// Mode is the effective mode (off, alert or reap).
	Mode string `json:"mode"`
	// LastCheckAt is the instant of the last successful load reading; zero
	// while the check has never read one (mode off, or before the first tick).
	LastCheckAt time.Time `json:"last_check_at"`
	// LastLoad is the 1-minute load average of the last successful reading.
	LastLoad float64 `json:"last_load"`
	// LastLimit is the threshold (multiple x core count) of the same reading.
	LastLimit float64 `json:"last_limit"`
	// Breached is true when the last successful reading is over the threshold,
	// i.e. the breach streak the check counts minutes on is in progress right
	// now (false when the load is back under the limit).
	Breached bool `json:"breached"`
	// OrphansReaped is the cumulative number of leftover processes the reaper
	// has ended (reap mode; 0 in alert or off mode).
	OrphansReaped int `json:"orphans_reaped"`
}

// HostLoadCheck is the check. Zero-value fields use real sources.
type HostLoadCheck struct {
	Config   func() HostLoadConfig
	Now      func() time.Time
	Load     func() (float64, error)
	Cores    func() int
	Sessions func() []HostLoadSession
	Alert    func(title, message string)
	Reaper   *OrphanReaper
	// TopGroups is how many process groups an alert names; 0 means 5.
	TopGroups int

	mu      sync.Mutex
	since   time.Time
	alerted bool

	// Monitoring snapshot for Status(); guarded by mu. statusMode records
	// the effective mode seen on the last Check; the live config still wins
	// in Status() when the check has never ticked.
	statusMode    string
	lastCheckAt   time.Time
	lastLoad      float64
	lastLimit     float64
	lastBreached  bool
	orphansReaped int
}

func (h *HostLoadCheck) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *HostLoadCheck) cores() int {
	if h.Cores != nil {
		return h.Cores()
	}
	return runtime.NumCPU()
}

func (h *HostLoadCheck) load() (float64, error) {
	if h.Load != nil {
		return h.Load()
	}
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, fmt.Errorf("empty loadavg")
	}
	return strconv.ParseFloat(f[0], 64)
}

func (h *HostLoadCheck) reaper() *OrphanReaper {
	if h.Reaper != nil {
		return h.Reaper
	}
	return &OrphanReaper{}
}

// Check runs one evaluation. Call it on each tick. It raises at most one alert
// per breach episode; an episode ends when the load falls back under the limit.
func (h *HostLoadCheck) Check() HostLoadResult {
	var cfg HostLoadConfig
	if h.Config != nil {
		cfg = h.Config()
	}
	mode := cfg.EffectiveMode()
	var res HostLoadResult
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statusMode = string(mode)
	if mode == HostLoadModeOff {
		h.since, h.alerted = time.Time{}, false
		return res
	}
	load, err := h.load()
	if err != nil {
		return res
	}
	cores := h.cores()
	if cores < 1 {
		cores = 1
	}
	res.Load = load
	res.Limit = cfg.EffectiveMultiple() * float64(cores)
	if load <= res.Limit {
		h.since, h.alerted = time.Time{}, false
		h.recordReading(h.now(), load, res.Limit, false)
		return res
	}
	res.Breached = true
	now := h.now()
	if h.since.IsZero() {
		h.since = now
	}
	h.recordReading(now, load, res.Limit, true)
	if now.Sub(h.since) < time.Duration(cfg.EffectiveMinutes())*time.Minute || h.alerted {
		return res
	}
	h.alerted = true

	var sessions []HostLoadSession
	if h.Sessions != nil {
		sessions = h.Sessions()
	}
	res.Groups = h.topGroups(sessions)
	if h.Alert != nil {
		h.Alert("Host load high: Automata may be slowed", hostLoadMessage(res, cfg, h.now().Sub(h.since)))
	}
	res.Alerted = true
	if mode == HostLoadModeReap {
		res.Reaped = h.reapEnded(sessions)
		h.orphansReaped += res.Reaped.Count
	}
	return res
}

// recordReading stores the last successful reading for Status(). Call
// h.mu held.
func (h *HostLoadCheck) recordReading(at time.Time, load, limit float64, breached bool) {
	h.lastCheckAt = at
	h.lastLoad = load
	h.lastLimit = limit
	h.lastBreached = breached
}

// Status is the check's read-only snapshot for the monitoring surfaces (see
// HostLoadStatus). It is safe for concurrent use and never blocks meaningfully:
// it reads the check's own state only.
func (h *HostLoadCheck) Status() HostLoadStatus {
	if h == nil {
		return HostLoadStatus{}
	}
	mode := ""
	if h.Config != nil {
		mode = string(h.Config().EffectiveMode())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.statusMode != "" {
		mode = h.statusMode
	}
	return HostLoadStatus{
		Mode:          mode,
		LastCheckAt:   h.lastCheckAt,
		LastLoad:      h.lastLoad,
		LastLimit:     h.lastLimit,
		Breached:      h.lastBreached,
		OrphansReaped: h.orphansReaped,
	}
}

// hostLoadMessage renders the alert text.
func hostLoadMessage(res HostLoadResult, cfg HostLoadConfig, dur time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Load average %.2f is above %.2f (%.1f x cores) for %d minute(s). Mode: %s.",
		res.Load, res.Limit, cfg.EffectiveMultiple(), int(dur.Minutes()), cfg.EffectiveMode())
	if len(res.Groups) == 0 {
		b.WriteString(" No process groups could be read.")
	}
	for _, g := range res.Groups {
		fmt.Fprintf(&b, "\n- group %d (%d process(es), %s): ", g.PGID, g.Procs, g.Command)
		switch {
		case g.Owner == "":
			b.WriteString("not in any Automaton worktree or scratch folder")
		case g.Running:
			fmt.Fprintf(&b, "%s folder of running Automaton session %s", g.Owner, g.Session)
		default:
			fmt.Fprintf(&b, "%s folder of ended Automaton session %s", g.Owner, g.Session)
		}
	}
	return b.String()
}

type hostLoadProc struct {
	pid, pgid int
	ticks     uint64
}

// readProcs lists live processes with group id and cumulative CPU ticks.
func (h *HostLoadCheck) readProcs() []hostLoadProc {
	r := h.reaper()
	ents, err := os.ReadDir(r.procRoot())
	if err != nil {
		return nil
	}
	var out []hostLoadProc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(r.procRoot(), e.Name(), "stat"))
		if err != nil {
			continue
		}
		s := string(b)
		p := strings.LastIndexByte(s, ')')
		if p < 0 {
			continue
		}
		f := strings.Fields(s[p+1:])
		if len(f) < 14 || f[0] == "Z" || f[0] == "X" {
			continue
		}
		pgid, e1 := strconv.Atoi(f[2])
		ut, e2 := strconv.ParseUint(f[11], 10, 64)
		st, e3 := strconv.ParseUint(f[12], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		out = append(out, hostLoadProc{pid: pid, pgid: pgid, ticks: ut + st})
	}
	return out
}

// topGroups ranks process groups by CPU ticks and classifies each by session
// folder evidence (the orphan reaper's evidence rule, never a name match).
func (h *HostLoadCheck) topGroups(sessions []HostLoadSession) []HostLoadGroup {
	procs := h.readProcs()
	groups := map[int]*HostLoadGroup{}
	for _, p := range procs {
		g := groups[p.pgid]
		if g == nil {
			g = &HostLoadGroup{PGID: p.pgid}
			groups[p.pgid] = g
		}
		g.Procs++
		g.CPUTicks += p.ticks
	}
	// pid -> owner, from per-folder evidence.
	type owner struct {
		kind, session string
		running       bool
		cmd           string
	}
	owners := map[int]owner{}
	r := h.reaper()
	for _, s := range sessions {
		for _, set := range []struct {
			kind string
			dirs []string
		}{{"worktree", s.WorktreeDirs}, {"scratch", s.ScratchDirs}} {
			if len(set.dirs) == 0 {
				continue
			}
			for _, p := range r.Find(OrphanEvidence{Dirs: set.dirs}) {
				if _, ok := owners[p.PID]; !ok {
					owners[p.PID] = owner{set.kind, s.ID, s.Running, p.Command}
				}
			}
		}
	}
	for _, p := range procs {
		g := groups[p.pgid]
		if o, ok := owners[p.pid]; ok && g.Owner == "" {
			g.Owner, g.Session, g.Running, g.Command = o.kind, o.session, o.running, o.cmd
		}
	}
	out := make([]HostLoadGroup, 0, len(groups))
	for _, g := range groups {
		if g.Command == "" {
			g.Command = r.shortCommand(g.PGID, nil)
			if g.Command == "" {
				g.Command = "unknown"
			}
		}
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPUTicks != out[j].CPUTicks {
			return out[i].CPUTicks > out[j].CPUTicks
		}
		return out[i].PGID < out[j].PGID
	})
	n := h.TopGroups
	if n <= 0 {
		n = hostLoadTopGroups
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// reapEnded ends only processes of ended sessions that the reaper's evidence
// rule proves, by exact pid. Running sessions are never touched.
func (h *HostLoadCheck) reapEnded(sessions []HostLoadSession) OrphanReapResult {
	var total OrphanReapResult
	r := h.reaper()
	for _, s := range sessions {
		if s.Running {
			continue
		}
		dirs := append(append([]string{}, s.WorktreeDirs...), s.ScratchDirs...)
		if len(dirs) == 0 {
			continue
		}
		res := r.EndOrphans(OrphanEvidence{Dirs: dirs})
		total.Ended = append(total.Ended, res.Ended...)
		total.Survivors = append(total.Survivors, res.Survivors...)
	}
	total.Count = len(total.Ended)
	return total
}

package trace

import (
	"sort"
	"strings"
)

// Dimension is the grouping axis of a roll-up.
type Dimension string

// Roll-up dimensions.
const (
	DimSession   Dimension = "session"
	DimTask      Dimension = "task"
	DimStory     Dimension = "story"
	DimAutomaton Dimension = "automaton"
	DimAgent     Dimension = "agent"
	DimBackend   Dimension = "backend"
	// DimPackName and DimPackVersion group by the installable pack that
	// produced the event (see Event.PackName and Event.PackVersion).
	DimPackName    Dimension = "pack_name"
	DimPackVersion Dimension = "pack_version"
)

// Dimensions lists every roll-up dimension in a stable order. The pack
// dimensions come last so the order of the original six is unchanged.
var Dimensions = []Dimension{DimSession, DimTask, DimStory, DimAutomaton, DimAgent, DimBackend, DimPackName, DimPackVersion}

// DefaultLoopThreshold is the repeat count at which the same tool with the
// same argument summary is flagged as a repeated-call loop.
const DefaultLoopThreshold = 5

// DefaultSlowest is how many slowest tools a roll-up lists.
const DefaultSlowest = 5

// RollupOptions tunes Build. Zero values use the defaults.
type RollupOptions struct {
	// LoopThreshold: same tool+args summary this many times raises a warning.
	LoopThreshold int
	// Slowest caps the slowest-tools list.
	Slowest int
}

func (o RollupOptions) withDefaults() RollupOptions {
	if o.LoopThreshold <= 0 {
		o.LoopThreshold = DefaultLoopThreshold
	}
	if o.Slowest <= 0 {
		o.Slowest = DefaultSlowest
	}
	return o
}

// ToolStat is the per-tool slice of a roll-up.
type ToolStat struct {
	Name      string `json:"name"`
	Calls     int    `json:"calls"`
	Errors    int    `json:"errors"`
	TotalMS   int64  `json:"total_ms"`
	MaxMS     int64  `json:"max_ms"`
	AverageMS int64  `json:"avg_ms"`
}

// LoopWarning flags a tool called repeatedly with the same argument summary.
type LoopWarning struct {
	Tool       string `json:"tool"`
	ArgSummary string `json:"arg_summary,omitempty"`
	Count      int    `json:"count"`
}

// Rollup aggregates the trace events of one group.
type Rollup struct {
	Dimension Dimension `json:"dimension"`
	Key       string    `json:"key"`
	Events    int       `json:"events"`
	// ToolCounts is calls per tool name.
	ToolCounts map[string]int `json:"tool_counts"`
	// SlowestTools is the tools with the largest single-call duration.
	SlowestTools []ToolStat `json:"slowest_tools"`
	// ToolCalls and Errors feed ErrorRate (errors / events considered).
	ToolCalls int `json:"tool_calls"`
	Errors    int `json:"errors"`
	// ErrorRate is Errors divided by ToolCalls, capped at 1; 0 when there
	// are no tool calls.
	ErrorRate float64 `json:"error_rate"`
	// LoopWarnings lists repeated-call loops; recorded loop_warning events
	// are counted separately in RecordedLoopWarnings.
	LoopWarnings         []LoopWarning `json:"loop_warnings"`
	RecordedLoopWarnings int           `json:"recorded_loop_warnings"`
	// CapabilityDenials counts denied capability checks and permission
	// decisions.
	CapabilityDenials int `json:"capability_denials"`
	// ModelRequests, TokensIn and TokensOut total the model requests.
	ModelRequests int   `json:"model_requests"`
	TokensIn      int64 `json:"tokens_in"`
	TokensOut     int64 `json:"tokens_out"`
}

// Usage is the request/token slice that feeds the usage and cost views.
type Usage struct {
	ModelRequests int   `json:"model_requests"`
	TokensIn      int64 `json:"tokens_in"`
	TokensOut     int64 `json:"tokens_out"`
}

// Usage returns the request/token totals of the roll-up.
func (r Rollup) Usage() Usage {
	return Usage{ModelRequests: r.ModelRequests, TokensIn: r.TokensIn, TokensOut: r.TokensOut}
}

// IsDenial reports whether ev is a denied capability check or permission
// decision.
func IsDenial(ev Event) bool {
	if ev.Kind != KindCapabilityCheck && ev.Kind != KindPermissionDecision {
		return false
	}
	o := strings.ToLower(strings.TrimSpace(ev.Outcome))
	return o == "denied" || o == "deny" || strings.HasPrefix(o, "denied") || o == "blocked"
}

// GroupKey returns the key of ev along dim ("" when the event carries none).
func GroupKey(ev Event, dim Dimension) string {
	switch dim {
	case DimSession:
		return ev.SessionID
	case DimTask:
		return ev.TaskID
	case DimStory:
		return ev.StoryID
	case DimAutomaton:
		return ev.AutomatonID
	case DimAgent:
		return ev.Agent
	case DimBackend:
		return ev.Backend
	case DimPackName:
		return ev.PackName
	case DimPackVersion:
		return ev.PackVersion
	}
	return ""
}

type toolAcc struct {
	calls, errors int
	total, max    int64
}

type rollupAcc struct {
	r     Rollup
	tools map[string]*toolAcc
	reps  map[[2]string]int
}

func newAcc(dim Dimension, key string) *rollupAcc {
	return &rollupAcc{
		r:     Rollup{Dimension: dim, Key: key, ToolCounts: map[string]int{}},
		tools: map[string]*toolAcc{},
		reps:  map[[2]string]int{},
	}
}

func (a *rollupAcc) add(ev Event) {
	r := &a.r
	r.Events++
	if IsError(ev) {
		r.Errors++
	}
	switch ev.Kind {
	case KindToolCall:
		r.ToolCalls++
		r.ToolCounts[ev.Name]++
		t := a.tools[ev.Name]
		if t == nil {
			t = &toolAcc{}
			a.tools[ev.Name] = t
		}
		t.calls++
		t.total += ev.DurationMS
		if ev.DurationMS > t.max {
			t.max = ev.DurationMS
		}
		a.reps[[2]string{ev.Name, ev.ArgSummary}]++
	case KindToolResult:
		// Results carry the duration and failure of a call; a failed
		// result counts against the tool but not as another call.
		t := a.tools[ev.Name]
		if t == nil {
			t = &toolAcc{}
			a.tools[ev.Name] = t
		}
		t.total += ev.DurationMS
		if ev.DurationMS > t.max {
			t.max = ev.DurationMS
		}
		if IsError(ev) {
			t.errors++
		}
	case KindModelRequest:
		r.ModelRequests++
		r.TokensIn += ev.TokensIn
		r.TokensOut += ev.TokensOut
	case KindLoopWarning:
		r.RecordedLoopWarnings++
	}
	if IsDenial(ev) {
		r.CapabilityDenials++
	}
}

func (a *rollupAcc) finish(opts RollupOptions) Rollup {
	r := a.r
	// Error rate: error events and failed outcomes over tool calls,
	// capped at 1.
	denom := r.ToolCalls
	if denom > 0 {
		r.ErrorRate = float64(r.Errors) / float64(denom)
		if r.ErrorRate > 1 {
			r.ErrorRate = 1
		}
	}
	stats := make([]ToolStat, 0, len(a.tools))
	for name, t := range a.tools {
		st := ToolStat{Name: name, Calls: t.calls, Errors: t.errors, TotalMS: t.total, MaxMS: t.max}
		if t.calls > 0 {
			st.AverageMS = t.total / int64(t.calls)
		}
		stats = append(stats, st)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].MaxMS != stats[j].MaxMS {
			return stats[i].MaxMS > stats[j].MaxMS
		}
		return stats[i].Name < stats[j].Name
	})
	if len(stats) > opts.Slowest {
		stats = stats[:opts.Slowest]
	}
	r.SlowestTools = stats
	r.LoopWarnings = []LoopWarning{}
	for k, n := range a.reps {
		if n >= opts.LoopThreshold {
			r.LoopWarnings = append(r.LoopWarnings, LoopWarning{Tool: k[0], ArgSummary: k[1], Count: n})
		}
	}
	sort.Slice(r.LoopWarnings, func(i, j int) bool {
		if r.LoopWarnings[i].Count != r.LoopWarnings[j].Count {
			return r.LoopWarnings[i].Count > r.LoopWarnings[j].Count
		}
		if r.LoopWarnings[i].Tool != r.LoopWarnings[j].Tool {
			return r.LoopWarnings[i].Tool < r.LoopWarnings[j].Tool
		}
		return r.LoopWarnings[i].ArgSummary < r.LoopWarnings[j].ArgSummary
	})
	return r
}

// BuildRollups groups events by dim and returns one Rollup per key, sorted
// by key. Events with no key along dim are skipped. Output is deterministic
// and does not depend on the clock.
func BuildRollups(events []Event, dim Dimension, opts RollupOptions) []Rollup {
	opts = opts.withDefaults()
	accs := map[string]*rollupAcc{}
	for _, ev := range events {
		key := GroupKey(ev, dim)
		if key == "" {
			continue
		}
		a := accs[key]
		if a == nil {
			a = newAcc(dim, key)
			accs[key] = a
		}
		a.add(ev)
	}
	out := make([]Rollup, 0, len(accs))
	for _, a := range accs {
		out = append(out, a.finish(opts))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// RollupAll builds the roll-up for every dimension.
func RollupAll(events []Event, opts RollupOptions) map[Dimension][]Rollup {
	out := make(map[Dimension][]Rollup, len(Dimensions))
	for _, d := range Dimensions {
		out[d] = BuildRollups(events, d, opts)
	}
	return out
}

// ReadAll returns the events of every indexed session.
func (st *Store) ReadAll() ([]Event, error) {
	var all []Event
	for _, e := range st.Index() {
		evs, err := st.Read(e.Session, Filter{})
		if err != nil {
			return nil, err
		}
		all = append(all, evs...)
	}
	return all, nil
}

// Rollups reads the whole store and rolls it up along dim.
func (st *Store) Rollups(dim Dimension, opts RollupOptions) ([]Rollup, error) {
	evs, err := st.ReadAll()
	if err != nil {
		return nil, err
	}
	return BuildRollups(evs, dim, opts), nil
}

// UsageSummary totals model requests and tokens per backend and overall,
// for the additive field of the cost report.
type UsageSummary struct {
	Total     Usage            `json:"total"`
	ByBackend map[string]Usage `json:"by_backend"`
}

// SummarizeUsage totals request/token counts over events.
func SummarizeUsage(events []Event) UsageSummary {
	s := UsageSummary{ByBackend: map[string]Usage{}}
	for _, ev := range events {
		if ev.Kind != KindModelRequest {
			continue
		}
		s.Total.ModelRequests++
		s.Total.TokensIn += ev.TokensIn
		s.Total.TokensOut += ev.TokensOut
		b := ev.Backend
		if b == "" {
			b = "unknown"
		}
		u := s.ByBackend[b]
		u.ModelRequests++
		u.TokensIn += ev.TokensIn
		u.TokensOut += ev.TokensOut
		s.ByBackend[b] = u
	}
	return s
}

package supervisorloop

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/planrecords"
)

// ToolServerDatawatch is the only tool server a supervisor build carries.
const ToolServerDatawatch = "datawatch"

// SessionLayer supplies the session facts of layer one for a project.
type SessionLayer interface {
	// ProjectDir is the project's directory.
	ProjectDir(project string) (string, error)
	// SupervisorProfile names the profile a supervisor session runs under.
	SupervisorProfile(project string) (string, error)
}

// RulesSource supplies layer two. It is backed by the skills registry, which
// holds the shipped default build supervisor pack, and by the rules context
// policy, which reads the project rules file.
type RulesSource interface {
	// DefaultPack is the shipped default build supervisor pack.
	DefaultPack() (string, error)
	// ProjectPack is the project's own rules pack; ok is false when it has none.
	ProjectPack(project string) (text string, ok bool, err error)
	// ProjectRulesFile is the project rules file as the rules context policy
	// reads it; ok is false when the project has none.
	ProjectRulesFile(project string) (text string, ok bool, err error)
}

// PlanSource supplies layer three from the project's own plans.
type PlanSource interface {
	// ProjectPlans returns the project's plans in priority order, with the
	// Automata of each in run order.
	ProjectPlans(project string) ([]planrecords.PlanDigest, error)
	// BlockedReason says why an Automaton is blocked; empty when unknown.
	BlockedReason(project, automatonID string) (string, error)
}

// BuildContext is layer one: a lean build context.
type BuildContext struct {
	Profile     string
	ToolServers []string
	ProjectDir  string
	Model       string
}

// RulesLayer is layer two.
type RulesLayer struct {
	DefaultPack string
	ProjectPack string
	RulesFile   string
}

// WorkItem is one Automaton of a plan.
type WorkItem struct {
	ID            string
	Title         string
	State         string
	Blocked       bool
	BlockedReason string
}

// PlanDirection is one plan as the direction layer states it.
type PlanDirection struct {
	Title    string
	Goal     string
	Order    int
	State    string
	Work     []WorkItem
	NextWork string
}

// DirectionLayer is layer three.
type DirectionLayer struct {
	Plans []PlanDirection
}

// Handover is the three-layer handover for one project's supervisor.
type Handover struct {
	Project     string
	AssembledAt time.Time
	Build       BuildContext
	Rules       RulesLayer
	Direction   DirectionLayer
}

// HandoverAssembler assembles handovers from its sources.
type HandoverAssembler struct {
	Session   SessionLayer
	Rules     RulesSource
	Plans     PlanSource
	Overrides OverridesSource
	Clock     Clock
}

// AssembleHandover builds the three layers for project.
func (a *HandoverAssembler) AssembleHandover(project string) (Handover, error) {
	if strings.TrimSpace(project) == "" {
		return Handover{}, errors.New("handover: project is required")
	}
	if a.Session == nil || a.Rules == nil || a.Plans == nil || a.Clock == nil {
		return Handover{}, errors.New("handover: session, rules, plans and clock are required")
	}
	set, err := LoadSupervisorSettings(a.Overrides, project)
	if err != nil {
		return Handover{}, err
	}
	dir, err := a.Session.ProjectDir(project)
	if err != nil {
		return Handover{}, fmt.Errorf("handover: project dir: %w", err)
	}
	profile, err := a.Session.SupervisorProfile(project)
	if err != nil {
		return Handover{}, fmt.Errorf("handover: supervisor profile: %w", err)
	}
	h := Handover{
		Project: project, AssembledAt: a.Clock.Now(),
		Build: BuildContext{Profile: profile, ToolServers: []string{ToolServerDatawatch}, ProjectDir: dir, Model: set.Model},
	}
	if h.Rules.DefaultPack, err = a.Rules.DefaultPack(); err != nil {
		return Handover{}, fmt.Errorf("handover: default pack: %w", err)
	}
	if h.Rules.ProjectPack, _, err = a.Rules.ProjectPack(project); err != nil {
		return Handover{}, fmt.Errorf("handover: project pack: %w", err)
	}
	if h.Rules.RulesFile, _, err = a.Rules.ProjectRulesFile(project); err != nil {
		return Handover{}, fmt.Errorf("handover: project rules file: %w", err)
	}
	digests, err := a.Plans.ProjectPlans(project)
	if err != nil {
		return Handover{}, fmt.Errorf("handover: plans: %w", err)
	}
	for _, d := range digests {
		pd := PlanDirection{Title: d.Summary.Title, Goal: d.Summary.Goal, Order: d.Summary.Order, State: string(d.Summary.State)}
		if d.Next != nil {
			pd.NextWork = d.Next.Title
		}
		for _, au := range d.Automata {
			w := WorkItem{ID: au.ID, Title: au.Title, State: au.State, Blocked: au.Run == planrecords.RunBlocked}
			if w.Blocked {
				if w.BlockedReason, err = a.Plans.BlockedReason(project, au.ID); err != nil {
					return Handover{}, fmt.Errorf("handover: blocked reason: %w", err)
				}
			}
			pd.Work = append(pd.Work, w)
		}
		h.Direction.Plans = append(h.Direction.Plans, pd)
	}
	return h, nil
}

// Render writes the handover as the text a supervisor session starts from.
func (h Handover) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Supervisor handover: %s\nAssembled: %s\n\n", h.Project, h.AssembledAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "## Build context\n- Profile: %s\n- Tool servers: %s\n- Project directory: %s\n- Model: %s\n\n",
		h.Build.Profile, strings.Join(h.Build.ToolServers, ", "), h.Build.ProjectDir, h.Build.Model)
	b.WriteString("## Rules\n")
	for _, s := range []struct{ name, text string }{
		{"Default build supervisor pack", h.Rules.DefaultPack},
		{"Project rules pack", h.Rules.ProjectPack},
		{"Project rules file", h.Rules.RulesFile},
	} {
		if strings.TrimSpace(s.text) != "" {
			fmt.Fprintf(&b, "### %s\n%s\n\n", s.name, strings.TrimSpace(s.text))
		}
	}
	b.WriteString("## Direction\n")
	for _, p := range h.Direction.Plans {
		fmt.Fprintf(&b, "### %d. %s [%s]\nGoal: %s\n", p.Order, p.Title, p.State, p.Goal)
		if p.NextWork != "" {
			fmt.Fprintf(&b, "Next: %s\n", p.NextWork)
		}
		for _, w := range p.Work {
			fmt.Fprintf(&b, "- %s (%s)", w.Title, w.State)
			if w.Blocked {
				fmt.Fprintf(&b, " BLOCKED: %s", w.BlockedReason)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

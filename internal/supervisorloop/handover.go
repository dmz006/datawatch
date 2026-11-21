package supervisorloop

import (
	"errors"
	"fmt"
	"path/filepath"
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

// RulesFilePather is an optional extra of a RulesSource: the path of the
// project rules file, so the handover can point at it.
type RulesFilePather interface {
	ProjectRulesFilePath(project string) string
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
	// RulesFile is the project rules file text. It is not rendered: the
	// handover carries RulesFilePath and RulesHeadings instead.
	RulesFile     string
	RulesFilePath string
	// RulesHeadings are the rules file's section headings, in order.
	RulesHeadings []string
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
	var haveRules bool
	if h.Rules.RulesFile, haveRules, err = a.Rules.ProjectRulesFile(project); err != nil {
		return Handover{}, fmt.Errorf("handover: project rules file: %w", err)
	}
	if haveRules {
		h.Rules.RulesFilePath = filepath.Join(dir, "AGENT.md")
		if rp, ok := a.Rules.(RulesFilePather); ok {
			h.Rules.RulesFilePath = rp.ProjectRulesFilePath(project)
		}
		h.Rules.RulesHeadings = MarkdownHeadings(h.Rules.RulesFile)
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

// MaxHandoverChars is the bound on a rendered handover for any project.
const MaxHandoverChars = 24000

// Caps on the two layers whose size the project controls; with the build
// context and the shipped pack they keep Render under MaxHandoverChars.
const (
	maxProjectPackChars = 7000
	maxDirectionChars   = 7000
	maxRulesHeadings    = 80
)

// MarkdownHeadings lists the heading lines of a markdown text, in order,
// ignoring fenced code blocks.
func MarkdownHeadings(text string) []string {
	var out []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(t, "#") && strings.HasPrefix(strings.TrimLeft(t, "#"), " ") {
			if len(t) > 120 {
				t = t[:120]
			}
			out = append(out, t)
		}
	}
	return out
}

// capText cuts text to at most n bytes at a line boundary and says so.
func capText(text string, n int, note string) string {
	if len(text) <= n {
		return text
	}
	cut := text[:n]
	if i := strings.LastIndex(cut, "\n"); i > 0 {
		cut = cut[:i]
	}
	return cut + "\n[truncated: " + note + "]"
}

// Render writes the handover as the text a supervisor session starts from.
// It never exceeds MaxHandoverChars: the project rules file is carried by
// path and heading list, not in full.
func (h Handover) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Supervisor handover: %s\nAssembled: %s\n\n", h.Project, h.AssembledAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "## Build context\n- Profile: %s\n- Tool servers: %s\n- Project directory: %s\n- Model: %s\n\n",
		h.Build.Profile, strings.Join(h.Build.ToolServers, ", "), h.Build.ProjectDir, h.Build.Model)
	b.WriteString("## Rules\n")
	for _, s := range []struct {
		name, text string
		max        int
	}{
		{"Default build supervisor pack", h.Rules.DefaultPack, 8000},
		{"Project rules pack", h.Rules.ProjectPack, maxProjectPackChars},
	} {
		if strings.TrimSpace(s.text) != "" {
			fmt.Fprintf(&b, "### %s\n%s\n\n", s.name, capText(strings.TrimSpace(s.text), s.max, "read packs/build-supervisor/rules.md in the project for the rest"))
		}
	}
	if h.Rules.RulesFilePath != "" {
		fmt.Fprintf(&b, "### Project rules file\nNot embedded. Read %s by path when you need a rule. Sections:\n", h.Rules.RulesFilePath)
		for i, hd := range h.Rules.RulesHeadings {
			if i == maxRulesHeadings {
				fmt.Fprintf(&b, "- ... %d more headings\n", len(h.Rules.RulesHeadings)-i)
				break
			}
			fmt.Fprintf(&b, "- %s\n", hd)
		}
		b.WriteString("\n")
	}
	var d strings.Builder
	for _, p := range h.Direction.Plans {
		fmt.Fprintf(&d, "### %d. %s [%s]\nGoal: %s\n", p.Order, p.Title, p.State, p.Goal)
		if p.NextWork != "" {
			fmt.Fprintf(&d, "Next: %s\n", p.NextWork)
		}
		for _, w := range p.Work {
			fmt.Fprintf(&d, "- %s (%s)", w.Title, w.State)
			if w.Blocked {
				fmt.Fprintf(&d, " BLOCKED: %s", w.BlockedReason)
			}
			d.WriteString("\n")
		}
		d.WriteString("\n")
	}
	b.WriteString("## Direction\n")
	b.WriteString(capText(d.String(), maxDirectionChars, "list the project's plans for the rest"))
	// The ending instruction is added after the bound is applied, so a long
	// handover never loses it: a session that is not told the marker does not end.
	return capText(b.String(), MaxHandoverChars-len(endingSection), "handover bound") + endingSection
}

// EndingInstruction tells a supervisor pass how to finish. The daemon ends a
// one-shot session when it sees a DATAWATCH_COMPLETE line, the same way it ends
// a worker session; without the marker a finished pass sits idle at its prompt.
const EndingInstruction = "When the pass is done, print DATAWATCH_COMPLETE: followed by a one-line summary as your very last line. " +
	"The daemon ends the session when it sees that line, so do not wait for anything after it."

const endingSection = "\n\n## Ending the pass\n" + EndingInstruction + "\n"

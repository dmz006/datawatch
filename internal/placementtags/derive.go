package placementtags

import (
	"fmt"
	"sort"
	"strings"
)

// PlacementFacts holds the concrete placement values for a piece of work.
// Any field that is empty simply produces no tag for its axis; the rest of
// the fields need not be populated.
type PlacementFacts struct {
	// Compute is the node name that runs the work (the compute axis).
	Compute string
	// Peer is the federated peer name; empty for work that runs locally.
	Peer string
	// Backend is the compute/LLM backend in use.
	Backend string
	// Lane is the lane the work runs in: "gpu" or "claude".
	Lane string
	// Role is the role the work plays in: worker, verifier, planner or judge.
	Role string
	// Project is the project alias the work belongs to.
	Project string
	// Automation, Stage and Kind carry the surrounding automation context
	// (automation id, pipeline stage and work kind). They are facts about the
	// placement, not configurable built-in axes; expose them as tags by
	// configuring a custom axis that reads them through Fields.
	Automation string
	Stage      string
	Kind       string
	// Fields holds custom axis values keyed by the field name a custom axis
	// reads. It may be nil.
	Fields map[string]string
}

// AxisSetting controls one built-in placement axis.
type AxisSetting struct {
	// Enabled turns the axis on or off. Disabled axes never yield a tag.
	Enabled bool
	// Template is the tag to build. It must contain the axis's placeholder
	// (see the axis table in the package); the placeholder is replaced with
	// the fact value. Default templates are "<axis>:<placeholder>".
	Template string
}

// CustomAxis configures an extra, user-defined axis that always uses the
// {value} placeholder and reads its value from Facts.Fields[Field].
type CustomAxis struct {
	// Name is a stable label for the axis, used only in error messages.
	Name string
	// Field is the key in PlacementFacts.Fields the axis reads its value from.
	Field string
	// Template is the tag to build; it must contain the {value} placeholder.
	Template string
	// Enabled turns the axis on or off.
	Enabled bool
}

// Settings controls which placement axes are derived. There is one
// AxisSetting per built-in axis plus any number of custom axes.
type Settings struct {
	Compute AxisSetting
	Peer    AxisSetting
	Backend AxisSetting
	Lane    AxisSetting
	Role    AxisSetting
	Project AxisSetting
	// Custom holds user-defined axes that read their value from Facts.Fields.
	Custom []CustomAxis
}

// axisPlaceholder is the placeholder each built-in axis substitutes into its
// template. A template that omits its own placeholder is rejected by
// ValidateSettings.
const (
	placeholderNode    = "{node}"
	placeholderPeer    = "{peer}"
	placeholderBackend = "{backend}"
	placeholderLane    = "{lane}"
	placeholderRole    = "{role}"
	placeholderProject = "{project}"
	// placeholderValue is the single placeholder every custom axis must use.
	placeholderValue = "{value}"
)

// axisSpec describes one built-in axis for derivation and validation.
type axisSpec struct {
	name        string
	placeholder string
	value       func(PlacementFacts) string
	setting     AxisSetting
}

// builtins returns the six built-in axes in a stable order, paired with the
// caller's settings.
func builtins(f PlacementFacts, s Settings) []axisSpec {
	return []axisSpec{
		{"compute", placeholderNode, func(f PlacementFacts) string { return f.Compute }, s.Compute},
		{"peer", placeholderPeer, func(f PlacementFacts) string { return f.Peer }, s.Peer},
		{"backend", placeholderBackend, func(f PlacementFacts) string { return f.Backend }, s.Backend},
		{"lane", placeholderLane, func(f PlacementFacts) string { return f.Lane }, s.Lane},
		{"role", placeholderRole, func(f PlacementFacts) string { return f.Role }, s.Role},
		{"project", placeholderProject, func(f PlacementFacts) string { return f.Project }, s.Project},
	}
}

// DefaultSettings returns settings with every built-in axis enabled using the
// conventional "<axis>:<placeholder>" templates and no custom axes.
func DefaultSettings() Settings {
	return Settings{
		Compute: AxisSetting{Enabled: true, Template: "compute:" + placeholderNode},
		Peer:    AxisSetting{Enabled: true, Template: "peer:" + placeholderPeer},
		Backend: AxisSetting{Enabled: true, Template: "backend:" + placeholderBackend},
		Lane:    AxisSetting{Enabled: true, Template: "lane:" + placeholderLane},
		Role:    AxisSetting{Enabled: true, Template: "role:" + placeholderRole},
		Project: AxisSetting{Enabled: true, Template: "project:" + placeholderProject},
	}
}

// Derive returns the sorted, de-duplicated placement tags for the facts under
// the settings. A tag is produced for each enabled axis whose fact value is
// non-empty, by replacing the axis's placeholder in its template with the
// value. Disabled axes, empty values, and duplicate tag strings are all
// skipped. A zero-value Settings therefore yields no tags.
func Derive(f PlacementFacts, s Settings) []string {
	tags := make(map[string]struct{})
	add := func(tag string) {
		if tag != "" {
			tags[tag] = struct{}{}
		}
	}

	for _, ax := range builtins(f, s) {
		if !ax.setting.Enabled {
			continue
		}
		value := ax.value(f)
		if value == "" {
			continue
		}
		add(strings.ReplaceAll(ax.setting.Template, ax.placeholder, value))
	}

	for _, c := range s.Custom {
		if !c.Enabled {
			continue
		}
		value := f.Fields[c.Field]
		if value == "" {
			continue
		}
		add(strings.ReplaceAll(c.Template, placeholderValue, value))
	}

	out := make([]string, 0, len(tags))
	for tag := range tags {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// ValidateSettings reports an error if any enabled or disabled axis carries a
// template that does not contain its own placeholder. Built-in templates must
// contain that axis's placeholder (for example {node} for compute); custom
// templates must contain {value}.
func ValidateSettings(s Settings) error {
	for _, ax := range builtins(PlacementFacts{}, s) {
		if !strings.Contains(ax.setting.Template, ax.placeholder) {
			return fmt.Errorf("placementtags: %s template %q must contain %s", ax.name, ax.setting.Template, ax.placeholder)
		}
	}
	for i, c := range s.Custom {
		if !strings.Contains(c.Template, placeholderValue) {
			name := c.Name
			if name == "" {
				name = fmt.Sprintf("custom[%d]", i)
			}
			return fmt.Errorf("placementtags: custom axis %q template %q must contain %s", name, c.Template, placeholderValue)
		}
	}
	return nil
}

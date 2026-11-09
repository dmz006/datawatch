// Release size resolution for an Automaton. ResolveReleaseSize walks the
// layers from most specific to least specific and reports the release size,
// which layer decided it, and whether the change must go through the major
// approval mode. It is a pure function: it takes plain values, does no I/O
// and never reads the diff.
//
// Layers, most specific first:
//  1. An explicit release_size on the Automaton (patch, minor, major or none).
//  2. The Automation (arc) the Automaton belongs to, when it is a milestone
//     that has completed and the project major_trigger is "milestone": the
//     release raises the major step.
//  3. The project kind map (kind to size, with the explicit override applied
//     by the caller through config.EffectiveReleaseSizeMap).
//  4. The built-in default: feature is minor, bug is patch, and any other
//     kind with no map entry resolves to none.
//
// A major is never inferred from a diff or a kind. Only an explicit major or
// a completed milestone under the milestone trigger yields one.
package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Sources a release size can be resolved from, reported in
// ReleaseSizeResult.Source.
const (
	ReleaseSizeSourceExplicit            = "explicit"
	ReleaseSizeSourceAutomationMilestone = "automation_milestone"
	ReleaseSizeSourceProjectMap          = "project_map"
	ReleaseSizeSourceBuiltInDefault      = "built_in_default"
)

// ReleaseSizeAutomation describes the Automation (arc) an Automaton belongs
// to, as far as release size resolution needs it.
type ReleaseSizeAutomation struct {
	// Milestone is true when the Automation is marked as a milestone.
	Milestone bool
	// Completed is true when every member of the Automation has finished.
	Completed bool
}

// ReleaseSizeInput is everything ResolveReleaseSize reads.
type ReleaseSizeInput struct {
	// ReleaseSize is the Automaton's explicit release_size. Empty is unset.
	ReleaseSize string
	// BreakingChange is the Automaton's breaking-change flag.
	BreakingChange bool
	// Kind is the effective kind of the Automaton: feature, bug or other.
	Kind string
	// Automation is the Automation the Automaton belongs to; nil when it
	// belongs to none.
	Automation *ReleaseSizeAutomation
	// KindMap is the project's kind-to-size map, as returned by
	// config.MergePipelineConfig.EffectiveReleaseSizeMap. nil means no map.
	KindMap map[string]string
	// MajorTrigger is the project's effective major_trigger: manual,
	// milestone or never. Empty means manual.
	MajorTrigger string
}

// ReleaseSizeResult is the resolved release size.
type ReleaseSizeResult struct {
	// Size is patch, minor, major or none.
	Size string
	// Source is the layer that decided Size: one of the ReleaseSizeSource*
	// constants.
	Source string
	// RaisesMajorApproval is true when the release must go through the major
	// approval mode. It is set by a breaking change whatever the size, and
	// Size is left as resolved.
	RaisesMajorApproval bool
}

// ResolveReleaseSize resolves the release size of one Automaton. See the
// package comment for the layers.
func ResolveReleaseSize(in ReleaseSizeInput) ReleaseSizeResult {
	res := ReleaseSizeResult{RaisesMajorApproval: in.BreakingChange}

	// Layer 1: an explicit size wins, including an explicit major under any
	// major trigger and an explicit none.
	if explicit, ok := NormalizeReleaseSize(in.ReleaseSize); ok && explicit != "" {
		res.Size = explicit
		res.Source = ReleaseSizeSourceExplicit
		return res
	}

	// Layer 2: a completed milestone Automation raises the major step, but
	// only when the project trigger is milestone.
	if in.Automation != nil && in.Automation.Milestone && in.Automation.Completed &&
		normalizeMajorTrigger(in.MajorTrigger) == config.MajorTriggerMilestone {
		res.Size = ReleaseSizeMajor
		res.Source = ReleaseSizeSourceAutomationMilestone
		return res
	}

	// Layer 3: the project kind map. A major entry is ignored here, because a
	// major is never inferred from a kind; the lookup then falls through.
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if size, ok := lookupKindSize(in.KindMap, kind); ok {
		res.Size = size
		res.Source = ReleaseSizeSourceProjectMap
		return res
	}

	// Layer 4: the built-in default, with unknown kinds resolving to none.
	res.Size = builtInKindSize(kind)
	res.Source = ReleaseSizeSourceBuiltInDefault
	return res
}

// lookupKindSize returns the non-major size the map gives kind. A map entry
// that is missing, invalid, or major reports false so the next layer decides.
func lookupKindSize(m map[string]string, kind string) (string, bool) {
	if kind == "" || m == nil {
		return "", false
	}
	raw, ok := m[kind]
	if !ok {
		return "", false
	}
	size, valid := NormalizeReleaseSize(raw)
	if !valid || size == "" || size == ReleaseSizeMajor {
		return "", false
	}
	return size, true
}

// builtInKindSize is the built-in kind-to-size default. Feature is minor, bug
// is patch, and every other kind is none.
func builtInKindSize(kind string) string {
	switch kind {
	case config.ReleaseKindFeature:
		return ReleaseSizeMinor
	case config.ReleaseKindBug:
		return ReleaseSizePatch
	}
	return ReleaseSizeNone
}

// normalizeMajorTrigger maps an empty trigger to the default manual trigger.
func normalizeMajorTrigger(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return config.MajorTriggerManual
	}
	return v
}

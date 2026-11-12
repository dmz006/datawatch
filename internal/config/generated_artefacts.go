// generated_artefacts.go — switches for the artefacts an Automaton
// generates from its change record. Every switch is on when unset.
package config

// GeneratedArtefacts holds one optional switch per generated artefact. A nil
// pointer means "on" so an explicit off survives a YAML or JSON round-trip.
type GeneratedArtefacts struct {
	ChangeRecord     *bool `yaml:"change_record" json:"change_record"`
	GateReport       *bool `yaml:"gate_report" json:"gate_report"`
	ChangelogDraft   *bool `yaml:"changelog_draft" json:"changelog_draft"`
	ParityDraft      *bool `yaml:"parity_draft" json:"parity_draft"`
	TrackerRows      *bool `yaml:"tracker_rows" json:"tracker_rows"`
	RegistryRow      *bool `yaml:"registry_row" json:"registry_row"`
	CombinedDocsTask *bool `yaml:"combined_docs_task" json:"combined_docs_task"`
}

// EffectiveGeneratedArtefacts is the resolved, always-valid set of switches.
type EffectiveGeneratedArtefactsSettings struct {
	ChangeRecord     bool `json:"change_record"`
	GateReport       bool `json:"gate_report"`
	ChangelogDraft   bool `json:"changelog_draft"`
	ParityDraft      bool `json:"parity_draft"`
	TrackerRows      bool `json:"tracker_rows"`
	RegistryRow      bool `json:"registry_row"`
	CombinedDocsTask bool `json:"combined_docs_task"`
}

func onUnlessOff(b *bool) bool { return b == nil || *b }

// Effective resolves the switches: unset means on.
func (g GeneratedArtefacts) Effective() EffectiveGeneratedArtefactsSettings {
	return EffectiveGeneratedArtefactsSettings{
		ChangeRecord:     onUnlessOff(g.ChangeRecord),
		GateReport:       onUnlessOff(g.GateReport),
		ChangelogDraft:   onUnlessOff(g.ChangelogDraft),
		ParityDraft:      onUnlessOff(g.ParityDraft),
		TrackerRows:      onUnlessOff(g.TrackerRows),
		RegistryRow:      onUnlessOff(g.RegistryRow),
		CombinedDocsTask: onUnlessOff(g.CombinedDocsTask),
	}
}

// EffectiveGeneratedArtefacts resolves the generated-artefact switches in
// effect for this autonomous config. An unset config yields everything on.
func (a *AutonomousConfig) EffectiveGeneratedArtefacts() EffectiveGeneratedArtefactsSettings {
	return a.GeneratedArtefacts.Effective()
}

// GeneratedArtefactKeys lists the switch names in a stable order.
func GeneratedArtefactKeys() []string {
	return []string{"change_record", "gate_report", "changelog_draft", "parity_draft",
		"tracker_rows", "registry_row", "combined_docs_task"}
}

// Set stores one switch by its YAML name as an explicit value. It returns
// false for an unknown name and leaves the block unchanged.
func (g *GeneratedArtefacts) Set(name string, v bool) bool {
	var p **bool
	switch name {
	case "change_record":
		p = &g.ChangeRecord
	case "gate_report":
		p = &g.GateReport
	case "changelog_draft":
		p = &g.ChangelogDraft
	case "parity_draft":
		p = &g.ParityDraft
	case "tracker_rows":
		p = &g.TrackerRows
	case "registry_row":
		p = &g.RegistryRow
	case "combined_docs_task":
		p = &g.CombinedDocsTask
	default:
		return false
	}
	b := v
	*p = &b
	return true
}

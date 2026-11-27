package acceptance

import (
	_ "embed"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var builtinDefaults []byte

// Defaults are the default items, as data.
type Defaults struct {
	Version   string            `yaml:"version" json:"version"`
	Kinds     map[Kind][]Item   `yaml:"kinds" json:"kinds"`
	Scheduled []Item            `yaml:"scheduled" json:"scheduled"`
	Fallback  []Item            `yaml:"fallback" json:"fallback"`
	Packs     map[string][]Item `yaml:"packs" json:"packs"`
}

// LoadDefaults reads the built-in defaults and, when path is set, overlays the
// project's file: a kind, pack or the scheduled or fallback list it names
// replaces the built-in one. An unreadable file is an error, so a broken
// project file never silently weakens a list.
func LoadDefaults(path string) (Defaults, error) {
	var d Defaults
	if err := yaml.Unmarshal(builtinDefaults, &d); err != nil {
		return d, fmt.Errorf("built-in acceptance defaults: %w", err)
	}
	if path == "" {
		return d, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-set config file path
	if err != nil {
		return d, fmt.Errorf("acceptance defaults file: %w", err)
	}
	var o Defaults
	if err := yaml.Unmarshal(raw, &o); err != nil {
		return d, fmt.Errorf("acceptance defaults file: %w", err)
	}
	for k, v := range o.Kinds {
		d.Kinds[k] = v
	}
	for k, v := range o.Packs {
		d.Packs[k] = v
	}
	if o.Scheduled != nil {
		d.Scheduled = o.Scheduled
	}
	if o.Fallback != nil {
		d.Fallback = o.Fallback
	}
	if o.Version != "" {
		d.Version = d.Version + "+" + o.Version
	} else {
		d.Version = d.Version + "+" + hashOf(string(raw))[:8]
	}
	return d, nil
}

// itemsFor returns fresh copies of the default items of one request.
func (d Defaults) itemsFor(kind Kind, scheduled bool, packs []string) ([]Item, error) {
	var out []Item
	add := func(list []Item) {
		for _, it := range list {
			it.Source = SourceDefault
			if it.State != nil {
				s := *it.State
				it.State = &s
			}
			if it.Wait != nil {
				w := *it.Wait
				it.Wait = &w
			}
			out = append(out, it)
		}
	}
	add(d.Kinds[kind])
	if scheduled {
		add(d.Scheduled)
	}
	for _, p := range packs {
		list, ok := d.Packs[p]
		if !ok {
			return nil, fmt.Errorf("unknown acceptance pack %q", p)
		}
		add(list)
	}
	return out, nil
}

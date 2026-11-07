package config

// The four axes added in the automata-pages redesign operator decision
// (docs/plans/2026-11-06-automata-pages-redesign.md, section 12):
// automaton, automation, stage and kind all default on, like project does now
// (it used to be the one built-in axis off by default).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var newPlacementAxes = []struct {
	axis    string
	get     func(PlacementConfig) PlacementAxis
	newTmpl string
	defTmpl string
}{
	{"automaton", func(p PlacementConfig) PlacementAxis { return p.Automaton }, "auto-{automaton}", "automaton:{automaton}"},
	{"automation", func(p PlacementConfig) PlacementAxis { return p.Automation }, "aut-{automation}", "automation:{automation}"},
	{"stage", func(p PlacementConfig) PlacementAxis { return p.Stage }, "st-{stage}", "stage:{stage}"},
	{"kind", func(p PlacementConfig) PlacementAxis { return p.Kind }, "k-{kind}", "kind:{kind}"},
}

// TestPlacementNewAxesDefaultOn: fresh config has all four new axes enabled
// with their conventional "<axis>:{placeholder}" templates, and project on.
func TestPlacementNewAxesDefaultOn(t *testing.T) {
	p := DefaultPlacementConfig()
	for _, c := range newPlacementAxes {
		ax := c.get(p)
		if !ax.Enabled {
			t.Errorf("%s axis default must be enabled, got %+v", c.axis, ax)
		}
		if ax.Template != c.defTmpl {
			t.Errorf("%s axis default template = %q, want %q", c.axis, ax.Template, c.defTmpl)
		}
	}
	if !p.Project.Enabled {
		t.Errorf("project axis must be on by default, got %+v", p.Project)
	}
}

// TestPlacementNewAxesSetRoundTrip: each new axis accepts .enabled and
// .template on the Set/validate path, survives a save/load cycle, and is
// refused when the template drops its own placeholder.
func TestPlacementNewAxesSetRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	tags := &cfg.Autonomous.Tags

	for _, c := range newPlacementAxes {
		// A template without the axis' own placeholder is refused and leaves
		// the block untouched.
		axisBefore := c.get(cfg.Autonomous.Tags.Placement)
		if err := tags.Set("placement."+c.axis+".template", "no-placeholder"); err == nil {
			t.Errorf("placement.%s.template without {%s} must be refused", c.axis, c.axis)
		} else if c.get(cfg.Autonomous.Tags.Placement) != axisBefore {
			t.Errorf("refused %s template changed the block to %+v", c.axis, c.get(cfg.Autonomous.Tags.Placement))
		}
		// .enabled and a good .template are accepted and land.
		if err := tags.Set("placement."+c.axis+".enabled", false); err != nil {
			t.Fatalf("Set placement.%s.enabled=false: %v", c.axis, err)
		}
		if err := tags.Set("placement."+c.axis+".enabled", true); err != nil {
			t.Fatalf("Set placement.%s.enabled=true: %v", c.axis, err)
		}
		if err := tags.Set("placement."+c.axis+".template", c.newTmpl); err != nil {
			t.Fatalf("Set placement.%s.template=%q: %v", c.axis, c.newTmpl, err)
		}
	}

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	placed := back.Autonomous.Tags.Placement
	for _, c := range newPlacementAxes {
		ax := c.get(placed)
		if !ax.Enabled || ax.Template != c.newTmpl {
			t.Errorf("reloaded placement.%s = %+v, want {enabled:true template:%q}", c.axis, ax, c.newTmpl)
		}
	}
}

// TestPlacementNewAxesRefusals: the shared validator names each new axis in
// its refusal, the way it names the old ones.
func TestPlacementNewAxesRefusals(t *testing.T) {
	for _, c := range newPlacementAxes {
		t.Run(c.axis, func(t *testing.T) {
			err := ValidateTagSetting("autonomous.tags.placement."+c.axis+".template", "no-placeholder")
			if err == nil || !strings.Contains(err.Error(), "autonomous.tags.placement."+c.axis) ||
				!strings.Contains(err.Error(), "{"+c.axis+"}") {
				t.Fatalf("refusal = %v, want it naming the %s axis and its {%s} placeholder", err, c.axis, c.axis)
			}
			// The .enabled key accepts a bool and nothing else.
			if err := ValidateTagSetting("autonomous.tags.placement."+c.axis+".enabled", true); err != nil {
				t.Fatalf("%s.enabled=true: want accepted, got %v", c.axis, err)
			}
			if err := ValidateTagSetting("autonomous.tags.placement."+c.axis+".switch", true); err == nil {
				t.Fatalf("%s.switch: want refused", c.axis)
			}
		})
		// An unknown axis is still unknown after the change.
		if err := ValidateTagSetting("autonomous.tags.placement.cpu.enabled", true); err == nil {
			t.Fatal("placement.cpu.enabled: want refusal")
		}
	}
}

const oldPlacementYAML = `autonomous:
  tags:
    placement:
      compute:
        enabled: false
        template: "compute:{node}"
      peer:
        enabled: true
        template: "peer:{peer}"
      backend:
        enabled: true
        template: "backend:{backend}"
      lane:
        enabled: true
        template: "lane:{lane}"
      role:
        enabled: true
        template: "role:{role}"
      project:
        enabled: false
        template: "project:{project}"
`

// TestPlacementOldConfigFileLoadsNewAxisDefaults: a config.yaml written before
// the change — placement names only the old six axes, explicitly — loads with
// the four new axes at their new defaults (on, conventional templates) while
// the written values hold.
func TestPlacementOldConfigFileLoadsNewAxisDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(oldPlacementYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load old config: %v", err)
	}
	p := back.Autonomous.Tags.Placement
	if p.Compute.Enabled {
		t.Errorf("explicit compute off must survive the load, got %+v", p.Compute)
	}
	if p.Project.Enabled {
		t.Errorf("explicit project off must survive the load, got %+v", p.Project)
	}
	for _, c := range newPlacementAxes {
		ax := c.get(p)
		if !ax.Enabled || ax.Template != c.defTmpl {
			t.Errorf("%s axis from old file = %+v, want {enabled:true template:%q}", c.axis, ax, c.defTmpl)
		}
	}
}

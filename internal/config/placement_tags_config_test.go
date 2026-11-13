package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/placementtags"
)

func TestPlacementConfigDefaults(t *testing.T) {
	p := DefaultPlacementConfig()
	if !p.Enabled {
		t.Fatalf("the master switch defaults on")
	}
	if got, want := p.Compute, (PlacementAxis{Enabled: true, Template: "compute:{node}"}); got != want {
		t.Errorf("compute default = %+v, want %+v", got, want)
	}
	if got, want := p.Peer, (PlacementAxis{Enabled: true, Template: "peer:{peer}"}); got != want {
		t.Errorf("peer default = %+v, want %+v", got, want)
	}
	if got, want := p.Backend, (PlacementAxis{Enabled: true, Template: "backend:{backend}"}); got != want {
		t.Errorf("backend default = %+v, want %+v", got, want)
	}
	if got, want := p.Lane, (PlacementAxis{Enabled: true, Template: "lane:{lane}"}); got != want {
		t.Errorf("lane default = %+v, want %+v", got, want)
	}
	if got, want := p.Role, (PlacementAxis{Enabled: true, Template: "role:{role}"}); got != want {
		t.Errorf("role default = %+v, want %+v", got, want)
	}
	if got, want := p.Project, (PlacementAxis{Enabled: true, Template: "project:{project}"}); got != want {
		t.Errorf("project default = %+v, want %+v", got, want)
	}
	if got, want := p.Automaton, (PlacementAxis{Enabled: true, Template: "automaton:{automaton}"}); got != want {
		t.Errorf("automaton default = %+v, want %+v", got, want)
	}
	if got, want := p.Automation, (PlacementAxis{Enabled: true, Template: "automation:{automation}"}); got != want {
		t.Errorf("automation default = %+v, want %+v", got, want)
	}
	if got, want := p.Stage, (PlacementAxis{Enabled: true, Template: "stage:{stage}"}); got != want {
		t.Errorf("stage default = %+v, want %+v", got, want)
	}
	if got, want := p.Kind, (PlacementAxis{Enabled: true, Template: "kind:{kind}"}); got != want {
		t.Errorf("kind default = %+v, want %+v", got, want)
	}
	if len(p.Custom) != 0 {
		t.Errorf("custom axes default empty, got %v", p.Custom)
	}

	// The templates are the derivation package's own templates, so the config
	// block and the derivation cannot drift apart.
	def := placementtags.DefaultSettings()
	pairs := []struct {
		axis string
		got  string
		def  string
	}{
		{"compute", p.Compute.Template, def.Compute.Template},
		{"peer", p.Peer.Template, def.Peer.Template},
		{"backend", p.Backend.Template, def.Backend.Template},
		{"lane", p.Lane.Template, def.Lane.Template},
		{"role", p.Role.Template, def.Role.Template},
		{"project", p.Project.Template, def.Project.Template},
		{"automaton", p.Automaton.Template, def.Automaton.Template},
		{"automation", p.Automation.Template, def.Automation.Template},
		{"stage", p.Stage.Template, def.Stage.Template},
		{"kind", p.Kind.Template, def.Kind.Template},
	}
	for _, e := range pairs {
		if e.got != e.def {
			t.Errorf("%s template %q does not match the derivation package template %q",
				e.axis, e.got, e.def)
		}
	}
}

func TestPlacementConfigEffectiveFillsUnsetAxes(t *testing.T) {
	got := PlacementConfig{
		Enabled: true,
		Compute: PlacementAxis{Enabled: false, Template: "compute:{node}"},
	}.EffectivePlacement()
	if got.Enabled != true || got.Compute.Enabled != false {
		t.Fatalf("Effective changed a set value: %+v", got)
	}
	if got.Project != (PlacementAxis{Enabled: true, Template: "project:{project}"}) {
		t.Fatalf("Effective did not fill the unset project axis: %+v", got.Project)
	}
	// A never-set block takes all defaults, master switch included.
	zero := (PlacementConfig{}).EffectivePlacement()
	if !reflect.DeepEqual(zero, DefaultPlacementConfig()) {
		t.Fatalf("unset block took %+v, want the defaults", zero)
	}
}

// placementConfigYAML renders the block for a file the tests load.
const placementConfigYAML = `autonomous:
  tags:
    placement:
      enabled: false
      compute:
        enabled: false
        template: "compute:{node}"
      custom:
        - name: "env-9"
          field: "Node"
          template: "env:{value}"
          enabled: true
`

func TestPlacementConfigYAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	placement := DefaultPlacementConfig()
	placement.Role = PlacementAxis{Enabled: true, Template: "owner role:{role}"}
	placement.Custom = []PlacementCustomAxis{
		{Name: "model-9", Field: "Model", Template: "model:{value}", Enabled: true},
		{Name: "env-9", Field: "Node", Template: "env:{value}", Enabled: false},
	}
	cfg.Autonomous.Tags.Placement = placement
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// The saved file must carry the explicit values, including the off master axis.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, want := range []string{
		"compute:{node}",
		"owner role:{role}",
		"name: model-9",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("saved config lacks %q:\n%s", want, raw)
		}
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(back.Autonomous.Tags.Placement, cfg.Autonomous.Tags.Placement) {
		t.Fatalf("placement block did not survive save/load:\n got %+v\nwant %+v",
			back.Autonomous.Tags.Placement, cfg.Autonomous.Tags.Placement)
	}
}

func TestPlacementConfigMasterSwitchOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(placementConfigYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cfg.Autonomous.Tags.Placement
	if p.Enabled {
		t.Fatalf("the master switch must stay off when explicitly false")
	}
	if p.Compute.Enabled {
		t.Fatalf("an explicitly off axis must stay off")
	}
	if got := p.Custom; len(got) != 1 || got[0].Name != "env-9" || got[0].Field != "Node" ||
		got[0].Template != "env:{value}" || !got[0].Enabled {
		t.Fatalf("custom axis = %+v", got)
	}
	// The axes the file did not set keep their defaults.
	if p.Project != (PlacementAxis{Enabled: true, Template: "project:{project}"}) {
		t.Fatalf("unset project axis reloaded as %+v", p.Project)
	}
	if p.Lane != (PlacementAxis{Enabled: true, Template: "lane:{lane}"}) {
		t.Fatalf("unset lane axis reloaded as %+v, want the default", p.Lane)
	}
}

// TestPlacementConfigLoadRefusesInvalidValues drives each validation refusal
// through a real Load: an invalid block must be refused before it is stored.
func TestPlacementConfigLoadRefusesInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "compute template without placeholder",
			yaml: `    placement:
      compute:
        enabled: true
        template: "node-"
`,
			want: `autonomous.tags.placement.compute: template "node-" must contain {node}`,
		},
		{
			name: "project template without placeholder",
			yaml: `    placement:
      project:
        enabled: true
        template: "{role}"
`,
			want: `autonomous.tags.placement.project: template "{role}" must contain {project}`,
		},
		{
			name: "custom name with uppercase and underscore",
			yaml: `    placement:
      custom:
        - name: "Env_9"
          field: "Node"
          template: "env:{value}"
`,
			want: `autonomous.tags.placement.custom: name "Env_9" must be lowercase letters, digits and hyphens`,
		},
		{
			name: "custom empty name",
			yaml: `    placement:
      custom:
        - name: ""
          field: "Node"
          template: "env:{value}"
`,
			want: `autonomous.tags.placement.custom: name "" must be lowercase letters, digits and hyphens`,
		},
		{
			name: "custom name repeated",
			yaml: `    placement:
      custom:
        - name: "env-9"
          field: "Node"
          template: "a:{value}"
        - name: "env-9"
          field: "Prdid"
          template: "b:{value}"
`,
			want: `autonomous.tags.placement.custom: name "env-9" is used more than once`,
		},
		{
			name: "custom name collides with a built-in axis",
			yaml: `    placement:
      custom:
        - name: "lane"
          field: "Node"
          template: "env:{value}"
`,
			want: `autonomous.tags.placement.custom: name "lane" collides with a built-in placement axis`,
		},
		{
			name: "custom field outside the allowed list",
			yaml: `    placement:
      custom:
        - name: "env-9"
          field: "WorkDir"
          template: "env:{value}"
`,
			want: `autonomous.tags.placement.custom: field "WorkDir" must be one of Name, Backend, Model, Node, PRDID, Role, Project`,
		},
		{
			name: "custom field the right name in the wrong case",
			yaml: `    placement:
      custom:
        - name: "env-9"
          field: "prdid"
          template: "env:{value}"
`,
			want: `autonomous.tags.placement.custom: field "prdid" must be one of Name, Backend, Model, Node, PRDID, Role, Project`,
		},
		{
			name: "custom template without the value placeholder",
			yaml: `    placement:
      custom:
        - name: "env-9"
          field: "Node"
          template: "env-{node}"
`,
			want: `autonomous.tags.placement.custom: template "env-{node}" must contain {value}`,
		},
		{
			name: "invalid values are refused while the master switch is off too",
			yaml: `    placement:
      enabled: false
      custom:
        - name: "Env"
          field: "Node"
          template: "env:{value}"
`,
			want: `autonomous.tags.placement.custom: name "Env" must be lowercase letters, digits and hyphens`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("autonomous:\n  tags:\n"+tc.yaml), 0600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want the refusal %q", err, tc.want)
			}
		})
	}
}

func TestPlacementConfigValidateAcceptsValidBlock(t *testing.T) {
	p := DefaultPlacementConfig()
	p.Custom = []PlacementCustomAxis{
		{Name: "model-9", Field: "Model", Template: "model:{value}", Enabled: true},
		{Name: "prd-9", Field: "PRDID", Template: "prd:{value}", Enabled: false},
		{Name: "a", Field: "Name", Template: "name:{value}", Enabled: true},
	}
	if err := validatePlacement(p); err != nil {
		t.Fatalf("validatePlacement refused a valid block: %v", err)
	}
}

func TestPlacementCustomNameOK(t *testing.T) {
	for _, ok := range []string{"env-9", "a", "9", "ci-stage-2"} {
		if !placementCustomNameOK(ok) {
			t.Errorf("placementCustomNameOK(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Env", "env_9", "env 9", "énv"} {
		if placementCustomNameOK(bad) {
			t.Errorf("placementCustomNameOK(%q) = true, want false", bad)
		}
	}
}

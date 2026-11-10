package packs

import (
	"strings"
	"testing"
)

const validManifest = `
name: datawatch-standard-tasks
version: 1.2.0
description: Standard tasks for datawatch Automatons
kinds: [feature, bug]
tasks:
  - title: Review the change
    prompt: prompts/review.md
    verification_script: verify/review.sh
    default_tier: small
    read_only: true
    report_path: reports/review.md
    tools: [Read, Grep]
    file_scopes: [internal/**]
  - title: Update the docs
    prompt: prompts/docs.md
    read_only: false
templates:
  - feature-automaton.yaml
  - bug-automaton.yaml
inputs:
  - name: target_branch
    type: string
    default: main
    required: true
    secret: false
  - name: api_token
    type: string
    required: false
    secret: true
`

func TestParseManifestValid(t *testing.T) {
	p, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: unexpected error: %v", err)
	}
	if p.Name != "datawatch-standard-tasks" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Version != "1.2.0" {
		t.Errorf("Version = %q", p.Version)
	}
	if p.Description != "Standard tasks for datawatch Automatons" {
		t.Errorf("Description = %q", p.Description)
	}
	if len(p.Kinds) != 2 || p.Kinds[0] != KindFeature || p.Kinds[1] != KindBug {
		t.Errorf("Kinds = %v", p.Kinds)
	}
	if len(p.Tasks) != 2 {
		t.Fatalf("len(Tasks) = %d, want 2", len(p.Tasks))
	}
	task := p.Tasks[0]
	if task.Title != "Review the change" || task.Prompt != "prompts/review.md" ||
		task.VerificationScript != "verify/review.sh" || task.DefaultTier != "small" ||
		!task.ReadOnly || task.ReportPath != "reports/review.md" {
		t.Errorf("task[0] scalar fields = %+v", task)
	}
	if len(task.Tools) != 2 || task.Tools[0] != "Read" || task.Tools[1] != "Grep" {
		t.Errorf("task[0].Tools = %v", task.Tools)
	}
	if len(task.FileScopes) != 1 || task.FileScopes[0] != "internal/**" {
		t.Errorf("task[0].FileScopes = %v", task.FileScopes)
	}
	if p.Tasks[1].ReadOnly {
		t.Errorf("task[1].ReadOnly = true, want false")
	}
	if len(p.Templates) != 2 || p.Templates[0] != "feature-automaton.yaml" {
		t.Errorf("Templates = %v", p.Templates)
	}
	if len(p.Inputs) != 2 {
		t.Fatalf("len(Inputs) = %d, want 2", len(p.Inputs))
	}
	in := p.Inputs[0]
	if in.Name != "target_branch" || in.Type != "string" || in.Default != "main" || !in.Required || in.Secret {
		t.Errorf("inputs[0] = %+v", in)
	}
	if !p.Inputs[1].Secret || p.Inputs[1].Required {
		t.Errorf("inputs[1] = %+v", p.Inputs[1])
	}
}

func TestParseManifestMinimalAndTrimmed(t *testing.T) {
	p, err := ParseManifest([]byte("name: \"  padded  \"\nversion: \" 0.1.0-rc.1+build.5 \"\n"))
	if err != nil {
		t.Fatalf("ParseManifest: unexpected error: %v", err)
	}
	if p.Name != "padded" {
		t.Errorf("Name = %q, want trimmed", p.Name)
	}
	if p.Version != "0.1.0-rc.1+build.5" {
		t.Errorf("Version = %q, want trimmed", p.Version)
	}
	if len(p.Kinds) != 0 || len(p.Tasks) != 0 {
		t.Errorf("expected empty kinds and tasks, got %v / %v", p.Kinds, p.Tasks)
	}
}

func TestParseManifestErrors(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "missing name",
			yaml:    "version: 1.0.0\n",
			wantErr: "name is required",
		},
		{
			name:    "blank name",
			yaml:    "name: \"   \"\nversion: 1.0.0\n",
			wantErr: "name is required",
		},
		{
			name:    "missing version",
			yaml:    "name: pack\n",
			wantErr: "version is required",
		},
		{
			name:    "blank version",
			yaml:    "name: pack\nversion: \" \"\n",
			wantErr: "version is required",
		},
		{
			name:    "version not semver",
			yaml:    "name: pack\nversion: 1.0\n",
			wantErr: "is not a semantic version",
		},
		{
			name:    "unknown kind",
			yaml:    "name: pack\nversion: 1.0.0\nkinds: [feature, wizard]\n",
			wantErr: `unknown kind "wizard"`,
		},
		{
			name:    "duplicate task title",
			yaml:    "name: pack\nversion: 1.0.0\ntasks:\n  - title: Same\n  - title: Same\n",
			wantErr: `duplicate task title "Same"`,
		},
		{
			name:    "duplicate task title after trimming",
			yaml:    "name: pack\nversion: 1.0.0\ntasks:\n  - title: Same\n  - title: \" Same \"\n",
			wantErr: `duplicate task title "Same"`,
		},
		{
			name:    "malformed yaml",
			yaml:    "name: [unclosed\n",
			wantErr: "pack manifest:",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseManifest([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("ParseManifest: expected error containing %q, got pack %+v", tc.wantErr, p)
			}
			if p != nil {
				t.Errorf("ParseManifest: returned pack alongside error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParseManifestAcceptsEachKind(t *testing.T) {
	for _, k := range []string{KindFeature, KindBug, KindOther, KindMaintenance} {
		if _, err := ParseManifest([]byte("name: pack\nversion: 1.0.0\nkinds: [" + k + "]\n")); err != nil {
			t.Errorf("kind %q: unexpected error: %v", k, err)
		}
	}
}

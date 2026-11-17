package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestClaudeExplicitModelPolicy(t *testing.T) {
	const (
		regularType   = config.TaskTypeDaemonBackendCode
		mechanicalTyp = config.TaskTypeDocsSet
	)
	cases := []struct {
		name       string
		namedModel string
		taskType   string
		wantModel  string
		wantSource string
	}{
		{
			name:       "operator-named sonnet passes through on a regular type",
			namedModel: "sonnet",
			taskType:   regularType,
			wantModel:  "sonnet",
			wantSource: ModelSourceOperator,
		},
		{
			name:       "operator-named premium model passes through unchanged",
			namedModel: "claude-opus-4-1",
			taskType:   regularType,
			wantModel:  "claude-opus-4-1",
			wantSource: ModelSourceOperator,
		},
		{
			name:       "operator-named premium model passes through on a mechanical type",
			namedModel: "fable-max",
			taskType:   mechanicalTyp,
			wantModel:  "fable-max",
			wantSource: ModelSourceOperator,
		},
		{
			name:       "operator-named custom model passes through byte-for-byte",
			namedModel: "ollama/qwen3.8:27b",
			taskType:   regularType,
			wantModel:  "ollama/qwen3.8:27b",
			wantSource: ModelSourceOperator,
		},
		{
			name:       "named model with surrounding whitespace passes through byte-for-byte",
			namedModel: "  gpt-5  ",
			taskType:   regularType,
			wantModel:  "  gpt-5  ",
			wantSource: ModelSourceOperator,
		},
		{
			name:       "empty named model on a regular type resolves to sonnet",
			namedModel: "",
			taskType:   regularType,
			wantModel:  DefaultClaudeModel,
			wantSource: ModelSourceDaemonDefault,
		},
		{
			name:       "empty named model on a mechanical type resolves to haiku",
			namedModel: "",
			taskType:   mechanicalTyp,
			wantModel:  MechanicalClaudeModel,
			wantSource: ModelSourceDaemonDefault,
		},
		{
			name:       "empty named model with an unknown task type resolves to sonnet",
			namedModel: "",
			taskType:   "",
			wantModel:  DefaultClaudeModel,
			wantSource: ModelSourceDaemonDefault,
		},
		{
			name:       "whitespace-only named model is treated as empty on a regular type",
			namedModel: " \t\n  ",
			taskType:   regularType,
			wantModel:  DefaultClaudeModel,
			wantSource: ModelSourceDaemonDefault,
		},
		{
			name:       "whitespace-only named model is treated as empty on a mechanical type",
			namedModel: " \t\n  ",
			taskType:   mechanicalTyp,
			wantModel:  MechanicalClaudeModel,
			wantSource: ModelSourceDaemonDefault,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, source := ResolveClaudeModel(tc.namedModel, tc.taskType)
			if model != tc.wantModel {
				t.Errorf("model = %q, want %q", model, tc.wantModel)
			}
			if source != tc.wantSource {
				t.Errorf("source = %q, want %q", source, tc.wantSource)
			}
		})
	}

	t.Run("premium detection is case-insensitive and name-based", func(t *testing.T) {
		for _, m := range []string{"opus", "Claude-Opus-4-1", "FABLE-7", "claude-fable-mini"} {
			if !IsPremiumModel(m) {
				t.Errorf("IsPremiumModel(%q) = false, want true", m)
			}
		}
		for _, m := range []string{"sonnet", "haiku", "gpt-5", ""} {
			if IsPremiumModel(m) {
				t.Errorf("IsPremiumModel(%q) = true, want false", m)
			}
		}
	})

	t.Run("daemon defaults are never premium", func(t *testing.T) {
		if IsPremiumModel(DefaultClaudeModel) || IsPremiumModel(MechanicalClaudeModel) {
			t.Errorf("a daemon default model is premium: %q / %q", DefaultClaudeModel, MechanicalClaudeModel)
		}
	})
}

func TestClaudeExplicitModelMechanical(t *testing.T) {
	wantMechanical := []string{
		config.TaskTypeDocsSet,
		config.TaskTypeChangelogEntry,
		config.TaskTypeParityIssueDraft,
		config.TaskTypeBoundedLoadStep,
		config.TaskTypeVersionNote,
		config.TaskTypeLocaleStrings,
		config.TaskTypeFormattingCleanup,
		config.TaskTypeTests,
	}
	wantCode := []string{
		config.TaskTypeUICode,
		config.TaskTypeDaemonBackendCode,
		config.TaskTypeMCPAPISurface,
		config.TaskTypeMergeConflictRepair,
		config.TaskTypeReviewRepair,
		config.TaskTypeFinalGate,
	}

	// Every mechanical constant is in the mechanical set (haiku row).
	for _, tt := range wantMechanical {
		if !IsMechanicalTaskType(tt) {
			t.Errorf("IsMechanicalTaskType(%q) = false, want true (haiku row)", tt)
		}
	}
	// Code/verification/judge-style constants are not (sonnet row).
	for _, tt := range wantCode {
		if IsMechanicalTaskType(tt) {
			t.Errorf("IsMechanicalTaskType(%q) = true, want false (sonnet row)", tt)
		}
	}
	// The split covers exactly the full default task-routing task-type list,
	// with no type left in or out of both sets.
	all := config.TaskRoutingTaskTypes()
	var covered []string
	for _, tt := range all {
		wantIn := false
		for _, m := range wantMechanical {
			if tt == m {
				wantIn = true
			}
		}
		if got := IsMechanicalTaskType(tt); got != wantIn {
			covered = append(covered, tt)
			t.Errorf("type %q: mechanical=%v, want %v", tt, got, wantIn)
		}
	}
	if len(covered) > 0 {
		t.Fatalf("task types mis-split: %v", covered)
	}
	if len(wantMechanical)+len(wantCode) != len(all) {
		t.Fatalf("expected split covers %d types, routing list has %d", len(wantMechanical)+len(wantCode), len(all))
	}
	// The mechanical set must not have any type outside the routing list.
	for _, tt := range wantMechanical {
		if !config.IsTaskRoutingTaskType(tt) {
			t.Errorf("mechanical type %q is not a known routing task type", tt)
		}
	}
}

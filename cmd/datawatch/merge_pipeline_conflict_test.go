package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// mergePipelineConflictView builds a show body whose conflict_confirmation
// block is the given JSON fragment.
func mergePipelineConflictView(block string) string {
	return `{"project":"demo","definition":{"steps":[{"name":"run tests","kind":"test","command":"make test"}]},"source":"default","settings":{"file_name":".datawatch/merge-pipeline.yaml"},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"","conflict_model_in_effect":"","conflict_confirmation":` + block + `,"overrides":{}}`
}

const mergePipelineConflictLinePrefix = "Conflict confirmation: "

// TestMergePipelineConflict_PrintsLineWhenEnabled: with mode on and a
// confirmation count above zero, show prints the confirmation line with the
// mode, N in effect, counted and remaining.
func TestMergePipelineConflict_PrintsLineWhenEnabled(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineConflictView(
		`{"mode":"on","confirm_first_n":3,"counted":1,"remaining":2,"enabled":true}`))
	out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"show", "--project", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	want := "Conflict confirmation: mode on, the first 3 model-resolved conflicts need confirmation, counted 1, remaining 2\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	if strings.Count(out, mergePipelineConflictLinePrefix) != 1 {
		t.Errorf("want exactly one confirmation line:\n%s", out)
	}
}

// TestMergePipelineConflict_NothingPrintedWhenOff: with mode shadow or a
// confirm count of zero, show prints no confirmation line at all.
func TestMergePipelineConflict_NothingPrintedWhenOff(t *testing.T) {
	for name, block := range map[string]string{
		"shadow mode":      `{"mode":"shadow","confirm_first_n":3,"counted":0,"remaining":3,"enabled":false}`,
		"zero confirm N":   `{"mode":"on","confirm_first_n":0,"counted":0,"remaining":0,"enabled":false}`,
		"mode on, no data": `{"mode":"on","confirm_first_n":0,"counted":null,"remaining":null,"enabled":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			newLaneFillCLIDaemon(t, http.StatusOK, mergePipelineConflictView(block))
			out, err := runLaneFillCmd(t, newAutonomousMergePipelineCmd(), []string{"show", "--project", "demo"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, mergePipelineConflictLinePrefix) {
				t.Errorf("confirmation line printed while off:\n%s", out)
			}
		})
	}
}

// TestMergePipelineConflict_UnreadableCounter: an enabled confirmation whose
// counter could not be read prints unknown counts, never a zero.
func TestMergePipelineConflict_UnreadableCounter(t *testing.T) {
	line := mergePipelineConflictConfirmLine(mergepipeline.ConflictConfirmationView{
		Mode: "on", ConfirmFirstN: 2, Enabled: true, Error: "conflict counter unreadable: boom",
	})
	want := "Conflict confirmation: mode on, the first 2 model-resolved conflicts need confirmation, counted unknown, remaining unknown\n"
	if line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
}

// TestMergePipelineConflict_HelperEmptyWhenDisabled: the helper itself returns
// the empty string for every disabled block, whatever the counts say.
func TestMergePipelineConflict_HelperEmptyWhenDisabled(t *testing.T) {
	zero := 0
	if got := mergePipelineConflictConfirmLine(mergepipeline.ConflictConfirmationView{
		Mode: "shadow", ConfirmFirstN: 3, Counted: &zero, Remaining: &zero, Enabled: false,
	}); got != "" {
		t.Errorf("disabled block printed %q", got)
	}
}

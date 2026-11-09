package router

// The conflict settings of the merge pipeline in the chat channel: configure
// accepts conflict_mode and conflict_confirm_first_n, globally and for one
// project, and refuses a bad value with a message that names the key before
// anything is written.

import (
	"strings"
	"testing"
)

func TestMergePipelineCommConflict(t *testing.T) {
	valid := map[string]string{
		"autonomous.merge_pipeline.conflict_mode":                          "on",
		"autonomous.merge_pipeline.conflict_confirm_first_n":               "3",
		"autonomous.merge_pipeline.projects.demo.conflict_mode":            "shadow",
		"autonomous.merge_pipeline.projects.demo.conflict_confirm_first_n": "0",
	}
	for key, value := range valid {
		h := newReleaseCommHarness()
		reply := h.say(key + "=" + value)
		if !strings.Contains(reply, "Set "+key+" = "+value) {
			t.Errorf("%s=%s: reply lacks the confirmation: %q", key, value, reply)
		}
		if h.got[key] != value {
			t.Errorf("%s: configure got %q, want %q", key, h.got[key], value)
		}
	}

	h := newReleaseCommHarness()
	for _, c := range []struct{ key, value string }{
		{"autonomous.merge_pipeline.conflict_mode", "auto"},
		{"autonomous.merge_pipeline.projects.demo.conflict_mode", "yes"},
		{"autonomous.merge_pipeline.conflict_confirm_first_n", "-1"},
		{"autonomous.merge_pipeline.conflict_confirm_first_n", "many"},
	} {
		reply := h.say(c.key + "=" + c.value)
		if !strings.Contains(reply, "Configure failed") || !strings.Contains(reply, c.key) {
			t.Errorf("%s=%s: refusal must name the key: %q", c.key, c.value, reply)
		}
		if _, wrote := h.got[c.key]; wrote {
			t.Errorf("%s=%s: a refused value must not be written", c.key, c.value)
		}
	}
}

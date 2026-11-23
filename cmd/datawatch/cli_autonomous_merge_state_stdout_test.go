// Checks that prd-list and prd-get keep standard output a single JSON
// document when an Automaton carries the read-only merge_state field: the
// plain-text "branch not merged" note belongs on standard error.
package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const mergeStateFieldJSON = `"merge_state":"finished_not_merged"`

func TestPRDListGet_MergeStateNoteGoesToStderr(t *testing.T) {
	cases := []struct {
		name     string
		newCmd   func() *cobra.Command
		args     []string
		body     string
		wantNote string
	}{
		{
			name:     "list",
			newCmd:   newAutonomousPRDListCmd,
			body:     `{"prds":[{"id":"a1","title":"Sample Automaton",` + mergeStateFieldJSON + `}]}`,
			wantNote: "branch not merged",
		},
		{
			name:     "get",
			newCmd:   newAutonomousPRDGetCmd,
			args:     []string{"a1"},
			body:     `{"id":"a1","title":"Sample Automaton",` + mergeStateFieldJSON + `}`,
			wantNote: "branch work not merged into the base branch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCaptureServer(t, &captureDaemon{replyBody: tc.body})
			stdout, stderr := runCapturingStreams(t, tc.newCmd(), tc.args)

			assertSingleJSONValue(t, stdout)
			if !strings.Contains(stderr, tc.wantNote) {
				t.Errorf("standard error lacks %q:\n%s", tc.wantNote, stderr)
			}
			if strings.Contains(stdout, "branch not merged") {
				t.Errorf("standard output carries the merge state note:\n%s", stdout)
			}
		})
	}
}

func TestPRDListGet_NoMergeStateStdoutIsOneDocument(t *testing.T) {
	cases := []struct {
		name   string
		newCmd func() *cobra.Command
		args   []string
		body   string
	}{
		{"list", newAutonomousPRDListCmd, nil, `{"prds":[{"id":"a1","title":"Sample Automaton"}]}`},
		{"get", newAutonomousPRDGetCmd, []string{"a1"}, `{"id":"a1","title":"Sample Automaton"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCaptureServer(t, &captureDaemon{replyBody: tc.body})
			stdout, stderr := runCapturingStreams(t, tc.newCmd(), tc.args)

			assertSingleJSONValue(t, stdout)
			if strings.Contains(stderr, "branch not merged") || strings.Contains(stderr, "merge_state") {
				t.Errorf("no merge_state field, yet the note appears:\n%s", stderr)
			}
		})
	}
}

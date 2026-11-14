// Checks that prd-list and prd-get keep standard output a single JSON
// document when an Automaton carries a planning failure: the plain-text
// planning failure notes belong on standard error.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const planningFailureJSON = `"planning_failure":{"reason":"decompose produced no tasks","category":"empty_plan","source":"decompose-lint","phrases":["no tasks"],"retryable":true,"retry_counts":{"empty_plan":1},"retry_budget":3,"occurred_at":"2026-01-02T03:04:05Z"}`

// runCapturingStreams runs cmd and returns what it wrote to standard output
// and standard error, whether through the cobra writers or os.Stdout/os.Stderr.
func runCapturingStreams(t *testing.T, cmd *cobra.Command, args []string) (string, string) {
	t.Helper()
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs(args)

	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	doneOut, doneErr := make(chan string), make(chan string)
	go func() { b, _ := io.ReadAll(rOut); doneOut <- string(b) }()
	go func() { b, _ := io.ReadAll(rErr); doneErr <- string(b) }()

	execErr := cmd.Execute()

	os.Stdout, os.Stderr = oldOut, oldErr
	_ = wOut.Close()
	_ = wErr.Close()
	pipedOut, pipedErr := <-doneOut, <-doneErr
	if execErr != nil {
		t.Fatalf("command failed: %v", execErr)
	}
	return outBuf.String() + pipedOut, errBuf.String() + pipedErr
}

// assertSingleJSONValue fails unless s holds exactly one JSON value.
func assertSingleJSONValue(t *testing.T, s string) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("standard output is not JSON: %v\n%s", err, s)
	}
	if dec.More() {
		t.Fatalf("standard output has more than one JSON value:\n%s", s)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("second Decode = %v, want io.EOF; standard output:\n%s", err, s)
	}
}

func TestPRDListGet_PlanningFailureNotesGoToStderr(t *testing.T) {
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
			body:     `{"prds":[{"id":"a1","title":"Sample Automaton",` + planningFailureJSON + `}]}`,
			wantNote: "planning failed",
		},
		{
			name:     "get",
			newCmd:   newAutonomousPRDGetCmd,
			args:     []string{"a1"},
			body:     `{"id":"a1","title":"Sample Automaton",` + planningFailureJSON + `}`,
			wantNote: "Planning failed",
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
			if strings.Contains(strings.ToLower(stdout), "planning failed") {
				t.Errorf("standard output carries the planning failure note:\n%s", stdout)
			}
		})
	}
}

func TestPRDListGet_NoPlanningFailureStdoutIsOneDocument(t *testing.T) {
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
			if stderr != "" {
				t.Errorf("standard error should be empty, got:\n%s", stderr)
			}
		})
	}
}

package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func TestNeedsTUITaskDelivery(t *testing.T) {
	cases := []struct {
		name  string
		sess  session.Session
		want  bool
	}{
		{
			name: "pi-prompt one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "pi-prompt"},
			want: false,
		},
		{
			name: "goose-prompt one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "goose-prompt"},
			want: false,
		},
		{
			name: "aider one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "aider"},
			want: false,
		},
		{
			name: "opencode-acp one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "opencode-acp"},
			want: false,
		},
		{
			name: "goose one-shot waiting_input with task returns true",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "goose"},
			want: true,
		},
		{
			name: "not one-shot returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: false, Task: "do the thing", BackendFamily: "goose"},
			want: false,
		},
		{
			name: "empty task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "", BackendFamily: "goose"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := tc.sess
			if got := needsTUITaskDelivery(&sess); got != tc.want {
				t.Fatalf("needsTUITaskDelivery() = %v, want %v", got, tc.want)
			}
		})
	}
}

package main

import "testing"

func TestPrdGetPushStatusLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "pushed",
			in:   `{"id":"abcd1234","git":{"branch":"automaton/abcd1234","push_state":"pushed","pushed_ref":"refs/heads/automaton/abcd1234","pushed_sha":"0123456789abcdef","pushed_at":"2026-11-01T12:00:00Z","push_warning":""}}`,
			want: "push: pushed refs/heads/automaton/abcd1234@0123456",
		},
		{
			name: "pushed with short sha value",
			in:   `{"id":"abcd1234","git":{"push_state":"pushed","pushed_ref":"main","pushed_sha":"abcde"}}`,
			want: "push: pushed main@abcde",
		},
		{
			name: "not pushed",
			in:   `{"id":"abcd1234","git":{"branch":"automaton/abcd1234","push_state":"not_pushed","push_warning":"no origin remote"}}`,
			want: "push: NOT PUSHED — no origin remote",
		},
		{
			name: "nothing attempted",
			in:   `{"id":"abcd1234","spec":"a spec","status":"running","git":{"branch":"automaton/abcd1234"}}`,
			want: "",
		},
		{
			name: "no git object at all",
			in:   `{"id":"abcd1234","spec":"a spec"}`,
			want: "",
		},
		{
			name: "summary view not pushed",
			in:   `{"id":"abcd1234","push_state":"not_pushed","push_warning":"push refused by daemon policy"}`,
			want: "push: NOT PUSHED — push refused by daemon policy",
		},
		{
			name: "summary view pushed",
			in:   `{"id":"abcd1234","push_state":"pushed"}`,
			want: "push: pushed @",
		},
		{
			name: "invalid json",
			in:   `this is not json`,
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := prdPushLine([]byte(tc.in)); got != tc.want {
				t.Errorf("prdPushLine = %q, want %q", got, tc.want)
			}
		})
	}
}

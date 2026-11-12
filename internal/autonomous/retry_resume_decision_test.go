package autonomous

import "testing"

func TestDecideRetryResume(t *testing.T) {
	const limit = 100000

	cases := []struct {
		name       string
		in         retryResumeInput
		wantResume bool
		wantReason string
	}{
		{
			name: "alive, supported, under limit resumes",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: true,
				ContextTokens: 5000, MaxContextTokens: limit,
			},
			wantResume: true,
			wantReason: "resumed previous session",
		},
		{
			name: "unknown context (0) resumes",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: true,
				ContextTokens: 0, MaxContextTokens: limit,
			},
			wantResume: true,
			wantReason: "resumed previous session",
		},
		{
			name: "context exactly at limit resumes",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: true,
				ContextTokens: limit, MaxContextTokens: limit,
			},
			wantResume: true,
			wantReason: "resumed previous session",
		},
		{
			name: "context one token over limit does not resume",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: true,
				ContextTokens: limit + 1, MaxContextTokens: limit,
			},
			wantResume: false,
			wantReason: "context above the limit; starting a fresh attempt",
		},
		{
			name: "session ended does not resume",
			in: retryResumeInput{
				SessionAlive: false, BackendSupportsResume: true,
				ContextTokens: 5000, MaxContextTokens: limit,
			},
			wantResume: false,
			wantReason: "session ended; starting a fresh attempt",
		},
		{
			name: "backend cannot resume does not resume",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: false,
				ContextTokens: 5000, MaxContextTokens: limit,
			},
			wantResume: false,
			wantReason: "backend cannot resume; starting a fresh attempt",
		},
		{
			name: "max context 0 disables resume by setting",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: true,
				ContextTokens: 5000, MaxContextTokens: 0,
			},
			wantResume: false,
			wantReason: "resume disabled by setting; starting a fresh attempt",
		},
		{
			name: "negative max context disables resume by setting",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: true,
				ContextTokens: 0, MaxContextTokens: -1,
			},
			wantResume: false,
			wantReason: "resume disabled by setting; starting a fresh attempt",
		},
		{
			name: "session ended takes precedence over backend and setting",
			in: retryResumeInput{
				SessionAlive: false, BackendSupportsResume: false,
				ContextTokens: 1, MaxContextTokens: 0,
			},
			wantResume: false,
			wantReason: "session ended; starting a fresh attempt",
		},
		{
			name: "backend unsupported takes precedence over setting",
			in: retryResumeInput{
				SessionAlive: true, BackendSupportsResume: false,
				ContextTokens: 1, MaxContextTokens: 0,
			},
			wantResume: false,
			wantReason: "backend cannot resume; starting a fresh attempt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotResume, gotReason := decideRetryResume(tc.in)
			if gotResume != tc.wantResume {
				t.Errorf("resume = %v, want %v", gotResume, tc.wantResume)
			}
			if gotReason != tc.wantReason {
				t.Errorf("reason = %q, want %q", gotReason, tc.wantReason)
			}
		})
	}
}

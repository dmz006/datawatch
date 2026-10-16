package session

import "testing"

func TestExtractCompletionSummary(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain line",
			in:   "some output\nDATAWATCH_COMPLETE: fixed the bug\ntrailing noise",
			want: "fixed the bug",
		},
		{
			name: "ansi wrapped line",
			in:   "DATAWATCH_COMPLETE: \x1b[1mcolored\x1b[0m summary done\rstuff",
			want: "colored summary done",
		},
		{
			name: "box prefixed line",
			in:   "header\n│ DATAWATCH_COMPLETE: shipped the feature\n",
			want: "shipped the feature",
		},
		{
			name: "placeholder only output",
			in:   "DATAWATCH_COMPLETE: <one-sentence summary of what was done>\n",
			want: "",
		},
		{
			name: "empty marker",
			in:   "DATAWATCH_COMPLETE:\n",
			want: "",
		},
		{
			name: "two markers last real one wins",
			in:   "DATAWATCH_COMPLETE: first summary\nDATAWATCH_COMPLETE: <one-sentence summary of what was done>\nDATAWATCH_COMPLETE: second summary",
			want: "second summary",
		},
		{
			name: "overlong summary truncated",
			in:   "DATAWATCH_COMPLETE: " + makeLongString(300),
			want: makeLongString(200),
		},
		{
			name: "no marker",
			in:   "just some\nplain output here",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCompletionSummary(tt.in)
			if got != tt.want {
				t.Fatalf("ExtractCompletionSummary(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func makeLongString(n int) string {
	b := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		b = append(b, 'x')
	}
	return string(b)
}

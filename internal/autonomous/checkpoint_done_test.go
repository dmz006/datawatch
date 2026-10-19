package autonomous

import "testing"

func TestCheckpointSaysAllDone(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "all steps completed with remaining none",
			text: "# Checkpoint: retry hint parser\n" +
				"Timestamp: 2026-10-19T09:10:00Z\n\n" +
				"## Steps completed\n" +
				"1. Read relocateCheckpoint\n" +
				"2. Added the parser\n" +
				"3. Added the tests\n\n" +
				"## Current step\n" +
				"none\n\n" +
				"## Remaining\n" +
				"none\n",
			want: true,
		},
		{
			name: "remaining lists steps 6 and 7",
			text: "## Steps completed\n" +
				"- Steps 1 to 5\n\n" +
				"## Current step\n" +
				"none\n\n" +
				"## Remaining\n" +
				"- Step 6: wire the verify hook\n" +
				"- Step 7: update the docs\n",
			want: false,
		},
		{
			name: "current step still open with remaining empty",
			text: "## Steps completed\n" +
				"- Step 1\n" +
				"- Step 2\n\n" +
				"## Current step\n" +
				"Step 3: rewrite the retry hint\n\n" +
				"## Remaining\n",
			want: false,
		},
		{
			name: "empty string",
			text: "",
			want: false,
		},
		{
			name: "remaining heading absent but text says all steps done",
			text: "Timestamp 2026-10-19 09:40. Task: add the checkpoint parser.\n" +
				"Steps completed: 1 through 6.\n" +
				"All steps done; the tests pass.\n",
			want: true,
		},
		{
			name: "mixed case remaining steps heading",
			text: "### Remaining Steps\n\n- None\n",
			want: true,
		},
		{
			name: "bold uppercase remaining heading with checkbox nothing",
			text: "**REMAINING steps**:\n- [x] nothing (all done)\n",
			want: true,
		},
		{
			name: "current step still open wording overrides done remaining",
			text: "## Current step\n" +
				"Step 4 still open: waiting on the test run\n\n" +
				"## Remaining\n" +
				"none\n",
			want: false,
		},
		{
			name: "current step has unfinished work without open wording",
			text: "## Current step\n" +
				"Step 5: write the release notes\n\n" +
				"## Remaining\n" +
				"none\n",
			want: false,
		},
		{
			name: "inline remaining none",
			text: "Steps completed: 1-4.\nRemaining: none\n",
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkpointSaysAllDone(tc.text); got != tc.want {
				t.Errorf("checkpointSaysAllDone() = %v, want %v", got, tc.want)
			}
		})
	}
}

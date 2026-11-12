package autonomous

import "testing"

func TestTaskAttemptKind(t *testing.T) {
	cases := []struct {
		name string
		task *Task
		want string
	}{
		{"final gate", &Task{Kind: TaskKindFinalGate, FilesPlanned: []string{"a.go"}}, attemptKindFinalGate},
		{"final gate beats read only", &Task{Kind: TaskKindFinalGate, ReadOnly: true}, attemptKindFinalGate},
		{"read only no files", &Task{ReadOnly: true}, attemptKindNote},
		{"read only with files", &Task{ReadOnly: true, FilesPlanned: []string{"a.go"}}, attemptKindNote},
		{"empty files", &Task{}, attemptKindNote},
		{"nil task", nil, attemptKindNote},
		{"markdown only", &Task{FilesPlanned: []string{"README.md", "notes/x.MD"}}, attemptKindDocs},
		{"under docs dir", &Task{FilesPlanned: []string{"docs/howto/x.html", "docs/img/a.png"}}, attemptKindDocs},
		{"nested docs dir", &Task{FilesPlanned: []string{"pkg/docs/guide.txt"}}, attemptKindDocs},
		{"mixed docs and code", &Task{FilesPlanned: []string{"docs/a.md", "internal/x.go"}}, attemptKindImplementation},
		{"code only", &Task{FilesPlanned: []string{"internal/x.go"}}, attemptKindImplementation},
		{"docs-like name not dir", &Task{FilesPlanned: []string{"internal/docsx/a.go"}}, attemptKindImplementation},
		{"smoke kind is implementation", &Task{Kind: TaskKindSmoke, FilesPlanned: []string{"a.go"}}, attemptKindImplementation},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := taskAttemptKind(c.task); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

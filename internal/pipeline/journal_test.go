package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type journalStarter struct{ state string }

func (s *journalStarter) StartSession(string, string, string) (string, error) { return "sess1", nil }
func (s *journalStarter) GetSessionState(string) string                       { return s.state }

func journalPipeline(t *testing.T) *Pipeline {
	t.Helper()
	p, err := NewPipeline("secret words", t.TempDir(), []*Task{{ID: "t1", Title: "secret words", State: StatePending}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestARestartListsTheInterruptedPipeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipelines.rec")
	j, err := NewJournal(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := NewExecutor(&journalStarter{state: "running"}, "shell")
	e.SetJournal(j)
	p := journalPipeline(t)
	if err := e.Start(p); err != nil {
		t.Fatal(err)
	}
	rs := j.Records()
	if len(rs) != 1 || rs[0].ID != p.ID || rs[0].State != StateRunning || rs[0].EndedAt != nil || rs[0].StartedAt.IsZero() {
		t.Fatalf("records=%+v", rs)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret words") {
		t.Fatalf("content in the journal: %s", data)
	}
	// The daemon stops here. The next one reads the journal.
	next, err := NewJournal(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	rs = next.Records()
	if len(rs) != 1 || !rs[0].Interrupted {
		t.Fatalf("after a restart: %+v", rs)
	}
	if len(NewExecutor(&journalStarter{}, "shell").List()) != 0 {
		t.Fatal("an interrupted pipeline is listed, not resumed")
	}
}

func TestTheJournalRecordsHowAPipelineEnded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipelines.rec")
	j, _ := NewJournal(path, nil)
	e := NewExecutor(&journalStarter{state: "failed"}, "shell")
	e.SetJournal(j)
	if err := e.Start(journalPipeline(t)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		rs := j.Records()
		if len(rs) == 1 && rs[0].EndedAt != nil {
			if rs[0].State != StateFailed || rs[0].Interrupted {
				t.Fatalf("records=%+v", rs)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no end recorded: %+v", rs)
		}
		time.Sleep(50 * time.Millisecond)
	}
	next, _ := NewJournal(path, nil)
	if next.Records()[0].Interrupted {
		t.Fatal("an ended pipeline is not interrupted")
	}
}

func TestAPipelineWhoseStartCannotBeSavedDoesNotStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gone")
	if err := os.WriteFile(dir, []byte("a file, so no folder can be made here"), 0o600); err != nil {
		t.Fatal(err)
	}
	j, _ := NewJournal(filepath.Join(dir, "pipelines.rec"), nil)
	if j == nil {
		t.Fatal("journal")
	}
	starter := &journalStarter{}
	e := NewExecutor(starter, "shell")
	e.SetJournal(j)
	if err := e.Start(journalPipeline(t)); err == nil {
		t.Fatal("started although the start could not be saved")
	}
	if len(e.List()) != 0 {
		t.Fatal("the pipeline was kept")
	}
}

func TestAnUnreadableJournalIsAnErrorAndIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipelines.rec")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJournal(path, nil); err == nil {
		t.Fatal("want an error")
	}
	if data, _ := os.ReadFile(path); string(data) != "not json" {
		t.Fatalf("overwritten: %q", data)
	}
}

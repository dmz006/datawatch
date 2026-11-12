package autonomous

import (
	"sync"
	"testing"
	"time"
)

func snapshotFixture(t *testing.T) (*Store, string) {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Now()
	prd := &PRD{ID: "snap0001", Spec: "s", Status: PRDApproved, CreatedAt: now, Skills: []string{"a"}, RetryPolicy: map[string]int{"x": 1}}
	if err := s.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if err := s.SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do", AttemptRefs: []string{"d"}}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	return s, prd.ID
}

func TestSnapshotPRD_MissReturnsFalse(t *testing.T) {
	s, _ := snapshotFixture(t)
	if p, ok := s.SnapshotPRD("nope"); ok || p != nil {
		t.Fatalf("want (nil,false), got (%v,%v)", p, ok)
	}
}

func TestSnapshotPRD_IsDeep(t *testing.T) {
	s, id := snapshotFixture(t)
	snap, ok := s.SnapshotPRD(id)
	if !ok {
		t.Fatal("snapshot miss")
	}
	snap.Story[0].Title = "changed"
	snap.Story[0].Tasks[0].Title = "changed"
	snap.Story[0].Tasks[0].AttemptRefs[0] = "changed"
	snap.Skills[0] = "changed"
	snap.RetryPolicy["x"] = 99
	snap.Story[0].Tasks = append(snap.Story[0].Tasks, Task{Title: "extra"})

	live, _ := s.GetPRD(id)
	if live.Story[0].Title != "S" || live.Story[0].Tasks[0].Title != "T" ||
		live.Story[0].Tasks[0].AttemptRefs[0] != "d" || live.Skills[0] != "a" ||
		live.RetryPolicy["x"] != 1 || len(live.Story[0].Tasks) != 1 {
		t.Fatalf("mutating the snapshot altered the stored PRD: %+v", live)
	}
}

func TestSnapshotPRD_ReflectsStateAtCallTime(t *testing.T) {
	s, id := snapshotFixture(t)
	before, _ := s.SnapshotPRD(id)
	live, _ := s.GetPRD(id)
	live.Status = PRDRunning
	_ = s.SavePRD(live)
	after, _ := s.SnapshotPRD(id)
	if before.Status != PRDApproved {
		t.Fatalf("earlier snapshot changed: %s", before.Status)
	}
	if after.Status != PRDRunning {
		t.Fatalf("later snapshot stale: %s", after.Status)
	}
}

func TestSnapshotPRD_ConcurrentWithMutation(t *testing.T) {
	s, id := snapshotFixture(t)
	taskID := ""
	live, _ := s.GetPRD(id)
	taskID = live.Story[0].Tasks[0].ID

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			s.MutateTask(taskID, func(tk *Task) {
				tk.Title = "t"
				tk.Status = TaskInProgress
				tk.AttemptRefs = append(tk.AttemptRefs, "x")
			})
		}
		close(stop)
	}()
	for {
		select {
		case <-stop:
			wg.Wait()
			return
		default:
			if p, ok := s.SnapshotPRD(id); ok {
				_ = p.Story[0].Tasks[0].Status
			}
		}
	}
}

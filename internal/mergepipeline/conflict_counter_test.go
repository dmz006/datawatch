package mergepipeline

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConflictCounter(t *testing.T) {
	t.Run("counts are kept per project", func(t *testing.T) {
		c, err := NewConflictCounter(t.TempDir())
		if err != nil {
			t.Fatalf("NewConflictCounter: %v", err)
		}
		if n, err := c.Count("alpha"); err != nil || n != 0 {
			t.Fatalf("fresh Count(alpha) = %d, %v; want 0, nil", n, err)
		}
		for i := 1; i <= 3; i++ {
			if n, err := c.Increment("alpha"); err != nil || n != i {
				t.Fatalf("Increment(alpha) #%d = %d, %v; want %d, nil", i, n, err, i)
			}
		}
		if n, err := c.Increment("beta"); err != nil || n != 1 {
			t.Fatalf("Increment(beta) = %d, %v; want 1, nil", n, err)
		}
		if n, _ := c.Count("alpha"); n != 3 {
			t.Errorf("Count(alpha) = %d; want 3", n)
		}
		if n, _ := c.Count("beta"); n != 1 {
			t.Errorf("Count(beta) = %d; want 1", n)
		}
		if n, _ := c.Count("gamma"); n != 0 {
			t.Errorf("Count(gamma) = %d; want 0", n)
		}
	})

	t.Run("persists across reopening", func(t *testing.T) {
		dir := t.TempDir()
		c, err := NewConflictCounter(dir)
		if err != nil {
			t.Fatalf("NewConflictCounter: %v", err)
		}
		if _, err := c.Increment("alpha"); err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if _, err := c.Increment("alpha"); err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if _, err := c.Increment("beta"); err != nil {
			t.Fatalf("Increment: %v", err)
		}

		reopened, err := NewConflictCounter(dir)
		if err != nil {
			t.Fatalf("reopen NewConflictCounter: %v", err)
		}
		if n, _ := reopened.Count("alpha"); n != 2 {
			t.Errorf("reopened Count(alpha) = %d; want 2", n)
		}
		if n, _ := reopened.Count("beta"); n != 1 {
			t.Errorf("reopened Count(beta) = %d; want 1", n)
		}
		if n, err := reopened.Increment("alpha"); err != nil || n != 3 {
			t.Errorf("reopened Increment(alpha) = %d, %v; want 3, nil", n, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "merge-pipeline", "conflict-counter.json.tmp")); !os.IsNotExist(err) {
			t.Errorf("temporary file left behind after persist: %v", err)
		}
	})

	t.Run("concurrent increments give the exact total", func(t *testing.T) {
		const workers = 50
		const perWorker = 20
		c, err := NewConflictCounter(t.TempDir())
		if err != nil {
			t.Fatalf("NewConflictCounter: %v", err)
		}
		var wg sync.WaitGroup
		errs := make(chan error, workers*perWorker)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perWorker; i++ {
					if _, err := c.Increment("alpha"); err != nil {
						errs <- err
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("Increment under concurrency: %v", err)
		}
		if n, _ := c.Count("alpha"); n != workers*perWorker {
			t.Errorf("Count(alpha) = %d; want %d", n, workers*perWorker)
		}
	})

	t.Run("corrupt file returns an error and is not reset", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "merge-pipeline", "conflict-counter.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		garbage := []byte("{not json")
		if err := os.WriteFile(path, garbage, 0o600); err != nil {
			t.Fatalf("write corrupt file: %v", err)
		}
		if c, err := NewConflictCounter(dir); err == nil {
			t.Fatalf("NewConflictCounter on corrupt file returned nil error (counter %v)", c)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != string(garbage) {
			t.Errorf("corrupt file was modified: %q", got)
		}
	})

	t.Run("negative count in file returns an error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "merge-pipeline", "conflict-counter.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(`{"alpha": -1}`), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if _, err := NewConflictCounter(dir); err == nil {
			t.Fatal("NewConflictCounter accepted a negative count")
		}
	})

	t.Run("empty project name is rejected", func(t *testing.T) {
		c, err := NewConflictCounter(t.TempDir())
		if err != nil {
			t.Fatalf("NewConflictCounter: %v", err)
		}
		if _, err := c.Increment("  "); err == nil {
			t.Error("Increment accepted an empty project name")
		}
		if _, err := c.Count(""); err == nil {
			t.Error("Count accepted an empty project name")
		}
	})
}

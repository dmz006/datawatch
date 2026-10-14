package autonomous

import "testing"

func TestEffectiveTaskConcurrency(t *testing.T) {
	cases := []struct {
		name             string
		override, global int
		want             int
	}{
		{"per-Automaton override wins", 3, 5, 3},
		{"override 1 forces sequential", 1, 5, 1},
		{"no override uses global default", 0, 4, 4},
		{"negative override uses global default", -2, 4, 4},
		{"global 0 is sequential", 0, 0, 0},
		{"global 1 is sequential", 0, 1, 1},
	}
	for _, c := range cases {
		if got := effectiveTaskConcurrency(c.override, c.global); got != c.want {
			t.Errorf("%s: effectiveTaskConcurrency(%d, %d) = %d, want %d", c.name, c.override, c.global, got, c.want)
		}
	}
}

// The deprecated max_parallel_tasks key must never change fan-out.
func TestMaxParallelTasksHasNoEffectOnConcurrency(t *testing.T) {
	if DefaultConfig().MaxParallelTasks != 0 {
		t.Fatalf("DefaultConfig().MaxParallelTasks = %d, want 0 (no injected default)", DefaultConfig().MaxParallelTasks)
	}
	for _, global := range []int{0, 1, 4} {
		base := DefaultConfig()
		base.MaxConcurrentTasks = global
		want := effectiveTaskConcurrency(0, base.MaxConcurrentTasks)
		for _, mpt := range []int{1, 3, 10} {
			cfg := base
			cfg.MaxParallelTasks = mpt
			m := &Manager{cfg: cfg}
			m.mu.Lock()
			g := m.cfg.MaxConcurrentTasks
			m.mu.Unlock()
			if got := effectiveTaskConcurrency(0, g); got != want {
				t.Errorf("MaxParallelTasks=%d changed concurrency: got %d, want %d", mpt, got, want)
			}
		}
	}
}

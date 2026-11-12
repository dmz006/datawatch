package session

import (
	"fmt"
	"sync"
	"testing"
)

func TestRunnerRegistryRegisterAndRelease(t *testing.T) {
	m, _ := newTestManagerWithFake(t)

	if m.HasRunner("host-abcd") {
		t.Fatal("unexpected runner before registration")
	}
	m.RegisterRunner("host-abcd")
	m.RegisterRunner("host-abcd") // idempotent
	if !m.HasRunner("host-abcd") {
		t.Fatal("expected runner after registration")
	}
	if m.HasRunner("host-other") {
		t.Fatal("unrelated ID must not report a runner")
	}
	m.ReleaseRunner("host-abcd")
	if m.HasRunner("host-abcd") {
		t.Fatal("expected no runner after release")
	}
}

func TestRunnerRegistryReleaseUnknownIsNoop(t *testing.T) {
	m, _ := newTestManagerWithFake(t)

	m.ReleaseRunner("never-registered")
	m.RegisterRunner("host-keep")
	m.ReleaseRunner("never-registered")
	if !m.HasRunner("host-keep") {
		t.Fatal("releasing an unknown ID must not affect other entries")
	}
	if m.HasRunner("never-registered") {
		t.Fatal("unknown ID must not appear after release")
	}
}

func TestRunnerRegistryConcurrentUse(t *testing.T) {
	m, _ := newTestManagerWithFake(t)

	const workers = 64
	const rounds = 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			own := fmt.Sprintf("host-%04d", w)
			shared := "host-shared"
			for i := 0; i < rounds; i++ {
				m.RegisterRunner(own)
				if !m.HasRunner(own) {
					t.Errorf("worker %d: own ID missing after register", w)
					return
				}
				m.RegisterRunner(shared)
				_ = m.HasRunner(shared)
				m.ReleaseRunner(shared)
				m.ReleaseRunner(own)
				if m.HasRunner(own) {
					t.Errorf("worker %d: own ID present after release", w)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	for w := 0; w < workers; w++ {
		if m.HasRunner(fmt.Sprintf("host-%04d", w)) {
			t.Fatalf("worker %d ID left behind", w)
		}
	}
}

// Chat `configure` accepts the three dependency update keys, answers with the
// stored value, and refuses an invalid schedule or max_majors before
// configureFn is called.

package router

import (
	"strings"
	"sync"
	"testing"
)

func newDependencyUpdateRouter(t *testing.T, store map[string]string, calls *int) (*Router, *sync.WaitGroup, *[]string) {
	t.Helper()
	var mu sync.Mutex
	sent := &[]string{}
	wg := &sync.WaitGroup{}
	capture := func(s string) {
		mu.Lock()
		*sent = append(*sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		mu.Lock()
		defer mu.Unlock()
		*calls++
		store[key] = value
		return nil
	})
	return r, wg, sent
}

func TestDependencyUpdateConfigureSetsAndReadsBack(t *testing.T) {
	store := map[string]string{}
	calls := 0
	r, wg, sent := newDependencyUpdateRouter(t, store, &calls)
	cases := map[string]string{
		"autonomous.automation.dependency_update.enabled":    "false",
		"autonomous.automation.dependency_update.schedule":   "friday 23:30",
		"autonomous.automation.dependency_update.max_majors": "0",
	}
	for key, val := range cases {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: key + "=" + val})
		wg.Wait()
		if store[key] != val {
			t.Fatalf("%s stored %q, want %q", key, store[key], val)
		}
		want := "[h] Set " + key + " = " + val + ". Restart may be required."
		if got := (*sent)[len(*sent)-1]; got != want {
			t.Fatalf("reply = %q, want %q", got, want)
		}
	}
	if calls != 3 {
		t.Fatalf("configureFn calls = %d, want 3", calls)
	}
}

func TestDependencyUpdateConfigureRefusesInvalid(t *testing.T) {
	bad := map[string]string{
		"autonomous.automation.dependency_update.schedule":   "someday 25:99",
		"autonomous.automation.dependency_update.max_majors": "21",
		"autonomous.automation.dependency_update.enabled":    "maybe",
	}
	for key, val := range bad {
		store := map[string]string{}
		calls := 0
		r, wg, sent := newDependencyUpdateRouter(t, store, &calls)
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: key + "=" + val})
		wg.Wait()
		if calls != 0 || len(store) != 0 {
			t.Fatalf("%s=%s: configureFn called %d times, store %v", key, val, calls, store)
		}
		got := (*sent)[0]
		if !strings.HasPrefix(got, "[h] Refused: ") {
			t.Fatalf("%s=%s: reply = %q", key, val, got)
		}
	}
	// The refusal for a bad schedule names the allowed form.
	store := map[string]string{}
	calls := 0
	r, wg, sent := newDependencyUpdateRouter(t, store, &calls)
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.automation.dependency_update.schedule=someday 25:99"})
	wg.Wait()
	if !strings.Contains((*sent)[0], "weekday HH:MM") {
		t.Fatalf("reply lacks the allowed form: %q", (*sent)[0])
	}
}

package session

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Leak guard for the deferred background writer.
//
// A store's writer goroutine lives until Close, so a test that constructs a
// Store (or a Manager, which wraps one) and finishes without closing it
// leaves a writer behind. The writer fires up to one flushInterval later
// into the test's t.TempDir — which by then is deleted. That produces
// "[warn] sessions store flush: acquire lock ... no such file or directory"
// lines under some test, and the "TempDir RemoveAll cleanup: ... directory
// not empty" failure gets blamed on whatever test happened to be running
// when the writer wrote — not on the test that leaked.
//
// The registry below records every store whose Close has not run, together
// with the test (or non-test caller) that constructed it, so the offending
// test can be failed by name. In production the cost is one map entry per
// live store; production daemons close their store on the way out.
//
// A test's teardown must run the registry's store Close BEFORE its
// t.TempDir() RemoveAll runs (t.Cleanup is LIFO, so registering the close
// after the TempDir is created is exactly the right order).

var (
	liveStoreMu   sync.Mutex
	liveStoreReg  = make(map[*Store]string) // store -> creator description
)

func liveStoreRegister(s *Store, creator string) {
	liveStoreMu.Lock()
	defer liveStoreMu.Unlock()
	liveStoreReg[s] = creator
}

func liveStoreUnregister(s *Store) {
	liveStoreMu.Lock()
	defer liveStoreMu.Unlock()
	delete(liveStoreReg, s)
}

// LiveStores returns one line per session store whose Close has not run:
//
//	<path> (created by <constructor stack>)
//
// sorted by path. Tests use this both in per-test teardown (fail the
// test by name if its store is still open) and as an end-of-package
// sweep (fail the suite if any test failed to close its store at all).
func LiveStores() []string {
	liveStoreMu.Lock()
	defer liveStoreMu.Unlock()
	out := make([]string, 0, len(liveStoreReg))
	for s, creator := range liveStoreReg {
		out = append(out, fmt.Sprintf("%s (created by %s)", s.path, creator))
	}
	sort.Strings(out)
	return out
}

// liveTestCreator walks the construction call stack and returns the
// innermost test function it finds, e.g. "internal/server.TestFoo" for a
// Manager built directly by TestFoo, or "internal/server.TestFoo.func1"
// for one built inside a fixture/helper under TestFoo. Outside a test it
// returns the nearest caller chain so production leaks are still
// attributable in the LiveStores output.
func liveTestCreator() string {
	var pcs [64]uintptr
	n := runtime.Callers(1, pcs[:])
	if n == 0 {
		return "unknown caller"
	}
	chain := make([]string, 0, 4)
	iter := runtime.CallersFrames(pcs[:n])
	for {
		f, more := iter.Next()
		if !more {
			break
		}
		short := f.Function
		if i := strings.LastIndex(short, "."); i >= 0 {
			short = short[i+1:]
		}
		if strings.HasPrefix(short, "Test") {
			return f.Function
		}
		if f.Function != "" && !strings.HasPrefix(f.Function, "runtime.") {
			chain = append(chain, short)
		}
		if len(chain) >= 24 {
			break
		}
	}
	if len(chain) > 0 {
		cap := 8
		if len(chain) > cap {
			chain = chain[len(chain)-cap:]
		}
		return strings.Join(chain, " <- ")
	}
	return "unknown caller"
}

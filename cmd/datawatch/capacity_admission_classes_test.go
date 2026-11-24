package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

func admissionRegistry(t *testing.T) *inference.Registry {
	t.Helper()
	reg := inference.NewRegistry()
	for _, l := range []inference.LLM{
		{Name: "claude-code", Kind: inference.KindClaudeCode},
		{Name: "claude-lean", Kind: inference.KindClaudeCode},
		{Name: "pi", Kind: inference.KindPi},
		{Name: "ollama-local", Kind: inference.KindOllama},
	} {
		l := l
		if err := reg.Add(&l); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func hasPool(pools []string, want string) bool {
	for _, p := range pools {
		if p == want {
			return true
		}
	}
	return false
}

// Every claude-code entry also draws on the account pool; other kinds do not.
func TestResolverAddsAccountPoolForClaudeEntries(t *testing.T) {
	reg := admissionRegistry(t)
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return nil })
	for _, name := range []string{"claude-code", "claude-lean"} {
		pools, _, _ := resolve(name, "")
		if !hasPool(pools, "llm:"+name) || !hasPool(pools, "account:claude") {
			t.Errorf("%s pools = %v, want llm:%s and account:claude", name, pools, name)
		}
	}
	pools, _, _ := resolve("pi", "")
	if hasPool(pools, "account:claude") {
		t.Errorf("pi pools = %v, must not draw on the Claude account", pools)
	}
}

// A subscription backend that resolves to no llm entry is reported and held
// to the conservative subscription cap, never left unlimited.
func TestUnmappedSubscriptionBackendIsReportedAndCapped(t *testing.T) {
	reg := inference.NewRegistry() // no entries at all
	unmappedBackends.reset()
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return nil })
	pools, _, _ := resolve("claude-code", "sonnet")
	if !hasPool(pools, "llm:claude-code") || !hasPool(pools, "account:claude") {
		t.Fatalf("pools = %v, want an llm pool and the account pool for an unmapped claude-code backend", pools)
	}
	got := unmappedBackends.list()
	if len(got) != 1 || got[0].Backend != "claude-code" || got[0].Count != 1 {
		t.Fatalf("unmapped = %+v, want claude-code reported once", got)
	}
	led := capacity.New(capacity.Options{})
	unmappedBackends.apply(led)
	if led.Limit("llm:claude-code") != 1 {
		t.Fatalf("limit = %d, want 1 (subscription default), not unlimited", led.Limit("llm:claude-code"))
	}
	// A local kind with no entry is not a subscription kind: no report.
	unmappedBackends.reset()
	resolve("ollama", "")
	if len(unmappedBackends.list()) != 0 {
		t.Fatal("a non-subscription backend must not be reported")
	}
}

// The pool sync sets the account limit and the supervisor buffers from config.
func TestSyncAdmissionPoolsSetsAccountLimitAndReserves(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := admissionRegistry(t)
	led := capacity.New(capacity.Options{})
	syncLLMLimits(cfg, reg, led)
	syncAdmissionPools(cfg, reg, led)
	if got := led.Limit("account:claude"); got != 2 {
		t.Errorf("account:claude limit = %d, want 2", got)
	}
	for pool, want := range map[string]int{
		"account:claude": 1, "llm:claude-code": 1, "llm:claude-lean": 1, "llm:pi": 1, "host": 4,
	} {
		if got := led.Reserve(pool); got != want {
			t.Errorf("Reserve(%s) = %d, want %d", pool, got, want)
		}
	}
	if led.Reserve("llm:ollama-local") != 0 {
		t.Error("a local entry gets no supervisor buffer")
	}
	zero := 0
	cfg.Autonomous.CapacityAccountClaudeLimitSetting = &zero
	cfg.Autonomous.CapacitySupervisorReserveClaudeSetting = &zero
	syncAdmissionPools(cfg, reg, led)
	if led.Limit("account:claude") != 0 || led.Reserve("llm:claude-code") != 0 {
		t.Error("explicit zero must clear the account limit and the buffer")
	}
}

// The account cap holds across entries: claude-code 1 + claude-lean 1 fills
// account:claude (2), so a third worker waits though each entry has room.
func TestAccountPoolCapsEntriesTogether(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := admissionRegistry(t)
	two := 2
	cfg.Autonomous.CapacityAccountClaudeLimitSetting = &two
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	syncLLMLimits(cfg, reg, led)
	led.SetLimit("llm:claude-lean", 2) // each entry alone would admit a second lane
	led.SetLimit("llm:claude-code", 2)
	syncAdmissionPools(cfg, reg, led)
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return nil })
	acq := func(holder, backend string) error {
		pools, _, _ := resolve(backend, "")
		return led.Acquire(context.Background(), capacity.Request{Holder: holder, Pools: pools}, 40*time.Millisecond, nil, nil)
	}
	if err := acq("a", "claude-code"); err != nil {
		t.Fatal(err)
	}
	if err := acq("b", "claude-lean"); err != nil {
		t.Fatal(err)
	}
	if err := acq("c", "claude-lean"); !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("third lane err = %v, want wait timeout from the account cap", err)
	}
}

// The manager admitter: a start nobody admitted takes an operator lease even
// when the pool is full, records the overage, and gives it back on release.
func TestManagerStartAdmitterOperatorOverCapRecordsOverage(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := admissionRegistry(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	syncLLMLimits(cfg, reg, led)
	syncAdmissionPools(cfg, reg, led)
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return nil })
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "worker-1", Pools: []string{"host", "llm:claude-code", "account:claude"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	admit := managerStartAdmitter(cfg, led, resolve, func() string { return "claude-code" })
	release, bind, err := admit(context.Background(), session.StartAdmission{Class: "operator", Backend: "claude-code"})
	if err != nil {
		t.Fatalf("operator start refused: %v", err)
	}
	bind("sess-1")
	var over *capacity.Lease
	for _, ls := range led.Snapshot().Leases {
		if ls.SessionID == "sess-1" {
			ls := ls
			over = &ls
		}
	}
	if over == nil || over.Class != capacity.ClassOperator || len(over.Overage) == 0 {
		t.Fatalf("lease = %+v, want an operator lease with an overage record", over)
	}
	release()
	for _, ls := range led.Snapshot().Leases {
		if ls.SessionID == "sess-1" {
			t.Fatal("release must free the lease")
		}
	}
	// A worker-class start with no upstream admission obeys the cap and waits.
	cfg.Session.CapacityWaitSeconds = 1
	if _, _, err := admit(context.Background(), session.StartAdmission{Class: "worker", Backend: "claude-code"}); err == nil {
		t.Fatal("a worker start over the cap must be refused after its wait")
	}
}

func TestAdoptOverLimitNoticeRaisesOneAlertText(t *testing.T) {
	var title, body string
	adoptOverLimitNotice(func(ti, bo string) { title, body = ti, bo })("llm:pi-prompt", 8, 3)
	if title == "" || !strings.Contains(body, "llm:pi-prompt") || !strings.Contains(body, "8") || !strings.Contains(body, "3") {
		t.Fatalf("alert = %q / %q", title, body)
	}
	adoptOverLimitNotice(nil)("p", 2, 1) // no alert function: no panic
}

func TestOperatorOverageNoticeHonoursSetting(t *testing.T) {
	cfg := config.DefaultConfig()
	n := 0
	notice := operatorOverageNotice(cfg, func(string, string) { n++ })
	ls := capacity.Lease{Holder: "start-1", Overage: []capacity.Overage{{Pool: "llm:claude-code", Limit: 1, Over: 1}}}
	notice(ls)
	if n != 0 {
		t.Fatal("the alert is off by default")
	}
	cfg.Autonomous.CapacityOperatorOverageAlert = true
	notice(ls)
	if n != 1 {
		t.Fatalf("alerts = %d, want 1 with the setting on", n)
	}
}

// A local-engine backend (pi) with no LLM entry is governed by the node pools,
// not a subscription plan, so it is not reported as unmapped.
func TestLocalEngineBackendIsNotReportedUnmapped(t *testing.T) {
	reg := inference.NewRegistry()
	unmappedBackends.reset()
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return nil })
	pools, _, _ := resolve("pi", "ollama/qwen")
	if len(unmappedBackends.list()) != 0 || hasPool(pools, "llm:pi") {
		t.Fatalf("pi with no entry: pools %v unmapped %v, want neither", pools, unmappedBackends.list())
	}
}

// A restarted worker with a full pool waits at the head of the queue and starts
// before older waiting fresh work when a slot frees.
func TestManagerStartAdmitterRestartedWorkerStartsBeforeFreshWork(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Autonomous.CapacityWaitTimeoutSeconds = 5
	reg := admissionRegistry(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	syncLLMLimits(cfg, reg, led)
	syncAdmissionPools(cfg, reg, led)
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return nil })
	pools := []string{"host", "llm:claude-code", "account:claude"}
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "running", Pools: pools}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	granted := make(chan string, 2)
	go func() {
		if err := led.Acquire(context.Background(), capacity.Request{Holder: "fresh", Pools: pools, Priority: 90}, 5*time.Second, nil, nil); err == nil {
			granted <- "fresh"
		}
	}()
	time.Sleep(50 * time.Millisecond)
	admit := managerStartAdmitter(cfg, led, resolve, func() string { return "claude-code" })
	go func() {
		if _, _, err := admit(context.Background(), session.StartAdmission{Class: "worker", Restart: true, Backend: "claude-code"}); err == nil {
			granted <- "restart"
		}
	}()
	time.Sleep(50 * time.Millisecond)
	led.Release("running")
	select {
	case first := <-granted:
		if first != "restart" {
			t.Fatalf("first grant = %s, want the restarted worker", first)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no grant after the slot freed")
	}
}

func TestSyncAdmissionPoolsSetsOperatorAllowance(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := admissionRegistry(t)
	led := capacity.New(capacity.Options{})
	mgr, err := session.NewManager("h", t.TempDir(), "echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	syncAdmissionPoolsFor(cfg, reg, led, mgr)
	if got := mgr.SessionCap().OperatorSlots; got != 5 {
		t.Fatalf("operator slots = %d, want the default 5", got)
	}
	three := 3
	cfg.Autonomous.CapacityOperatorReserveHostSetting = &three
	syncAdmissionPoolsFor(cfg, reg, led, mgr)
	if got := mgr.SessionCap().OperatorSlots; got != 3 {
		t.Fatalf("operator slots = %d, want 3 after the setting changed", got)
	}
}

func TestOverSessionCapNoticeNamesTheSetting(t *testing.T) {
	cfg := config.DefaultConfig()
	var title, body string
	overSessionCapNotice(cfg, func(ti, bo string) { title, body = ti, bo })("operator", 2)
	if title == "" || !strings.Contains(body, "autonomous.capacity_operator_reserve_host") || !strings.Contains(body, "2 above") {
		t.Fatalf("alert = %q / %q", title, body)
	}
	overSessionCapNotice(cfg, nil)("operator", 1) // no alert function: no panic
}

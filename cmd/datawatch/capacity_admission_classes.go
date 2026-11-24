package main

// Admission classes (worker, supervisor, operator), the account-level Claude
// pool, the supervisor buffers and the report of subscription backends that
// resolve to no llm pool. The ledger rules live in internal/capacity; this file
// wires them to the daemon's config, registry and session manager.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

// accountClaudePool is the pool every claude-code entry also draws on, so the
// per-entry caps cannot add up past the account's.
const accountClaudePool = "account:claude"

// unmappedBackend is a subscription backend a request named that resolves to
// no LLM entry, so it has no pool of its own.
type unmappedBackend struct {
	Backend  string    `json:"backend"`
	Model    string    `json:"model,omitempty"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

type unmappedTracker struct {
	mu sync.Mutex
	m  map[string]*unmappedBackend
}

// unmappedBackends is the daemon's report of subscription backends with no
// llm pool. It never calls into the ledger (the resolver runs under the
// ledger's lock); apply does that from outside it.
var unmappedBackends = &unmappedTracker{m: map[string]*unmappedBackend{}}

func (u *unmappedTracker) note(backend, model string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	e := u.m[backend]
	if e == nil {
		e = &unmappedBackend{Backend: backend}
		u.m[backend] = e
	}
	e.Count++
	e.Model, e.LastSeen = model, time.Now()
}

func (u *unmappedTracker) list() []unmappedBackend {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]unmappedBackend, 0, len(u.m))
	for _, e := range u.m {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Backend < out[j].Backend })
	return out
}

func (u *unmappedTracker) reset() {
	u.mu.Lock()
	u.m = map[string]*unmappedBackend{}
	u.mu.Unlock()
}

// apply gives every reported backend's pool the subscription default of one
// in-flight session when it has no limit, so an entry that is missing is
// capped rather than unlimited. A real entry's limit (syncLLMLimits) replaces it.
func (u *unmappedTracker) apply(led *capacity.Ledger) {
	for _, e := range u.list() {
		if pool := "llm:" + e.Backend; led.Limit(pool) == 0 {
			led.SetLimit(pool, 1)
		}
	}
}

// syncAdmissionPools sets the account:claude limit and the supervisor buffers
// from config. It runs on the same tick as syncLLMLimits and only touches a
// value that changed.
func syncAdmissionPools(cfg *config.Config, reg *inference.Registry, led *capacity.Ledger) {
	syncAdmissionPoolsFor(cfg, reg, led, nil)
}

// syncAdmissionPoolsFor is syncAdmissionPools that also hands the host buffer
// to the session manager's hard session cap (nil mgr skips that).
func syncAdmissionPoolsFor(cfg *config.Config, reg *inference.Registry, led *capacity.Ledger, mgr *session.Manager) {
	if mgr != nil {
		mgr.SetSupervisorSlots(cfg.Autonomous.CapacitySupervisorReserveHost())
	}
	setLimit := func(pool string, n int) {
		if led.Limit(pool) != n {
			led.SetLimit(pool, n)
		}
	}
	setReserve := func(pool string, n int) {
		if led.Reserve(pool) != n {
			led.SetReserve(pool, n)
		}
	}
	a := &cfg.Autonomous
	setLimit(accountClaudePool, a.CapacityAccountClaudeLimit())
	setReserve(accountClaudePool, a.CapacitySupervisorReserveClaude())
	setReserve("host", a.CapacitySupervisorReserveHost())
	for _, l := range reg.List() {
		n := 0
		switch l.Kind {
		case inference.KindClaudeCode:
			n = a.CapacitySupervisorReserveClaude()
		case inference.KindPi, inference.KindPiPrompt:
			n = a.CapacitySupervisorReservePi()
		}
		setReserve("llm:"+l.Name, n)
	}
	unmappedBackends.apply(led)
}

func newStartHolder() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "start-" + hex.EncodeToString(b), nil
}

// managerStartAdmitter is the session manager's admission for a start no
// caller admitted upstream (chat, MCP, schedules, WebSocket, restarts, the
// pipeline adapter). It admits on the host, llm and account pools only: those
// paths place no compute node. An unknown class is an operator start.
func managerStartAdmitter(cfg *config.Config, led *capacity.Ledger, resolve func(backend, model string) ([]string, string, []capacity.NodeChoice), defaultBackend func() string) session.StartAdmitFn {
	return func(ctx context.Context, a session.StartAdmission) (func(), func(string), error) {
		class := capacity.Class(a.Class)
		switch class {
		case capacity.ClassWorker, capacity.ClassSupervisor, capacity.ClassOperator:
		default:
			class = capacity.ClassOperator
		}
		backend := a.Backend
		if backend == "" && defaultBackend != nil {
			backend = defaultBackend()
		}
		all, _, _ := resolve(backend, a.Model)
		unmappedBackends.apply(led)
		pools := make([]string, 0, len(all))
		for _, p := range all {
			if !strings.HasPrefix(p, "node:") {
				pools = append(pools, p)
			}
		}
		if len(pools) == 0 {
			return func() {}, func(string) {}, nil
		}
		holder, err := newStartHolder()
		if err != nil {
			return nil, nil, fmt.Errorf("capacity: could not generate holder id: %w", err)
		}
		wait := 8
		if cfg.Session.CapacityWaitSeconds > 0 {
			wait = cfg.Session.CapacityWaitSeconds
		}
		wctx, cancel := context.WithTimeout(ctx, time.Duration(wait)*time.Second)
		defer cancel()
		req := capacity.Request{Holder: holder, Pools: pools, Model: a.Model, Class: class, Interactive: class == capacity.ClassOperator}
		if err := led.Acquire(wctx, req, 0, func() bool { return false }, func(string) {}); err != nil {
			return nil, nil, fmt.Errorf("capacity: no %s slot free after %ds: %w", class, wait, err)
		}
		return func() { led.Release(holder) }, func(id string) { led.Bind(holder, id) }, nil
	}
}

// operatorOverageNotice is the ledger's OnOverage hook: an alert for an
// operator lease granted over a pool's limit, when the operator turned the
// alert on. The overage is recorded on the lease whatever this setting says.
func operatorOverageNotice(cfg *config.Config, alertFn func(title, body string)) func(capacity.Lease) {
	return func(ls capacity.Lease) {
		if alertFn == nil || !cfg.Autonomous.CapacityOperatorOverageAlert {
			return
		}
		var parts []string
		for _, o := range ls.Overage {
			parts = append(parts, fmt.Sprintf("%s is %d over its limit of %d", o.Pool, o.Over, o.Limit))
		}
		alertFn("Operator session started over a capacity limit", fmt.Sprintf("Session %s was admitted as an operator start: %s. Autonomous work waits until the pool is back under its limit.", ls.Holder, strings.Join(parts, "; ")))
	}
}

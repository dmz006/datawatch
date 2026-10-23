package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// holderNameAPI is an AutonomousAPI fake supplying both the per-task views
// and the holder-name resolver, backed by one fixed title snapshot (the same
// shape the real manager serves out of its PRD store).
type holderNameAPI struct {
	automatonStatusAPI
	waiting []autonomous.TaskWaitView
	running []autonomous.TaskHoldView
}

func (a *holderNameAPI) TaskVisibility() ([]autonomous.TaskWaitView, []autonomous.TaskHoldView) {
	return a.waiting, a.running
}

func (a *holderNameAPI) ResolveHolder(holderID string) autonomous.HolderName {
	r := autonomous.NewTitleResolver(autonomous.TitleResolverOptions{
		Lookup: func() autonomous.TitleSnapshot { return holderNameSnapshot() },
	})
	return r.Resolve(holderID)
}

// holderNameSnapshot is the fixed title store: two named PRDs and tasks,
// including "t-cccc2222" whose PRD (prd-x) has no title at all — the
// "(untitled …)" fallback path.
func holderNameSnapshot() autonomous.TitleSnapshot {
	return autonomous.TitleSnapshot{
		PRDs: map[string]string{
			"prd-a": "Automate billing",
			"prd-b": "Verify flows",
			// prd-x is deliberately absent: "t-cccc2222" belongs to it.
		},
		Tasks: map[string]autonomous.TaskRef{
			"t-aaaa1111": {PRDID: "prd-a", Title: "Wire up the webhooks"},
			"t-bbbb3333": {PRDID: "prd-b", Title: "Run migrations"},
			"t-cccc2222": {PRDID: "prd-x", Title: "Ship the CLI"},
		},
	}
}

type capPool struct {
	Name        string                      `json:"name"`
	Holders     []string                    `json:"holders"`
	Holder      *string                     `json:"holder"`
	HolderNames []autonomous.HolderNameView `json:"holder_names"`
}
type capLease struct {
	Holder     string                    `json:"holder"`
	PRDID      string                    `json:"prd_id"`
	HolderName autonomous.HolderNameView `json:"holder_name"`
}
type capWaiter struct {
	Holder     string                    `json:"holder"`
	PRDID      string                    `json:"prd_id"`
	HolderName autonomous.HolderNameView `json:"holder_name"`
}
type capTaskWait struct {
	TaskID    string `json:"task_id"`
	PRDID     string `json:"prd_id"`
	PRDTitle  string `json:"prd_title"`
	TaskTitle string `json:"task_title"`
	Reason    string `json:"reason"`
	Readable  string `json:"readable"`
	ShortID   string `json:"short_id"`
}
type capTaskHold struct {
	TaskID    string `json:"task_id"`
	PRDID     string `json:"prd_id"`
	PRDTitle  string `json:"prd_title"`
	TaskTitle string `json:"task_title"`
	Readable  string `json:"readable"`
	ShortID   string `json:"short_id"`
}
type capBody struct {
	Pools   []capPool   `json:"pools"`
	Leases  []capLease  `json:"leases"`
	Waiting []capWaiter `json:"waiting"`
	Tasks   struct {
		Waiting []capTaskWait `json:"waiting"`
		Running []capTaskHold `json:"running"`
	} `json:"tasks"`
}

// TestHandleCapacity_HolderNames asserts the readable fields sit next to the
// old raw id fields: pools[].holders (string array) and pools[].holder
// (comma string) are unchanged and each gains a parallel readable form
// (pools[].holder_names, leases[].holder_name, waiting[].holder_name),
// and tasks.waiting/running gain readable + short_id beside the existing
// prd_title/task_title. Covers a task holder, a verifier holder and a holder
// whose PRD title is missing ("(untitled <short id>)").
func TestHandleCapacity_HolderNames(t *testing.T) {
	api := &holderNameAPI{
		waiting: []autonomous.TaskWaitView{
			{TaskID: "t-bbbb3333", PRDID: "prd-b", PRDTitle: "Verify flows", TaskTitle: "Run migrations",
				Cause: autonomous.WaitCauseNodeBusy, Reason: "node:gpu busy, held by t-aaaa1111", Since: time.Now().UTC()},
			{TaskID: "t-cccc2222", PRDID: "prd-x", TaskTitle: "Ship the CLI",
				Cause: autonomous.WaitCausePoolFull, Reason: "pool node:cpu full", Since: time.Now().UTC()},
		},
		running: []autonomous.TaskHoldView{
			{TaskID: "t-aaaa1111", PRDID: "prd-a", PRDTitle: "Automate billing", TaskTitle: "Wire up the webhooks",
				Pools: []string{"host", "node:gpu"}, Node: "gpu", SessionID: "sess-1", Since: time.Now().UTC()},
			{TaskID: "t-cccc2222", PRDID: "prd-x", TaskTitle: "Ship the CLI",
				Pools: []string{"host", "node:cpu"}, Node: "cpu", Since: time.Now().UTC()},
		},
	}

	s := &Server{autonomousMgr: api}
	l := capacity.New(capacity.Options{})
	s.SetCapacity(l)
	l.SetLimit("node:gpu", 1)
	l.SetLimit("node:cpu", 1)

	// Two held node slots: the task holder on node:gpu and the untitled-PRD
	// task on node:cpu.
	held := make(chan struct{}, 2)
	go func() {
		_ = l.Acquire(context.Background(), capacity.Request{Holder: "t-aaaa1111", PRDID: "prd-a", Pools: []string{"node:gpu"}}, 0, nil, nil)
		held <- struct{}{}
	}()
	go func() {
		_ = l.Acquire(context.Background(), capacity.Request{Holder: "t-cccc2222", PRDID: "prd-x", Pools: []string{"node:cpu"}}, 0, nil, nil)
		held <- struct{}{}
	}()
	<-held
	<-held

	// A verifier asks for the full node:gpu slot and parks in waiting[].
	go func() {
		_ = l.Acquire(context.Background(), capacity.Request{Holder: "verify:t-bbbb3333", PRDID: "prd-b", Pools: []string{"node:gpu"}}, 30*time.Second, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(l.Snapshot().Waiting) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if len(l.Snapshot().Waiting) == 0 {
		t.Fatal("verifier never reached the wait queue")
	}

	// --- short form (default): old id fields AND new readable fields ---
	var body capBody
	{
		rec := httptest.NewRecorder()
		s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/capacity = %d: %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v\n%s", err, rec.Body.String())
		}
	}

	// pools: node:gpu — holders/holder (old) untouched, holder_names (new).
	gpu := findPool(t, body.Pools, "node:gpu")
	if len(gpu.Holders) != 1 || gpu.Holders[0] != "t-aaaa1111" {
		t.Errorf("node:gpu holders (old) = %v, want [t-aaaa1111]", gpu.Holders)
	}
	if gpu.Holder == nil || *gpu.Holder != "t-aaaa1111" {
		t.Errorf("node:gpu holder (old) = %v, want t-aaaa1111", gpu.Holder)
	}
	if len(gpu.HolderNames) != 1 {
		t.Fatalf("node:gpu holder_names = %v, want one readable entry", gpu.HolderNames)
	}
	assertTaskHolderName(t, gpu.HolderNames[0], "pools[].holder_names")

	// pools: node:cpu — the untitled-PRD holder alongside its raw id.
	cpu := findPool(t, body.Pools, "node:cpu")
	if len(cpu.Holders) != 1 || cpu.Holders[0] != "t-cccc2222" {
		t.Errorf("node:cpu holders (old) = %v, want [t-cccc2222]", cpu.Holders)
	}
	if len(cpu.HolderNames) != 1 {
		t.Fatalf("node:cpu holder_names = %v, want one readable entry", cpu.HolderNames)
	}
	assertUntitledHolderName(t, cpu.HolderNames[0], "pools[].holder_names")

	// leases: old holder/prd_id kept, holder_name added (task holder).
	lz := findCapacityLease(t, body.Leases, "t-aaaa1111")
	if lz.PRDID != "prd-a" {
		t.Errorf("task lease prd_id (old) = %q, want prd-a", lz.PRDID)
	}
	assertTaskHolderName(t, lz.HolderName, "leases[].holder_name")

	// waiting: old holder/prd_id + reason kept, holder_name added (verifier).
	wt := findCapacityWaiter(t, body.Waiting, "verify:t-bbbb3333")
	if wt.PRDID != "prd-b" {
		t.Errorf("verifier waiter prd_id (old) = %q, want prd-b", wt.PRDID)
	}
	assertVerifierHolderName(t, wt.HolderName, "waiting[].holder_name")

	// tasks.waiting: readable + short_id beside existing prd_title/task_title.
	tw := findTaskWait(t, body.Tasks.Waiting, "t-bbbb3333")
	if tw.PRDTitle != "Verify flows" || tw.TaskTitle != "Run migrations" || tw.Reason == "" {
		t.Errorf("tasks.waiting lost old id/title fields: %+v", tw)
	}
	if tw.Readable != "Verify flows / Run migrations (t-bbbb33)" {
		t.Errorf("tasks.waiting readable = %q, want %q", tw.Readable, "Verify flows / Run migrations (t-bbbb33)")
	}
	if tw.ShortID != "t-bbbb33" {
		t.Errorf("tasks.waiting short_id = %q, want t-bbbb33", tw.ShortID)
	}

	// tasks.waiting: the untitled PRD gets the "(untitled …)" fallback.
	untitled := findTaskWait(t, body.Tasks.Waiting, "t-cccc2222")
	if untitled.PRDTitle != "" || untitled.TaskTitle != "Ship the CLI" {
		t.Errorf("untitled tasks.waiting old fields = %+v", untitled)
	}
	if !strings.Contains(untitled.Readable, "(untitled prd-x)") {
		t.Errorf("untitled tasks.waiting readable = %q, want a (untitled prd-x) fallback", untitled.Readable)
	}
	if untitled.ShortID != "t-cccc22" {
		t.Errorf("untitled tasks.waiting short_id = %q, want t-cccc22", untitled.ShortID)
	}

	// tasks.running: readable + short_id beside existing prd_title/task_title.
	rr := findTaskHold(t, body.Tasks.Running, "t-aaaa1111")
	if rr.PRDTitle != "Automate billing" || rr.TaskTitle != "Wire up the webhooks" {
		t.Errorf("tasks.running lost old id/title fields: %+v", rr)
	}
	if rr.Readable != "Automate billing / Wire up the webhooks (t-aaaa11)" {
		t.Errorf("tasks.running readable = %q, want %q", rr.Readable, "Automate billing / Wire up the webhooks (t-aaaa11)")
	}
	if rr.ShortID != "t-aaaa11" {
		t.Errorf("tasks.running short_id = %q, want t-aaaa11", rr.ShortID)
	}

	// --- raw-key checks: waiting[] keeps its plain reason; full only with long=1 ---
	{
		rec := httptest.NewRecorder()
		s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET (raw) = %d: %s", rec.Code, rec.Body.String())
		}
		var raw struct {
			Leases  []map[string]json.RawMessage `json:"leases"`
			Waiting []map[string]json.RawMessage `json:"waiting"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("raw decode: %v", err)
		}
		seenVerifier := false
		for _, w := range raw.Waiting {
			if !strings.Contains(string(w["holder"]), "verify:") {
				continue
			}
			seenVerifier = true
			if _, ok := w["reason"]; !ok {
				t.Errorf("waiting entry for verify: holder lost its plain reason key: %s", w)
			}
			if _, ok := w["holder_name"]; !ok {
				t.Errorf("waiting entry for verify: holder has no holder_name: %s", w)
			}
		}
		if !seenVerifier {
			t.Fatalf("no verify: waiter in response: %+v", raw.Waiting)
		}
		for _, l := range raw.Leases {
			if !strings.Contains(string(l["holder"]), "t-aaaa11") {
				continue
			}
			var v map[string]json.RawMessage
			_ = json.Unmarshal(l["holder_name"], &v)
			if _, ok := v["full"]; ok {
				t.Errorf("full must be omitted without long=1: %+v", v)
			}
		}
	}
	{
		rec := httptest.NewRecorder()
		s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity?long=1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET long=1 = %d: %s", rec.Code, rec.Body.String())
		}
		var raw struct {
			Leases []map[string]json.RawMessage `json:"leases"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("raw decode: %v", err)
		}
		seenFull := false
		for _, l := range raw.Leases {
			if !strings.Contains(string(l["holder"]), "t-aaaa11") {
				continue
			}
			var v map[string]json.RawMessage
			_ = json.Unmarshal(l["holder_name"], &v)
			if full, ok := v["full"]; ok {
				seenFull = true
				var txt string
				_ = json.Unmarshal(full, &txt)
				if txt == "" {
					t.Errorf("full is empty with long=1: %+v", v)
				}
			}
		}
		if !seenFull {
			t.Errorf("long=1 must expose full in the task holder's holder_name: %+v", raw.Leases)
		}
	}
}

func findPool(t *testing.T, pools []capPool, name string) capPool {
	t.Helper()
	for _, p := range pools {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("pool %q not in response: %+v", name, pools)
	return capPool{}
}

func findCapacityLease(t *testing.T, leases []capLease, holder string) capLease {
	t.Helper()
	for _, l := range leases {
		if l.Holder == holder {
			return l
		}
	}
	t.Fatalf("lease with holder %q not in response: %+v", holder, leases)
	return capLease{}
}

func findCapacityWaiter(t *testing.T, waiters []capWaiter, holder string) capWaiter {
	t.Helper()
	for _, w := range waiters {
		if w.Holder == holder {
			return w
		}
	}
	t.Fatalf("waiter with holder %q not in response: %+v", holder, waiters)
	return capWaiter{}
}

func findTaskWait(t *testing.T, views []capTaskWait, taskID string) capTaskWait {
	t.Helper()
	for _, v := range views {
		if v.TaskID == taskID {
			return v
		}
	}
	t.Fatalf("tasks.waiting entry %q not in response: %+v", taskID, views)
	return capTaskWait{}
}

func findTaskHold(t *testing.T, views []capTaskHold, taskID string) capTaskHold {
	t.Helper()
	for _, v := range views {
		if v.TaskID == taskID {
			return v
		}
	}
	t.Fatalf("tasks.running entry %q not in response: %+v", taskID, views)
	return capTaskHold{}
}

func assertTaskHolderName(t *testing.T, v autonomous.HolderNameView, where string) {
	t.Helper()
	if v.ID != "t-aaaa1111" {
		t.Errorf("%s id = %q, want t-aaaa1111", where, v.ID)
	}
	if v.Kind != autonomous.HolderKindTask {
		t.Errorf("%s kind = %q, want task", where, v.Kind)
	}
	if v.TaskTitle != "Wire up the webhooks" {
		t.Errorf("%s task_title = %q, want %q", where, v.TaskTitle, "Wire up the webhooks")
	}
	if v.PRDID != "prd-a" || v.PRDTitle != "Automate billing" {
		t.Errorf("%s prd = %q/%q, want prd-a/Automate billing", where, v.PRDID, v.PRDTitle)
	}
	if v.ShortID != "t-aaaa11" {
		t.Errorf("%s short_id = %q, want t-aaaa11", where, v.ShortID)
	}
	if v.Readable != "Automate billing / Wire up the webhooks (t-aaaa11)" {
		t.Errorf("%s readable = %q, want %q", where, v.Readable, "Automate billing / Wire up the webhooks (t-aaaa11)")
	}
}

func assertVerifierHolderName(t *testing.T, v autonomous.HolderNameView, where string) {
	t.Helper()
	if v.ID != "verify:t-bbbb3333" {
		t.Errorf("%s id = %q, want verify:t-bbbb3333", where, v.ID)
	}
	if v.Kind != autonomous.HolderKindVerifier {
		t.Errorf("%s kind = %q, want verifier", where, v.Kind)
	}
	if v.TaskTitle != "Run migrations" {
		t.Errorf("%s task_title = %q, want %q", where, v.TaskTitle, "Run migrations")
	}
	if v.PRDID != "prd-b" || v.PRDTitle != "Verify flows" {
		t.Errorf("%s prd = %q/%q, want prd-b/Verify flows", where, v.PRDID, v.PRDTitle)
	}
	if v.ShortID != "t-bbbb33" {
		t.Errorf("%s short_id = %q, want t-bbbb33", where, v.ShortID)
	}
	if v.Readable != "Verifying: Run migrations (t-bbbb33)" {
		t.Errorf("%s readable = %q, want %q", where, v.Readable, "Verifying: Run migrations (t-bbbb33)")
	}
}

func assertUntitledHolderName(t *testing.T, v autonomous.HolderNameView, where string) {
	t.Helper()
	if v.ID != "t-cccc2222" {
		t.Errorf("%s id = %q, want t-cccc2222", where, v.ID)
	}
	if v.Kind != autonomous.HolderKindTask {
		t.Errorf("%s kind = %q, want task", where, v.Kind)
	}
	if v.PRDID != "prd-x" || v.PRDTitle != "" {
		t.Errorf("%s prd = %q/%q, want prd-x/(empty)", where, v.PRDID, v.PRDTitle)
	}
	if v.ShortID != "t-cccc22" {
		t.Errorf("%s short_id = %q, want t-cccc22", where, v.ShortID)
	}
	if !strings.Contains(v.Readable, "(untitled prd-x)") {
		t.Errorf("%s readable = %q, want a (untitled prd-x) fallback", where, v.Readable)
	}
	if !strings.Contains(v.Readable, "Ship the CLI") {
		t.Errorf("%s readable = %q, want the task title", where, v.Readable)
	}
}

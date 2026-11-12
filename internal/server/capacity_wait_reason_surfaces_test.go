package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// These tests pin that every read of GET /api/capacity (and so MCP
// capacity_status, which proxies the body, and the CLI text built from it)
// carries the CURRENT wait reason once an adoption hold has ended, never the
// stale adoption text a task persisted while the hold was on.

const (
	wrStaleAdoption = "waiting for work resumed after the restart to be adopted"
	wrAdoptTimeout  = 3 * time.Minute
)

// wrClock is a manually advanced capacity.Clock: timers fire only on Advance.
type wrClock struct {
	mu  sync.Mutex
	now time.Time
	tms []wrTimer
}

type wrTimer struct {
	at time.Time
	ch chan time.Time
}

func (c *wrClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *wrClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.tms = append(c.tms, wrTimer{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *wrClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	keep := c.tms[:0]
	for _, tm := range c.tms {
		if !tm.at.After(c.now) {
			tm.ch <- c.now
		} else {
			keep = append(keep, tm)
		}
	}
	c.tms = keep
}

func (c *wrClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tms)
}

// wrAPI is the autonomous fake: it queues one task whose persisted wait
// reason is the stale adoption text, and words the task view the way the
// manager does — the ledger waiter's live reason wins over the persisted one.
type wrAPI struct {
	automatonStatusAPI
	led       *capacity.Ledger
	persisted string
}

func (a *wrAPI) TaskVisibility() ([]autonomous.TaskWaitView, []autonomous.TaskHoldView) {
	reason := a.persisted
	var since time.Time
	for _, w := range a.led.Snapshot().Waiting {
		if w.Holder == "t1" && strings.TrimSpace(w.Reason) != "" {
			reason, since = w.Reason, w.Since
		}
	}
	v := autonomous.TaskWaitView{TaskID: "t1", PRDID: "prd-1", Cause: autonomous.WaitCausePoolFull, Reason: reason, Since: since}
	v.ReadableReason = a.ReadableWaitSentence(reason)
	return []autonomous.TaskWaitView{v}, []autonomous.TaskHoldView{}
}

func (a *wrAPI) ReadableWaitSentence(reason string) string {
	return autonomous.ReadableWaitText(reason, autonomous.WaitReasonHolders(reason), nil)
}

// wrCapacityBody builds the ledger (fixed clock, adoption hold already ended,
// t1 queued behind a node holder) and returns the raw /api/capacity body.
func wrCapacityBody(t *testing.T) []byte {
	t.Helper()
	clk := &wrClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	l := capacity.New(capacity.Options{PollInterval: 24 * time.Hour, Clock: clk, AdoptionTimeout: wrAdoptTimeout})
	l.SetLimit("node:alpha", 1)
	if err := l.Acquire(context.Background(), capacity.Request{Holder: "owner", PRDID: "prd-0", Pools: []string{"node:alpha"}, Node: "alpha"}, 0, nil, nil); err != nil {
		t.Fatalf("owner: %v", err)
	}
	l.BeginAdoption()
	go func() {
		_ = l.Acquire(context.Background(), capacity.Request{Holder: "t1", PRDID: "prd-1", Pools: []string{"node:alpha"}, Node: "alpha"}, 0, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for clk.pending() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("waiter never queued")
		}
		time.Sleep(time.Millisecond)
	}
	// The hold has ended; no ledger tick has run since (poll is 24h).
	clk.Advance(wrAdoptTimeout + time.Second)

	api := &wrAPI{led: l, persisted: wrStaleAdoption}
	s := &Server{autonomousMgr: api}
	s.SetCapacity(l)
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/capacity = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

const wrNodePrefix = "waiting for node alpha: 1/1 engines in use, held by "

func TestCapacityWaitReason_RESTCarriesCurrentReason(t *testing.T) {
	raw := wrCapacityBody(t)
	var b struct {
		Waiting []struct {
			Reason string `json:"reason"`
		} `json:"waiting"`
		Tasks struct {
			Waiting []struct {
				Reason         string `json:"reason"`
				ReadableReason string `json:"readable_reason"`
			} `json:"waiting"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Waiting) != 1 || len(b.Tasks.Waiting) != 1 {
		t.Fatalf("want one waiter and one queued task: %s", raw)
	}
	got := map[string]string{
		"waiting[].reason":                b.Waiting[0].Reason,
		"tasks.waiting[].reason":          b.Tasks.Waiting[0].Reason,
		"tasks.waiting[].readable_reason": b.Tasks.Waiting[0].ReadableReason,
	}
	for name, v := range got {
		if !strings.HasPrefix(v, wrNodePrefix) {
			t.Errorf("%s = %q; want the node-engine reason", name, v)
		}
		if strings.Contains(v, "adopted") {
			t.Errorf("%s = %q; must not carry the stale adoption text", name, v)
		}
	}
}

// The CLI text is built from the same body, so it must print the same reason.
func TestCapacityWaitReason_CLITextPrintsCurrentReason(t *testing.T) {
	out, err := autonomous.FormatCapacityText(wrCapacityBody(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, wrNodePrefix) {
		t.Fatalf("CLI text lacks the node-engine reason:\n%s", out)
	}
	if strings.Contains(out, "adopted") {
		t.Fatalf("CLI text carries the stale adoption text:\n%s", out)
	}
}

package main

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// nlLedger builds a ledger the way the daemon does for the node limit rules: its
// Options come from applyNodeLimitOptions over cfg. Two nodes of one slot each,
// and an llm pool of three, so a pool counted by node and a pool counted per
// lease read differently.
func nlLedger(cfg *config.Config) *capacity.Ledger {
	var opt capacity.Options
	applyNodeLimitOptions(&opt, cfg)
	opt.PollInterval = time.Millisecond
	l := capacity.New(opt)
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	l.SetLimit("llm:m", 3)
	return l
}

func nlReq(holder, node string) capacity.Request {
	return capacity.Request{Holder: holder, PRDID: "p", Pools: []string{"host", "llm:m"}, Node: node}
}

func nlSetting(cfg *config.Config, name string, on bool) {
	switch name {
	case "strict":
		cfg.Autonomous.CapacityStrictNodeLimit = &on
	case "by_node":
		cfg.Autonomous.CapacityCountPoolByNode = &on
	case "adopt_first":
		cfg.Autonomous.CapacityAdoptBeforeAdmit = &on
	}
}

func nlNodeHeld(l *capacity.Ledger, pool string) (held, over int) {
	for _, p := range l.Snapshot().Pools {
		if p.Name == pool {
			return p.Held, p.Overflow
		}
	}
	return 0, 0
}

// The wiring reads the adoption wait from config and the switches default on.
func TestNodeLimitOptions_DefaultsOn(t *testing.T) {
	cfg := config.DefaultConfig()
	var opt capacity.Options
	applyNodeLimitOptions(&opt, cfg)
	if !opt.StrictNodeLimit() || !opt.CountPoolByNode() || !opt.AdoptBeforeAdmit() {
		t.Fatal("a node limit switch defaulted off")
	}
	if opt.AdoptionTimeout != 30*time.Second {
		t.Fatalf("AdoptionTimeout = %v, want 30s", opt.AdoptionTimeout)
	}
	cfg.Autonomous.CapacityAdoptionWaitSeconds = 7
	applyNodeLimitOptions(&opt, cfg)
	if opt.AdoptionTimeout != 7*time.Second {
		t.Fatalf("AdoptionTimeout = %v, want 7s", opt.AdoptionTimeout)
	}
}

// Strict node limit: a resumed worker adopted onto a full node is held outside
// the node pool (never over its limit); off, it joins the pool as before. The
// switch is read at call time, so a flip after the ledger is built still applies.
func TestNodeLimitOptions_StrictOffRestoresOldAdopt(t *testing.T) {
	cfg := config.DefaultConfig()
	l := nlLedger(cfg)
	l.Adopt(nlReq("w1", "a"), "s1")
	l.Adopt(nlReq("w2", "a"), "s2")
	if held, over := nlNodeHeld(l, "node:a"); held != 1 || over != 1 {
		t.Fatalf("on: node:a held=%d overflow=%d, want 1/1 (never over the limit)", held, over)
	}

	cfg2 := config.DefaultConfig()
	nlSetting(cfg2, "strict", false)
	l2 := nlLedger(cfg2)
	l2.Adopt(nlReq("w1", "a"), "s1")
	l2.Adopt(nlReq("w2", "a"), "s2")
	if held, over := nlNodeHeld(l2, "node:a"); held != 2 || over != 0 {
		t.Fatalf("off: node:a held=%d overflow=%d, want 2/0 (the earlier behaviour)", held, over)
	}

	// Flipping the setting on a live ledger changes the next adoption.
	cfg3 := config.DefaultConfig()
	l3 := nlLedger(cfg3)
	nlSetting(cfg3, "strict", false)
	l3.Adopt(nlReq("w1", "a"), "s1")
	l3.Adopt(nlReq("w2", "a"), "s2")
	if held, _ := nlNodeHeld(l3, "node:a"); held != 2 {
		t.Fatalf("flipped off after build: node:a held=%d, want 2", held)
	}
}

// Pool counted by node: two sessions piled on one node count once against the
// llm pool, so the pool still has room for a free node; off, each counts.
func TestNodeLimitOptions_ByNodeOffRestoresPerLeaseCount(t *testing.T) {
	probe := nlReq("v", "b")
	cfg := config.DefaultConfig()
	nlSetting(cfg, "strict", false) // put both sessions on node a so only the pool rule differs
	for _, tc := range []struct {
		byNode bool
		want   bool
	}{{true, true}, {false, false}} {
		nlSetting(cfg, "by_node", tc.byNode)
		l := nlLedger(cfg)
		l.SetLimit("llm:m", 2)
		l.Adopt(nlReq("w1", "a"), "s1")
		l.Adopt(nlReq("w2", "a"), "s2")
		if got := l.WouldGrant(probe); got != tc.want {
			t.Errorf("count_pool_by_node=%v: a request on the free node granted=%v, want %v", tc.byNode, got, tc.want)
		}
	}
}

// Adopt before admit: with it on, BeginAdoption holds requests until adoption
// completes; off, BeginAdoption does nothing and requests are placed at once.
func TestNodeLimitOptions_AdoptFirstOffRestoresImmediateAdmit(t *testing.T) {
	cfg := config.DefaultConfig()
	l := nlLedger(cfg)
	l.BeginAdoption()
	if l.WouldGrant(nlReq("n1", "a")) {
		t.Fatal("on: a request was admitted before adoption completed")
	}
	l.MarkAdoptionComplete()
	if !l.WouldGrant(nlReq("n1", "a")) {
		t.Fatal("on: a request was held after adoption completed")
	}

	nlSetting(cfg, "adopt_first", false)
	l2 := nlLedger(cfg)
	l2.BeginAdoption()
	if !l2.WouldGrant(nlReq("n1", "a")) {
		t.Fatal("off: a request was held back for adoption")
	}
}

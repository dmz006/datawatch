package main

import "testing"

func TestParseProcStatCPUTicks_Normal(t *testing.T) {
	// utime (field 14) = 1000, stime (field 15) = 500.
	stat := "123 (bash) S 1 123 123 0 -1 4194304 100 200 300 400 1000 500 0"
	ticks, ok := parseProcStatCPUTicks(stat)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if ticks != 1500 {
		t.Fatalf("got %d ticks, want 1500", ticks)
	}
}

func TestParseProcStatCPUTicks_CommWithSpacesAndParentheses(t *testing.T) {
	// comm is "my (weird) proc"; the last ')' must be the split point so
	// utime (field 14) and stime (field 15) still land after it.
	stat := "55 (my (weird) proc) S 1 1 1 0 -1 4194304 10 0 0 0 12 34"
	ticks, ok := parseProcStatCPUTicks(stat)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if ticks != 46 {
		t.Fatalf("got %d ticks, want 46", ticks)
	}
}

func TestParseProcStatCPUTicks_ZeroTicks(t *testing.T) {
	stat := "7 (sleep) S 1 7 7 0 -1 0 0 0 0 0 0 0 0 0 0"
	ticks, ok := parseProcStatCPUTicks(stat)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if ticks != 0 {
		t.Fatalf("got %d ticks, want 0", ticks)
	}
}

func TestParseProcStatCPUTicks_Malformed(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"no closing paren",
		"(proc) S",         // too few fields after comm
		"(proc) S 1 x 1 1", // non-numeric where utime should be
	} {
		if _, ok := parseProcStatCPUTicks(in); ok {
			t.Errorf("expected parse to fail for %q", in)
		}
	}
}

func TestChildCPUTicksFromTree_SumsDescendantsOnly(t *testing.T) {
	// Tree:      10
	//            / \
	//          20   30
	//          |     |
	//          40    50
	ticksOf := map[int]uint64{
		10: 999, // root (pane shell) must NOT be counted
		20: 100,
		30: 200,
		40: 7,
		50: 3,
	}
	readTicks := func(pid int) (uint64, bool) {
		ticks, ok := ticksOf[pid]
		return ticks, ok
	}
	children := map[int][]int{
		10: {20, 30},
		20: {40},
		30: {50},
	}
	got := childCPUTicksFromTree([]int{10}, children, readTicks)
	if got != 310 {
		t.Fatalf("got %d ticks, want 310 (roots excluded)", got)
	}
}

func TestChildCPUTicksFromTree_VanishedProcessContributesZero(t *testing.T) {
	// Pid 40 died before the sampler read it: readTicks reports false for
	// it; the rest still counts.
	readTicks := func(pid int) (uint64, bool) {
		if pid == 40 {
			return 0, false
		}
		ticks, ok := map[int]uint64{20: 100, 30: 200, 50: 3}[pid]
		return ticks, ok
	}
	children := map[int][]int{
		10: {20, 30},
		20: {40},
		30: {50},
	}
	got := childCPUTicksFromTree([]int{10}, children, readTicks)
	if got != 303 {
		t.Fatalf("got %d ticks, want 303 (vanished pid contributes 0)", got)
	}
}

func TestChildCPUTicksFromTree_EmptyTree(t *testing.T) {
	readTicks := func(pid int) (uint64, bool) { return 1, true }
	if got := childCPUTicksFromTree(nil, map[int][]int{}, readTicks); got != 0 {
		t.Fatalf("got %d ticks for empty roots, want 0", got)
	}
	// Roots with no children: nothing below to count.
	if got := childCPUTicksFromTree([]int{1, 2}, map[int][]int{}, readTicks); got != 0 {
		t.Fatalf("got %d ticks for childless roots, want 0", got)
	}
}

func TestChildCPUTicksFromTree_CycleIsSafe(t *testing.T) {
	readTicks := func(pid int) (uint64, bool) { return 5, true }
	// 20 is a child of 10 and (illicitly) also a parent of 10.
	children := map[int][]int{10: {20}, 20: {10}}
	got := childCPUTicksFromTree([]int{10}, children, readTicks)
	if got != 5 {
		t.Fatalf("got %d ticks on cyclic tree, want 5 (visited once)", got)
	}
}

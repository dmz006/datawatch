package main

import (
	"context"
	"sync"
	"testing"
	"time"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestHostLoadWiring_ConfigConversion(t *testing.T) {
	got := hostLoadConfigFrom(autonomouspkg.Config{HostLoadAlert: config.HostLoadAlertConfig{Mode: "reap", Multiple: 2, Minutes: 4}})
	if got.Mode != autonomouspkg.HostLoadModeReap || got.Multiple != 2 || got.Minutes != 4 {
		t.Fatalf("converted = %+v", got)
	}
	zero := hostLoadConfigFrom(autonomouspkg.Config{})
	if zero.EffectiveMode() != autonomouspkg.HostLoadModeAlert || zero.EffectiveMultiple() != 1.5 || zero.EffectiveMinutes() != 10 {
		t.Fatalf("zero config does not read as the defaults: %+v", zero)
	}
}

func TestHostLoadWiring_SessionsMapFoldersAndState(t *testing.T) {
	list := []*session.Session{
		nil,
		{FullID: "h-1", State: session.StateRunning, ProjectDir: "/w/one", TrackingDir: "/s/one"},
		{FullID: "h-2", State: session.StateComplete, ProjectDir: "/w/two"},
		{FullID: "h-3", State: session.StateWaitingInput},
	}
	got := hostLoadSessionsFrom(list)
	if len(got) != 3 {
		t.Fatalf("got %d sessions, want 3 (nil skipped)", len(got))
	}
	if !got[0].Running || got[0].WorktreeDirs[0] != "/w/one" || got[0].ScratchDirs[0] != "/s/one" {
		t.Errorf("session 1 = %+v", got[0])
	}
	if got[1].Running || len(got[1].ScratchDirs) != 0 {
		t.Errorf("session 2 = %+v, want ended with no scratch", got[1])
	}
	if !got[2].Running || len(got[2].WorktreeDirs) != 0 {
		t.Errorf("session 3 = %+v, want running with no folders", got[2])
	}
}

func TestHostLoadWiring_CheckReadsLiveConfigEveryTick(t *testing.T) {
	var mu sync.Mutex
	mode := "off"
	alerts := 0
	check := newHostLoadCheck(
		func() autonomouspkg.Config {
			mu.Lock()
			defer mu.Unlock()
			return autonomouspkg.Config{HostLoadAlert: config.HostLoadAlertConfig{Mode: mode}}
		},
		func() []*session.Session { return nil },
		func(string, string) { alerts++ },
	)
	check.Load = func() (float64, error) { return 0, nil }
	if res := check.Check(); res.Breached || res.Load != 0 {
		t.Fatalf("mode off still evaluated: %+v", res)
	}
	mu.Lock()
	mode = "alert"
	mu.Unlock()
	check.Load = func() (float64, error) { return 1, nil }
	check.Cores = func() int { return 4 }
	if res := check.Check(); res.Limit != 6 {
		t.Fatalf("limit = %v after switching to alert, want 1.5*4", res.Limit)
	}
	if alerts != 0 {
		t.Fatalf("alert raised without a breach")
	}
}

func TestHostLoadWiring_StartStopsWithContext(t *testing.T) {
	var mu sync.Mutex
	ticks := 0
	check := newHostLoadCheck(
		func() autonomouspkg.Config { return autonomouspkg.Config{} },
		func() []*session.Session { return nil },
		func(string, string) {},
	)
	check.Load = func() (float64, error) { mu.Lock(); ticks++; mu.Unlock(); return 0, nil }
	ctx, cancel := context.WithCancel(context.Background())
	startHostLoadCheck(ctx, 5*time.Millisecond, check)
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := ticks
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("check ran %d times, want at least 2", n)
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	after := ticks
	mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if ticks != after {
		t.Fatalf("check kept running after the context ended (%d -> %d)", after, ticks)
	}
}

func TestHostLoadWiring_AlertSinkRaisesWarning(t *testing.T) {
	store, err := alertspkg.NewStore(t.TempDir() + "/alerts.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	hostLoadAlertSink(store)("Host load high", "details")
	all := store.List()
	if len(all) != 1 || all[0].Level != alertspkg.LevelWarn || all[0].Title != "Host load high" {
		t.Fatalf("alerts = %+v", all)
	}
}

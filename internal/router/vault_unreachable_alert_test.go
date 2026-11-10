package router

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

const vaultAlertKey = "secrets.vault.unreachable_alert_after_seconds"

func TestVaultUnreachableAlertConfigureCarriesKeyAndRefusal(t *testing.T) {
	wg := sync.WaitGroup{}
	var mu sync.Mutex
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	var gotKey, gotValue string
	var failWith error
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		mu.Lock()
		defer mu.Unlock()
		gotKey, gotValue = key, value
		return failWith
	})

	for _, v := range []string{"45", "0"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: vaultAlertKey + "=" + v})
		wg.Wait()
		mu.Lock()
		if gotKey != vaultAlertKey || gotValue != v {
			t.Fatalf("configureFn got %q=%q", gotKey, gotValue)
		}
		if last := sent[len(sent)-1]; !strings.Contains(last, "Set "+vaultAlertKey+" = "+v) {
			t.Fatalf("reply lacks confirmation: %q", last)
		}
		mu.Unlock()
	}

	mu.Lock()
	failWith = errors.New(vaultAlertKey + ": must be a whole number >= 0 (0 = off, default 120)")
	mu.Unlock()
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: vaultAlertKey + "=-1"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if last := sent[len(sent)-1]; !strings.Contains(last, "Configure failed") || !strings.Contains(last, "whole number >= 0") {
		t.Fatalf("refusal not carried: %q", last)
	}
}

func TestVaultUnreachableAlertConfigureHelpListsKey(t *testing.T) {
	done := make(chan string, 1)
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) { done <- s }}}
	r.SetConfigureFunc(func(string, string) error { return nil })
	r.handleConfigure(Command{Type: CmdConfigure, Text: "help"})
	if got := <-done; !strings.Contains(got, vaultAlertKey) {
		t.Errorf("configure help lacks %s", vaultAlertKey)
	}
}

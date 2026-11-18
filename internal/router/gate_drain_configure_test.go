package router

import (
	"strings"
	"testing"
)

func TestGateDrainConfigure_CheckValue(t *testing.T) {
	for _, ok := range []string{"0", "10", " 30 "} {
		if err := checkGateDrainValue(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"-1", "1.5", "x", ""} {
		if err := checkGateDrainValue(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestGateDrainConfigure_CarriedAndRefused(t *testing.T) {
	var gotKey, gotValue string
	r, wg, sent, mu := newDaemonTaskConfigureRouter(func(k, v string) error {
		gotKey, gotValue = k, v
		return nil
	})
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: gateDrainKey + "=5"})
	wg.Wait()
	if gotKey != gateDrainKey || gotValue != "5" {
		t.Fatalf("got %q=%q", gotKey, gotValue)
	}
	gotKey = ""
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: gateDrainKey + "=-2"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	last := (*sent)[len(*sent)-1]
	if gotKey != "" || !strings.Contains(last, gateDrainKey) {
		t.Fatalf("negative value should be refused before the daemon: key=%q reply=%q", gotKey, last)
	}
}

func TestGateDrainConfigure_UsageNamesKey(t *testing.T) {
	r, wg, sent, mu := newDaemonTaskConfigureRouter(func(string, string) error { return nil })
	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	for i, s := range *sent {
		if !strings.Contains(s, gateDrainKey) {
			t.Errorf("reply %d lacks %s", i, gateDrainKey)
		}
	}
}

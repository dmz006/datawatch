// The operator's decision of 2026-11-27: damaged request storage never stops
// datawatch from starting, and the operator is told what was repaired.

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
)

type wiredAlerts struct{ all []string }

func (a *wiredAlerts) raise(title, body string) { a.all = append(a.all, title+": "+body) }

func (a *wiredAlerts) want(t *testing.T, words ...string) {
	t.Helper()
	text := strings.Join(a.all, "\n")
	for _, w := range words {
		if !strings.Contains(text, w) {
			t.Fatalf("the alert does not say %q: %q", w, text)
		}
	}
}

// movedStore is a data folder after one good start: the queue is in queue.d.
func movedStore(t *testing.T, pw []byte) (cfg *config.Config, queuePath string) {
	t.Helper()
	cfg = deliveryTestConfig(t)
	queuePath = queueStorePath(cfg)
	if err := os.WriteFile(queuePath, []byte(oldQueue), 0o600); err != nil {
		t.Fatal(err)
	}
	w := deliveryTestWiring()
	if err := w.openKey(cfg, pw); err != nil {
		t.Fatal(err)
	}
	q, err := openStartupQueue(queuePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := w.start(ctx, deliveryDeps{cfg: cfg, sessions: wireTestManager(t, cfg), queue: q, firstPassDelay: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if !queueMoved(queuePath) {
		t.Fatal("the queue did not move")
	}
	return cfg, queuePath
}

// startAgain is the next daemon start, as main.go does it.
func startAgain(t *testing.T, cfg *config.Config, pw []byte) (*deliveryWiring, *wiredAlerts, *delivery.Engine, func() int) {
	t.Helper()
	w := deliveryTestWiring()
	if err := w.openKeyAtStart(cfg, pw); err != nil {
		t.Fatalf("the key stopped the start: %v", err)
	}
	q, err := openStartupQueue(queueStorePath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	alerts := &wiredAlerts{}
	e, err := w.start(ctx, deliveryDeps{cfg: cfg, sessions: wireTestManager(t, cfg), queue: q, alert: alerts.raise,
		firstPassDelay: time.Hour})
	if err != nil {
		t.Fatalf("damaged request storage stopped the start: %v", err)
	}
	return w, alerts, e, func() int {
		items, err := q.ListErr("", "")
		if err != nil {
			return -1
		}
		return len(items)
	}
}

func TestReqWireADamagedManifestIsRepairedAndReported(t *testing.T) {
	cfg, _ := movedStore(t, []byte("pw"))
	if err := os.WriteFile(filepath.Join(cfg.DataDir, requestsDir, "meta.rec"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, alerts, e, queued := startAgain(t, cfg, []byte("pw"))
	if w.requestStore() == nil || queued() < 1 {
		t.Fatalf("requests are not on after the repair: %v, %d queued", w.requestStore(), queued())
	}
	alerts.want(t, "repaired", "Rebuilt: the manifest", "repair-", "Nothing was marked done")
	rep := e.Report()
	var keys []string
	for _, en := range append(append([]delivery.Entry(nil), rep.Items...), rep.Backlog...) {
		keys = append(keys, en.Key)
	}
	if !strings.Contains(strings.Join(keys, " "), "requests_repaired") {
		t.Fatalf("the repair is not on the delivery list: %v", keys)
	}
}

func TestReqWireADamagedRecordDoesNotStopTheOthers(t *testing.T) {
	cfg, _ := movedStore(t, nil)
	files, err := filepath.Glob(filepath.Join(cfg.DataDir, requestsDir, "q-*.rec"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no queue item file: %v", err)
	}
	if err := os.WriteFile(files[0], []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, alerts, _, queued := startAgain(t, cfg, nil)
	if w.requestStore() == nil || queued() < 0 {
		t.Fatal("requests and the queue must work")
	}
	alerts.want(t, "set aside and kept", filepath.Base(files[0]))
}

func TestReqWireAWrongKeyTurnsRequestsOffAndStarts(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, cfg *config.Config) []byte{
		"the password does not open the key file": func(*testing.T, *config.Config) []byte { return []byte("other") },
		"a damaged key file": func(t *testing.T, cfg *config.Config) []byte {
			if err := os.WriteFile(filepath.Join(cfg.DataDir, filepath.FromSlash(delivery.KeyFile)), []byte("junk"), 0o600); err != nil {
				t.Fatal(err)
			}
			return []byte("pw")
		},
		"a missing key file": func(t *testing.T, cfg *config.Config) []byte {
			if err := os.Remove(filepath.Join(cfg.DataDir, filepath.FromSlash(delivery.KeyFile))); err != nil {
				t.Fatal(err)
			}
			return []byte("pw")
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, _ := movedStore(t, []byte("pw"))
			pw := damage(t, cfg)
			before := filesUnder(t, cfg.DataDir)
			w, alerts, e, queued := startAgain(t, cfg, pw)
			if w.requestStore() != nil || e != nil || queued() != -1 {
				t.Fatalf("requests, the check and the moved queue must be off: %v %v %d", w.requestStore(), e, queued())
			}
			alerts.want(t, "Delivery requests are off", "Nothing", "delivery check")
			after := filesUnder(t, cfg.DataDir)
			for f, body := range before {
				if after[f] != body {
					t.Errorf("%s changed", f)
				}
			}
			for f := range after {
				if _, ok := before[f]; !ok && (strings.Contains(f, requestsDir) || strings.HasSuffix(f, ".rec")) {
					t.Errorf("%s appeared", f)
				}
			}
		})
	}
}

// Starting without secure mode over sealed records is refused as before: it is
// the wrong mode, not damage.
func TestReqWireANonSecureStartOverSealedRecordsIsStillRefused(t *testing.T) {
	cfg, _ := movedStore(t, []byte("pw"))
	if err := deliveryTestWiring().openKeyAtStart(cfg, nil); !errors.Is(err, delivery.ErrStoreKey) {
		t.Fatalf("%v", err)
	}
}

func TestReqWireAnUnrepairableStoreTurnsOnlyRequestsOff(t *testing.T) {
	cfg, path := movedStore(t, nil)
	if err := os.WriteFile(filepath.Join(cfg.DataDir, requestsDir, "meta.rec"), []byte(`{"version":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := filesUnder(t, filepath.Join(cfg.DataDir, requestsDir))
	w, alerts, e, queued := startAgain(t, cfg, nil)
	if w.requestStore() != nil || e == nil || queued() != -1 {
		t.Fatalf("only requests (and the moved queue) are off: %v %v %d", w.requestStore(), e, queued())
	}
	alerts.want(t, "Delivery requests are off", "Nothing in it was changed", "work queue")
	after := filesUnder(t, filepath.Join(cfg.DataDir, requestsDir))
	if len(after) != len(before) || after["/meta.rec"] != before["/meta.rec"] {
		t.Fatalf("queue.d changed: %v", after)
	}
	if !queueMoved(path) {
		t.Fatal("queue.json changed")
	}
}

func TestReqWireADamagedStoreWithTheCheckOffStillStarts(t *testing.T) {
	cfg, _ := movedStore(t, nil)
	if err := os.WriteFile(filepath.Join(cfg.DataDir, requestsDir, "meta.rec"), []byte(`{"version":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	off := false
	cfg.Delivery.Enabled = &off
	_, alerts, _, queued := startAgain(t, cfg, nil)
	if queued() != -1 {
		t.Fatal("the moved queue cannot be read")
	}
	alerts.want(t, "Delivery requests are off")
}

// The daemon itself uses the start-time key open and passes its alerts.
func TestReqWireDaemonStartsOverADamagedStore(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"loadConfigDeriveKeys(deliveryWire.openKeyAtStart)",
		"deliveryWire.openKeyAtStart(cfg, nil)",
		"alertStore.AddSystem(alertspkg.LevelError, title, body)\n\t\t\t}, queue: queueStore}); err != nil {",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("main.go lacks %q", want)
		}
	}
}

// A plain install looks through the records for sealed ones before it opens
// them: a record that cannot be read must not stop the start there.
func TestReqWireAnUnreadableRecordDoesNotStopAPlainStart(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	cfg, _ := movedStore(t, nil)
	files, err := filepath.Glob(filepath.Join(cfg.DataDir, requestsDir, "q-*.rec"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no queue item file: %v", err)
	}
	if err := os.Chmod(files[0], 0); err != nil {
		t.Fatal(err)
	}
	w, alerts, _, queued := startAgain(t, cfg, nil)
	if w.requestStore() == nil || queued() < 0 {
		t.Fatal("the others must work")
	}
	alerts.want(t, "set aside and kept", filepath.Base(files[0]))
}

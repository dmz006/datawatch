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
	pipelinePkg "github.com/dmz006/datawatch/internal/pipeline"
	"github.com/dmz006/datawatch/internal/secfile"
	"github.com/dmz006/datawatch/internal/session"
)

func deliveryTestWiring() *deliveryWiring {
	w := newDeliveryWiring()
	w.keyParams = delivery.KeyParams{Time: 1, MemoryKiB: 8 * 1024, Threads: 1}
	return w
}

func deliveryTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.Hostname = "host1"
	cfg.Autonomous.Enabled = false
	return cfg
}

// filesUnder lists every file below dir with its content.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			out[strings.TrimPrefix(p, dir)] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A start that stops at a later store has written nothing: the key is only
// held in memory until the wiring starts, which is after every store opened.
func TestAFailedStartWritesNoDeliveryRecord(t *testing.T) {
	cfg := deliveryTestConfig(t)
	queue := filepath.Join(cfg.DataDir, "queue.json")
	if err := os.WriteFile(queue, []byte(`{"items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := filesUnder(t, cfg.DataDir)
	w := deliveryTestWiring()
	if err := w.openKey(cfg, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	w.filterFailed("f1", "host1-ab12", "send_failed") // before the engine exists
	after := filesUnder(t, cfg.DataDir)
	if len(after) != len(before) || after["/queue.json"] != before["/queue.json"] {
		t.Fatalf("changed before start: %v", after)
	}
}

func TestAWrongPasswordStopsTheStart(t *testing.T) {
	cfg := deliveryTestConfig(t)
	w := deliveryTestWiring()
	if err := w.openKey(cfg, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if err := w.key.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, pw := range [][]byte{[]byte("other"), nil} {
		if err := deliveryTestWiring().openKey(cfg, pw); !errors.Is(err, delivery.ErrStoreKey) {
			t.Fatalf("password %q: %v", pw, err)
		}
	}
}

func TestStartRunsTheCheckAndListsAFailedFilterSend(t *testing.T) {
	cfg := deliveryTestConfig(t)
	w := deliveryTestWiring()
	if err := w.openKey(cfg, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	// Recorded before the engine exists, so it must be kept.
	w.filterFailed("f1", "host1-ab12", "send_failed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e, err := w.start(ctx, deliveryDeps{cfg: cfg, firstPassDelay: time.Hour})
	if err != nil || e == nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := e.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	r := e.Report()
	var ids []string
	for _, en := range append(append([]delivery.Entry(nil), r.Items...), r.Backlog...) {
		ids = append(ids, en.Key)
	}
	if !strings.Contains(strings.Join(ids, " "), "filter:f1@host1-ab12:send_failed") {
		t.Fatalf("listed: %v", ids)
	}
	// A secure install: the key is written, and the records are sealed.
	files := filesUnder(t, cfg.DataDir)
	if _, ok := files["/"+delivery.KeyFile]; !ok {
		t.Fatalf("no key file: %v", files)
	}
	for name, body := range files {
		if strings.HasSuffix(name, ".rec") && strings.Contains(body, "host1-ab12") {
			t.Fatalf("%s is plain", name)
		}
	}
}

func TestDisabledStartsNoCheckAndWritesNothing(t *testing.T) {
	cfg := deliveryTestConfig(t)
	off := false
	cfg.Delivery.Enabled = &off
	w := deliveryTestWiring()
	if err := w.openKey(cfg, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	e, err := w.start(context.Background(), deliveryDeps{cfg: cfg})
	if err != nil || e != nil {
		t.Fatalf("e=%v err=%v", e, err)
	}
	if files := filesUnder(t, cfg.DataDir); len(files) != 0 {
		t.Fatalf("written: %v", files)
	}
}

// imap-mcp with no recipient set would be sent to a made-up one, so an
// accepted send would not mean anyone was told.
func TestIMAPWithoutARecipientIsNotAChannel(t *testing.T) {
	cfg := deliveryTestConfig(t)
	w := deliveryTestWiring()
	send := func(string) error { return nil }
	w.addRouterChannel(cfg, "imap_mcp", send)
	w.addRouterChannel(cfg, "signal", send)
	w.addRouterChannel(cfg, "signal", send)
	cfg.ImapMcp.To = "someone@example.invalid"
	w.addRouterChannel(cfg, "imap_mcp", send)
	var names []string
	for _, c := range w.channelList() {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, " "); got != "signal signal#2 imap_mcp" {
		t.Fatalf("channels=%q", got)
	}
}

// A refused or failed typed send by an output filter is listed.
func TestFilterSendFailuresAreRecorded(t *testing.T) {
	w := deliveryTestWiring()
	h := w.wrapFilterHandlers(session.ActionHandlers{
		MayType: func(_, id string) error {
			if id == "refused" {
				return errors.New("no")
			}
			return nil
		},
		SendInput:   func(string, string, string) error { return errors.New("pane gone: secret words") },
		AddSchedule: func(string, string, string) error { return errors.New("store full") },
	})
	if h.MayType("s1", "refused") == nil || h.SendInput("s1", "f1", "secret words") == nil || h.AddSchedule("s1", "f2", "secret words") == nil {
		t.Fatal("the wrapped handlers must return the same errors")
	}
	got := map[string]bool{}
	for _, c := range w.pending {
		got[c.ID+":"+c.State] = true
		if strings.Contains(c.ID+c.State+c.Problem, "secret") {
			t.Fatalf("content recorded: %+v", c)
		}
	}
	for _, want := range []string{"refused@s1:refused", "f1@s1:send_failed", "f2@s1:schedule_failed"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	// A filter that matches every line is recorded once in a while, not each time.
	n := len(w.pending)
	for i := 0; i < 100; i++ {
		_ = h.SendInput("s1", "f1", "x")
	}
	if len(w.pending) != n {
		t.Errorf("recorded %d more times", len(w.pending)-n)
	}
}

// A start that stopped between writing the key and sealing the plain records
// is finished by the next one, and the plain pipeline journal is sealed at
// the secure start, not at the next pipeline.
func TestASecureStartSealsWhatWasPlainEvenAfterAStopInBetween(t *testing.T) {
	cfg := deliveryTestConfig(t)
	dir := filepath.Join(cfg.DataDir, delivery.HeartbeatDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A non-secure run leaves plain records.
	plain := deliveryTestWiring()
	if err := plain.openKey(cfg, nil); err != nil {
		t.Fatal(err)
	}
	e, err := plain.start(ctx, deliveryDeps{cfg: cfg, firstPassDelay: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dir, "pipelines.rec")
	if err := os.WriteFile(journal, []byte(`{"version":1,"runs":[{"id":"pipe-1","state":"failed","started_at":"2026-11-01T00:00:00Z","ended_at":"2026-11-01T01:00:00Z"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The first secure start writes the key and stops.
	first := deliveryTestWiring()
	if err := first.openKey(cfg, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if err := first.key.Commit(); err != nil {
		t.Fatal(err)
	}
	// The next one finishes.
	w := deliveryTestWiring()
	if err := w.openKey(cfg, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	e, err = w.start(ctx, deliveryDeps{cfg: cfg, pipelines: pipelinePkg.NewExecutor(nil, "shell"), firstPassDelay: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"state.rec", "startmark.rec", "pipelines.rec"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !secfile.IsEncrypted(data) {
			t.Fatalf("%s is not sealed at the start: %v", name, err)
		}
	}
	r, err := e.Pass(ctx)
	if err != nil || strings.Contains(strings.Join(r.Red, ","), delivery.RedRecords) {
		t.Fatalf("err=%v red=%v", err, r.Red)
	}
}

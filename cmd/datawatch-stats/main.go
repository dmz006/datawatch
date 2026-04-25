// datawatch-stats — Shape B standalone observer daemon.
//
// Reuses internal/observer end-to-end. Task 1 of BL172 (S11): wire
// the collector and emit snapshots locally. Task 3 will add the HTTPS
// push loop to the parent's /api/observer/peers/{name}/stats endpoint;
// today --print and --serve cover the operator's "what would I send?"
// debugging case.
//
// Flags:
//
//	--datawatch <url>     primary parent URL (used by Task 3 push loop)
//	--name <peer-name>    stable peer name; defaults to hostname
//	--push-interval <dur> snapshot cadence (default 5 s, min 1 s)
//	--listen <addr>       optional /api/stats sidecar listener (e.g. :9001)
//	--ebpf-enabled <s>    auto / true / false; default auto
//	--once                print one snapshot to stdout and exit (debugging)
//	--print               print every snapshot to stdout (debugging)
//
// See docs/plans/2026-04-25-bl172-shape-b-standalone-daemon.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dmz006/datawatch/internal/observer"
)

var Version = "dev"

func main() {
	var (
		parentURL    = flag.String("datawatch", "", "primary datawatch URL (peer push target)")
		peerName     = flag.String("name", "", "stable peer name (defaults to hostname)")
		pushInterval = flag.Duration("push-interval", 5*time.Second, "snapshot cadence; min 1s")
		listenAddr   = flag.String("listen", "", "optional sidecar /api/stats listen address (e.g. :9001)")
		ebpfMode     = flag.String("ebpf-enabled", "auto", "auto / true / false")
		tokenPath    = flag.String("token-file", "", "path to persist the parent-issued bearer token (default: $HOME/.datawatch-stats/peer.token)")
		insecureTLS  = flag.Bool("insecure-tls", false, "skip TLS verify when posting to --datawatch (dev / self-signed)")
		shape        = flag.String("shape", "B", "deployment shape: B (standalone host) | C (cluster container — DCGM + k8s metrics + mandatory eBPF)")
		dcgmURL      = flag.String("dcgm-url", "", "DCGM exporter URL for per-pid GPU metrics (Shape C). Defaults to http://localhost:9400/metrics on Shape C.")
		once         = flag.Bool("once", false, "print one snapshot to stdout and exit")
		printEvery   = flag.Bool("print", false, "print every snapshot to stdout")
		showVersion  = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("datawatch-stats %s\n", Version)
		return
	}

	if *pushInterval < time.Second {
		*pushInterval = time.Second
	}
	if *peerName == "" {
		if h, err := os.Hostname(); err == nil {
			*peerName = h
		} else {
			*peerName = "unknown"
		}
	}

	cfg := observer.DefaultConfig()
	cfg.EBPFEnabled = *ebpfMode
	// Standalone daemon does not host sessions of its own — turn off
	// the session-attribution pass since there's nothing to attribute to.
	cfg.Envelopes.SessionAttribution = false

	// BL173 — Shape C tweaks: longer push interval (cluster scale),
	// eBPF mandatory (loader still degrades gracefully on missing
	// CAP_BPF, but the operator manifest must grant it).
	if strings.ToUpper(*shape) == "C" {
		if *pushInterval == 5*time.Second {
			*pushInterval = 10 * time.Second
		}
		if cfg.EBPFEnabled == "auto" {
			cfg.EBPFEnabled = "true"
		}
		if *dcgmURL == "" {
			*dcgmURL = "http://localhost:9400/metrics"
		}
		fmt.Fprintf(os.Stderr, "[stats] shape C — DCGM %s, push %s\n", *dcgmURL, *pushInterval)
	}

	col := observer.NewCollector(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// BL173 task 2 — DCGM scrape feeds per-pid GPU metrics into the
	// observer's GPU envelope. Nil-safe when --dcgm-url is empty.
	dcgm := observer.NewDCGMScraper(*dcgmURL, 5*time.Second)
	if dcgm != nil {
		dcgm.Start(ctx)
	}

	col.Start(ctx)

	// One-shot mode: wait one tick then dump and exit.
	if *once {
		time.Sleep(time.Duration(cfg.TickIntervalMs)*time.Millisecond + 200*time.Millisecond)
		if snap := col.Latest(); snap != nil {
			emitSnapshot(os.Stdout, snap, *peerName)
		} else {
			fmt.Fprintln(os.Stderr, "[stats] collector produced no snapshot — wait longer or check /proc access")
			os.Exit(1)
		}
		col.Stop()
		return
	}

	// Optional sidecar listener so a local operator can curl :9001/api/stats
	// without going through the parent — useful on Ollama / GPU boxes.
	if *listenAddr != "" {
		go serveSidecar(*listenAddr, col, *peerName)
		fmt.Fprintf(os.Stderr, "[stats] sidecar listener on %s\n", *listenAddr)
	}

	// Set up the peer client when --datawatch is supplied. Without
	// it we run as a local-only collector (sidecar / debug mode).
	var peer *peerClient
	if *parentURL != "" {
		tp := *tokenPath
		if tp == "" {
			home, _ := os.UserHomeDir()
			tp = filepath.Join(home, ".datawatch-stats", "peer.token")
		}
		var err error
		peer, err = newPeerClient(*parentURL, *peerName, tp, *insecureTLS)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[stats] peer client: %v\n", err)
			os.Exit(1)
		}
		peer.loadToken()
		if !peer.hasToken() {
			regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := peer.register(regCtx, "B", Version, runtimeHostInfo())
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[stats] register failed: %v (will retry on first push)\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "[stats] registered with %s as %s\n", *parentURL, *peerName)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[stats] reusing persisted token for %s\n", *peerName)
		}
	}

	fmt.Fprintf(os.Stderr, "[stats] datawatch-stats %s started — name=%s push=%s ebpf=%s\n",
		Version, *peerName, *pushInterval, *ebpfMode)

	ticker := time.NewTicker(*pushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			col.Stop()
			fmt.Fprintln(os.Stderr, "[stats] shutting down")
			return
		case <-ticker.C:
			snap := col.Latest()
			if snap == nil {
				continue
			}
			if *printEvery {
				emitSnapshot(os.Stdout, snap, *peerName)
			}
			if peer != nil {
				pushCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				if err := peer.push(pushCtx, snap, "B", Version, runtimeHostInfo()); err != nil {
					fmt.Fprintf(os.Stderr, "[stats] push: %v\n", err)
				}
				cancel()
			}
		}
	}
}

// emitSnapshot serialises one StatsResponse v2 with the Shape B
// envelope wrapping (shape, peer_name) attached. Format mirrors what
// Task 3 will POST to the parent.
func emitSnapshot(w *os.File, snap *observer.StatsResponse, peerName string) {
	wrap := map[string]any{
		"shape":     "B",
		"peer_name": peerName,
		"snapshot":  snap,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(wrap); err != nil {
		fmt.Fprintf(os.Stderr, "[stats] encode: %v\n", err)
	}
}

// serveSidecar exposes a minimal /api/stats endpoint locally. No auth —
// bind to 127.0.0.1 if you don't want it exposed.
func serveSidecar(addr string, col *observer.Collector, peerName string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		snap := col.Latest()
		if snap == nil {
			http.Error(w, "no snapshot yet", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"shape":     "B",
			"peer_name": peerName,
			"snapshot":  snap,
		})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "[stats] sidecar listener: %v\n", err)
	}
}

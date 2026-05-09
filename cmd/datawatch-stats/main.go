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
	"github.com/dmz006/datawatch/internal/observerpeer"
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
		// v6.22.5 — operator: "datawatch-stats has a bug on -help not
		// being --help, but it also does not have an option to setup
		// ebpf and it needs an easy way to get output of session
		// connections so it can be debugged."
		debugConn = flag.Bool("debug-connections", false, "dump every register/push attempt (URL, token-prefix, status, body snippet) to stderr — for diagnosing remote-Node connection failures")
		printOnce = flag.Bool("print-once", false, "send one push (or print one snapshot) with --debug-connections enabled, then exit; combines --once + --debug-connections for fast diagnosis")
		// Operator-aliases: -help and -h should print usage and exit 0
		// (Go's flag pkg by default exits 2 with no banner). We register
		// no-op flags + intercept Parse errors below so the operator's
		// muscle memory works.
		helpAlias  = flag.Bool("help", false, "show this help and exit (alias for -h)")
		helpAliasH = flag.Bool("h", false, "show this help and exit")
		// v6.22.6 — operator: 'no option to setup ebpf'. Prints kernel
		// version, CAP_BPF probe result, exact setcap command, kernel
		// config requirements, and a systemd unit fragment.
		setupEBPF = flag.Bool("setup-ebpf", false, "print kernel/CAP_BPF/setcap diagnostic + setup instructions (eBPF probe loader requirements) and exit")
	)
	// BL290 — operator wants help text to show double-dash flag form so it
	// matches docs (`--datawatch`, `--insecure-tls`, etc.). Go's stdlib
	// `flag` package emits single-dash by default but accepts both forms,
	// so this is purely cosmetic + doc-consistency. Single-dash continues
	// to work for backward compat.
	flag.CommandLine.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "datawatch-stats — Shape B/C standalone observer daemon (version %s)\n\n", Version)
		fmt.Fprintln(flag.CommandLine.Output(), "Usage of datawatch-stats:")
		flag.VisitAll(func(f *flag.Flag) {
			defaultStr := ""
			if f.DefValue != "" && f.DefValue != "false" {
				defaultStr = fmt.Sprintf(" (default %q)", f.DefValue)
			}
			fmt.Fprintf(flag.CommandLine.Output(), "  --%-18s %s%s\n", f.Name, f.Usage, defaultStr)
		})
	}
	flag.Parse()

	if *helpAlias || *helpAliasH {
		flag.CommandLine.Usage()
		return
	}

	if *showVersion {
		fmt.Printf("datawatch-stats %s\n", Version)
		return
	}

	if *setupEBPF {
		runSetupEBPF()
		return
	}

	// v6.22.5 — --print-once = --once + --debug-connections + --print
	// for fast diagnosis. Sets the underlying flags so downstream code
	// behaves consistently.
	if *printOnce {
		*once = true
		*debugConn = true
		*printEvery = true
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

	// BL173 task 4 — k8s metrics-server scrape for Shape C. Nil when
	// not running inside a pod (KUBERNETES_SERVICE_HOST unset).
	if strings.ToUpper(*shape) == "C" {
		k8sMetrics := observer.NewK8sMetricsScraper(60 * time.Second)
		if k8sMetrics != nil {
			k8sMetrics.Start(ctx)
			// Fold the scraper's snapshot into snap.Cluster.Nodes on
			// every observer tick. Latest() returns a copy so
			// concurrent collector reads are safe.
			col.SetClusterNodesFn(k8sMetrics.Latest)
			fmt.Fprintln(os.Stderr, "[stats] k8s metrics-server scraper started → snap.Cluster.Nodes")
		}
	}

	col.Start(ctx)

	// v6.22.5 — peer client setup hoisted above the --once block so
	// --print-once can do a real register+push round-trip with debug
	// tracing. Without --datawatch this is a no-op (peer stays nil).
	var peer *observerpeer.Client
	if *parentURL != "" {
		tp := *tokenPath
		if tp == "" {
			home, _ := os.UserHomeDir()
			tp = filepath.Join(home, ".datawatch-stats", "peer.token")
		}
		var err error
		peer, err = observerpeer.New(observerpeer.Config{
			ParentURL: *parentURL,
			Name:      *peerName,
			Shape:     strings.ToUpper(*shape),
			TokenPath: tp,
			Insecure:  *insecureTLS,
			Debug:     *debugConn, // v6.22.5 — per-request stderr trace
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[stats] peer client: %v\n", err)
			os.Exit(1)
		}
		peer.LoadToken()
		if !peer.HasToken() {
			regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := peer.Register(regCtx, Version, observerpeer.HostInfo())
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

	// One-shot mode: wait one tick, dump, optionally push, then exit.
	// Hoisted below peer setup so --print-once exercises the round-trip.
	if *once {
		time.Sleep(time.Duration(cfg.TickIntervalMs)*time.Millisecond + 200*time.Millisecond)
		snap := col.Latest()
		if snap == nil {
			fmt.Fprintln(os.Stderr, "[stats] collector produced no snapshot — wait longer or check /proc access")
			os.Exit(1)
		}
		if *printEvery {
			emitSnapshot(os.Stdout, snap, *peerName, *shape)
		}
		if peer != nil {
			pushCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			err := peer.Push(pushCtx, snap, Version, observerpeer.HostInfo())
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[stats] one-shot push: %v\n", err)
				col.Stop()
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "[stats] one-shot push succeeded")
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

	fmt.Fprintf(os.Stderr, "[stats] datawatch-stats %s started — name=%s push=%s ebpf=%s debug=%v\n",
		Version, *peerName, *pushInterval, *ebpfMode, *debugConn)

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
				emitSnapshot(os.Stdout, snap, *peerName, *shape)
			}
			if peer != nil {
				pushCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				if err := peer.Push(pushCtx, snap, Version, observerpeer.HostInfo()); err != nil {
					fmt.Fprintf(os.Stderr, "[stats] push: %v\n", err)
				}
				cancel()
			}
		}
	}
}

// runSetupEBPF prints a diagnostic + setup recipe for getting eBPF
// probes loaded on the current host. v6.22.6 — operator: 'no option
// to setup ebpf'. Operator runs `datawatch-stats --setup-ebpf` once
// per Node and follows the printed instructions.
func runSetupEBPF() {
	fmt.Println("datawatch-stats — eBPF setup diagnostic + recipe")
	fmt.Println("==================================================")

	// Kernel version.
	if b, err := os.ReadFile("/proc/version"); err == nil {
		fmt.Printf("\nKernel:\n  %s", string(b))
	} else {
		fmt.Printf("\nKernel: (could not read /proc/version: %v)\n", err)
	}

	// CAP_BPF probe.
	hasCap := observer.ProbeBPFCapability()
	fmt.Println("\nCAP_BPF probe (this binary's effective capabilities):")
	if hasCap {
		fmt.Println("  ✓ CAP_BPF is granted — eBPF probe loader will run.")
	} else {
		fmt.Println("  ✗ CAP_BPF is NOT granted on this binary.")
		exe, _ := os.Executable()
		fmt.Println("\n  To grant CAP_BPF (preferred over running as root):")
		fmt.Printf("    sudo setcap cap_bpf,cap_perfmon,cap_net_admin+ep %s\n", exe)
		fmt.Println("\n  Then verify with:")
		fmt.Printf("    getcap %s\n", exe)
		fmt.Println("\n  Note: setcap is lost when the binary is replaced (e.g. on")
		fmt.Println("  upgrade). Re-run after every install. Consider deploying via")
		fmt.Println("  a systemd unit with AmbientCapabilities (see below).")
	}

	// Kernel config check.
	fmt.Println("\nKernel config requirements (CONFIG_*):")
	fmt.Println("  CONFIG_BPF=y           CONFIG_BPF_SYSCALL=y     CONFIG_BPF_JIT=y")
	fmt.Println("  CONFIG_HAVE_EBPF_JIT=y CONFIG_BPF_EVENTS=y      CONFIG_KPROBES=y")
	fmt.Println("  CONFIG_PERF_EVENTS=y   CONFIG_FUNCTION_TRACER=y CONFIG_FTRACE=y")
	fmt.Println("\n  Check with one of:")
	fmt.Println("    zcat /proc/config.gz | grep -E 'BPF|KPROBES|PERF_EVENTS|FTRACE'")
	fmt.Println("    grep -E 'BPF|KPROBES|PERF_EVENTS|FTRACE' /boot/config-$(uname -r)")
	fmt.Println("\n  Most distros from kernel 5.8+ ship these by default.")

	// systemd unit fragment.
	exe, _ := os.Executable()
	fmt.Println("\nState directory (must be created + owned BEFORE the unit starts):")
	fmt.Println("  # Order matters — create, then chown, then chmod, then write the token")
	fmt.Println("  # so the token file inherits the datawatch user instead of root.")
	fmt.Println("  id datawatch || sudo useradd -r -s /usr/sbin/nologin datawatch")
	fmt.Println("  sudo mkdir -p /var/lib/datawatch-stats")
	fmt.Println("  sudo chown -R datawatch:datawatch /var/lib/datawatch-stats")
	fmt.Println("  sudo chmod 700 /var/lib/datawatch-stats")
	fmt.Println()
	fmt.Println("systemd unit fragment (recommended; runs as datawatch user with")
	fmt.Println("ambient caps so eBPF works without root):")
	fmt.Println("  # /etc/systemd/system/datawatch-stats.service")
	fmt.Println("  [Unit]")
	fmt.Println("  Description=datawatch-stats peer collector")
	fmt.Println("  After=network-online.target")
	fmt.Println("  Wants=network-online.target")
	fmt.Println()
	fmt.Println("  [Service]")
	fmt.Println("  Type=simple")
	fmt.Println("  User=datawatch")
	fmt.Println("  Group=datawatch")
	fmt.Printf("  ExecStart=%s --datawatch https://YOUR-PRIMARY:8443 --insecure-tls\n", exe)
	fmt.Println("  AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_NET_ADMIN")
	fmt.Println("  CapabilityBoundingSet=CAP_BPF CAP_PERFMON CAP_NET_ADMIN")
	fmt.Println("  NoNewPrivileges=true")
	fmt.Println("  Restart=on-failure")
	fmt.Println("  RestartSec=5")
	fmt.Println()
	fmt.Println("  [Install]")
	fmt.Println("  WantedBy=multi-user.target")
	fmt.Println()
	fmt.Println("  After writing the unit file:")
	fmt.Println("    sudo systemctl daemon-reload")
	fmt.Println("    sudo systemctl enable --now datawatch-stats")
	fmt.Println("    sudo journalctl -u datawatch-stats -f")
	fmt.Println()
	fmt.Println("Then re-run:  datawatch-stats --setup-ebpf")
	fmt.Println("to confirm CAP_BPF probe passes.")
}

// emitSnapshot serialises one StatsResponse v2 with the Shape B
// envelope wrapping (shape, peer_name) attached. Format mirrors what
// Task 3 will POST to the parent.
func emitSnapshot(w *os.File, snap *observer.StatsResponse, peerName, shape string) {
	if shape == "" {
		shape = "B"
	}
	wrap := map[string]any{
		"shape":     strings.ToUpper(shape),
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

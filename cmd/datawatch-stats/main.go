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
	"os/exec"
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
		parentURL    = flag.String("datawatch", "", "primary datawatch URL (peer push target). v7.0.0+: comma-separated for multi-parent (e.g. https://primary:8443,https://secondary:8443) — pushes the same snapshot to each. Per-parent token persisted independently.")
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
		// v7.0.0-alpha.6 — operator: 'containers have no envelope...
		// processes have no details... add debugging to the
		// communications we extended the stats for so it can debug
		// issues with functionality on demand'. Probes each envelope
		// collection kind and reports specific failure reasons +
		// suggested fixes (#211).
		diag = flag.Bool("diag", false, "probe each envelope-collection kind (docker, /proc, DCGM, eBPF, ollama-tap) + print specific failure reasons + suggested fixes; for diagnosing empty/failed envelopes")
	)
	// BL290 — operator wants help text to show double-dash flag form so it
	// matches docs (`--datawatch`, `--insecure-tls`, etc.). Go's stdlib
	// `flag` package emits single-dash by default but accepts both forms,
	// so this is purely cosmetic + doc-consistency. Single-dash continues
	// to work for backward compat.
	flag.CommandLine.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "datawatch-stats — Shape B/C standalone observer daemon (version %s)\n\n", Version)
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "Usage of datawatch-stats:")
		flag.VisitAll(func(f *flag.Flag) {
			defaultStr := ""
			if f.DefValue != "" && f.DefValue != "false" {
				defaultStr = fmt.Sprintf(" (default %q)", f.DefValue)
			}
			_, _ = fmt.Fprintf(flag.CommandLine.Output(), "  --%-18s %s%s\n", f.Name, f.Usage, defaultStr)
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

	if *diag {
		runDiagnostic()
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
	//
	// v7.0.0-alpha.8 (#203) — multi-parent support. --datawatch accepts
	// comma-separated URLs OR can be passed multiple times via env
	// (DATAWATCH_PARENTS env var, comma-separated). Each parent gets
	// its own Client with independent token persisted as
	// peer-<sanitized-host>.token (legacy peer.token kept for
	// single-parent backwards compat).
	parents := splitParentURLs(*parentURL)
	if env := os.Getenv("DATAWATCH_PARENTS"); env != "" {
		parents = append(parents, splitParentURLs(env)...)
	}
	parents = uniqueStrings(parents)
	var peers []*observerpeer.Client
	for _, parent := range parents {
		tp := *tokenPath
		if tp == "" {
			home, _ := os.UserHomeDir()
			if len(parents) == 1 {
				tp = filepath.Join(home, ".datawatch-stats", "peer.token")
			} else {
				tp = filepath.Join(home, ".datawatch-stats", "peer-"+sanitizeForFile(parent)+".token")
			}
		} else if len(parents) > 1 {
			// Operator supplied a single token-file path with multi-
			// parents — log a warning + namespace per-parent next to it.
			fmt.Fprintf(os.Stderr, "[stats] WARN: --token-file with multiple parents — namespacing as %s.<parent>\n", tp)
			tp = tp + "." + sanitizeForFile(parent)
		}
		p, err := observerpeer.New(observerpeer.Config{
			ParentURL: parent,
			Name:      *peerName,
			Shape:     strings.ToUpper(*shape),
			TokenPath: tp,
			Insecure:  *insecureTLS,
			Debug:     *debugConn,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[stats] peer client (%s): %v\n", parent, err)
			os.Exit(1)
		}
		p.LoadToken()
		if !p.HasToken() {
			regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := p.Register(regCtx, Version, observerpeer.HostInfo())
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[stats] register %s failed: %v (will retry on first push)\n", parent, err)
			} else {
				fmt.Fprintf(os.Stderr, "[stats] registered with %s as %s\n", parent, *peerName)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[stats] reusing persisted token for %s @ %s\n", *peerName, parent)
		}
		peers = append(peers, p)
	}
	// Backwards-compat — keep the original `peer` variable for the
	// --once / --print-once block by pointing at the first parent.
	var peer *observerpeer.Client
	if len(peers) > 0 {
		peer = peers[0]
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
			// v7.0.0-alpha.8 (#203) — push snapshot to every parent.
			// One goroutine per push so a slow parent doesn't delay
			// others; per-parent timeout/retry handled inside .Push.
			for _, p := range peers {
				go func(p *observerpeer.Client) {
					pushCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
					if err := p.Push(pushCtx, snap, Version, observerpeer.HostInfo()); err != nil {
						fmt.Fprintf(os.Stderr, "[stats] push: %v\n", err)
					}
					cancel()
				}(p)
			}
		}
	}
}

// splitParentURLs (#203) parses --datawatch / DATAWATCH_PARENTS into
// individual URLs. Empty input returns nil.
func splitParentURLs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out := []string{}
	for _, u := range strings.Split(s, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

// uniqueStrings dedupes preserving order.
func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// sanitizeForFile turns a URL into a safe filename component.
// "https://primary:8443" → "primary_8443".
func sanitizeForFile(s string) string {
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	out := []rune{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
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

// runDiagnostic probes each envelope-collection kind and prints
// specific failure reasons + suggested fixes. Operator runs once
// when envelopes come back empty/failed (see #211).
//
// Probes:
//   1. /proc/self/status — process visibility baseline
//   2. /proc/<other-pid>/cmdline — can we see other processes?
//   3. docker.exec — is `docker` on PATH? group membership?
//   4. DCGM scrape — is dcgm-exporter reachable?
//   5. CAP_BPF — already covered by --setup-ebpf, repeated here
//      with a one-line summary.
//   6. ollama /api/ps — can we reach a local ollama?
func runDiagnostic() {
	fmt.Println("datawatch-stats — envelope diagnostic")
	fmt.Println("=====================================")
	fmt.Println()

	// 1. Process visibility baseline.
	fmt.Println("[1/6] /proc visibility (own process):")
	if _, err := os.ReadFile("/proc/self/status"); err != nil {
		fmt.Printf("  ✗ FAIL: %v\n", err)
		fmt.Println("    fix: are you in a container without /proc mount?")
	} else {
		fmt.Println("  ✓ ok — /proc/self/status readable")
	}
	fmt.Println()

	// 2. Other-process visibility.
	fmt.Println("[2/6] /proc visibility (other PIDs — for envelope process details):")
	otherProc := "/proc/1/cmdline"
	if data, err := os.ReadFile(otherProc); err != nil {
		fmt.Printf("  ✗ FAIL reading %s: %v\n", otherProc, err)
		fmt.Println("    fix: typical when running unprivileged in a container")
		fmt.Println("    OR when /proc is hidepid=2 mounted")
		fmt.Println("    Check: mount | grep proc")
	} else {
		fmt.Printf("  ✓ ok — read %d bytes from %s\n", len(data), otherProc)
	}
	fmt.Println()

	// 3. docker access.
	fmt.Println("[3/6] docker access (for container envelopes):")
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("  ⚠ docker binary NOT on PATH — container envelopes will be empty")
		fmt.Println("    fix: install docker OR ignore (no containers to discover)")
	} else {
		// Try `docker ps --format {{.ID}}` — if it errors with
		// "permission denied" the user isn't in the docker group.
		out, err := exec.Command("docker", "ps", "--format", "{{.ID}}").CombinedOutput()
		if err != nil {
			fmt.Printf("  ✗ FAIL: docker ps: %v\n", err)
			fmt.Printf("    output: %s\n", strings.TrimSpace(string(out)))
			fmt.Println("    fix: add the running user to the docker group:")
			fmt.Println("           sudo usermod -aG docker datawatch")
			fmt.Println("    THEN restart the systemd unit (logout/login won't propagate to a daemon):")
			fmt.Println("           sudo systemctl restart datawatch-stats")
		} else {
			n := strings.Count(strings.TrimSpace(string(out)), "\n") + 1
			if strings.TrimSpace(string(out)) == "" {
				n = 0
			}
			fmt.Printf("  ✓ ok — docker reachable, %d container(s) visible\n", n)
		}
	}
	fmt.Println()

	// 4. DCGM exporter.
	fmt.Println("[4/6] DCGM exporter (for per-pid GPU metrics, Shape C):")
	dcgmEndpoint := "http://localhost:9400/metrics"
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(dcgmEndpoint)
	if err != nil {
		fmt.Printf("  ⚠ DCGM not reachable at %s: %v\n", dcgmEndpoint, err)
		fmt.Println("    fix: install nvidia-dcgm-exporter if you want per-pid GPU metrics;")
		fmt.Println("         non-fatal if no NVIDIA GPU (Shape B works without DCGM).")
	} else {
		_ = resp.Body.Close()
		fmt.Printf("  ✓ ok — DCGM responded HTTP %d at %s\n", resp.StatusCode, dcgmEndpoint)
	}
	fmt.Println()

	// 5. CAP_BPF.
	fmt.Println("[5/6] CAP_BPF (for eBPF process / network capture):")
	if observer.ProbeBPFCapability() {
		fmt.Println("  ✓ ok — CAP_BPF granted")
	} else {
		fmt.Println("  ⚠ CAP_BPF NOT granted — eBPF envelopes will be no-op probes")
		fmt.Println("    fix: run `datawatch-stats --setup-ebpf` for full setcap recipe")
	}
	fmt.Println()

	// 6. ollama tap.
	fmt.Println("[6/6] Ollama /api/ps (for ollama loaded-model envelopes):")
	ollamaEndpoint := os.Getenv("OLLAMA_HOST")
	if ollamaEndpoint == "" {
		ollamaEndpoint = "http://localhost:11434"
	}
	if !strings.HasPrefix(ollamaEndpoint, "http") {
		ollamaEndpoint = "http://" + ollamaEndpoint
	}
	or, oerr := (&http.Client{Timeout: 2 * time.Second}).Get(ollamaEndpoint + "/api/ps")
	if oerr != nil {
		fmt.Printf("  ⚠ Ollama not reachable at %s/api/ps: %v\n", ollamaEndpoint, oerr)
		fmt.Println("    fix: set OLLAMA_HOST env var if ollama lives elsewhere;")
		fmt.Println("         non-fatal if this Node doesn't host ollama.")
	} else {
		_ = or.Body.Close()
		fmt.Printf("  ✓ ok — Ollama responded HTTP %d at %s/api/ps\n", or.StatusCode, ollamaEndpoint)
	}
	fmt.Println()

	fmt.Println("=====================================")
	fmt.Println("Common envelope-empty causes:")
	fmt.Println("  • Container envelopes empty: check [3/6] docker access (group membership)")
	fmt.Println("  • Process details empty: check [2/6] /proc visibility (hidepid / unprivileged container)")
	fmt.Println("  • GPU envelopes empty: check [4/6] DCGM AND/OR [5/6] CAP_BPF")
	fmt.Println("  • Ollama envelopes empty: check [6/6] Ollama reachability")
	fmt.Println()
	fmt.Println("If everything above is ✓ but envelopes still empty:")
	fmt.Println("  • Run: datawatch-stats --once --print  (see what the snapshot looks like)")
	fmt.Println("  • Run: datawatch-stats --print-once --insecure-tls --datawatch <parent>")
	fmt.Println("    (full round-trip with debug-connections)")
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
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "[stats] sidecar listener: %v\n", err)
	}
}

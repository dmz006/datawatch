package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/bwmarrin/discordgo"
	slackgo "github.com/slack-go/slack"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	wizardpkg "github.com/dmz006/datawatch/internal/wizard"
	"golang.org/x/term"
	"github.com/dmz006/datawatch/internal/llm/backends/aider"
	"github.com/dmz006/datawatch/internal/llm/backends/gemini"
	"github.com/dmz006/datawatch/internal/llm/backends/goose"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
	"github.com/dmz006/datawatch/internal/llm/backends/shell"
	"github.com/dmz006/datawatch/internal/llm/claudecode"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/messaging/backends/discord"
	emailmsg "github.com/dmz006/datawatch/internal/messaging/backends/email"
	ghwebhook "github.com/dmz006/datawatch/internal/messaging/backends/github"
	"github.com/dmz006/datawatch/internal/messaging/backends/matrix"
	ntfymsg "github.com/dmz006/datawatch/internal/messaging/backends/ntfy"
	"github.com/dmz006/datawatch/internal/messaging/backends/slack"
	"github.com/dmz006/datawatch/internal/messaging/backends/telegram"
	"github.com/dmz006/datawatch/internal/messaging/backends/twilio"
	"github.com/dmz006/datawatch/internal/messaging/backends/webhook"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
	signalpkg "github.com/dmz006/datawatch/internal/signal"
	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "0.2.0"

var (
	cfgPath    string
	verbose    bool
	secureMode bool
)

func main() {
	root := &cobra.Command{
		Use:   "datawatch",
		Short: "Bridge messaging groups to AI coding tmux sessions",
		Long: `datawatch is a daemon that links messaging groups (Signal, Telegram, Matrix, webhooks)
to AI coding tmux sessions. Send commands to start, monitor, and interact with AI coding tasks.`,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default: ~/.datawatch/config.yaml)")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose/debug logging")
	root.PersistentFlags().BoolVar(&secureMode, "secure", false, "use encrypted config file (prompts for password)")

	root.AddCommand(
		newStartCmd(),
		newStopCmd(),
		newLinkCmd(),
		newConfigCmd(),
		newSetupCmd(),
		newSessionCmd(),
		newMCPCmd(),
		newBackendCmd(),
		newVersionCmd(),
		newCompletionCmd(root),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// resolveConfigPath returns the effective config file path.
func resolveConfigPath() string {
	if cfgPath != "" {
		return cfgPath
	}
	return config.ConfigPath()
}

// loadConfig loads configuration from the resolved path (plaintext).
func loadConfig() (*config.Config, error) {
	return config.Load(resolveConfigPath())
}

// loadConfigSecure loads config, prompting for a password if --secure is set.
func loadConfigSecure() (*config.Config, error) {
	path := resolveConfigPath()
	if !secureMode {
		return config.Load(path)
	}
	pw, err := promptPassword(false)
	if err != nil {
		return nil, fmt.Errorf("read password: %w", err)
	}
	cfg, err := config.LoadSecure(path, pw)
	zeroBytes(pw)
	return cfg, err
}

// saveConfigSecure saves config, encrypting if --secure is set.
func saveConfigSecure(cfg *config.Config) error {
	path := resolveConfigPath()
	if !secureMode {
		return config.Save(cfg, path)
	}
	pw, err := promptPassword(true)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	err = config.SaveSecure(cfg, path, pw)
	zeroBytes(pw)
	return err
}

// promptPassword reads a password from the terminal without echo.
// If confirm is true, asks twice and verifies they match.
func promptPassword(confirm bool) ([]byte, error) {
	fmt.Print("Config password: ")
	pw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return nil, err
	}
	if !confirm {
		return pw, nil
	}
	fmt.Print("Confirm password: ")
	pw2, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return nil, err
	}
	if string(pw) != string(pw2) {
		zeroBytes(pw)
		zeroBytes(pw2)
		return nil, fmt.Errorf("passwords do not match")
	}
	zeroBytes(pw2)
	return pw, nil
}

// zeroBytes overwrites a byte slice with zeros to clear sensitive data from memory.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func debugf(format string, args ...interface{}) {
	if verbose {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// ---- start command --------------------------------------------------------

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the datawatch daemon",
		Long: `Start the datawatch daemon.

By default, daemonizes and runs in the background. Use --foreground to run in the terminal.
Flags override the corresponding config.yaml values for this run only.`,
		RunE: runStart,
	}
	cmd.Flags().String("llm-backend", "", "LLM backend to use (overrides session.llm_backend in config)")
	cmd.Flags().String("host", "", "HTTP server bind address (overrides server.host in config)")
	cmd.Flags().Int("port", 0, "HTTP server port (overrides server.port in config)")
	cmd.Flags().Bool("no-server", false, "Disable the HTTP/WebSocket PWA server")
	cmd.Flags().Bool("no-mcp", false, "Disable the MCP server")
	cmd.Flags().Bool("foreground", false, "Run in the terminal instead of daemonizing")
	return cmd
}

func runStart(cmd *cobra.Command, _ []string) error {
	fg, _ := cmd.Flags().GetBool("foreground")
	if !fg {
		if secureMode {
			fmt.Println("[warn] --secure with daemon mode requires interactive password entry.")
			fmt.Println("       Use --foreground to run in the terminal with an encrypted config.")
		}
		return daemonize()
	}

	cfg, err := loadConfigSecure()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply flag overrides
	if v, _ := cmd.Flags().GetString("llm-backend"); v != "" {
		cfg.Session.LLMBackend = v
	}
	if v, _ := cmd.Flags().GetString("host"); v != "" {
		cfg.Server.Host = v
	}
	if v, _ := cmd.Flags().GetInt("port"); v != 0 {
		cfg.Server.Port = v
	}
	if v, _ := cmd.Flags().GetBool("no-server"); v {
		cfg.Server.Enabled = false
	}
	if v, _ := cmd.Flags().GetBool("no-mcp"); v {
		cfg.MCP.Enabled = false
	}

	debugf("hostname=%s", cfg.Hostname)

	// Register LLM backends from config (explicit registration, not auto-init)
	if cfg.Aider.Enabled {
		llm.Register(aider.New(cfg.Aider.Binary))
	}
	if cfg.Goose.Enabled {
		llm.Register(goose.New(cfg.Goose.Binary))
	}
	if cfg.Gemini.Enabled {
		llm.Register(gemini.New(cfg.Gemini.Binary))
	}
	if cfg.OpenCode.Enabled {
		llm.Register(opencode.New(cfg.OpenCode.Binary))
	}
	if cfg.Shell.Enabled && cfg.Shell.ScriptPath != "" {
		llm.Register(shell.New(cfg.Shell.ScriptPath))
	}

	// Create session manager
	idleTimeout := time.Duration(cfg.Session.InputIdleTimeout) * time.Second
	mgr, err := session.NewManager(cfg.Hostname, cfg.DataDir, cfg.Session.ClaudeCodeBin, idleTimeout)
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
	}

	// Re-register claude-code with config-driven options (skip_permissions etc.)
	llm.Register(claudecode.NewWithOptions(cfg.Session.ClaudeCodeBin, cfg.Session.SkipPermissions))

	// Wire the active LLM backend to the session manager
	activeBackend, backendErr := llm.Get(cfg.Session.LLMBackend)
	if backendErr != nil {
		fmt.Printf("[warn] LLM backend %q not found, falling back to claude-code: %v\n", cfg.Session.LLMBackend, backendErr)
		activeBackend, _ = llm.Get("claude-code")
	}
	if activeBackend != nil {
		b := activeBackend // capture
		mgr.SetLLMBackend(b.Name(), func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
			return b.Launch(ctx, task, tmuxSession, projectDir, logFile)
		})
	}
	mgr.SetAutoGit(cfg.Session.AutoGitCommit, cfg.Session.AutoGitInit)

	// Handle SIGINT / SIGTERM gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		if cfg.Session.KillSessionsOnExit {
			fmt.Println("Killing active sessions...")
			if err := mgr.KillAll(); err != nil {
				fmt.Printf("[warn] kill sessions on exit: %v\n", err)
			}
		}
		cancel()
	}()

	// Resume monitors for sessions that survived a previous daemon restart
	mgr.ResumeMonitors(ctx)

	// Create shared wizard manager and register all service wizards
	wm := wizardpkg.NewManager(resolveConfigPath())
	wizardpkg.RegisterAll(wm)

	var (
		routers    []*router.Router
		wg         sync.WaitGroup
		httpServer *server.HTTPServer
	)

	// Signal backend (if configured)
	if cfg.Signal.AccountNumber != "" && cfg.Signal.GroupID != "" {
		debugf("starting signal-cli backend account=%s group=%s", cfg.Signal.AccountNumber, cfg.Signal.GroupID)
		backend, err := signalpkg.NewSignalCLIBackend(cfg.Signal.ConfigDir, cfg.Signal.AccountNumber)
		if err != nil {
			return fmt.Errorf("start signal-cli: %w", err)
		}
		defer backend.Close() //nolint:errcheck
		adapted := signalpkg.NewMessagingAdapter(backend)
		r := router.NewRouter(cfg.Hostname, cfg.Signal.GroupID, adapted, mgr, cfg.Session.TailLines, wm)
		routers = append(routers, r)
		fmt.Printf("[%s] Signal backend enabled (group: %s)\n", cfg.Hostname, cfg.Signal.GroupID)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
				fmt.Printf("[%s] Signal router error: %v\n", cfg.Hostname, rErr)
			}
		}()
	}

	// Telegram
	if cfg.Telegram.Enabled && cfg.Telegram.Token != "" {
		tgB, err := telegram.New(cfg.Telegram.Token, cfg.Telegram.ChatID)
		if err != nil {
			fmt.Printf("[warn] Telegram backend: %v\n", err)
		} else {
			defer tgB.Close() //nolint:errcheck
			chatIDStr := fmt.Sprintf("%d", cfg.Telegram.ChatID)
			if cfg.Telegram.ChatID == 0 {
				fmt.Printf("[%s] Telegram: chat_id is 0 — add this bot to a Telegram group, then set chat_id in config.yaml\n", cfg.Hostname)
			}
			r := router.NewRouter(cfg.Hostname, chatIDStr, tgB, mgr, cfg.Session.TailLines, wm)
			routers = append(routers, r)
			fmt.Printf("[%s] Telegram backend enabled\n", cfg.Hostname)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
					fmt.Printf("[%s] Telegram router error: %v\n", cfg.Hostname, rErr)
				}
			}()
		}
	}

	// Discord
	if cfg.Discord.Enabled && cfg.Discord.Token != "" {
		discordB, err := discord.New(cfg.Discord.Token, cfg.Discord.ChannelID)
		if err != nil {
			fmt.Printf("[warn] Discord backend: %v\n", err)
		} else {
			defer discordB.Close() //nolint:errcheck
			channelID := cfg.Discord.ChannelID
			if channelID == "" {
				fmt.Printf("[%s] Discord: channel_id is empty — create a Discord channel and set channel_id in config.yaml\n", cfg.Hostname)
				channelID = "discord"
			}
			r := router.NewRouter(cfg.Hostname, channelID, discordB, mgr, cfg.Session.TailLines, wm)
			routers = append(routers, r)
			fmt.Printf("[%s] Discord backend enabled (channel: %s)\n", cfg.Hostname, channelID)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
					fmt.Printf("[%s] Discord router error: %v\n", cfg.Hostname, rErr)
				}
			}()
		}
	}

	// Slack
	if cfg.Slack.Enabled && cfg.Slack.Token != "" {
		slackB, err := slack.New(cfg.Slack.Token, cfg.Slack.ChannelID)
		if err != nil {
			fmt.Printf("[warn] Slack backend: %v\n", err)
		} else {
			defer slackB.Close() //nolint:errcheck
			channelID := cfg.Slack.ChannelID
			if channelID == "" {
				fmt.Printf("[%s] Slack: channel_id is empty — create a Slack channel and set channel_id in config.yaml\n", cfg.Hostname)
				channelID = "slack"
			}
			r := router.NewRouter(cfg.Hostname, channelID, slackB, mgr, cfg.Session.TailLines, wm)
			routers = append(routers, r)
			fmt.Printf("[%s] Slack backend enabled (channel: %s)\n", cfg.Hostname, channelID)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
					fmt.Printf("[%s] Slack router error: %v\n", cfg.Hostname, rErr)
				}
			}()
		}
	}

	// Twilio SMS
	if cfg.Twilio.Enabled && cfg.Twilio.AccountSID != "" {
		twilioB := twilio.New(cfg.Twilio.AccountSID, cfg.Twilio.AuthToken, cfg.Twilio.FromNumber, cfg.Twilio.ToNumber, cfg.Twilio.WebhookAddr)
		defer twilioB.Close() //nolint:errcheck
		r := router.NewRouter(cfg.Hostname, cfg.Twilio.ToNumber, twilioB, mgr, cfg.Session.TailLines, wm)
		routers = append(routers, r)
		fmt.Printf("[%s] Twilio SMS backend enabled (from: %s, webhook: %s)\n",
			cfg.Hostname, cfg.Twilio.FromNumber, cfg.Twilio.WebhookAddr)
		fmt.Printf("[%s] Twilio: configure webhook at https://console.twilio.com → your number → Messaging → webhook URL → %s/sms\n",
			cfg.Hostname, cfg.Twilio.WebhookAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
				fmt.Printf("[%s] Twilio router error: %v\n", cfg.Hostname, rErr)
			}
		}()
	}

	// Matrix
	if cfg.Matrix.Enabled && cfg.Matrix.AccessToken != "" {
		matrixB, err := matrix.New(cfg.Matrix.Homeserver, cfg.Matrix.UserID, cfg.Matrix.AccessToken, cfg.Matrix.RoomID)
		if err != nil {
			fmt.Printf("[warn] Matrix backend: %v\n", err)
		} else {
			defer matrixB.Close() //nolint:errcheck
			r := router.NewRouter(cfg.Hostname, cfg.Matrix.RoomID, matrixB, mgr, cfg.Session.TailLines, wm)
			routers = append(routers, r)
			fmt.Printf("[%s] Matrix backend enabled (room: %s)\n", cfg.Hostname, cfg.Matrix.RoomID)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
					fmt.Printf("[%s] Matrix router error: %v\n", cfg.Hostname, rErr)
				}
			}()
		}
	}

	// ntfy (send-only, wire as state-change notifier, not a router)
	var ntfyBackend *ntfymsg.Backend
	if cfg.Ntfy.Enabled && cfg.Ntfy.Topic != "" {
		ntfyBackend = ntfymsg.New(cfg.Ntfy.ServerURL, cfg.Ntfy.Topic, cfg.Ntfy.Token)
		fmt.Printf("[%s] ntfy notifications enabled (topic: %s)\n", cfg.Hostname, cfg.Ntfy.Topic)
	}

	// Email (send-only)
	var emailBackend *emailmsg.Backend
	if cfg.Email.Enabled && cfg.Email.Host != "" {
		emailBackend = emailmsg.New(cfg.Email.Host, cfg.Email.Port, cfg.Email.Username, cfg.Email.Password, cfg.Email.From, cfg.Email.To)
		fmt.Printf("[%s] Email notifications enabled (%s -> %s)\n", cfg.Hostname, cfg.Email.From, cfg.Email.To)
	}

	// GitHub webhook
	if cfg.GitHubWebhook.Enabled {
		ghB := ghwebhook.New(cfg.GitHubWebhook.Addr, cfg.GitHubWebhook.Secret)
		r := router.NewRouter(cfg.Hostname, "github", ghB, mgr, cfg.Session.TailLines, wm)
		routers = append(routers, r)
		fmt.Printf("[%s] GitHub webhook listening on %s\n", cfg.Hostname, cfg.GitHubWebhook.Addr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
				fmt.Printf("[%s] GitHub webhook error: %v\n", cfg.Hostname, rErr)
			}
		}()
	}

	// Generic webhook
	if cfg.Webhook.Enabled {
		wbB := webhook.New(cfg.Webhook.Addr, cfg.Webhook.Token)
		r := router.NewRouter(cfg.Hostname, "webhook", wbB, mgr, cfg.Session.TailLines, wm)
		routers = append(routers, r)
		fmt.Printf("[%s] Generic webhook listening on %s\n", cfg.Hostname, cfg.Webhook.Addr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rErr := r.Run(ctx); rErr != nil && rErr != context.Canceled {
				fmt.Printf("[%s] Webhook error: %v\n", cfg.Hostname, rErr)
			}
		}()
	}

	// Start the PWA/WebSocket server if enabled
	if cfg.Server.Enabled {
		httpServer = server.New(&cfg.Server, cfg, resolveConfigPath(), cfg.DataDir, mgr, cfg.Hostname, llm.Names())
		scheme := "http"
		if cfg.Server.TLSEnabled {
			scheme = "https"
		}
		addr := fmt.Sprintf("%s://%s:%d", scheme, cfg.Server.Host, cfg.Server.Port)
		fmt.Printf("[%s] PWA server: %s\n", cfg.Hostname, addr)
		go func() {
			if srvErr := httpServer.Start(ctx); srvErr != nil && srvErr != context.Canceled {
				fmt.Printf("[%s] PWA server error: %v\n", cfg.Hostname, srvErr)
			}
		}()
	}

	// Start MCP SSE server for remote AI client access (if configured)
	if cfg.MCP.SSEEnabled {
		mcpSrv := mcp.New(cfg.Hostname, mgr, &cfg.MCP, cfg.DataDir)
		scheme := "http"
		if cfg.MCP.TLSEnabled {
			scheme = "https"
		}
		fmt.Printf("[%s] MCP SSE server: %s://%s:%d (remote AI can connect here)\n",
			cfg.Hostname, scheme, cfg.MCP.SSEHost, cfg.MCP.SSEPort)
		go func() {
			if srvErr := mcpSrv.ServeSSE(ctx); srvErr != nil && srvErr != context.Canceled {
				fmt.Printf("[%s] MCP SSE server error: %v\n", cfg.Hostname, srvErr)
			}
		}()
	}

	// Wire state-change callbacks composing all routers + HTTP server + ntfy + email
	mgr.SetStateChangeHandler(func(sess *session.Session, old session.State) {
		for _, r := range routers {
			r.HandleStateChange(sess, old)
		}
		if httpServer != nil {
			httpServer.NotifyStateChange(sess, old)
		}
		if ntfyBackend != nil {
			msg := fmt.Sprintf("[%s][%s] %s -> %s: %s", cfg.Hostname, sess.ID, old, sess.State, truncate(sess.Task, 60))
			ntfyBackend.Send(cfg.Ntfy.Topic, msg) //nolint:errcheck
		}
		if emailBackend != nil {
			msg := fmt.Sprintf("[%s][%s] State: %s -> %s\nTask: %s", cfg.Hostname, sess.ID, old, sess.State, sess.Task)
			emailBackend.Send(cfg.Email.To, msg) //nolint:errcheck
		}
	})
	mgr.SetNeedsInputHandler(func(sess *session.Session, prompt string) {
		for _, r := range routers {
			r.HandleNeedsInput(sess, prompt)
		}
		if httpServer != nil {
			httpServer.NotifyNeedsInput(sess, prompt)
		}
	})

	fmt.Printf("[%s] datawatch v%s started.\n", cfg.Hostname, Version)

	if len(routers) == 0 && !cfg.Server.Enabled && !cfg.MCP.SSEEnabled {
		return fmt.Errorf("no backends enabled — run `datawatch setup <service>` to configure a messaging backend\n" +
			"  Available: signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web\n" +
			"  Or run `datawatch config show` to see current configuration")
	}

	// Wait for all routers to finish (or ctx to be cancelled)
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
	case <-doneCh:
	}
	return nil
}

// ---- daemon helpers -------------------------------------------------------

// daemonize re-invokes the current binary with --foreground appended, detaches
// it from the terminal, and writes its PID to ~/.datawatch/daemon.pid.
func daemonize() error {
	cfg, _ := loadConfig()
	dataDir := expandHome(cfg.DataDir)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	logPath := filepath.Join(dataDir, "daemon.log")
	pidPath := filepath.Join(dataDir, "daemon.pid")

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	args := appendForegroundFlag(os.Args[1:])

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	child := exec.Command(exe, args...)
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := child.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	logFile.Close()

	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child.Process.Pid)), 0644); err != nil {
		fmt.Printf("[warn] could not write PID file: %v\n", err)
	}

	fmt.Printf("datawatch daemon started (PID %d)\n", child.Process.Pid)
	fmt.Printf("Logs: tail -f %s\n", logPath)
	fmt.Printf("Stop: datawatch stop\n")
	return nil
}

// appendForegroundFlag inserts --foreground into argv right after the "start"
// subcommand (or at the end if not found), skipping it if already present.
func appendForegroundFlag(args []string) []string {
	for _, a := range args {
		if a == "--foreground" || a == "-foreground" {
			return args
		}
	}
	for i, a := range args {
		if a == "start" {
			out := make([]string, 0, len(args)+1)
			out = append(out, args[:i+1]...)
			out = append(out, "--foreground")
			out = append(out, args[i+1:]...)
			return out
		}
	}
	return append(args, "--foreground")
}

// expandHome replaces a leading ~ with the user home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}

// ---- stop command ---------------------------------------------------------

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running datawatch daemon",
		Long: `Stop the datawatch daemon by sending SIGTERM to the process recorded in the PID file.

By default, running AI sessions are left intact. Use --sessions to also kill them.`,
		RunE: runStop,
	}
	cmd.Flags().Bool("sessions", false, "Also kill all running AI sessions")
	return cmd
}

func runStop(cmd *cobra.Command, _ []string) error {
	cfg, _ := loadConfig()
	pidPath := filepath.Join(expandHome(cfg.DataDir), "daemon.pid")

	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("daemon not running (no PID file at %s)", pidPath)
	}

	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		return fmt.Errorf("invalid PID in %s", pidPath)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	killSessions, _ := cmd.Flags().GetBool("sessions")
	if killSessions {
		fmt.Println("Killing active sessions...")
		_ = runSessionStopAll(cfg) // best-effort
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		_ = os.Remove(pidPath)
		return fmt.Errorf("send SIGTERM to PID %d: %w (process may have already exited)", pid, err)
	}

	_ = os.Remove(pidPath)
	fmt.Printf("Sent SIGTERM to daemon (PID %d)\n", pid)
	return nil
}

// ---- link command ---------------------------------------------------------

func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link",
		Short: "Link this device to a Signal account and set up the control group",
		Long: `Link this machine to your Signal account via QR code, then automatically
create a Signal group to use as the control channel.

After scanning the QR code, datawatch will:
  1. Create a "datawatch-<hostname>" Signal group
  2. Save the group ID to config
  3. Print the command to start the daemon

You only need to run this once per machine.`,
		RunE: runLink,
	}
}

func runLink(_ *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Auto-create config file/dir if this is first run
	cfgFilePath := resolveConfigPath()
	if _, statErr := os.Stat(cfgFilePath); os.IsNotExist(statErr) {
		if saveErr := config.Save(cfg, cfgFilePath); saveErr != nil {
			return fmt.Errorf("auto-create config: %w", saveErr)
		}
		fmt.Printf("Config created at %s\n", cfgFilePath)
	}

	reader := bufio.NewReader(os.Stdin)

	if cfg.Signal.AccountNumber == "" {
		fmt.Print("Signal account phone number (e.g. +12125551234): ")
		num, _ := reader.ReadString('\n')
		cfg.Signal.AccountNumber = strings.TrimSpace(num)
		if cfg.Signal.AccountNumber == "" {
			return fmt.Errorf("account number is required")
		}
		if err := config.Save(cfg, resolveConfigPath()); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}

	deviceName := cfg.Signal.DeviceName
	if deviceName == "" {
		deviceName = cfg.Hostname
	}

	fmt.Printf("Linking device '%s' to Signal account %s...\n", deviceName, cfg.Signal.AccountNumber)
	fmt.Println("Scan the QR code with your Signal app:")
	fmt.Println("  Settings → Linked Devices → Link New Device")
	fmt.Println()

	err = linkViaSubprocess(cfg.Signal.ConfigDir, deviceName, func(uri string) {
		qrterminal.GenerateHalfBlock(uri, qrterminal.L, os.Stdout)
		fmt.Println()
		fmt.Printf("URI: %s\n\n", uri)
		fmt.Println("Waiting for you to scan the QR code...")
	})
	if err != nil {
		return fmt.Errorf("linking failed: %w", err)
	}

	fmt.Println("\nDevice linked successfully!")

	// Auto-create the control group if not already configured.
	if cfg.Signal.GroupID == "" {
		groupName := "datawatch-" + cfg.Hostname
		fmt.Printf("\nCreating Signal control group '%s'...\n", groupName)

		backend, err := signalpkg.NewSignalCLIBackend(cfg.Signal.ConfigDir, cfg.Signal.AccountNumber)
		if err != nil {
			fmt.Printf("Warning: could not start signal-cli to create group: %v\n", err)
			fmt.Println("Create a group manually and run: datawatch config init")
		} else {
			defer backend.Close()
			groupID, err := backend.CreateGroup(groupName)
			if err != nil {
				fmt.Printf("Warning: could not create group: %v\n", err)
				fmt.Println("Create a group manually and run: datawatch config init")
			} else {
				cfg.Signal.GroupID = groupID
				if err := config.Save(cfg, resolveConfigPath()); err != nil {
					return fmt.Errorf("save config: %w", err)
				}
				fmt.Printf("Group created: %s (ID: %s)\n", groupName, groupID)
				fmt.Println("\nSetup complete! Start the daemon with:")
				fmt.Println("  datawatch start")
				fmt.Println()
				fmt.Printf("Send 'help' in the '%s' group on Signal to verify.\n", groupName)
				_ = reader // suppress unused warning
				return nil
			}
		}
	} else {
		fmt.Printf("\nUsing existing group ID from config: %s\n", cfg.Signal.GroupID)
		fmt.Println("\nSetup complete! Start the daemon with:")
		fmt.Println("  datawatch start")
		_ = reader
		return nil
	}

	fmt.Println("\nNext steps:")
	fmt.Println("  1. Create a Signal group from your phone and add yourself")
	fmt.Println("  2. Get the group ID: signal-cli -u <number> listGroups")
	fmt.Println("  3. Run: datawatch config init")
	fmt.Println("  4. Run: datawatch start")
	return nil
}

// linkViaSubprocess runs `signal-cli link -n <deviceName>`, parses the sgnl:// URI from
// stdout/stderr, calls onQR, and waits for the process to complete.
func linkViaSubprocess(configDir, deviceName string, onQR func(string)) error {
	args := []string{"link", "-n", deviceName}
	if configDir != "" {
		args = append([]string{"--config", configDir}, args...)
	}
	return linkViaCommand(exec.Command("signal-cli", args...), onQR)
}

// linkViaCommand is the testable core of linkViaSubprocess. It scans both stdout and
// stderr of cmd concurrently for the sgnl:// URI and invokes onQR exactly once when
// found. If cmd exits non-zero, any captured diagnostic lines are appended to the error.
func linkViaCommand(cmd *exec.Cmd, onQR func(string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start signal-cli link: %w", err)
	}

	var (
		once      sync.Once
		mu        sync.Mutex
		diagLines []string
	)

	scanStream := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "sgnl://") {
				once.Do(func() { onQR(line) })
			} else if strings.TrimSpace(line) != "" {
				mu.Lock()
				diagLines = append(diagLines, line)
				mu.Unlock()
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanStream(stderr)
	}()
	scanStream(stdout)
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		mu.Lock()
		diag := strings.Join(diagLines, "\n")
		mu.Unlock()
		if diag != "" {
			return fmt.Errorf("%w\n%s", err, diag)
		}
		return err
	}
	return nil
}

// ---- config command -------------------------------------------------------

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage datawatch configuration",
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigShowCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive configuration wizard",
		RunE:  runConfigInit,
	}
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	cfg, err := loadConfigSecure()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	reader := bufio.NewReader(os.Stdin)
	prompt := func(label, defaultVal string) string {
		if defaultVal != "" {
			fmt.Printf("%s [%s]: ", label, defaultVal)
		} else {
			fmt.Printf("%s: ", label)
		}
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultVal
		}
		return line
	}

	fmt.Println("datawatch configuration wizard")
	fmt.Println("==============================")
	fmt.Println("This writes your config file. Configure messaging backends later with:")
	fmt.Println("  datawatch setup <service>")
	fmt.Println("  Available: signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web")
	fmt.Println()

	cfg.Hostname = prompt("Hostname (identifies this machine in messages)", cfg.Hostname)
	cfg.DataDir = prompt("Data directory", cfg.DataDir)
	cfg.Session.ClaudeCodeBin = prompt("Claude binary path", cfg.Session.ClaudeCodeBin)
	cfg.Session.LLMBackend = prompt("Default LLM backend (claude-code|aider|goose|gemini|opencode)", cfg.Session.LLMBackend)

	// Signal section — optional, shown as example
	fmt.Println()
	fmt.Println("Signal (optional — or use `datawatch setup signal` later):")
	cfg.Signal.AccountNumber = prompt("  Signal phone number (press Enter to skip)", cfg.Signal.AccountNumber)
	if cfg.Signal.AccountNumber != "" {
		cfg.Signal.DeviceName = prompt("  Signal device name", cfg.Signal.DeviceName)
	}

	if err := saveConfigSecure(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nConfiguration saved to %s\n\n", resolveConfigPath())
	fmt.Println("Next steps:")
	if cfg.Signal.AccountNumber != "" {
		fmt.Println("  datawatch setup signal   (link Signal device and create control group)")
	}
	fmt.Println("  datawatch setup <service>  (configure any messaging backend)")
	fmt.Println("  datawatch start            (start the daemon)")
	return nil
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print current configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			fmt.Printf("Config file: %s\n\n", resolveConfigPath())
			fmt.Printf("hostname:           %s\n", cfg.Hostname)
			fmt.Printf("data_dir:           %s\n", cfg.DataDir)

			fmt.Println()
			fmt.Println("[signal]")
			fmt.Printf("  account_number:   %s\n", cfg.Signal.AccountNumber)
			fmt.Printf("  group_id:         %s\n", cfg.Signal.GroupID)
			fmt.Printf("  config_dir:       %s\n", cfg.Signal.ConfigDir)
			fmt.Printf("  device_name:      %s\n", cfg.Signal.DeviceName)

			fmt.Println()
			fmt.Println("[session]")
			fmt.Printf("  llm_backend:           %s\n", cfg.Session.LLMBackend)
			fmt.Printf("  claude_code_bin:       %s\n", cfg.Session.ClaudeCodeBin)
			fmt.Printf("  default_project_dir:   %s\n", cfg.Session.DefaultProjectDir)
			fmt.Printf("  max_sessions:          %d\n", cfg.Session.MaxSessions)
			fmt.Printf("  input_idle_timeout:    %ds\n", cfg.Session.InputIdleTimeout)
			fmt.Printf("  tail_lines:            %d\n", cfg.Session.TailLines)
			fmt.Printf("  auto_git_commit:       %v\n", cfg.Session.AutoGitCommit)
			fmt.Printf("  auto_git_init:         %v\n", cfg.Session.AutoGitInit)
			fmt.Printf("  skip_permissions:      %v\n", cfg.Session.SkipPermissions)
			fmt.Printf("  kill_sessions_on_exit: %v\n", cfg.Session.KillSessionsOnExit)

			fmt.Println()
			fmt.Println("[server]")
			fmt.Printf("  enabled:  %v\n", cfg.Server.Enabled)
			fmt.Printf("  host:     %s\n", cfg.Server.Host)
			fmt.Printf("  port:     %d\n", cfg.Server.Port)
			fmt.Printf("  tls:      %v\n", cfg.Server.TLSEnabled)

			// Show enabled messaging backends
			fmt.Println()
			fmt.Println("[messaging backends]")
			if cfg.Telegram.Enabled {
				fmt.Printf("  telegram: enabled (chat_id: %d)\n", cfg.Telegram.ChatID)
			}
			if cfg.Discord.Enabled {
				fmt.Printf("  discord:  enabled (channel: %s)\n", cfg.Discord.ChannelID)
			}
			if cfg.Slack.Enabled {
				fmt.Printf("  slack:    enabled (channel: %s)\n", cfg.Slack.ChannelID)
			}
			if cfg.Matrix.Enabled {
				fmt.Printf("  matrix:   enabled (room: %s)\n", cfg.Matrix.RoomID)
			}
			if cfg.Ntfy.Enabled {
				fmt.Printf("  ntfy:     enabled (topic: %s)\n", cfg.Ntfy.Topic)
			}
			if cfg.Email.Enabled {
				fmt.Printf("  email:    enabled (%s -> %s)\n", cfg.Email.From, cfg.Email.To)
			}
			if cfg.Twilio.Enabled {
				fmt.Printf("  twilio:   enabled (from: %s)\n", cfg.Twilio.FromNumber)
			}
			if cfg.GitHubWebhook.Enabled {
				fmt.Printf("  github_webhook: enabled (addr: %s)\n", cfg.GitHubWebhook.Addr)
			}
			if cfg.Webhook.Enabled {
				fmt.Printf("  webhook:  enabled (addr: %s)\n", cfg.Webhook.Addr)
			}

			// Show enabled LLM backends
			fmt.Println()
			fmt.Println("[llm backends]")
			fmt.Printf("  claude-code: enabled (bin: %s)\n", cfg.Session.ClaudeCodeBin)
			if cfg.Aider.Enabled {
				fmt.Printf("  aider:      enabled (bin: %s)\n", cfg.Aider.Binary)
			}
			if cfg.Goose.Enabled {
				fmt.Printf("  goose:      enabled (bin: %s)\n", cfg.Goose.Binary)
			}
			if cfg.Gemini.Enabled {
				fmt.Printf("  gemini:     enabled (bin: %s)\n", cfg.Gemini.Binary)
			}
			if cfg.OpenCode.Enabled {
				fmt.Printf("  opencode:   enabled (bin: %s)\n", cfg.OpenCode.Binary)
			}
			if cfg.Shell.Enabled {
				fmt.Printf("  shell:      enabled (script: %s)\n", cfg.Shell.ScriptPath)
			}
			return nil
		},
	}
}

// ---- session command ------------------------------------------------------

func newSessionCmd() *cobra.Command {
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage AI coding sessions",
		Long:  "Manage sessions locally without needing the full daemon. Connects to running daemon if available.",
	}

	// session list
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionList(cfg)
		},
	})

	// session new "task description"
	newCmd := &cobra.Command{
		Use:   "new [task]",
		Short: "Start a new AI coding session",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			task := strings.Join(args, " ")
			dir, _ := cmd.Flags().GetString("dir")
			if dir == "" {
				dir, _ = os.Getwd()
			}
			name, _ := cmd.Flags().GetString("name")
			backend, _ := cmd.Flags().GetString("backend")
			return runSessionNew(cfg, task, dir, name, backend)
		},
	}
	newCmd.Flags().StringP("dir", "d", "", "Project directory (default: current directory)")
	newCmd.Flags().StringP("name", "n", "", "Optional human-readable name for this session")
	newCmd.Flags().String("backend", "", "LLM backend to use (overrides config; e.g. claude-code, aider)")
	sessionCmd.AddCommand(newCmd)

	// session status <id>
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "status <id>",
		Short: "Show session status and recent output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionStatus(cfg, args[0])
		},
	})

	// session send <id> <text>
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "send <id> <text>",
		Short: "Send input to a waiting session",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionSend(cfg, args[0], strings.Join(args[1:], " "))
		},
	})

	// session kill <id>
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "kill <id>",
		Short: "Terminate a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionKill(cfg, args[0])
		},
	})

	// session tail <id>
	tailCmd := &cobra.Command{
		Use:   "tail <id>",
		Short: "Show last N lines of session output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			n, _ := cmd.Flags().GetInt("lines")
			return runSessionTail(cfg, args[0], n)
		},
	}
	tailCmd.Flags().IntP("lines", "n", 20, "Number of lines to show")
	sessionCmd.AddCommand(tailCmd)

	// session attach <id>
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "attach <id>",
		Short: "Print the tmux attach command for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionAttach(cfg, args[0])
		},
	})

	// session log <id>  — prints path to session tracking folder
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "log <id>",
		Short: "Print path to session tracking folder (use with cd)",
		Long:  "Print the session tracking folder path.\nUsage: cd $(datawatch session log a3f2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionLog(cfg, args[0])
		},
	})

	// session history <id>  — git log of session tracking folder
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "history <id>",
		Short: "Show git commit history of session tracking folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionHistory(cfg, args[0])
		},
	})

	// session rename <id> <name>
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "rename <id> <name>",
		Short: "Set a human-readable name for a session",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionRename(cfg, args[0], strings.Join(args[1:], " "))
		},
	})

	// session stop-all — kill all running sessions on this host
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "stop-all",
		Short: "Kill all running sessions on this host",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionStopAll(cfg)
		},
	})

	return sessionCmd
}

// truncate shortens a string to at most n runes, appending "..." if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// daemonAPIURL returns the HTTP API base URL for the running daemon.
func daemonAPIURL(cfg *config.Config) string {
	return fmt.Sprintf("http://localhost:%d/api/command", cfg.Server.Port)
}

// tryDaemonCommand posts a command text to the daemon HTTP API.
// Returns true if the daemon responded, false if not reachable.
func tryDaemonCommand(cfg *config.Config, text string) (bool, error) {
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := http.Post(daemonAPIURL(cfg), "application/json", bytes.NewReader(body))
	if err != nil {
		return false, nil // daemon not running
	}
	defer resp.Body.Close()
	return true, nil
}

func runSessionList(cfg *config.Config) error {
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sessions := store.List()
	if len(sessions) == 0 {
		fmt.Println("No sessions.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATE\tBACKEND\tUPDATED\tNAME/TASK")
	for _, s := range sessions {
		display := s.Task
		if s.Name != "" {
			display = s.Name + ": " + s.Task
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.ID, s.State, s.LLMBackend, s.UpdatedAt.Format("15:04:05"),
			truncate(display, 60))
	}
	return w.Flush()
}

func runSessionNew(cfg *config.Config, task, dir, name, backend string) error {
	// Try the structured HTTP API first
	type startReq struct {
		Task       string `json:"task"`
		ProjectDir string `json:"project_dir,omitempty"`
		Backend    string `json:"backend,omitempty"`
		Name       string `json:"name,omitempty"`
	}
	body, _ := json.Marshal(startReq{Task: task, ProjectDir: dir, Backend: backend, Name: name})
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/api/sessions/start", cfg.Server.Port),
		"application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
		fmt.Printf("Session started. Task: %s\n", task)
		if name != "" {
			fmt.Printf("Name: %s\n", name)
		}
		if dir != "" {
			fmt.Printf("Project dir: %s\n", dir)
		}
		if backend != "" {
			fmt.Printf("Backend: %s\n", backend)
		}
		return nil
	}

	// Fallback: command text API (daemon may be running without new /api/sessions/start)
	var cmdText string
	if dir != "" {
		cmdText = fmt.Sprintf("new: %s: %s", dir, task)
	} else {
		cmdText = fmt.Sprintf("new: %s", task)
	}
	reached, err := tryDaemonCommand(cfg, cmdText)
	if err != nil {
		return err
	}
	if reached {
		fmt.Printf("Session started via daemon. Task: %s\n", task)
		return nil
	}

	return fmt.Errorf("daemon not running. Start it with: datawatch start")
}

func runSessionStatus(cfg *config.Config, id string) error {
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	fmt.Printf("Session:     %s\n", sess.FullID)
	if sess.Name != "" {
		fmt.Printf("Name:        %s\n", sess.Name)
	}
	fmt.Printf("State:       %s\n", sess.State)
	fmt.Printf("Backend:     %s\n", sess.LLMBackend)
	fmt.Printf("Task:        %s\n", sess.Task)
	fmt.Printf("Project Dir: %s\n", sess.ProjectDir)
	fmt.Printf("Tracking:    %s\n", sess.TrackingDir)
	fmt.Printf("Tmux:        %s\n", sess.TmuxSession)
	fmt.Printf("Created:     %s\n", sess.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:     %s\n", sess.UpdatedAt.Format(time.RFC3339))
	if sess.State == session.StateRateLimited && sess.RateLimitResetAt != nil {
		fmt.Printf("Rate limit resets at: %s\n", sess.RateLimitResetAt.Format(time.RFC3339))
	}

	fmt.Println()
	fmt.Println("--- Last 20 lines of output ---")
	tailCmd := exec.Command("tail", "-n", "20", sess.LogFile)
	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr
	_ = tailCmd.Run()

	return nil
}

func runSessionSend(cfg *config.Config, id, text string) error {
	// Try HTTP API first
	reached, err := tryDaemonCommand(cfg, fmt.Sprintf("send %s: %s", id, text))
	if err != nil {
		return err
	}
	if reached {
		fmt.Printf("Input sent to session %s\n", id)
		return nil
	}

	// Fall back to direct tmux operation
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	cmd := exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, text, "Enter")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys: %w\n%s", err, out)
	}
	fmt.Printf("Input sent to session %s (tmux: %s)\n", id, sess.TmuxSession)
	return nil
}

func runSessionKill(cfg *config.Config, id string) error {
	// Try HTTP API first
	reached, err := tryDaemonCommand(cfg, fmt.Sprintf("kill %s", id))
	if err != nil {
		return err
	}
	if reached {
		fmt.Printf("Kill command sent for session %s\n", id)
		return nil
	}

	// Fall back to direct tmux operation
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	cmd := exec.Command("tmux", "kill-session", "-t", sess.TmuxSession)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux kill-session: %w\n%s", err, out)
	}
	fmt.Printf("Session %s killed (tmux: %s)\n", id, sess.TmuxSession)
	return nil
}

func runSessionTail(cfg *config.Config, id string, n int) error {
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	cmd := exec.Command("tail", "-n", fmt.Sprintf("%d", n), sess.LogFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runSessionAttach(cfg *config.Config, id string) error {
	// Try HTTP API first
	reached, err := tryDaemonCommand(cfg, fmt.Sprintf("attach %s", id))
	if err != nil {
		return err
	}
	_ = reached

	// Load from store to get tmux session name
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	fmt.Printf("tmux attach-session -t %s\n", sess.TmuxSession)
	return nil
}

func runSessionLog(cfg *config.Config, id string) error {
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	fmt.Println(sess.TrackingDir)
	return nil
}

func runSessionHistory(cfg *config.Config, id string) error {
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	cmd := exec.Command("git", "log", "--oneline", "--color=always")
	cmd.Dir = sess.TrackingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runSessionRename(cfg *config.Config, id, name string) error {
	// Try HTTP API first
	body, _ := json.Marshal(map[string]string{"id": id, "name": name})
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/api/sessions/rename", cfg.Server.Port),
		"application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
		fmt.Printf("Session %s renamed to %q\n", id, name)
		return nil
	}

	// Fall back to direct store operation
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	sess.Name = name
	if err := store.Save(sess); err != nil {
		return err
	}
	fmt.Printf("Session %s renamed to %q\n", id, name)
	return nil
}

func runSessionStopAll(cfg *config.Config) error {
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		return err
	}
	sessions := store.List()
	killed := 0
	for _, sess := range sessions {
		if sess.State != session.StateRunning && sess.State != session.StateWaitingInput {
			continue
		}
		cmd := exec.Command("tmux", "kill-session", "-t", sess.TmuxSession)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("[warn] kill session %s: %v %s\n", sess.ID, err, out)
			continue
		}
		sess.State = session.StateKilled
		_ = store.Save(sess)
		fmt.Printf("Killed session %s (%s)\n", sess.ID, truncate(sess.Task, 40))
		killed++
	}
	if killed == 0 {
		fmt.Println("No active sessions to stop.")
	} else {
		fmt.Printf("Stopped %d session(s).\n", killed)
	}
	return nil
}

// ---- mcp command ----------------------------------------------------------

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdio for Cursor/Claude Desktop, or SSE for remote AI)",
		Long: `Start the MCP (Model Context Protocol) server.

By default, runs over stdin/stdout for local IDE clients (Cursor, Claude Desktop).
Add --sse to start the HTTP/SSE server for remote AI clients instead.

Local (Cursor/Claude Desktop) config:
  { "mcpServers": { "datawatch": { "command": "datawatch", "args": ["mcp"] } } }

Remote AI config (SSE):
  { "mcpServers": { "datawatch": { "url": "https://host:8081/sse", "headers": { "Authorization": "Bearer <token>" } } } }`,
		RunE: runMCP,
	}
	cmd.Flags().Bool("sse", false, "Start SSE server for remote AI clients (uses config mcp.sse_port)")
	return cmd
}

func runMCP(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	idleTimeout := time.Duration(cfg.Session.InputIdleTimeout) * time.Second
	mgr, err := session.NewManager(cfg.Hostname, cfg.DataDir, cfg.Session.ClaudeCodeBin, idleTimeout)
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	mgr.ResumeMonitors(ctx)
	mcpSrv := mcp.New(cfg.Hostname, mgr, &cfg.MCP, cfg.DataDir)

	sseMode, _ := cmd.Flags().GetBool("sse")
	if sseMode {
		return mcpSrv.ServeSSE(ctx)
	}
	return mcpSrv.ServeStdio(ctx)
}

// ---- version command ------------------------------------------------------

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("datawatch v%s\n", Version)
		},
	}
}

// ---- backend command -------------------------------------------------------

func newBackendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backend",
		Short: "Manage LLM and messaging backends",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered LLM backends",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			// Register backends from config (same as runStart)
			if cfg.Aider.Enabled {
				llm.Register(aider.New(cfg.Aider.Binary))
			}
			if cfg.Goose.Enabled {
				llm.Register(goose.New(cfg.Goose.Binary))
			}
			if cfg.Gemini.Enabled {
				llm.Register(gemini.New(cfg.Gemini.Binary))
			}
			if cfg.OpenCode.Enabled {
				llm.Register(opencode.New(cfg.OpenCode.Binary))
			}
			if cfg.Shell.Enabled && cfg.Shell.ScriptPath != "" {
				llm.Register(shell.New(cfg.Shell.ScriptPath))
			}

			names := llm.Names()
			active := cfg.Session.LLMBackend

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "BACKEND\tACTIVE\tVERSION")
			for _, name := range names {
				b, _ := llm.Get(name)
				marker := ""
				if name == active {
					marker = "*"
				}
				version := ""
				if b != nil {
					version = b.Version()
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", name, marker, version)
			}
			return w.Flush()
		},
	}

	cmd.AddCommand(listCmd)
	return cmd
}

// ---- setup command ---------------------------------------------------------

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup [service]",
		Short: "Interactive setup wizard for a messaging backend or web server",
		Long: `Run an interactive setup wizard to configure a backend.

Available services:
  signal    Link a Signal account and create a control group
  telegram  Configure a Telegram bot
  discord   Configure a Discord bot
  slack     Configure a Slack bot
  matrix    Configure a Matrix homeserver
  twilio    Configure Twilio SMS
  ntfy      Configure ntfy push notifications
  email     Configure SMTP email notifications
  webhook   Configure a generic webhook listener
  github    Configure a GitHub webhook listener
  web       Enable or disable the web UI / HTTP API server`,
	}
	cmd.AddCommand(
		newSetupSignalCmd(),
		newSetupTelegramCmd(),
		newSetupDiscordCmd(),
		newSetupSlackCmd(),
		newSetupMatrixCmd(),
		newSetupTwilioCmd(),
		newSetupNtfyCmd(),
		newSetupEmailCmd(),
		newSetupWebhookCmd(),
		newSetupGitHubCmd(),
		newSetupWebCmd(),
	)
	return cmd
}

// setupLoadOrInit loads config, auto-creating the file if it doesn't exist.
func setupLoadOrInit() (*config.Config, error) {
	path := resolveConfigPath()
	cfg, err := loadConfigSecure()
	if err != nil {
		cfg = config.DefaultConfig()
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if saveErr := config.Save(cfg, path); saveErr != nil {
			return nil, fmt.Errorf("auto-create config: %w", saveErr)
		}
		fmt.Printf("Config created at %s\n", path)
	}
	return cfg, nil
}

// setupSave saves config and prints confirmation.
func setupSave(cfg *config.Config) error {
	if err := saveConfigSecure(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("\nConfiguration saved to %s\n", resolveConfigPath())
	return nil
}

// cliPrompt prints a prompt and reads a line, returning defaultVal if blank.
func cliPrompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// ---- setup signal ----------------------------------------------------------

func newSetupSignalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "signal",
		Short: "Link a Signal account and create a control group",
		Long:  "Links this device to a Signal account via QR code, creates a control group, and saves config.",
		RunE:  runLink,
	}
}

// ---- setup telegram --------------------------------------------------------

func newSetupTelegramCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "telegram",
		Short: "Configure Telegram bot",
		RunE:  runSetupTelegram,
	}
}

func runSetupTelegram(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Telegram Setup")
	fmt.Println("==============")
	fmt.Println("1. Open Telegram and talk to @BotFather")
	fmt.Println("2. Send /newbot and follow the prompts")
	fmt.Println("3. Copy the API token BotFather gives you")
	fmt.Println()

	token := cliPrompt(reader, "Bot token", cfg.Telegram.Token)
	if token == "" {
		return fmt.Errorf("token required")
	}

	// Attempt to connect and list chats
	var chatID int64
	tgBot, err := newTelegramBot(token)
	if err != nil {
		fmt.Printf("[warn] could not connect to Telegram: %v\n", err)
	} else {
		chatID = selectTelegramChat(reader, tgBot, cfg.Telegram.ChatID)
	}

	cfg.Telegram.Token = token
	cfg.Telegram.ChatID = chatID
	cfg.Telegram.Enabled = true
	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Println("Telegram backend configured. Start the daemon with: datawatch start")
	return nil
}

// newTelegramBot attempts to create a Telegram bot API client.
func newTelegramBot(token string) (interface{ GetUpdates(tgbotapi.UpdateConfig) ([]tgbotapi.Update, error) }, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Connected as @%s\n", bot.Self.UserName)
	return bot, nil
}

// selectTelegramChat lists recent chats and lets the user pick, or enter manually.
func selectTelegramChat(reader *bufio.Reader, bot interface {
	GetUpdates(tgbotapi.UpdateConfig) ([]tgbotapi.Update, error)
}, current int64) int64 {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 5
	updates, _ := bot.GetUpdates(u)

	type chatEntry struct {
		id   int64
		name string
	}
	seen := map[int64]chatEntry{}
	for _, upd := range updates {
		if upd.Message != nil {
			name := upd.Message.Chat.Title
			if name == "" {
				name = "@" + upd.Message.Chat.UserName
			}
			seen[upd.Message.Chat.ID] = chatEntry{upd.Message.Chat.ID, name}
		}
	}

	if len(seen) == 0 {
		fmt.Println("No recent chats found. Add the bot to a group, send a message, then enter the chat ID:")
		var id int64
		fmt.Sscanf(cliPrompt(reader, "Chat ID", fmt.Sprintf("%d", current)), "%d", &id)
		return id
	}

	chats := make([]chatEntry, 0, len(seen))
	for _, c := range seen {
		chats = append(chats, c)
	}
	fmt.Println("\nAvailable chats:")
	for i, c := range chats {
		fmt.Printf("  %d. %s (ID: %d)\n", i+1, c.name, c.id)
	}
	fmt.Print("Select number (or 0 to enter manually): ")
	line, _ := reader.ReadString('\n')
	var sel int
	fmt.Sscanf(strings.TrimSpace(line), "%d", &sel)
	if sel >= 1 && sel <= len(chats) {
		return chats[sel-1].id
	}
	var id int64
	fmt.Sscanf(cliPrompt(reader, "Chat ID", fmt.Sprintf("%d", current)), "%d", &id)
	return id
}

// ---- setup discord ---------------------------------------------------------

func newSetupDiscordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discord",
		Short: "Configure Discord bot",
		RunE:  runSetupDiscord,
	}
}

func runSetupDiscord(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Discord Setup")
	fmt.Println("=============")
	fmt.Println("1. Go to https://discord.com/developers/applications")
	fmt.Println("2. New Application → Bot → Copy Token")
	fmt.Println("3. Under Bot: enable 'Message Content Intent'")
	fmt.Println("4. OAuth2 → URL Generator: scope=bot, permissions=Send Messages+Read Messages")
	fmt.Println("5. Invite the bot to your server")
	fmt.Println()

	token := cliPrompt(reader, "Bot token", cfg.Discord.Token)
	if token == "" {
		return fmt.Errorf("token required")
	}

	channelID := cfg.Discord.ChannelID
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Printf("[warn] could not connect to Discord: %v\n", err)
	} else if err := dg.Open(); err != nil {
		fmt.Printf("[warn] could not open Discord session: %v\n", err)
	} else {
		defer dg.Close() //nolint:errcheck
		type chanEntry struct{ id, display string }
		var chans []chanEntry

		guilds, _ := dg.UserGuilds(10, "", "", false)
		for _, g := range guilds {
			channels, _ := dg.GuildChannels(g.ID)
			for _, ch := range channels {
				if ch.Type == discordgo.ChannelTypeGuildText {
					chans = append(chans, chanEntry{
						id:      ch.ID,
						display: fmt.Sprintf("#%s in %s (ID: %s)", ch.Name, g.Name, ch.ID),
					})
				}
			}
		}

		if len(chans) > 0 {
			fmt.Println("\nAvailable text channels:")
			for i, c := range chans {
				fmt.Printf("  %d. %s\n", i+1, c.display)
			}
			fmt.Print("Select number (or 0 to enter manually): ")
			line, _ := reader.ReadString('\n')
			var sel int
			fmt.Sscanf(strings.TrimSpace(line), "%d", &sel)
			if sel >= 1 && sel <= len(chans) {
				channelID = chans[sel-1].id
			}
		}
	}

	if channelID == "" {
		channelID = cliPrompt(reader, "Channel ID", "")
	}

	cfg.Discord.Token = token
	cfg.Discord.ChannelID = channelID
	cfg.Discord.Enabled = true
	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Println("Discord backend configured. Start the daemon with: datawatch start")
	return nil
}

// ---- setup slack -----------------------------------------------------------

func newSetupSlackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "slack",
		Short: "Configure Slack bot",
		RunE:  runSetupSlack,
	}
}

func runSetupSlack(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Slack Setup")
	fmt.Println("===========")
	fmt.Println("1. Go to https://api.slack.com/apps → Create New App")
	fmt.Println("2. OAuth & Permissions → Bot Token Scopes:")
	fmt.Println("   channels:history, channels:read, chat:write, groups:history, groups:read")
	fmt.Println("3. Install to workspace → copy the Bot User OAuth Token (xoxb-...)")
	fmt.Println()

	token := cliPrompt(reader, "Bot token (xoxb-...)", cfg.Slack.Token)
	if token == "" {
		return fmt.Errorf("token required")
	}

	channelID := cfg.Slack.ChannelID
	client := slackgo.New(token)
	params := &slackgo.GetConversationsParameters{
		Types: []string{"public_channel", "private_channel"},
		Limit: 50,
	}
	channels, _, err := client.GetConversations(params)
	if err != nil {
		fmt.Printf("[warn] could not list Slack channels: %v\n", err)
	} else if len(channels) > 0 {
		type chanEntry struct{ id, name string }
		chans := make([]chanEntry, 0, len(channels))
		for _, ch := range channels {
			chans = append(chans, chanEntry{ch.ID, ch.Name})
		}
		fmt.Println("\nAvailable channels:")
		for i, c := range chans {
			fmt.Printf("  %d. #%s (ID: %s)\n", i+1, c.name, c.id)
		}
		fmt.Print("Select number (or 0 to enter manually): ")
		line, _ := reader.ReadString('\n')
		var sel int
		fmt.Sscanf(strings.TrimSpace(line), "%d", &sel)
		if sel >= 1 && sel <= len(chans) {
			channelID = chans[sel-1].id
		}
	}

	if channelID == "" {
		channelID = cliPrompt(reader, "Channel ID", "")
	}

	cfg.Slack.Token = token
	cfg.Slack.ChannelID = channelID
	cfg.Slack.Enabled = true
	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Println("Slack backend configured. Start the daemon with: datawatch start")
	return nil
}

// ---- setup matrix ----------------------------------------------------------

func newSetupMatrixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "matrix",
		Short: "Configure Matrix homeserver",
		RunE:  runSetupMatrix,
	}
}

func runSetupMatrix(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Matrix Setup")
	fmt.Println("============")
	fmt.Println("1. Create a Matrix bot account (matrix.org or your own server)")
	fmt.Println("2. In Element: Settings → Help & About → Access Token")
	fmt.Println()

	cfg.Matrix.Homeserver = cliPrompt(reader, "Homeserver URL (e.g. https://matrix.org)", cfg.Matrix.Homeserver)
	cfg.Matrix.UserID = cliPrompt(reader, "Bot user ID (e.g. @bot:matrix.org)", cfg.Matrix.UserID)
	cfg.Matrix.AccessToken = cliPrompt(reader, "Access token", cfg.Matrix.AccessToken)
	cfg.Matrix.RoomID = cliPrompt(reader, "Room ID (e.g. !abcdef:matrix.org)", cfg.Matrix.RoomID)
	cfg.Matrix.Enabled = true

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Println("Matrix backend configured. Start the daemon with: datawatch start")
	return nil
}

// ---- setup twilio ----------------------------------------------------------

func newSetupTwilioCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "twilio",
		Short: "Configure Twilio SMS",
		RunE:  runSetupTwilio,
	}
}

func runSetupTwilio(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Twilio SMS Setup")
	fmt.Println("================")
	fmt.Println("1. Log in to console.twilio.com")
	fmt.Println("2. Copy your Account SID and Auth Token from the dashboard")
	fmt.Println()

	cfg.Twilio.AccountSID = cliPrompt(reader, "Account SID (starts with AC)", cfg.Twilio.AccountSID)
	cfg.Twilio.AuthToken = cliPrompt(reader, "Auth Token", cfg.Twilio.AuthToken)
	cfg.Twilio.FromNumber = cliPrompt(reader, "Twilio phone number (e.g. +12125551234)", cfg.Twilio.FromNumber)
	cfg.Twilio.ToNumber = cliPrompt(reader, "Your phone number (destination)", cfg.Twilio.ToNumber)
	cfg.Twilio.WebhookAddr = cliPrompt(reader, "Webhook listen address", func() string {
		if cfg.Twilio.WebhookAddr != "" {
			return cfg.Twilio.WebhookAddr
		}
		return ":9003"
	}())
	cfg.Twilio.Enabled = true

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Printf("Twilio SMS configured. Configure webhook in Twilio console:\n")
	fmt.Printf("  Your number → Messaging → Webhook URL → http://YOUR_HOST%s/sms\n", cfg.Twilio.WebhookAddr)
	fmt.Println("Start the daemon with: datawatch start")
	return nil
}

// ---- setup ntfy ------------------------------------------------------------

func newSetupNtfyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ntfy",
		Short: "Configure ntfy push notifications",
		RunE:  runSetupNtfy,
	}
}

func runSetupNtfy(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("ntfy Setup")
	fmt.Println("==========")
	fmt.Println("ntfy is a simple push notification service (https://ntfy.sh).")
	fmt.Println("Choose a unique topic name. You'll receive alerts on this topic.")
	fmt.Println()

	serverURL := cliPrompt(reader, "ntfy server URL", func() string {
		if cfg.Ntfy.ServerURL != "" {
			return cfg.Ntfy.ServerURL
		}
		return "https://ntfy.sh"
	}())
	cfg.Ntfy.ServerURL = serverURL
	cfg.Ntfy.Topic = cliPrompt(reader, "Topic name", cfg.Ntfy.Topic)
	cfg.Ntfy.Token = cliPrompt(reader, "Access token (press Enter to skip)", cfg.Ntfy.Token)
	cfg.Ntfy.Enabled = true

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Printf("ntfy configured. Subscribe at: %s/%s\n", cfg.Ntfy.ServerURL, cfg.Ntfy.Topic)
	fmt.Println("Start the daemon with: datawatch start")
	return nil
}

// ---- setup email -----------------------------------------------------------

func newSetupEmailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "email",
		Short: "Configure SMTP email notifications",
		RunE:  runSetupEmail,
	}
}

func runSetupEmail(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Email (SMTP) Setup")
	fmt.Println("==================")
	fmt.Println("For Gmail: create an App Password at https://myaccount.google.com/apppasswords")
	fmt.Println()

	cfg.Email.Host = cliPrompt(reader, "SMTP server hostname (e.g. smtp.gmail.com)", cfg.Email.Host)
	portStr := cliPrompt(reader, "SMTP port", func() string {
		if cfg.Email.Port != 0 {
			return fmt.Sprintf("%d", cfg.Email.Port)
		}
		return "587"
	}())
	fmt.Sscanf(portStr, "%d", &cfg.Email.Port)
	cfg.Email.Username = cliPrompt(reader, "SMTP username", cfg.Email.Username)

	fmt.Print("SMTP password (or App Password): ")
	pw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		// Fall back to visible prompt if terminal doesn't support it
		cfg.Email.Password = cliPrompt(reader, "SMTP password", cfg.Email.Password)
	} else {
		cfg.Email.Password = string(pw)
	}

	cfg.Email.From = cliPrompt(reader, "From address", cfg.Email.From)
	cfg.Email.To = cliPrompt(reader, "To address (where alerts are sent)", cfg.Email.To)
	cfg.Email.Enabled = true

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Println("Email backend configured. Start the daemon with: datawatch start")
	return nil
}

// ---- setup webhook ---------------------------------------------------------

func newSetupWebhookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "webhook",
		Short: "Configure generic webhook listener",
		RunE:  runSetupWebhook,
	}
}

func runSetupWebhook(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Generic Webhook Setup")
	fmt.Println("=====================")
	fmt.Println("datawatch will listen for HTTP POST requests on the configured address.")
	fmt.Println()

	cfg.Webhook.Addr = cliPrompt(reader, "Listen address", func() string {
		if cfg.Webhook.Addr != "" {
			return cfg.Webhook.Addr
		}
		return ":9002"
	}())
	cfg.Webhook.Token = cliPrompt(reader, "Bearer token for authentication (press Enter to skip)", cfg.Webhook.Token)
	cfg.Webhook.Enabled = true

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Printf("Webhook configured. Point your sender at: http://YOUR_HOST%s\n", cfg.Webhook.Addr)
	fmt.Println("Start the daemon with: datawatch start")
	return nil
}

// ---- setup github ----------------------------------------------------------

func newSetupGitHubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "github",
		Short: "Configure GitHub webhook listener",
		RunE:  runSetupGitHub,
	}
}

func runSetupGitHub(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("GitHub Webhook Setup")
	fmt.Println("====================")
	fmt.Println("1. In your GitHub repo: Settings → Webhooks → Add webhook")
	fmt.Println("2. Set Content type to application/json")
	fmt.Println("3. Set Payload URL to: http://YOUR_HOST:<port>/webhook")
	fmt.Println("4. Choose a secret (any random string)")
	fmt.Println()

	cfg.GitHubWebhook.Addr = cliPrompt(reader, "Listen address", func() string {
		if cfg.GitHubWebhook.Addr != "" {
			return cfg.GitHubWebhook.Addr
		}
		return ":9001"
	}())
	cfg.GitHubWebhook.Secret = cliPrompt(reader, "Webhook secret", cfg.GitHubWebhook.Secret)
	cfg.GitHubWebhook.Enabled = true

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Printf("GitHub webhook configured. Set Payload URL to: http://YOUR_HOST%s\n", cfg.GitHubWebhook.Addr)
	fmt.Println("Start the daemon with: datawatch start")
	return nil
}

// ---- setup web -------------------------------------------------------------

func newSetupWebCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "web",
		Short: "Enable or disable the web UI / HTTP API server",
		RunE:  runSetupWeb,
	}
}

func runSetupWeb(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Web Server Setup")
	fmt.Println("================")
	status := "disabled"
	if cfg.Server.Enabled {
		status = fmt.Sprintf("enabled on port %d", cfg.Server.Port)
	}
	fmt.Printf("Current status: %s\n\n", status)

	choice := cliPrompt(reader, "Enable web server? (y/n)", "y")
	if strings.ToLower(choice) == "n" || strings.ToLower(choice) == "no" {
		cfg.Server.Enabled = false
		if err := setupSave(cfg); err != nil {
			return err
		}
		fmt.Println("Web server disabled.")
		return nil
	}

	cfg.Server.Host = cliPrompt(reader, "Bind address", func() string {
		if cfg.Server.Host != "" {
			return cfg.Server.Host
		}
		return "0.0.0.0"
	}())
	portStr := cliPrompt(reader, "Port", func() string {
		if cfg.Server.Port != 0 {
			return fmt.Sprintf("%d", cfg.Server.Port)
		}
		return "8080"
	}())
	fmt.Sscanf(portStr, "%d", &cfg.Server.Port)
	cfg.Server.Token = cliPrompt(reader, "Bearer token for authentication (press Enter to skip)", cfg.Server.Token)

	tlsChoice := cliPrompt(reader, "Enable TLS with auto-generated cert? (y/n)", "y")
	cfg.Server.TLSEnabled = strings.ToLower(tlsChoice) != "n" && strings.ToLower(tlsChoice) != "no"
	cfg.Server.TLSAutoGenerate = cfg.Server.TLSEnabled
	cfg.Server.Enabled = true

	if err := setupSave(cfg); err != nil {
		return err
	}

	scheme := "http"
	if cfg.Server.TLSEnabled {
		scheme = "https"
	}
	fmt.Printf("Web server configured. It will start at %s://%s:%d on next `datawatch start`.\n", scheme, cfg.Server.Host, cfg.Server.Port)
	if cfg.Server.TLSEnabled {
		fmt.Println("TLS certificate will be auto-generated to ~/.datawatch/tls/server/")
	}
	return nil
}

// ---- completion command ----------------------------------------------------

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for datawatch.

Add to your shell profile:
  bash:       source <(datawatch completion bash)
  zsh:        source <(datawatch completion zsh)
  fish:       datawatch completion fish | source
  powershell: datawatch completion powershell | Out-String | Invoke-Expression`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell %q; use bash, zsh, fish, or powershell", args[0])
			}
		},
	}
	return cmd
}

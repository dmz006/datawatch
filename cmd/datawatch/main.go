package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
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

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/messaging"
	wizardpkg "github.com/dmz006/datawatch/internal/wizard"
	"golang.org/x/term"
	"github.com/dmz006/datawatch/internal/llm/backends/aider"
	"github.com/dmz006/datawatch/internal/llm/backends/gemini"
	"github.com/dmz006/datawatch/internal/llm/backends/goose"
	"github.com/dmz006/datawatch/internal/llm/backends/ollama"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
	"github.com/dmz006/datawatch/internal/llm/backends/openwebui"
	"github.com/dmz006/datawatch/internal/llm/backends/shell"
	"github.com/dmz006/datawatch/internal/channel"
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
var Version = "0.6.4"

var (
	cfgPath    string
	verbose    bool
	secureMode bool
	serverName string // --server flag: name of remote server to target
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
	root.PersistentFlags().StringVar(&serverName, "server", "", "name of remote datawatch server to target (see 'setup server')")

	root.AddCommand(
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newStatusCmd(),
		newLinkCmd(),
		newConfigCmd(),
		newSetupCmd(),
		newSessionCmd(),
		newMCPCmd(),
		newBackendCmd(),
		newVersionCmd(),
		newUpdateCmd(),
		newCmdCmd(),
		newSeedCmd(),
		newTestCmd(),
		newDiagnoseCmd(),
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

// loadConfigAndDeriveKey loads the config and, when --secure is set, derives
// a 32-byte AES key for encrypting all data stores. The password is prompted
// once and then zeroed. Returns (config, nil key) in plaintext mode.
func loadConfigAndDeriveKey() (*config.Config, []byte, error) {
	path := resolveConfigPath()
	if !secureMode {
		cfg, err := config.Load(path)
		return cfg, nil, err
	}
	pw, err := promptPassword(false)
	if err != nil {
		return nil, nil, fmt.Errorf("read password: %w", err)
	}
	cfg, err := config.LoadSecure(path, pw)
	if err != nil {
		zeroBytes(pw)
		return nil, nil, err
	}
	// Derive a symmetric key for data store encryption from the same password.
	dataDir := expandHome(cfg.DataDir)
	salt, err := config.LoadOrGenerateSalt(dataDir)
	if err != nil {
		zeroBytes(pw)
		return cfg, nil, fmt.Errorf("derive data key: %w", err)
	}
	key := config.DeriveKey(pw, salt)
	zeroBytes(pw)
	return cfg, key, nil
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

	cfg, encKey, err := loadConfigAndDeriveKey()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Zero encKey on exit (best-effort; GC will eventually collect it anyway).
	defer zeroBytes(encKey)

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

	// Register LLM backends from config.
	// Always register configured backends regardless of Enabled flag so they
	// appear in the dropdown and can be selected per-session. Enabled only
	// controls whether the backend is used as the default (llm_backend).
	llm.Register(aider.New(cfg.Aider.Binary))
	llm.Register(goose.New(cfg.Goose.Binary))
	llm.Register(gemini.New(cfg.Gemini.Binary))
	llm.Register(opencode.New(cfg.OpenCode.Binary))
	llm.Register(opencode.NewACP(cfg.OpenCode.Binary))
	llm.Register(ollama.NewWithHost(cfg.Ollama.Model, "ollama", cfg.Ollama.Host))
	llm.Register(openwebui.New(cfg.OpenWebUI.URL, cfg.OpenWebUI.APIKey, cfg.OpenWebUI.Model))
	if cfg.Shell.ScriptPath != "" {
		llm.Register(shell.New(cfg.Shell.ScriptPath))
	}

	// Create session manager (passes encKey for encrypted session store when --secure)
	idleTimeout := time.Duration(cfg.Session.InputIdleTimeout) * time.Second
	mgr, err := session.NewManager(cfg.Hostname, cfg.DataDir, cfg.Session.ClaudeCodeBin, idleTimeout, encKey)
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
	}

	// If channel mode is enabled, extract the embedded channel server and register it with claude mcp.
	if cfg.Session.ChannelEnabled {
		if err := setupChannelMCP(cfg); err != nil {
			fmt.Printf("[warn] channel MCP setup: %v\n", err)
		}
	}

	// Re-register claude-code with config-driven options (skip_permissions, channel_enabled etc.)
	llm.Register(claudecode.NewWithOptions(cfg.Session.ClaudeCodeBin, cfg.Session.SkipPermissions, cfg.Session.ChannelEnabled))

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
		mgr.SetLLMBackendObj(b)
	}
	mgr.SetVerbose(verbose)
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

	// Create schedule store
	// All data stores use encrypted constructors when --secure is active.
	newScheduleStore := func(path string) (*session.ScheduleStore, error) {
		if encKey != nil {
			return session.NewScheduleStoreEncrypted(path, encKey)
		}
		return session.NewScheduleStore(path)
	}
	newCmdLibrary := func(path string) (*session.CmdLibrary, error) {
		if encKey != nil {
			return session.NewCmdLibraryEncrypted(path, encKey)
		}
		return session.NewCmdLibrary(path)
	}
	newAlertStore := func(path string) (*alertspkg.Store, error) {
		if encKey != nil {
			return alertspkg.NewStoreEncrypted(path, encKey)
		}
		return alertspkg.NewStore(path)
	}
	newFilterStore := func(path string) (*session.FilterStore, error) {
		if encKey != nil {
			return session.NewFilterStoreEncrypted(path, encKey)
		}
		return session.NewFilterStore(path)
	}

	schedStore, err := newScheduleStore(schedStorePath(cfg))
	if err != nil {
		return fmt.Errorf("open schedule store: %w", err)
	}

	// Create command library
	cmdLib, err := newCmdLibrary(filepath.Join(expandHome(cfg.DataDir), "commands.json"))
	if err != nil {
		return fmt.Errorf("open command library: %w", err)
	}

	// Create alert store
	alertStore, err := newAlertStore(filepath.Join(expandHome(cfg.DataDir), "alerts.json"))
	if err != nil {
		return fmt.Errorf("open alert store: %w", err)
	}

	// Create filter store
	filterStore, err := newFilterStore(filepath.Join(expandHome(cfg.DataDir), "filters.json"))
	if err != nil {
		return fmt.Errorf("open filter store: %w", err)
	}

	// Wire filter engine to session output
	filterEngine := session.NewFilterEngine(filterStore, session.ActionHandlers{
		SendInput: func(sessID, text string) error {
			return mgr.SendInput(sessID, text, "filter")
		},
		AddAlert: func(sessID, title, body string) {
			alertStore.Add(alertspkg.LevelInfo, title, body, sessID)
		},
		AddSchedule: func(sessID, command string) error {
			_, err := schedStore.Add(sessID, command, time.Time{}, "")
			return err
		},
		DetectPrompt: func(sessID, line string) {
			mgr.MarkWaitingInput(sessID, line)
		},
	})
	mgr.SetOutputHandler(func(sess *session.Session, line string) {
		filterEngine.ProcessLine(sess, line)
	})

	var (
		routers    []*router.Router
		wg         sync.WaitGroup
		httpServer *server.HTTPServer
	)

	// newRouter is a helper that creates a router and wires in schedule + version + alerts.
	newRouter := func(hostname, groupID string, backend messaging.Backend) *router.Router {
		r := router.NewRouter(hostname, groupID, backend, mgr, cfg.Session.TailLines, wm)
		r.SetScheduleStore(schedStore)
		r.SetAlertStore(alertStore)
		r.SetCmdLibrary(cmdLib)
		r.SetVersion(Version)
		r.SetUpdateChecker(func() string {
			v, _ := fetchLatestVersion()
			return v
		})
		r.SetRestartFunc(func() {
			selfPath, err2 := os.Executable()
			if err2 == nil {
				selfPath, _ = filepath.EvalSymlinks(selfPath)
				_ = syscall.Exec(selfPath, os.Args, os.Environ())
			}
			os.Exit(0)
		})
		return r
	}

	// Signal backend (if configured)
	if cfg.Signal.AccountNumber != "" && cfg.Signal.GroupID != "" {
		debugf("starting signal-cli backend account=%s group=%s", cfg.Signal.AccountNumber, cfg.Signal.GroupID)
		backend, err := signalpkg.NewSignalCLIBackend(cfg.Signal.ConfigDir, cfg.Signal.AccountNumber)
		if err != nil {
			return fmt.Errorf("start signal-cli: %w", err)
		}
		backend.SetVerbose(verbose)
		defer backend.Close() //nolint:errcheck
		adapted := signalpkg.NewMessagingAdapter(backend)
		r := newRouter(cfg.Hostname, cfg.Signal.GroupID, adapted)
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
			r := newRouter(cfg.Hostname, chatIDStr, tgB)
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
			r := newRouter(cfg.Hostname, channelID, discordB)
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
			r := newRouter(cfg.Hostname, channelID, slackB)
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
		r := newRouter(cfg.Hostname, cfg.Twilio.ToNumber, twilioB)
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
			r := newRouter(cfg.Hostname, cfg.Matrix.RoomID, matrixB)
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
		r := newRouter(cfg.Hostname, "github", ghB)
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
		r := newRouter(cfg.Hostname, "webhook", wbB)
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
		httpServer.SetScheduleStore(schedStore)
		httpServer.SetCmdLibrary(cmdLib)
		httpServer.SetAlertStore(alertStore)
		httpServer.SetFilterStore(filterStore)
		httpServer.SetUpdateFuncs(installPrebuiltBinary, fetchLatestVersion)
		httpServer.SetRestartFunc(func() {
			selfPath, err2 := os.Executable()
			if err2 == nil {
				selfPath, _ = filepath.EvalSymlinks(selfPath)
				_ = syscall.Exec(selfPath, os.Args, os.Environ())
			}
			os.Exit(0)
		})

		// Wire opencode ACP SSE replies through the same channel_reply WS broadcast
		// as claude MCP channel replies, so the web UI renders them as amber lines.
		hs := httpServer // capture
		opencode.OnChannelReply = func(fullID, text string) {
			hs.BroadcastChannelReply(fullID, text)
		}

		// Wire SetACPFullID: when a new session starts with opencode-acp backend,
		// associate the datawatch full_id with the tmux session name.
		mgr.SetOnSessionStart(func(sess *session.Session) {
			if sess.LLMBackend == "opencode-acp" {
				opencode.SetACPFullID(sess.TmuxSession, sess.FullID)
			}
		})
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

	// Register alert broadcast listener (must be after httpServer is created and routers are populated)
	alertStore.AddListener(func(a *alertspkg.Alert) {
		if httpServer != nil {
			httpServer.NotifyAlert(a)
		}
		for _, r := range routers {
			r.SendAlert(a)
		}
	})

	// Start the scheduler goroutine (fires timed commands and on-input-prompt commands)
	go runScheduler(ctx, schedStore, mgr)

	// Start MCP SSE server for remote AI client access (if configured)
	if cfg.MCP.SSEEnabled {
		mcpSrv := mcp.New(cfg.Hostname, mgr, &cfg.MCP, cfg.DataDir, mcp.Options{
			AlertStore:    alertStore,
			SchedStore:    schedStore,
			CmdLib:        cmdLib,
			Version:       Version,
			LatestVersion: fetchLatestVersion,
			RestartFn: func() {
				selfPath, err2 := os.Executable()
				if err2 == nil {
					selfPath, _ = filepath.EvalSymlinks(selfPath)
					_ = syscall.Exec(selfPath, os.Args, os.Environ())
				}
				os.Exit(0)
			},
		})
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
		// Create an alert so the user sees it in the web UI alert badge even if
		// no messaging backend is active.
		displayID := sess.ID
		if sess.Name != "" {
			displayID = sess.Name
		}
		alertStore.Add(alertspkg.LevelInfo,
			fmt.Sprintf("Session %s waiting for input", displayID),
			prompt,
			sess.FullID,
		)
		fireInputSchedules(schedStore, mgr, sess)
	})

	fmt.Printf("[%s] datawatch v%s started.\n", cfg.Hostname, Version)

	if len(routers) == 0 && !cfg.Server.Enabled && !cfg.MCP.SSEEnabled {
		return fmt.Errorf("no backends enabled — run `datawatch setup <service>` to configure a messaging backend\n" +
			"  Available: signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web\n" +
			"  Or run `datawatch config show` to see current configuration")
	}

	// Auto-update goroutine
	if cfg.Update.Enabled {
		go runAutoUpdater(ctx, cfg)
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

// runAutoUpdater checks for and installs updates on the configured schedule.
func runAutoUpdater(ctx context.Context, cfg *config.Config) {
	schedule := cfg.Update.Schedule
	if schedule == "" {
		schedule = "daily"
	}
	timeOfDay := cfg.Update.TimeOfDay
	if timeOfDay == "" {
		timeOfDay = "03:00"
	}

	nextRun := nextScheduledTime(schedule, timeOfDay)
	fmt.Printf("[updater] auto-update enabled (%s at %s), next check: %s\n", schedule, timeOfDay, nextRun.Format("2006-01-02 15:04"))

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(nextRun)):
		}

		fmt.Println("[updater] checking for updates...")
		latest, err := fetchLatestVersion()
		if err != nil {
			fmt.Printf("[updater] check failed: %v\n", err)
		} else if latest != "" && latest != Version {
			fmt.Printf("[updater] update available: v%s -> v%s, installing...\n", Version, latest)
			if err := installPrebuiltBinary(latest); err != nil {
				fmt.Printf("[updater] install failed: %v\n", err)
			} else {
				fmt.Printf("[updater] updated to v%s. Restart the daemon to apply (`datawatch stop && datawatch start`).\n", latest)
			}
		} else {
			fmt.Printf("[updater] already up to date (v%s)\n", Version)
		}

		nextRun = nextScheduledTime(schedule, timeOfDay)
	}
}

// nextScheduledTime returns the next time the given schedule should fire.
// schedule: "hourly", "daily", "weekly"; timeOfDay: "HH:MM" (24h).
func nextScheduledTime(schedule, timeOfDay string) time.Time {
	now := time.Now()
	var h, m int
	fmt.Sscanf(timeOfDay, "%d:%d", &h, &m)

	switch schedule {
	case "hourly":
		next := now.Truncate(time.Hour).Add(time.Hour)
		return next
	case "weekly":
		// Next Sunday at timeOfDay
		daysUntilSunday := int(time.Sunday - now.Weekday())
		if daysUntilSunday <= 0 {
			daysUntilSunday += 7
		}
		candidate := time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, h, m, 0, 0, now.Location())
		return candidate
	default: // "daily"
		candidate := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
		if candidate.Before(now) {
			candidate = candidate.Add(24 * time.Hour)
		}
		return candidate
	}
}

// installPrebuiltBinary downloads and installs a prebuilt binary from GitHub releases.
func installPrebuiltBinary(version string) error {
	goos := func() string {
		out, err := exec.Command("go", "env", "GOOS").Output()
		if err != nil {
			return "linux"
		}
		return strings.TrimSpace(string(out))
	}()
	goarch := func() string {
		out, err := exec.Command("go", "env", "GOARCH").Output()
		if err != nil {
			return "amd64"
		}
		return strings.TrimSpace(string(out))
	}()

	var archiveName, binaryInArchive string
	if goos == "windows" {
		archiveName = fmt.Sprintf("datawatch_%s_%s_%s.zip", version, goos, goarch)
		binaryInArchive = "datawatch.exe"
	} else {
		archiveName = fmt.Sprintf("datawatch_%s_%s_%s.tar.gz", version, goos, goarch)
		binaryInArchive = "datawatch"
	}

	url := fmt.Sprintf("https://github.com/dmz006/datawatch/releases/download/v%s/%s", version, archiveName)
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable path: %w", err)
	}
	selfPath, _ = filepath.EvalSymlinks(selfPath)

	tmpDir, err := os.MkdirTemp("", "datawatch-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, archiveName)
	httpClient := &http.Client{Timeout: 5 * time.Minute}

	fmt.Printf("[update] Downloading %s ...\n", archiveName)
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	lastPct := -1
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				return writeErr
			}
			downloaded += int64(n)
			if total > 0 {
				pct := int(downloaded * 100 / total)
				if pct != lastPct && pct%10 == 0 {
					fmt.Printf("[update] Download: %d%% (%d / %d KB)\n", pct, downloaded/1024, total/1024)
					lastPct = pct
				}
			} else if downloaded%(512*1024) == 0 {
				fmt.Printf("[update] Downloaded %d KB...\n", downloaded/1024)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			return readErr
		}
	}
	f.Close()
	fmt.Printf("[update] Download complete (%d KB). Extracting...\n", downloaded/1024)

	// Extract binary
	newBin := filepath.Join(tmpDir, "datawatch-new")
	var extractErr error
	if goos == "windows" {
		extractErr = extractFromZip(archivePath, binaryInArchive, newBin)
	} else {
		extractErr = extractFromTarGz(archivePath, binaryInArchive, newBin)
	}
	if extractErr != nil {
		return fmt.Errorf("extract binary: %w", extractErr)
	}

	fmt.Println("[update] Installing new binary...")
	// Replace current binary
	if err := os.Chmod(newBin, 0755); err != nil {
		return err
	}
	if err := replaceExecutable(selfPath, newBin); err != nil {
		return err
	}
	fmt.Printf("[update] Successfully updated to v%s.\n", version)
	return nil
}

func extractFromTarGz(archivePath, target, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Name == target || filepath.Base(hdr.Name) == target {
			out, err := os.Create(dest)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("binary %q not found in archive", target)
}

func extractFromZip(archivePath, target, dest string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == target || filepath.Base(f.Name) == target {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.Create(dest)
			if err != nil {
				rc.Close()
				return err
			}
			_, err = io.Copy(out, rc)
			rc.Close()
			out.Close()
			return err
		}
	}
	return fmt.Errorf("binary %q not found in zip", target)
}

func replaceExecutable(dest, src string) error {
	// Write to a temp file next to the destination, then rename (atomic on same fs)
	tmp := dest + ".new"
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// ---- daemon helpers -------------------------------------------------------
// daemonize() is defined in daemon_unix.go (Unix/macOS) and daemon_windows.go (Windows).

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

// setupChannelMCP extracts the embedded channel server and registers it with
// claude mcp. Called at daemon start when channel_enabled is true.
// Returns an error (non-fatal: caller prints a warning) if node is missing.
func setupChannelMCP(cfg *config.Config) error {
	// Require Node.js ≥ 18 — the channel server uses ESM top-level await.
	if _, err := channel.NodePath(); err != nil {
		return fmt.Errorf("channel_enabled requires Node.js (≥18) in PATH: %w\n"+
			"  Install: https://nodejs.org/en/download  or  sudo apt install nodejs npm\n"+
			"  Disable with: channel_enabled: false in config to suppress this warning", err)
	}

	dataDir := expandHome(cfg.DataDir)
	jsPath, err := channel.EnsureExtracted(dataDir)
	if err != nil {
		return fmt.Errorf("extract channel.js: %w", err)
	}

	// Build env: API URL and optional token.
	apiURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.Server.Port)
	if cfg.Server.Port == 0 {
		apiURL = "http://127.0.0.1:8080"
	}
	env := map[string]string{
		"DATAWATCH_API_URL": apiURL,
	}
	if cfg.Server.Token != "" {
		env["DATAWATCH_TOKEN"] = cfg.Server.Token
	}
	if cfg.Server.ChannelPort != 0 {
		env["DATAWATCH_CHANNEL_PORT"] = fmt.Sprintf("%d", cfg.Server.ChannelPort)
	}

	if err := channel.RegisterMCP(jsPath, env); err != nil {
		return fmt.Errorf("register claude mcp: %w", err)
	}
	fmt.Printf("[channel] registered channel server with claude mcp (%s)\n", jsPath)
	return nil
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

// ---- restart command ------------------------------------------------------

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the datawatch daemon",
		Long:  "Stop the running daemon (SIGTERM) and start a fresh one. Active AI sessions in tmux are preserved.",
		RunE:  runRestart,
	}
}

func runRestart(_ *cobra.Command, _ []string) error {
	cfg, _ := loadConfig()
	pidPath := filepath.Join(expandHome(cfg.DataDir), "daemon.pid")

	// Try graceful stop of existing daemon
	if data, err := os.ReadFile(pidPath); err == nil {
		var pid int
		if _, scanErr := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); scanErr == nil && pid > 0 {
			if proc, procErr := os.FindProcess(pid); procErr == nil {
				_ = proc.Signal(syscall.SIGTERM)
				// Wait up to 3s for the process to exit
				for i := 0; i < 30; i++ {
					time.Sleep(100 * time.Millisecond)
					if err2 := proc.Signal(syscall.Signal(0)); err2 != nil {
						break // process gone
					}
				}
			}
		}
		_ = os.Remove(pidPath)
	}

	// Start new daemon (daemonize calls os.Exit, so this is exec-and-exit)
	return daemonize()
}

// ---- status command -------------------------------------------------------

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status and active sessions",
		Long:  "Check if the datawatch daemon is running and list active sessions, highlighting any waiting for input.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _ := loadConfig()
			return runStatus(cfg)
		},
	}
}

func runStatus(cfg *config.Config) error {
	dataDir := expandHome(cfg.DataDir)
	pidPath := filepath.Join(dataDir, "daemon.pid")

	// --- daemon state ---
	daemonRunning := false
	daemonPID := 0
	if data, err := os.ReadFile(pidPath); err == nil {
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					daemonRunning = true
					daemonPID = pid
				}
			}
		}
	}

	if daemonRunning {
		fmt.Printf("daemon: running  (PID %d)\n", daemonPID)
	} else {
		fmt.Println("daemon: stopped")
	}

	// Try to get live data from the running daemon first.
	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	type apiSession struct {
		FullID     string `json:"full_id"`
		ID         string `json:"id"`
		Name       string `json:"name"`
		Task       string `json:"task"`
		State      string `json:"state"`
		LLMBackend string `json:"llm_backend"`
		UpdatedAt  string `json:"updated_at"`
	}
	var sessions []apiSession
	apiURL := fmt.Sprintf("http://localhost:%d/api/sessions", port)
	token := cfg.Server.Token
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err == nil {
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		client := &http.Client{Timeout: 2 * time.Second}
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			json.NewDecoder(resp.Body).Decode(&sessions) //nolint:errcheck
		}
	}

	// Fallback: read from local store if API unavailable.
	if len(sessions) == 0 {
		store, err := session.NewStore(filepath.Join(dataDir, "sessions.json"))
		if err == nil {
			for _, s := range store.List() {
				sessions = append(sessions, apiSession{
					FullID:     s.FullID,
					ID:         s.ID,
					Name:       s.Name,
					Task:       s.Task,
					State:      string(s.State),
					LLMBackend: s.LLMBackend,
					UpdatedAt:  s.UpdatedAt.Format("15:04:05"),
				})
			}
		}
	}

	// Filter to active sessions.
	var active []apiSession
	for _, s := range sessions {
		if s.State != "complete" && s.State != "killed" && s.State != "failed" {
			active = append(active, s)
		}
	}

	if len(active) == 0 {
		fmt.Println("sessions: none active")
		return nil
	}

	fmt.Printf("sessions: %d active\n\n", len(active))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATE\tBACKEND\tUPDATED\tNAME/TASK")
	for _, s := range active {
		display := s.Task
		if s.Name != "" {
			display = s.Name + ": " + s.Task
		}
		stateDisplay := s.State
		if s.State == "waiting_input" {
			stateDisplay = "WAITING INPUT ⚠"
		}
		updatedAt := s.UpdatedAt
		if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
			if t.Day() == time.Now().Day() {
				updatedAt = t.Format("15:04:05")
			} else {
				updatedAt = t.Format("Jan 02 15:04")
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.ID, stateDisplay, s.LLMBackend, updatedAt,
			truncate(display, 55))
	}
	w.Flush()
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

	// Check if Signal is already linked by looking for existing account data
	if isSignalAlreadyLinked(cfg.Signal.ConfigDir, cfg.Signal.AccountNumber) {
		fmt.Println("Signal is already set up on this device.")
		if cfg.Signal.AccountNumber != "" {
			fmt.Printf("  Account: %s\n", cfg.Signal.AccountNumber)
		}
		fmt.Printf("  Config dir: %s\n", cfg.Signal.ConfigDir)
		fmt.Println()
		fmt.Println("To re-link or reset:")
		fmt.Println("  1. Remove the config dir:  rm -rf " + cfg.Signal.ConfigDir)
		fmt.Println("  2. Clear config:           datawatch config set signal.account_number ''")
		fmt.Println("  3. Re-run:                 datawatch link")
		fmt.Println()
		fmt.Println("Or to remove this linked device from your phone:")
		fmt.Println("  Signal app → Settings → Linked Devices → find this device → remove")
		return nil
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

// isSignalAlreadyLinked returns true if signal-cli has existing account data in configDir.
func isSignalAlreadyLinked(configDir, accountNumber string) bool {
	// Check accounts.json — exists when at least one account is registered
	accountsFile := filepath.Join(configDir, "accounts.json")
	if data, err := os.ReadFile(accountsFile); err == nil && len(data) > 10 {
		// accounts.json exists with content
		if accountNumber == "" || strings.Contains(string(data), accountNumber) {
			return true
		}
	}
	// Fallback: check for data directory with account subdirectory
	dataDir := filepath.Join(configDir, "data")
	if accountNumber != "" {
		acctDir := filepath.Join(dataDir, accountNumber)
		if _, err := os.Stat(acctDir); err == nil {
			return true
		}
	} else {
		// Any account directory present
		entries, err := os.ReadDir(dataDir)
		if err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
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

	// session timeline <id> — show structured timeline events for a session
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "timeline <id>",
		Short: "Show the structured event timeline for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runSessionTimeline(cfg, args[0])
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

	// session schedule — manage scheduled commands for a session
	scheduleCmd := &cobra.Command{
		Use:   "schedule",
		Short: "Schedule commands to run for a session",
	}
	schedAddCmd := &cobra.Command{
		Use:   "add <session-id> <command>",
		Short: "Schedule a command for a session",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sessionID := args[0]
			command := strings.Join(args[1:], " ")
			at, _ := cmd.Flags().GetString("at")
			return runScheduleAdd(cfg, sessionID, command, at)
		},
	}
	schedAddCmd.Flags().String("at", "", "When to run: 'now', 'HH:MM', or RFC3339 timestamp. Default: on next input prompt")
	scheduleCmd.AddCommand(schedAddCmd)
	scheduleCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all scheduled commands",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runScheduleList(cfg)
		},
	})
	scheduleCmd.AddCommand(&cobra.Command{
		Use:   "cancel <schedule-id>",
		Short: "Cancel a pending scheduled command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runScheduleCancel(cfg, args[0])
		},
	})
	sessionCmd.AddCommand(scheduleCmd)

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

// daemonAPIURL returns the HTTP API /api/command URL for the target daemon.
// If --server is set, targets the named remote server from config.Servers.
func daemonAPIURL(cfg *config.Config) string {
	if serverName != "" && serverName != "local" {
		for _, s := range cfg.Servers {
			if s.Name == serverName && s.Enabled {
				return strings.TrimRight(s.URL, "/") + "/api/command"
			}
		}
		// Not found — fall through to localhost
		fmt.Fprintf(os.Stderr, "warning: server %q not found in config, using localhost\n", serverName)
	}
	return fmt.Sprintf("http://localhost:%d/api/command", cfg.Server.Port)
}

// daemonHTTPClient returns an http.Client with the appropriate auth header for the target server.
func daemonHTTPClient(cfg *config.Config) (*http.Client, string) {
	token := cfg.Server.Token
	if serverName != "" && serverName != "local" {
		for _, s := range cfg.Servers {
			if s.Name == serverName && s.Enabled {
				token = s.Token
				break
			}
		}
	}
	return &http.Client{Timeout: 15 * time.Second}, token
}

// tryDaemonRequest posts JSON to a daemon API endpoint with optional auth.
func tryDaemonRequest(cfg *config.Config, url string, body []byte) (bool, error) {
	client, token := daemonHTTPClient(cfg)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	resp.Body.Close()
	return true, nil
}

// tryDaemonCommand posts a command text to the daemon HTTP API.
// Returns true if the daemon responded, false if not reachable.
func tryDaemonCommand(cfg *config.Config, text string) (bool, error) {
	body, _ := json.Marshal(map[string]string{"text": text})
	return tryDaemonRequest(cfg, daemonAPIURL(cfg), body)
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

func runSessionTimeline(cfg *config.Config, id string) error {
	// Try HTTP API first
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/sessions/timeline?id=%s", cfg.Server.Port, id))
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var result struct {
			SessionID string   `json:"session_id"`
			Lines     []string `json:"lines"`
		}
		if err2 := json.NewDecoder(resp.Body).Decode(&result); err2 == nil {
			fmt.Printf("Timeline for %s:\n\n", result.SessionID)
			for _, l := range result.Lines {
				fmt.Println(l)
			}
			return nil
		}
	}

	// Fall back: read timeline.md directly from the session tracking dir
	store, err2 := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err2 != nil {
		return err2
	}
	sess, ok := store.GetByShortID(id)
	if !ok {
		sess, ok = store.Get(id)
	}
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	timelinePath := filepath.Join(expandHome(cfg.DataDir), "sessions", sess.FullID, "timeline.md")
	data, err3 := os.ReadFile(timelinePath)
	if err3 != nil {
		return fmt.Errorf("read timeline: %w", err3)
	}
	fmt.Printf("Timeline for %s:\n\n%s\n", sess.FullID, string(data))
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

// ---- schedule helper functions --------------------------------------------

func schedStorePath(cfg *config.Config) string {
	return filepath.Join(expandHome(cfg.DataDir), "schedule.json")
}

func runScheduleAdd(cfg *config.Config, sessionID, command, at string) error {
	store, err := session.NewScheduleStore(schedStorePath(cfg))
	if err != nil {
		return fmt.Errorf("open schedule store: %w", err)
	}
	var runAt time.Time
	if at != "" && at != "now" {
		// Try HH:MM first
		if t, err2 := time.Parse("15:04", at); err2 == nil {
			now := time.Now()
			runAt = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			if runAt.Before(now) {
				runAt = runAt.Add(24 * time.Hour)
			}
		} else {
			// Try RFC3339
			runAt, err = time.Parse(time.RFC3339, at)
			if err != nil {
				return fmt.Errorf("invalid --at value %q: use 'now', 'HH:MM', or RFC3339 timestamp", at)
			}
		}
	}
	sc, err := store.Add(sessionID, command, runAt, "")
	if err != nil {
		return fmt.Errorf("schedule add: %w", err)
	}
	when := "on next input prompt"
	if !sc.RunAt.IsZero() {
		when = sc.RunAt.Format("2006-01-02 15:04")
	}
	fmt.Printf("Scheduled [%s] for session %s at %s:\n  %s\n", sc.ID, sessionID, when, command)
	return nil
}

func runScheduleList(cfg *config.Config) error {
	store, err := session.NewScheduleStore(schedStorePath(cfg))
	if err != nil {
		return fmt.Errorf("open schedule store: %w", err)
	}
	entries := store.List()
	if len(entries) == 0 {
		fmt.Println("No scheduled commands.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSESSION\tSTATE\tWHEN\tCOMMAND")
	for _, sc := range entries {
		when := "on input"
		if !sc.RunAt.IsZero() {
			when = sc.RunAt.Format("15:04")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", sc.ID, sc.SessionID, sc.State, when, truncate(sc.Command, 40))
	}
	return w.Flush()
}

func runScheduleCancel(cfg *config.Config, id string) error {
	store, err := session.NewScheduleStore(schedStorePath(cfg))
	if err != nil {
		return fmt.Errorf("open schedule store: %w", err)
	}
	if err := store.Cancel(id); err != nil {
		return err
	}
	fmt.Printf("Scheduled command %s cancelled.\n", id)
	return nil
}

// fireInputSchedules fires any on-input-prompt scheduled commands for a session.
// Called from the combined NeedsInputHandler in runStart.
func fireInputSchedules(store *session.ScheduleStore, mgr *session.Manager, sess *session.Session) {
	pending := store.WaitingInputPending(sess.FullID)
	if len(pending) == 0 {
		pending = store.WaitingInputPending(sess.ID)
	}
	for _, sc := range pending {
		if err := mgr.SendInput(sess.FullID, sc.Command, "schedule"); err != nil {
			fmt.Printf("[scheduler] failed to send input for [%s]: %v\n", sc.ID, err)
			_ = store.MarkDone(sc.ID, true)
		} else {
			fmt.Printf("[scheduler] sent [%s] to session %s\n", sc.ID, sess.ID)
			_ = store.MarkDone(sc.ID, false)
			for _, next := range store.AfterDone(sc.ID) {
				if err2 := mgr.SendInput(sess.FullID, next.Command, "schedule"); err2 != nil {
					_ = store.MarkDone(next.ID, true)
				} else {
					_ = store.MarkDone(next.ID, false)
				}
			}
		}
	}
}

// runScheduler is a daemon goroutine that fires time-based scheduled commands every 10 seconds.
// On-input-prompt commands are handled by fireInputSchedules called from the NeedsInputHandler.
func runScheduler(ctx context.Context, store *session.ScheduleStore, mgr *session.Manager) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			for _, sc := range store.DuePending(t) {
				sess, ok := mgr.GetSession(sc.SessionID)
				if !ok {
					// Session may use short ID
					fmt.Printf("[scheduler] session %q not found for command [%s], skipping\n", sc.SessionID, sc.ID)
					_ = store.MarkDone(sc.ID, true)
					continue
				}
				if err := mgr.SendInput(sess.FullID, sc.Command, "schedule"); err != nil {
					fmt.Printf("[scheduler] failed to send input for [%s]: %v\n", sc.ID, err)
					_ = store.MarkDone(sc.ID, true)
				} else {
					fmt.Printf("[scheduler] sent [%s] to session %s\n", sc.ID, sess.ID)
					_ = store.MarkDone(sc.ID, false)
					for _, next := range store.AfterDone(sc.ID) {
						if err2 := mgr.SendInput(sess.FullID, next.Command, "schedule"); err2 != nil {
							_ = store.MarkDone(next.ID, true)
						} else {
							_ = store.MarkDone(next.ID, false)
						}
					}
				}
			}
		}
	}
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

	// Load optional stores for MCP (best-effort, non-fatal on error)
	mcpSchedStore, _ := session.NewScheduleStore(schedStorePath(cfg))
	mcpCmdLib, _ := session.NewCmdLibrary(cmdLibPath(cfg))
	mcpAlertStore, _ := alertspkg.NewStore(filepath.Join(expandHome(cfg.DataDir), "alerts.json"))

	mcpSrv := mcp.New(cfg.Hostname, mgr, &cfg.MCP, cfg.DataDir, mcp.Options{
		AlertStore:    mcpAlertStore,
		SchedStore:    mcpSchedStore,
		CmdLib:        mcpCmdLib,
		Version:       Version,
		LatestVersion: fetchLatestVersion,
	})

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

// ---- update command --------------------------------------------------------

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install datawatch updates",
		RunE:  runUpdate,
	}
	cmd.Flags().Bool("check", false, "Check for a new version without installing")
	return cmd
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")

	latest, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	fmt.Printf("Current version: v%s\n", Version)
	fmt.Printf("Latest version:  v%s\n", latest)

	if latest == Version {
		fmt.Println("Already up to date.")
		return nil
	}

	if checkOnly {
		fmt.Printf("Update available. Run `datawatch update` to install v%s.\n", latest)
		return nil
	}

	// Try prebuilt binary first; fall back to go install if not available
	fmt.Printf("Downloading prebuilt binary for v%s...\n", latest)
	if err := installPrebuiltBinary(latest); err != nil {
		fmt.Printf("[update] Prebuilt download failed (%v), falling back to go install...\n", err)
		goExe, goErr := exec.LookPath("go")
		if goErr != nil {
			return fmt.Errorf("go not found in PATH — install manually: go install github.com/dmz006/datawatch/cmd/datawatch@v%s", latest)
		}
		installCmd := exec.Command(goExe, "install", fmt.Sprintf("github.com/dmz006/datawatch/cmd/datawatch@v%s", latest))
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("go install failed: %w\nInstall manually: go install github.com/dmz006/datawatch/cmd/datawatch@v%s", err, latest)
		}
		fmt.Printf("Updated to v%s. Restart the daemon with `datawatch stop && datawatch start`.\n", latest)
	} else {
		fmt.Printf("Restart the daemon with `datawatch stop && datawatch start` to apply.\n")
	}
	return nil
}

// fetchLatestVersion queries the GitHub releases API for the latest tag.
func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/dmz006/datawatch/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "datawatch/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}
	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return strings.TrimPrefix(result.TagName, "v"), nil
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
		Short: "Interactive setup wizard for a backend, LLM, session defaults, or MCP",
		Long: `Run an interactive setup wizard to configure a backend or subsystem.

Messaging backends:
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
  web       Enable or disable the web UI / HTTP API server
  server    Add or update a remote datawatch server connection

LLM backends:
  llm claude-code   Configure claude CLI settings
  llm aider         Configure aider
  llm goose         Configure goose
  llm gemini        Configure Gemini CLI
  llm opencode      Configure opencode
  llm ollama        Configure local Ollama
  llm openwebui     Configure OpenWebUI
  llm shell         Configure custom shell script

Session and MCP:
  session   Configure session management defaults
  mcp       Configure the MCP server (Cursor, Claude Desktop, VS Code)`,
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
		newSetupServerCmd(),
		newSetupLLMCmd(),
		newSetupSessionCmd(),
		newSetupMCPCmd(),
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

// ---- setup server command --------------------------------------------------

func newSetupServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Add or update a remote datawatch server connection",
		RunE:  runSetupServer,
	}
}

func runSetupServer(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}

	fmt.Println("Remote Server Setup")
	fmt.Println("===================")
	fmt.Println("Add a connection to a remote datawatch instance.")
	fmt.Println()
	if len(cfg.Servers) > 0 {
		fmt.Println("Configured servers:")
		for _, s := range cfg.Servers {
			status := "enabled"
			if !s.Enabled {
				status = "disabled"
			}
			fmt.Printf("  %-12s  %-30s  [%s]\n", s.Name, s.URL, status)
		}
		fmt.Println()
	}

	reader := bufio.NewReader(os.Stdin)
	name := cliPrompt(reader, "Short name for this server (e.g. prod, pi, vps): ", "")
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.ContainsAny(name, " /\\") {
		return fmt.Errorf("name must not contain spaces or slashes")
	}

	url := cliPrompt(reader, "Server URL (e.g. http://192.168.1.10:8080): ", "")
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("URL must start with http:// or https://")
	}

	token := cliPrompt(reader, "Bearer token (press Enter to skip): ", "")

	// Test connectivity
	fmt.Printf("Testing connection to %s ...\n", url)
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(url, "/")+"/api/health", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Warning: could not reach server (%v). Saving anyway.\n", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			fmt.Println("Connection successful.")
		} else {
			fmt.Printf("Warning: server returned HTTP %d. Check URL and token.\n", resp.StatusCode)
		}
	}

	entry := config.RemoteServerConfig{
		Name:    name,
		URL:     url,
		Token:   token,
		Enabled: true,
	}
	// Replace existing or append
	replaced := false
	for i, s := range cfg.Servers {
		if s.Name == name {
			cfg.Servers[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Servers = append(cfg.Servers, entry)
	}

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Printf("Server %q saved. Use `datawatch --server %s session list` to target it.\n", name, name)
	return nil
}

// ---- setup llm command group -----------------------------------------------

func newSetupLLMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm [backend]",
		Short: "Configure an LLM backend",
		Long: `Configure one of the supported LLM backends.

Available backends:
  claude-code   claude CLI (default)
  aider         aider (multi-model coding assistant)
  goose         goose (Block/Square AI coding agent)
  gemini        Gemini CLI
  opencode      opencode CLI
  ollama        local Ollama instance
  openwebui     OpenWebUI server
  shell         custom shell script`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newSetupLLMClaudeCodeCmd(),
		newSetupLLMAiderCmd(),
		newSetupLLMGooseCmd(),
		newSetupLLMGeminiCmd(),
		newSetupLLMOpenCodeCmd(),
		newSetupLLMOllamaCmd(),
		newSetupLLMOpenWebUICmd(),
		newSetupLLMShellCmd(),
	)
	return cmd
}

func newSetupLLMClaudeCodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude-code",
		Short: "Configure claude-code LLM backend",
		RunE:  runSetupLLMClaudeCode,
	}
}

func runSetupLLMClaudeCode(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Claude Code LLM Backend Setup")
	fmt.Println("=============================")
	fmt.Println("Configures the claude CLI binary used to run AI coding sessions.")
	fmt.Println()

	cfg.Session.ClaudeCodeBin = cliPrompt(reader, "claude binary path", func() string {
		if cfg.Session.ClaudeCodeBin != "" {
			return cfg.Session.ClaudeCodeBin
		}
		return "claude"
	}())
	skipChoice := cliPrompt(reader, "Skip permissions (--dangerously-skip-permissions)? (y/n)", func() string {
		if cfg.Session.SkipPermissions {
			return "y"
		}
		return "n"
	}())
	cfg.Session.SkipPermissions = strings.ToLower(skipChoice) == "y" || strings.ToLower(skipChoice) == "yes"
	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Println("claude-code backend configured. claude-code is always enabled as the default backend.")
	return nil
}

func newSetupLLMAiderCmd() *cobra.Command {
	return &cobra.Command{Use: "aider", Short: "Configure aider LLM backend", RunE: runSetupLLMAider}
}

func runSetupLLMAider(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Aider LLM Backend Setup")
	fmt.Println("=======================")
	fmt.Println("aider is a multi-model coding assistant. Install with: pip install aider-install && aider-install")
	fmt.Println()

	cfg.Aider.Binary = cliPrompt(reader, "aider binary path", func() string {
		if cfg.Aider.Binary != "" {
			return cfg.Aider.Binary
		}
		return "aider"
	}())
	enableChoice := cliPrompt(reader, "Enable aider backend? (y/n)", func() string {
		if cfg.Aider.Enabled {
			return "y"
		}
		return "n"
	}())
	cfg.Aider.Enabled = strings.ToLower(enableChoice) == "y" || strings.ToLower(enableChoice) == "yes"
	return setupSave(cfg)
}

func newSetupLLMGooseCmd() *cobra.Command {
	return &cobra.Command{Use: "goose", Short: "Configure goose LLM backend", RunE: runSetupLLMGoose}
}

func runSetupLLMGoose(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Goose LLM Backend Setup")
	fmt.Println("=======================")
	fmt.Println("goose is Block's AI coding agent. Install from: https://github.com/block/goose")
	fmt.Println()

	cfg.Goose.Binary = cliPrompt(reader, "goose binary path", func() string {
		if cfg.Goose.Binary != "" {
			return cfg.Goose.Binary
		}
		return "goose"
	}())
	enableChoice := cliPrompt(reader, "Enable goose backend? (y/n)", func() string {
		if cfg.Goose.Enabled {
			return "y"
		}
		return "n"
	}())
	cfg.Goose.Enabled = strings.ToLower(enableChoice) == "y" || strings.ToLower(enableChoice) == "yes"
	return setupSave(cfg)
}

func newSetupLLMGeminiCmd() *cobra.Command {
	return &cobra.Command{Use: "gemini", Short: "Configure Gemini CLI LLM backend", RunE: runSetupLLMGemini}
}

func runSetupLLMGemini(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Gemini CLI LLM Backend Setup")
	fmt.Println("============================")
	fmt.Println("Gemini CLI runs Google's Gemini model. Install with: npm install -g @google/gemini-cli")
	fmt.Println()

	cfg.Gemini.Binary = cliPrompt(reader, "gemini binary path", func() string {
		if cfg.Gemini.Binary != "" {
			return cfg.Gemini.Binary
		}
		return "gemini"
	}())
	enableChoice := cliPrompt(reader, "Enable gemini backend? (y/n)", func() string {
		if cfg.Gemini.Enabled {
			return "y"
		}
		return "n"
	}())
	cfg.Gemini.Enabled = strings.ToLower(enableChoice) == "y" || strings.ToLower(enableChoice) == "yes"
	return setupSave(cfg)
}

func newSetupLLMOpenCodeCmd() *cobra.Command {
	return &cobra.Command{Use: "opencode", Short: "Configure opencode LLM backend", RunE: runSetupLLMOpenCode}
}

func runSetupLLMOpenCode(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("OpenCode LLM Backend Setup")
	fmt.Println("==========================")
	fmt.Println("opencode is an open-source AI coding assistant. Install from: https://opencode.ai")
	fmt.Println()

	cfg.OpenCode.Binary = cliPrompt(reader, "opencode binary path", func() string {
		if cfg.OpenCode.Binary != "" {
			return cfg.OpenCode.Binary
		}
		return "opencode"
	}())
	enableChoice := cliPrompt(reader, "Enable opencode backend? (y/n)", func() string {
		if cfg.OpenCode.Enabled {
			return "y"
		}
		return "n"
	}())
	cfg.OpenCode.Enabled = strings.ToLower(enableChoice) == "y" || strings.ToLower(enableChoice) == "yes"
	return setupSave(cfg)
}

func newSetupLLMOllamaCmd() *cobra.Command {
	return &cobra.Command{Use: "ollama", Short: "Configure Ollama local LLM backend", RunE: runSetupLLMOllama}
}

func runSetupLLMOllama(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Ollama LLM Backend Setup")
	fmt.Println("========================")
	fmt.Println("Ollama runs LLMs locally or on a remote host.")
	fmt.Println("Install from: https://ollama.com")
	fmt.Println()

	// Prompt for server URL first so we can list available models.
	hostDefault := "http://localhost:11434"
	if cfg.Ollama.Host != "" {
		hostDefault = cfg.Ollama.Host
	}
	cfg.Ollama.Host = cliPrompt(reader, "Ollama server URL", hostDefault)

	// Try to list available models from the server.
	fmt.Printf("Connecting to %s...\n", cfg.Ollama.Host)
	models, fetchErr := ollama.ListModels(cfg.Ollama.Host)
	if fetchErr != nil {
		fmt.Printf("Could not fetch models: %v\n", fetchErr)
		fmt.Println("Enter model name manually.")
		modelDefault := "llama3"
		if cfg.Ollama.Model != "" {
			modelDefault = cfg.Ollama.Model
		}
		cfg.Ollama.Model = cliPrompt(reader, "Model name", modelDefault)
	} else if len(models) == 0 {
		fmt.Println("No models found on server. Pull a model first with: ollama pull llama3")
		modelDefault := "llama3"
		if cfg.Ollama.Model != "" {
			modelDefault = cfg.Ollama.Model
		}
		cfg.Ollama.Model = cliPrompt(reader, "Model name", modelDefault)
	} else {
		fmt.Println("Available models:")
		for i, m := range models {
			fmt.Printf("  %d. %s\n", i+1, m)
		}
		fmt.Println()
		sel := cliPrompt(reader, "Enter number or model name", func() string {
			if cfg.Ollama.Model != "" {
				return cfg.Ollama.Model
			}
			return "1"
		}())
		// Resolve numeric selection.
		cfg.Ollama.Model = sel
		var idx int
		if n, err2 := fmt.Sscanf(sel, "%d", &idx); n == 1 && err2 == nil && idx >= 1 && idx <= len(models) {
			cfg.Ollama.Model = models[idx-1]
		}
	}

	cfg.Ollama.Enabled = true
	fmt.Printf("Ollama backend configured: model=%s host=%s\n", cfg.Ollama.Model, cfg.Ollama.Host)
	return setupSave(cfg)
}

func newSetupLLMOpenWebUICmd() *cobra.Command {
	return &cobra.Command{Use: "openwebui", Short: "Configure OpenWebUI LLM backend", RunE: runSetupLLMOpenWebUI}
}

func runSetupLLMOpenWebUI(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("OpenWebUI LLM Backend Setup")
	fmt.Println("===========================")
	fmt.Println("OpenWebUI provides a web-based UI for local and cloud LLMs.")
	fmt.Println()

	cfg.OpenWebUI.URL = cliPrompt(reader, "OpenWebUI URL (e.g. http://localhost:3000)", cfg.OpenWebUI.URL)
	cfg.OpenWebUI.Model = cliPrompt(reader, "Model name (e.g. llama3:latest)", cfg.OpenWebUI.Model)
	cfg.OpenWebUI.APIKey = cliPrompt(reader, "API key (press Enter to skip)", cfg.OpenWebUI.APIKey)
	enableChoice := cliPrompt(reader, "Enable OpenWebUI backend? (y/n)", func() string {
		if cfg.OpenWebUI.Enabled {
			return "y"
		}
		return "n"
	}())
	cfg.OpenWebUI.Enabled = strings.ToLower(enableChoice) == "y" || strings.ToLower(enableChoice) == "yes"
	return setupSave(cfg)
}

func newSetupLLMShellCmd() *cobra.Command {
	return &cobra.Command{Use: "shell", Short: "Configure shell script LLM backend", RunE: runSetupLLMShell}
}

func runSetupLLMShell(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Shell Script LLM Backend Setup")
	fmt.Println("==============================")
	fmt.Println("Runs a custom shell script as an LLM backend. The script receives the task")
	fmt.Println("as an argument and is expected to run interactively in a tmux session.")
	fmt.Println()

	cfg.Shell.ScriptPath = cliPrompt(reader, "Path to shell script", cfg.Shell.ScriptPath)
	if cfg.Shell.ScriptPath == "" {
		return fmt.Errorf("script path is required")
	}
	enableChoice := cliPrompt(reader, "Enable shell backend? (y/n)", func() string {
		if cfg.Shell.Enabled {
			return "y"
		}
		return "n"
	}())
	cfg.Shell.Enabled = strings.ToLower(enableChoice) == "y" || strings.ToLower(enableChoice) == "yes"
	return setupSave(cfg)
}

// ---- setup session command -------------------------------------------------

func newSetupSessionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session",
		Short: "Configure session management defaults",
		RunE:  runSetupSession,
	}
}

func runSetupSession(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Session Configuration")
	fmt.Println("=====================")
	fmt.Println("Configures session management defaults (applies to all new sessions).")
	fmt.Println()
	fmt.Println("Available LLM backends: claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell")
	fmt.Println()

	cfg.Session.LLMBackend = cliPrompt(reader, "Default LLM backend", func() string {
		if cfg.Session.LLMBackend != "" {
			return cfg.Session.LLMBackend
		}
		return "claude-code"
	}())
	maxStr := cliPrompt(reader, "Max concurrent sessions", fmt.Sprintf("%d", func() int {
		if cfg.Session.MaxSessions > 0 {
			return cfg.Session.MaxSessions
		}
		return 5
	}()))
	fmt.Sscanf(maxStr, "%d", &cfg.Session.MaxSessions) //nolint:errcheck

	idleStr := cliPrompt(reader, "Input idle timeout in seconds", fmt.Sprintf("%d", func() int {
		if cfg.Session.InputIdleTimeout > 0 {
			return cfg.Session.InputIdleTimeout
		}
		return 10
	}()))
	fmt.Sscanf(idleStr, "%d", &cfg.Session.InputIdleTimeout) //nolint:errcheck

	tailStr := cliPrompt(reader, "Default tail lines", fmt.Sprintf("%d", func() int {
		if cfg.Session.TailLines > 0 {
			return cfg.Session.TailLines
		}
		return 20
	}()))
	fmt.Sscanf(tailStr, "%d", &cfg.Session.TailLines) //nolint:errcheck

	cfg.Session.DefaultProjectDir = cliPrompt(reader, "Default project directory (press Enter for none)", cfg.Session.DefaultProjectDir)

	skipChoice := cliPrompt(reader, "Skip claude permissions by default? (y/n)", func() string {
		if cfg.Session.SkipPermissions {
			return "y"
		}
		return "n"
	}())
	cfg.Session.SkipPermissions = strings.ToLower(skipChoice) == "y" || strings.ToLower(skipChoice) == "yes"

	return setupSave(cfg)
}

// ---- setup mcp command -----------------------------------------------------

func newSetupMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Configure the MCP (Model Context Protocol) server",
		RunE:  runSetupMCP,
	}
}

func runSetupMCP(_ *cobra.Command, _ []string) error {
	cfg, err := setupLoadOrInit()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("MCP Server Setup")
	fmt.Println("================")
	fmt.Println("The MCP server lets Cursor, Claude Desktop, VS Code, and remote AI agents")
	fmt.Println("connect to datawatch via the Model Context Protocol.")
	fmt.Println()
	status := "disabled"
	if cfg.MCP.Enabled {
		status = "enabled"
	}
	fmt.Printf("Current status: %s\n\n", status)

	enableChoice := cliPrompt(reader, "Enable MCP? (y/n)", func() string {
		if cfg.MCP.Enabled {
			return "y"
		}
		return "y"
	}())
	cfg.MCP.Enabled = strings.ToLower(enableChoice) != "n" && strings.ToLower(enableChoice) != "no"
	if !cfg.MCP.Enabled {
		if err := setupSave(cfg); err != nil {
			return err
		}
		fmt.Println("MCP disabled.")
		return nil
	}

	sseChoice := cliPrompt(reader, "Enable SSE remote transport (for remote AI clients)? (y/n)", func() string {
		if cfg.MCP.SSEEnabled {
			return "y"
		}
		return "n"
	}())
	cfg.MCP.SSEEnabled = strings.ToLower(sseChoice) == "y" || strings.ToLower(sseChoice) == "yes"

	if cfg.MCP.SSEEnabled {
		cfg.MCP.SSEHost = cliPrompt(reader, "SSE bind address", func() string {
			if cfg.MCP.SSEHost != "" {
				return cfg.MCP.SSEHost
			}
			return "0.0.0.0"
		}())
		portStr := cliPrompt(reader, "SSE port", fmt.Sprintf("%d", func() int {
			if cfg.MCP.SSEPort != 0 {
				return cfg.MCP.SSEPort
			}
			return 8081
		}()))
		fmt.Sscanf(portStr, "%d", &cfg.MCP.SSEPort) //nolint:errcheck

		tlsChoice := cliPrompt(reader, "Enable TLS with auto-generated cert? (y/n)", "y")
		cfg.MCP.TLSEnabled = strings.ToLower(tlsChoice) != "n" && strings.ToLower(tlsChoice) != "no"
		cfg.MCP.TLSAutoGenerate = cfg.MCP.TLSEnabled

		cfg.MCP.Token = cliPrompt(reader, "Bearer token for authentication (press Enter to skip)", cfg.MCP.Token)
	}

	if err := setupSave(cfg); err != nil {
		return err
	}
	fmt.Println("MCP server configured.")
	if cfg.MCP.SSEEnabled {
		scheme := "http"
		if cfg.MCP.TLSEnabled {
			scheme = "https"
		}
		fmt.Printf("SSE server will start at %s://%s:%d on next `datawatch start`.\n", scheme, cfg.MCP.SSEHost, cfg.MCP.SSEPort)
		fmt.Println("Add to Cursor/Claude Desktop config: see docs/cursor-mcp.md")
	} else {
		fmt.Println("MCP stdio transport enabled for local IDE clients (Cursor, Claude Desktop, VS Code).")
		fmt.Println("See docs/cursor-mcp.md for connection instructions.")
	}
	return nil
}

// ---- completion command ----------------------------------------------------

// ---- cmd command --------------------------------------------------------

func cmdLibPath(cfg *config.Config) string {
	return filepath.Join(expandHome(cfg.DataDir), "commands.json")
}

func newCmdCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cmd",
		Short: "Manage the saved command library",
	}

	addCmd := &cobra.Command{
		Use:   "add <name> <command>",
		Short: "Add a named command",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lib, err := session.NewCmdLibrary(cmdLibPath(cfg))
			if err != nil {
				return err
			}
			c, err := lib.Add(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Printf("Added command %q [%s]\n", c.Name, c.ID)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all saved commands",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lib, err := session.NewCmdLibrary(cmdLibPath(cfg))
			if err != nil {
				return err
			}
			cmds := lib.List()
			if len(cmds) == 0 {
				fmt.Println("No saved commands. Use 'datawatch cmd add <name> <command>' to add one.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tCOMMAND\tSEEDED")
			for _, c := range cmds {
				seeded := ""
				if c.Seeded {
					seeded = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Command, seeded)
			}
			tw.Flush()
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a saved command by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lib, err := session.NewCmdLibrary(cmdLibPath(cfg))
			if err != nil {
				return err
			}
			if err := lib.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted command %q\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(addCmd, listCmd, deleteCmd)
	return cmd
}

// ---- seed command --------------------------------------------------------

// seededCommands are pre-populated saved commands for common AI session interactions.
var seededCommands = []session.SavedCommand{
	{Name: "approve", Command: "yes"},
	{Name: "reject", Command: "no"},
	{Name: "enter", Command: "\n"},
	{Name: "continue", Command: "continue"},
	{Name: "skip", Command: "skip"},
	{Name: "abort", Command: "\x03"},
}

// seededFilters are pre-populated output filter patterns for known claude-code prompts.
var seededFilters = []session.FilterPattern{
	// "Do you want to proceed?" style patterns → schedule auto-approve
	{Pattern: `Do you want to proceed\?`, Action: session.FilterActionSchedule, Value: "yes"},
	// Rate limit detection → alert
	{Pattern: `You've hit your limit|rate limit exceeded|quota exceeded`, Action: session.FilterActionAlert, Value: "Rate limit detected — session may be paused."},
	// Trust dialog → alert (don't auto-approve this one)
	{Pattern: `trust the files|Trust `, Action: session.FilterActionAlert, Value: "Trust dialog detected — review with 'status <id>' before approving."},
	// Prompt detection patterns — mark session as waiting_input immediately (no idle timeout needed).
	// These mirror the hardcoded promptPatterns but are user-configurable via the filter store.
	{Pattern: `Do you want to|Would you like|Proceed\?|Allow this action`, Action: session.FilterActionDetectPrompt},
	{Pattern: `\[y/N\]|\[Y/n\]|\(y/n\)|\(yes/no\)|\(y/n/always\)`, Action: session.FilterActionDetectPrompt},
	{Pattern: `Enter to confirm|Esc to cancel`, Action: session.FilterActionDetectPrompt},
	{Pattern: `Yes, I trust|trust this folder|Quick safety check`, Action: session.FilterActionDetectPrompt},
	{Pattern: `I am using this for local development|Loading development channels`, Action: session.FilterActionDetectPrompt},
}

func newSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Populate default saved commands and filters",
		Long: `Seed pre-populates the command library and filter store with useful defaults
for common AI session interactions. Existing entries are not overwritten.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lib, err := session.NewCmdLibrary(cmdLibPath(cfg))
			if err != nil {
				return err
			}
			if err := lib.Seed(seededCommands); err != nil {
				return fmt.Errorf("seed commands: %w", err)
			}
			fmt.Printf("Seeded %d commands into %s\n", len(seededCommands), cmdLibPath(cfg))

			filterPath := filepath.Join(expandHome(cfg.DataDir), "filters.json")
			fs, err := session.NewFilterStore(filterPath)
			if err != nil {
				return err
			}
			if err := fs.Seed(seededFilters); err != nil {
				return fmt.Errorf("seed filters: %w", err)
			}
			fmt.Printf("Seeded %d filters into %s\n", len(seededFilters), filterPath)
			return nil
		},
	}
}

// ---- test command ----------------------------------------------------------

// testInterfaceStatus holds non-sensitive status info for one interface.
type testInterfaceStatus struct {
	Name      string
	Category  string
	Enabled   bool
	Details   []string // non-secret details (endpoints, binary paths, model names)
	Checks    []string // checklist items needed to validate this interface
}

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test [--pr]",
		Short: "Collect interface status and optionally open a testing-tracker PR",
		Long: `Checks which interfaces are configured and enabled, collects non-sensitive
details (endpoints, binary names, models — never tokens or secrets), and prints
a status summary.

With --pr, opens a GitHub PR that updates docs/testing-tracker.md with the
collected details as test conditions.`,
		RunE: runTestCmd,
	}
	cmd.Flags().Bool("pr", false, "Open a GitHub PR updating docs/testing-tracker.md")
	return cmd
}

func runTestCmd(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	openPR, _ := cmd.Flags().GetBool("pr")

	statuses := collectInterfaceStatuses(cfg)

	// Print summary
	fmt.Printf("datawatch v%s — interface status\n", Version)
	fmt.Println(strings.Repeat("=", 50))

	var categories []string
	catMap := make(map[string][]testInterfaceStatus)
	for _, s := range statuses {
		if _, ok := catMap[s.Category]; !ok {
			categories = append(categories, s.Category)
		}
		catMap[s.Category] = append(catMap[s.Category], s)
	}

	for _, cat := range categories {
		fmt.Printf("\n%s:\n", cat)
		for _, s := range catMap[cat] {
			status := "disabled"
			if s.Enabled {
				status = "enabled"
			}
			fmt.Printf("  %-20s [%s]\n", s.Name, status)
			for _, d := range s.Details {
				fmt.Printf("    %s\n", d)
			}
		}
	}

	fmt.Println()
	fmt.Println("Validation checklists:")
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		fmt.Printf("\n  %s:\n", s.Name)
		for _, c := range s.Checks {
			fmt.Printf("    [ ] %s\n", c)
		}
	}

	if !openPR {
		fmt.Println("\nRun with --pr to open a GitHub PR updating docs/testing-tracker.md")
		return nil
	}

	return openTestingTrackerPR(cfg, statuses)
}

// collectInterfaceStatuses gathers non-sensitive status info for all interfaces.
func collectInterfaceStatuses(cfg *config.Config) []testInterfaceStatus {
	var out []testInterfaceStatus

	// Messaging backends
	out = append(out, testInterfaceStatus{
		Name:     "Signal",
		Category: "Messaging Backends",
		Enabled:  cfg.Signal.AccountNumber != "" && cfg.Signal.GroupID != "",
		Details: func() []string {
			if cfg.Signal.AccountNumber == "" {
				return []string{"not configured"}
			}
			return []string{fmt.Sprintf("account: %s", maskPhone(cfg.Signal.AccountNumber))}
		}(),
		Checks: []string{
			"Send 'help' in the datawatch Signal group — receive help text",
			"Send 'new: test task' — session starts, ID reported",
			"Send 'list' — sessions listed",
			"State change notification delivered when session finishes",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "Telegram",
		Category: "Messaging Backends",
		Enabled:  cfg.Telegram.Enabled && cfg.Telegram.Token != "",
		Details: func() []string {
			if cfg.Telegram.ChatID != 0 {
				return []string{fmt.Sprintf("chat_id: %d", cfg.Telegram.ChatID)}
			}
			return []string{"configured (no chat_id)"}
		}(),
		Checks: []string{
			"Send 'help' to the bot — receive help text",
			"Send 'list' — sessions listed",
			"State changes delivered as Telegram messages",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "Discord",
		Category: "Messaging Backends",
		Enabled:  cfg.Discord.Enabled && cfg.Discord.Token != "",
		Details: func() []string {
			if cfg.Discord.ChannelID != "" {
				return []string{fmt.Sprintf("channel_id: %s", cfg.Discord.ChannelID)}
			}
			return []string{"configured (no channel_id)"}
		}(),
		Checks: []string{
			"Send 'help' in the Discord channel — receive help text",
			"State changes delivered as Discord messages",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "Slack",
		Category: "Messaging Backends",
		Enabled:  cfg.Slack.Enabled && cfg.Slack.Token != "",
		Details: func() []string {
			if cfg.Slack.ChannelID != "" {
				return []string{fmt.Sprintf("channel_id: %s", cfg.Slack.ChannelID)}
			}
			return []string{"configured (no channel_id)"}
		}(),
		Checks: []string{
			"Send 'help' in the Slack channel — receive help text",
			"State changes delivered as Slack messages",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:    "Matrix",
		Category: "Messaging Backends",
		Enabled: cfg.Matrix.Enabled && cfg.Matrix.AccessToken != "",
		Details: func() []string {
			if cfg.Matrix.Homeserver != "" {
				return []string{
					fmt.Sprintf("homeserver: %s", cfg.Matrix.Homeserver),
					fmt.Sprintf("user_id: %s", cfg.Matrix.UserID),
					fmt.Sprintf("room_id: %s", cfg.Matrix.RoomID),
				}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"Send 'help' in the Matrix room — receive help text",
			"State changes delivered as Matrix messages",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "Twilio SMS",
		Category: "Messaging Backends",
		Enabled:  cfg.Twilio.Enabled && cfg.Twilio.AccountSID != "",
		Details: func() []string {
			if cfg.Twilio.FromNumber != "" {
				return []string{
					fmt.Sprintf("from: %s", maskPhone(cfg.Twilio.FromNumber)),
					fmt.Sprintf("to: %s", maskPhone(cfg.Twilio.ToNumber)),
					fmt.Sprintf("webhook_addr: %s", cfg.Twilio.WebhookAddr),
				}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"Send 'help' via SMS to from_number — receive help text",
			"State changes delivered as SMS",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "ntfy",
		Category: "Messaging Backends",
		Enabled:  cfg.Ntfy.Enabled && cfg.Ntfy.Topic != "",
		Details: func() []string {
			if cfg.Ntfy.ServerURL != "" {
				return []string{
					fmt.Sprintf("server: %s", cfg.Ntfy.ServerURL),
					fmt.Sprintf("topic: %s", cfg.Ntfy.Topic),
				}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"Start a session — receive ntfy push notification on state change",
			"Receive ntfy alert when alert fires",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "Email (SMTP)",
		Category: "Messaging Backends",
		Enabled:  cfg.Email.Enabled && cfg.Email.Host != "",
		Details: func() []string {
			if cfg.Email.Host != "" {
				return []string{
					fmt.Sprintf("smtp: %s:%d", cfg.Email.Host, cfg.Email.Port),
					fmt.Sprintf("from: %s", cfg.Email.From),
					fmt.Sprintf("to: %s", cfg.Email.To),
				}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"Start a session — receive email on state change",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "GitHub Webhook",
		Category: "Messaging Backends",
		Enabled:  cfg.GitHubWebhook.Enabled && cfg.GitHubWebhook.Addr != "",
		Details: func() []string {
			if cfg.GitHubWebhook.Addr != "" {
				return []string{fmt.Sprintf("listen: %s", cfg.GitHubWebhook.Addr)}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"Trigger a GitHub webhook — receive event in daemon logs",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "Generic Webhook",
		Category: "Messaging Backends",
		Enabled:  cfg.Webhook.Enabled && cfg.Webhook.Addr != "",
		Details: func() []string {
			if cfg.Webhook.Addr != "" {
				return []string{fmt.Sprintf("listen: %s", cfg.Webhook.Addr)}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"POST to webhook endpoint with a command — command is routed",
		},
	})

	// Web and API
	out = append(out, testInterfaceStatus{
		Name:     "Web UI",
		Category: "Web and API",
		Enabled:  cfg.Server.Enabled,
		Details: func() []string {
			if cfg.Server.Enabled {
				scheme := "http"
				if cfg.Server.TLSEnabled {
					scheme = "https"
				}
				return []string{fmt.Sprintf("url: %s://%s:%d", scheme, cfg.Server.Host, cfg.Server.Port)}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"Open web UI in browser — session list loads",
			"WebSocket connected — real-time updates work",
			"/api/health returns 200",
			"/api/sessions returns correct session list",
		},
	})

	// MCP
	out = append(out, testInterfaceStatus{
		Name:     "MCP stdio",
		Category: "MCP",
		Enabled:  cfg.MCP.Enabled,
		Details:  []string{"transport: stdio (local IDE integration)"},
		Checks: []string{
			"Connect from Cursor or Claude Desktop — tools listed",
			"list_sessions tool returns sessions",
			"start_session tool starts a session",
		},
	})
	out = append(out, testInterfaceStatus{
		Name:     "MCP SSE",
		Category: "MCP",
		Enabled:  cfg.MCP.Enabled && cfg.MCP.SSEEnabled,
		Details: func() []string {
			if cfg.MCP.SSEEnabled {
				scheme := "http"
				if cfg.MCP.TLSEnabled {
					scheme = "https"
				}
				return []string{fmt.Sprintf("url: %s://%s:%d", scheme, cfg.MCP.SSEHost, cfg.MCP.SSEPort)}
			}
			return []string{"not configured"}
		}(),
		Checks: []string{
			"Connect from remote AI client via SSE URL — tools listed",
			"list_sessions returns sessions",
		},
	})

	// LLM backends
	llmBackends := []struct {
		name    string
		enabled bool
		details []string
	}{
		{"claude-code", true, []string{fmt.Sprintf("binary: %s", cfg.Session.ClaudeCodeBin)}},
		{"aider", cfg.Aider.Enabled, []string{fmt.Sprintf("binary: %s", cfg.Aider.Binary)}},
		{"goose", cfg.Goose.Enabled, []string{fmt.Sprintf("binary: %s", cfg.Goose.Binary)}},
		{"gemini", cfg.Gemini.Enabled, []string{fmt.Sprintf("binary: %s", cfg.Gemini.Binary)}},
		{"opencode", cfg.OpenCode.Enabled, []string{fmt.Sprintf("binary: %s", cfg.OpenCode.Binary)}},
		{"ollama", cfg.Ollama.Enabled, []string{
			fmt.Sprintf("host: %s", cfg.Ollama.Host),
			fmt.Sprintf("model: %s", cfg.Ollama.Model),
		}},
		{"openwebui", cfg.OpenWebUI.Enabled, []string{
			fmt.Sprintf("url: %s", cfg.OpenWebUI.URL),
			fmt.Sprintf("model: %s", cfg.OpenWebUI.Model),
		}},
		{"shell", cfg.Shell.Enabled, []string{fmt.Sprintf("script: %s", cfg.Shell.ScriptPath)}},
	}
	for _, b := range llmBackends {
		out = append(out, testInterfaceStatus{
			Name:     b.name,
			Category: "LLM Backends",
			Enabled:  b.enabled,
			Details:  b.details,
			Checks: []string{
				fmt.Sprintf("Run: datawatch session new --backend %s 'echo hello'", b.name),
				"Session reaches running state and produces log output",
				"Session reaches complete or waiting_input state",
			},
		})
	}

	return out
}

// maskPhone replaces all but the last 4 chars of a phone number with asterisks.
func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return phone
	}
	return strings.Repeat("*", len(phone)-4) + phone[len(phone)-4:]
}

// openTestingTrackerPR creates a branch, updates testing-tracker.md, and opens a PR.
func openTestingTrackerPR(cfg *config.Config, statuses []testInterfaceStatus) error {
	// Find the testing-tracker file relative to git root
	trackerPath, err := findGitFile("docs/testing-tracker.md")
	if err != nil {
		return fmt.Errorf("find testing-tracker.md: %w", err)
	}

	// Read current tracker
	data, err := os.ReadFile(trackerPath)
	if err != nil {
		return fmt.Errorf("read testing-tracker: %w", err)
	}
	tracker := string(data)

	// Build updated test conditions for each enabled interface
	now := time.Now().Format("2006-01-02")
	hostnameStr := cfg.Hostname
	if hostnameStr == "" {
		hostnameStr, _ = os.Hostname()
	}
	conditionLine := fmt.Sprintf("Linux, %s, %s", hostnameStr, now)

	// For each enabled interface, if the tracker row still says "Not validated yet" or "—",
	// update Test Conditions with what we collected.
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		detailStr := strings.Join(s.Details, "; ")
		tracker = updateTrackerRow(tracker, s.Name, conditionLine+", "+detailStr)
	}

	// Write updated tracker
	if err := os.WriteFile(trackerPath, []byte(tracker), 0644); err != nil {
		return fmt.Errorf("write testing-tracker: %w", err)
	}
	fmt.Printf("Updated %s\n", trackerPath)

	// Check if gh is available
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		fmt.Println("gh CLI not found. Updated testing-tracker.md locally.")
		fmt.Println("Commit and push manually, then open a PR.")
		return nil
	}
	_ = ghPath

	// Create branch, commit, push, open PR
	branchName := fmt.Sprintf("test/tracker-%s-%s", hostnameStr, time.Now().Format("20060102"))
	gitRoot, _ := findGitRoot()

	// git checkout -b <branch>
	if out, err := runGitCmd(gitRoot, "checkout", "-b", branchName); err != nil {
		return fmt.Errorf("git checkout -b: %s: %w", out, err)
	}
	// git add docs/testing-tracker.md
	if out, err := runGitCmd(gitRoot, "add", trackerPath); err != nil {
		return fmt.Errorf("git add: %s: %w", out, err)
	}
	// git commit
	commitMsg := fmt.Sprintf("test(%s): update testing-tracker with enabled interface details", hostnameStr)
	if out, err := runGitCmd(gitRoot, "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("git commit: %s: %w", out, err)
	}
	// git push
	if out, err := runGitCmd(gitRoot, "push", "-u", "origin", branchName); err != nil {
		return fmt.Errorf("git push: %s: %w", out, err)
	}

	// Build PR body
	var sb strings.Builder
	sb.WriteString("## Interface Status Update\n\n")
	sb.WriteString(fmt.Sprintf("Collected from host `%s` on %s using `datawatch test --pr`.\n\n", hostnameStr, now))
	sb.WriteString("### Enabled Interfaces\n\n")
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", s.Name, s.Category, strings.Join(s.Details, ", ")))
	}
	sb.WriteString("\n### Validation Checklists\n\n")
	sb.WriteString("The following checks should be performed for each enabled interface before marking as Validated:\n\n")
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		sb.WriteString(fmt.Sprintf("**%s:**\n", s.Name))
		for _, c := range s.Checks {
			sb.WriteString(fmt.Sprintf("- [ ] %s\n", c))
		}
		sb.WriteString("\n")
	}

	prTitle := fmt.Sprintf("test(%s): testing-tracker update %s", hostnameStr, now)
	prCmd := exec.Command("gh", "pr", "create",
		"--title", prTitle,
		"--body", sb.String(),
		"--head", branchName,
		"--base", "main",
	)
	prCmd.Dir = gitRoot
	prOut, err := prCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh pr create: %s: %w", string(prOut), err)
	}
	fmt.Printf("PR created: %s\n", strings.TrimSpace(string(prOut)))

	// Switch back to main
	runGitCmd(gitRoot, "checkout", "main") //nolint:errcheck
	return nil
}

// updateTrackerRow updates the Test Conditions cell for the named interface row.
func updateTrackerRow(tracker, name, conditions string) string {
	lines := strings.Split(tracker, "\n")
	for i, line := range lines {
		// Match table rows: | Name | ...
		if !strings.HasPrefix(line, "| ") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			continue
		}
		cellName := strings.TrimSpace(cells[1])
		if !strings.EqualFold(cellName, name) {
			continue
		}
		// cells[3] is Test Conditions (0-indexed: | | Name | Tested | Validated | Conditions | Notes |)
		if len(cells) >= 5 && (strings.TrimSpace(cells[4]) == "—" || strings.TrimSpace(cells[4]) == "") {
			cells[4] = " " + conditions + " "
			lines[i] = strings.Join(cells, "|")
		}
		break
	}
	return strings.Join(lines, "\n")
}

func findGitFile(relPath string) (string, error) {
	root, err := findGitRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, relPath), nil
}

func findGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repo")
	}
	return strings.TrimSpace(string(out)), nil
}

func runGitCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ---- diagnose command -------------------------------------------------------

func newDiagnoseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose [signal|telegram|discord|slack|matrix|twilio|all]",
		Short: "Diagnose connectivity for messaging backends",
		Long: `Test connectivity and configuration for each messaging backend.

Checks:
  - Binary/dependency availability
  - Config completeness (required fields present)
  - Live connectivity test (lists groups/channels, validates IDs)
  - Sends a test message if --send-test is given

Signal diagnose also lists all known groups so you can verify the group_id in config.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDiagnose,
	}
	cmd.Flags().Bool("send-test", false, "Send a test message to verify outbound delivery")
	return cmd
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	target := "all"
	if len(args) == 1 {
		target = strings.ToLower(args[0])
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("[warn] Could not load config (%v) — using defaults\n", err)
		cfg = config.DefaultConfig()
	}

	sendTest, _ := cmd.Flags().GetBool("send-test")

	switch target {
	case "signal":
		return diagSignal(cfg, sendTest)
	case "telegram":
		return diagTelegram(cfg, sendTest)
	case "discord":
		return diagDiscord(cfg, sendTest)
	case "slack":
		return diagSlack(cfg, sendTest)
	case "all":
		_ = diagSignal(cfg, sendTest)
		_ = diagTelegram(cfg, sendTest)
		_ = diagDiscord(cfg, sendTest)
		_ = diagSlack(cfg, sendTest)
		diagOther(cfg)
		return nil
	default:
		return fmt.Errorf("unknown backend %q; choose: signal, telegram, discord, slack, all", target)
	}
}

func diagHeader(name string) {
	fmt.Printf("\n=== %s ===\n", name)
}

func diagOK(msg string)   { fmt.Printf("  [OK]   %s\n", msg) }
func diagFail(msg string) { fmt.Printf("  [FAIL] %s\n", msg) }
func diagWarn(msg string) { fmt.Printf("  [WARN] %s\n", msg) }
func diagInfo(msg string) { fmt.Printf("  [INFO] %s\n", msg) }

func diagSignal(cfg *config.Config, sendTest bool) error {
	diagHeader("Signal")

	// 1. Check signal-cli binary
	signalPath, err := exec.LookPath("signal-cli")
	if err != nil {
		diagFail("signal-cli not found in PATH — install it or check your PATH")
		diagInfo("Install: https://github.com/AsamK/signal-cli/releases")
		return nil
	}
	diagOK(fmt.Sprintf("signal-cli found: %s", signalPath))

	// 2. Check signal-cli version
	verOut, verErr := exec.Command("signal-cli", "--version").Output()
	if verErr == nil {
		diagOK(fmt.Sprintf("version: %s", strings.TrimSpace(string(verOut))))
	}

	// 3. Check config fields
	if cfg.Signal.AccountNumber == "" {
		diagFail("signal.account_number is not set — run: datawatch link")
		return nil
	}
	diagOK(fmt.Sprintf("account_number: %s", cfg.Signal.AccountNumber))

	if cfg.Signal.ConfigDir == "" {
		diagWarn("signal.config_dir is empty — using default")
	} else {
		diagOK(fmt.Sprintf("config_dir: %s", cfg.Signal.ConfigDir))
	}

	configDir := cfg.Signal.ConfigDir
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".local", "share", "signal-cli")
	}
	configDir = expandHome(configDir)

	if _, statErr := os.Stat(configDir); os.IsNotExist(statErr) {
		diagFail(fmt.Sprintf("config_dir %s does not exist — run: datawatch link", configDir))
		return nil
	}
	diagOK(fmt.Sprintf("config_dir exists: %s", configDir))

	if cfg.Signal.GroupID == "" {
		diagWarn("signal.group_id is not set — run: datawatch link  (or see group list below)")
	}

	// 4. Start signal-cli and list groups
	diagInfo("Starting signal-cli to list groups (may take a few seconds)...")
	backend, err := signalpkg.NewSignalCLIBackend(configDir, cfg.Signal.AccountNumber)
	if err != nil {
		diagFail(fmt.Sprintf("failed to start signal-cli: %v", err))
		diagInfo("Check signal-cli stderr output above for Java errors or auth issues")
		return nil
	}
	defer backend.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groups, err := backend.ListGroups(ctx)
	if err != nil {
		diagFail(fmt.Sprintf("listGroups failed: %v", err))
		diagInfo("This may mean: signal-cli is not linked, the account number is wrong,")
		diagInfo("or signal-cli needs to receive sync data (try sending a message first).")
		return nil
	}

	if len(groups) == 0 {
		diagWarn("No groups found. The account is linked but not in any Signal groups.")
		diagInfo("Create a group in Signal and add this linked device, then re-run diagnose.")
	} else {
		diagOK(fmt.Sprintf("Found %d group(s):", len(groups)))
		for _, g := range groups {
			marker := "  "
			if g.ID == cfg.Signal.GroupID {
				marker = "* "
			}
			fmt.Printf("    %s[%s] %s\n", marker, g.ID, g.Name)
		}
	}

	// 5. Validate configured group_id
	if cfg.Signal.GroupID != "" {
		found := false
		for _, g := range groups {
			if g.ID == cfg.Signal.GroupID {
				found = true
				diagOK(fmt.Sprintf("configured group_id matches group: %s", g.Name))
				break
			}
		}
		if !found {
			diagFail(fmt.Sprintf("configured group_id %q NOT found in the group list above", cfg.Signal.GroupID))
			diagInfo("Fix: copy the correct group ID from the list above into config.yaml signal.group_id")
			diagInfo("     Then restart: datawatch stop && datawatch start")
		}
	}

	// 6. Optional: send test message
	if sendTest && cfg.Signal.GroupID != "" {
		diagInfo("Sending test message to group...")
		if err := backend.Send(cfg.Signal.GroupID, "[datawatch] diagnose: connectivity test"); err != nil {
			diagFail(fmt.Sprintf("send failed: %v", err))
		} else {
			diagOK("Test message sent — check your Signal group for the message")
		}
	}

	return nil
}

func diagTelegram(cfg *config.Config, sendTest bool) error {
	diagHeader("Telegram")
	if !cfg.Telegram.Enabled {
		diagInfo("Telegram is disabled (telegram.enabled = false)")
		return nil
	}
	if cfg.Telegram.Token == "" {
		diagFail("telegram.token is not set — run: datawatch setup telegram")
		return nil
	}
	diagOK("telegram.token is set")
	if cfg.Telegram.ChatID == 0 {
		diagWarn("telegram.chat_id is 0 — the bot won't receive messages until chat_id is set")
		diagInfo("Send any message to the bot, then run: datawatch setup telegram")
	} else {
		diagOK(fmt.Sprintf("telegram.chat_id = %d", cfg.Telegram.ChatID))
	}

	// Live connectivity check via Telegram getMe API
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", cfg.Telegram.Token)
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Get(url)
	if err != nil {
		diagFail(fmt.Sprintf("Telegram API unreachable: %v", err))
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		var result struct {
			OK     bool `json:"ok"`
			Result struct {
				Username string `json:"username"`
				FirstName string `json:"first_name"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &result) == nil && result.OK {
			diagOK(fmt.Sprintf("bot connected: @%s (%s)", result.Result.Username, result.Result.FirstName))
		} else {
			diagOK("Telegram API responded OK")
		}
	} else {
		diagFail(fmt.Sprintf("Telegram API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	if sendTest && cfg.Telegram.ChatID != 0 {
		diagInfo("Sending test message to Telegram chat...")
		url2 := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.Telegram.Token)
		payload := fmt.Sprintf(`{"chat_id":%d,"text":"[datawatch] diagnose: connectivity test"}`, cfg.Telegram.ChatID)
		r2, err2 := (&http.Client{Timeout: 8 * time.Second}).Post(url2, "application/json",
			strings.NewReader(payload))
		if err2 != nil || r2.StatusCode != 200 {
			diagFail(fmt.Sprintf("send failed: %v", err2))
		} else {
			diagOK("Test message sent to Telegram")
			r2.Body.Close()
		}
	}
	return nil
}

func diagDiscord(cfg *config.Config, _ bool) error {
	diagHeader("Discord")
	if !cfg.Discord.Enabled {
		diagInfo("Discord is disabled (discord.enabled = false)")
		return nil
	}
	if cfg.Discord.Token == "" {
		diagFail("discord.token is not set — run: datawatch setup discord")
		return nil
	}
	diagOK("discord.token is set")
	if cfg.Discord.ChannelID == "" {
		diagWarn("discord.channel_id is not set — run: datawatch setup discord")
	} else {
		diagOK(fmt.Sprintf("discord.channel_id = %s", cfg.Discord.ChannelID))
	}

	// Live connectivity check via Discord API
	req, _ := http.NewRequest("GET", "https://discord.com/api/v10/users/@me", nil)
	req.Header.Set("Authorization", "Bot "+cfg.Discord.Token)
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		diagFail(fmt.Sprintf("Discord API unreachable: %v", err))
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		var result struct {
			Username string `json:"username"`
		}
		body, _ := io.ReadAll(resp.Body)
		if json.Unmarshal(body, &result) == nil {
			diagOK(fmt.Sprintf("bot connected: %s", result.Username))
		} else {
			diagOK("Discord API responded OK")
		}
	} else {
		diagFail(fmt.Sprintf("Discord API error %d — check bot token", resp.StatusCode))
	}
	return nil
}

func diagSlack(cfg *config.Config, _ bool) error {
	diagHeader("Slack")
	if !cfg.Slack.Enabled {
		diagInfo("Slack is disabled (slack.enabled = false)")
		return nil
	}
	if cfg.Slack.Token == "" {
		diagFail("slack.token is not set — run: datawatch setup slack")
		return nil
	}
	diagOK("slack.token is set")
	if cfg.Slack.ChannelID == "" {
		diagWarn("slack.channel_id is not set — run: datawatch setup slack")
	} else {
		diagOK(fmt.Sprintf("slack.channel_id = %s", cfg.Slack.ChannelID))
	}

	// Live connectivity check via Slack auth.test
	req, _ := http.NewRequest("GET", "https://slack.com/api/auth.test", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Slack.Token)
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		diagFail(fmt.Sprintf("Slack API unreachable: %v", err))
		return nil
	}
	defer resp.Body.Close()
	var result struct {
		OK   bool   `json:"ok"`
		User string `json:"user"`
		Team string `json:"team"`
		Error string `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(body, &result) == nil {
		if result.OK {
			diagOK(fmt.Sprintf("connected as %s in workspace %s", result.User, result.Team))
		} else {
			diagFail(fmt.Sprintf("Slack auth failed: %s", result.Error))
		}
	} else {
		diagFail(fmt.Sprintf("Slack API error %d", resp.StatusCode))
	}
	return nil
}

func diagOther(cfg *config.Config) {
	diagHeader("Other backends")
	if cfg.Matrix.Enabled {
		if cfg.Matrix.AccessToken == "" {
			diagFail("matrix.access_token not set — run: datawatch setup matrix")
		} else {
			diagOK(fmt.Sprintf("Matrix: enabled, homeserver=%s room=%s", cfg.Matrix.Homeserver, cfg.Matrix.RoomID))
		}
	}
	if cfg.Twilio.Enabled {
		if cfg.Twilio.AccountSID == "" {
			diagFail("twilio.account_sid not set — run: datawatch setup twilio")
		} else {
			diagOK(fmt.Sprintf("Twilio: enabled, from=%s to=%s", cfg.Twilio.FromNumber, cfg.Twilio.ToNumber))
		}
	}
	if cfg.Ntfy.Enabled {
		diagOK(fmt.Sprintf("ntfy: enabled, server=%s topic=%s", cfg.Ntfy.ServerURL, cfg.Ntfy.Topic))
	}
	if cfg.Email.Enabled {
		if cfg.Email.Host == "" {
			diagFail("email.host not set — run: datawatch setup email")
		} else {
			diagOK(fmt.Sprintf("Email: enabled, host=%s from=%s to=%s", cfg.Email.Host, cfg.Email.From, cfg.Email.To))
		}
	}
	if cfg.GitHubWebhook.Enabled {
		diagOK(fmt.Sprintf("GitHub webhook: listening on %s", cfg.GitHubWebhook.Addr))
	}
	if cfg.Webhook.Enabled {
		diagOK(fmt.Sprintf("Generic webhook: listening on %s", cfg.Webhook.Addr))
	}
	if cfg.MCP.Enabled {
		diagOK(fmt.Sprintf("MCP: enabled (SSE=%v)", cfg.MCP.SSEEnabled))
	}
	if cfg.Server.Enabled {
		diagOK(fmt.Sprintf("Web server: enabled on %s:%d", cfg.Server.Host, cfg.Server.Port))
	}
}

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

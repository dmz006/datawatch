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

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
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
var Version = "0.1.2"

var (
	cfgPath string
	verbose bool
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

	root.AddCommand(
		newStartCmd(),
		newLinkCmd(),
		newConfigCmd(),
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

// loadConfig loads configuration from the resolved path.
func loadConfig() (*config.Config, error) {
	return config.Load(resolveConfigPath())
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

Flags override the corresponding config.yaml values for this run only.`,
		RunE: runStart,
	}
	cmd.Flags().String("llm-backend", "", "LLM backend to use (overrides session.llm_backend in config)")
	cmd.Flags().String("host", "", "HTTP server bind address (overrides server.host in config)")
	cmd.Flags().Int("port", 0, "HTTP server port (overrides server.port in config)")
	cmd.Flags().Bool("no-server", false, "Disable the HTTP/WebSocket PWA server")
	cmd.Flags().Bool("no-mcp", false, "Disable the MCP server")
	return cmd
}

func runStart(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
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
		r := router.NewRouter(cfg.Hostname, cfg.Signal.GroupID, adapted, mgr, cfg.Session.TailLines)
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
			r := router.NewRouter(cfg.Hostname, chatIDStr, tgB, mgr, cfg.Session.TailLines)
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
			r := router.NewRouter(cfg.Hostname, channelID, discordB, mgr, cfg.Session.TailLines)
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
			r := router.NewRouter(cfg.Hostname, channelID, slackB, mgr, cfg.Session.TailLines)
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
		r := router.NewRouter(cfg.Hostname, cfg.Twilio.ToNumber, twilioB, mgr, cfg.Session.TailLines)
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
			r := router.NewRouter(cfg.Hostname, cfg.Matrix.RoomID, matrixB, mgr, cfg.Session.TailLines)
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
		r := router.NewRouter(cfg.Hostname, "github", ghB, mgr, cfg.Session.TailLines)
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
		r := router.NewRouter(cfg.Hostname, "webhook", wbB, mgr, cfg.Session.TailLines)
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
		httpServer = server.New(&cfg.Server, cfg.DataDir, mgr, cfg.Hostname, llm.Names())
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
		return fmt.Errorf("no backends enabled — configure signal, telegram, discord, slack, twilio, matrix, github_webhook, or webhook in config")
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
	path := resolveConfigPath()
	cfg, err := config.Load(path)
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
	fmt.Println()

	cfg.Signal.AccountNumber = prompt("Signal phone number (e.g. +12125551234)", cfg.Signal.AccountNumber)
	if cfg.Signal.GroupID != "" {
		fmt.Printf("Signal group ID [%s] (press Enter to keep): ", cfg.Signal.GroupID)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			cfg.Signal.GroupID = line
		}
	} else {
		fmt.Println("Signal group ID: (leave blank — 'datawatch link' will create one automatically)")
		cfg.Signal.GroupID = prompt("Signal group ID (base64, optional)", cfg.Signal.GroupID)
	}
	cfg.Hostname = prompt("Hostname (identifies this machine in messages)", cfg.Hostname)
	cfg.Signal.DeviceName = prompt("Device name shown in Signal linked devices", cfg.Signal.DeviceName)
	cfg.Session.ClaudeCodeBin = prompt("claude-code binary path", cfg.Session.ClaudeCodeBin)

	if err := config.Save(cfg, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nConfiguration saved to %s\n\n", path)
	fmt.Println("Next steps:")
	if cfg.Signal.GroupID == "" {
		fmt.Println("  1. Link your device and create the control group: datawatch link")
		fmt.Println("  2. Start the daemon: datawatch start")
	} else {
		fmt.Println("  1. Link your device (if not done yet): datawatch link")
		fmt.Println("  2. Start the daemon: datawatch start")
	}
	fmt.Println("  Send 'help' in your configured group to verify everything works.")
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

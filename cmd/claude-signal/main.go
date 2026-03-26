package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dmz006/claude-signal/internal/config"
	"github.com/dmz006/claude-signal/internal/router"
	"github.com/dmz006/claude-signal/internal/session"
	signalpkg "github.com/dmz006/claude-signal/internal/signal"
	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "0.1.0"

var (
	cfgPath string
	verbose bool
)

func main() {
	root := &cobra.Command{
		Use:   "claude-signal",
		Short: "Bridge Signal group messages to claude-code tmux sessions",
		Long: `claude-signal is a daemon that links a Signal group to claude-code tmux sessions.
Send commands in Signal to start, monitor, and interact with AI coding tasks.`,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default: ~/.claude-signal/config.yaml)")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose/debug logging")

	root.AddCommand(
		newStartCmd(),
		newLinkCmd(),
		newConfigCmd(),
		newSessionCmd(),
		newVersionCmd(),
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
	return &cobra.Command{
		Use:   "start",
		Short: "Start the claude-signal daemon",
		RunE:  runStart,
	}
}

func runStart(_ *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Signal.AccountNumber == "" {
		return fmt.Errorf("signal.account_number is required — run 'claude-signal config init' first")
	}
	if cfg.Signal.GroupID == "" {
		return fmt.Errorf("signal.group_id is required — run 'claude-signal config init' first")
	}

	debugf("hostname=%s group=%s", cfg.Hostname, cfg.Signal.GroupID)

	// Start signal-cli JSON-RPC backend
	backend, err := signalpkg.NewSignalCLIBackend(cfg.Signal.ConfigDir, cfg.Signal.AccountNumber)
	if err != nil {
		return fmt.Errorf("start signal-cli: %w", err)
	}
	defer backend.Close() //nolint:errcheck

	// Create session manager
	idleTimeout := time.Duration(cfg.Session.InputIdleTimeout) * time.Second
	mgr, err := session.NewManager(cfg.Hostname, cfg.DataDir, cfg.Session.ClaudeCodeBin, idleTimeout)
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
	}

	// Handle SIGINT / SIGTERM gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Resume monitors for sessions that survived a previous daemon restart
	mgr.ResumeMonitors(ctx)

	// Build and run the router
	r := router.NewRouter(cfg.Hostname, cfg.Signal.GroupID, backend, mgr, cfg.Session.TailLines)

	fmt.Printf("[%s] claude-signal v%s started. Listening on group %s\n",
		cfg.Hostname, Version, cfg.Signal.GroupID)

	if err := r.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("router error: %w", err)
	}
	return nil
}

// ---- link command ---------------------------------------------------------

func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link",
		Short: "Link this device to a Signal account via QR code",
		RunE:  runLink,
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

	fmt.Printf("Linking device '%s'...\n", deviceName)
	fmt.Println("Scan the QR code with Signal (Settings → Linked Devices → Link New Device):")
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
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Create a Signal group from your phone (include yourself)")
	fmt.Println("  2. Get the group ID: signal-cli -u <number> listGroups")
	fmt.Println("  3. Run: claude-signal config init")
	fmt.Println("  4. Run: claude-signal start")
	return nil
}

// linkViaSubprocess runs `signal-cli link -n <deviceName>`, parses the sgnl:// URI from
// stdout/stderr, calls onQR, and waits for the process to complete.
func linkViaSubprocess(configDir, deviceName string, onQR func(string)) error {
	args := []string{"link", "-n", deviceName}
	if configDir != "" {
		args = append([]string{"--config", configDir}, args...)
	}
	cmd := exec.Command("signal-cli", args...)

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

	// Read both streams looking for the sgnl:// URI
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "sgnl://") {
				onQR(line)
			}
		}
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "sgnl://") {
			onQR(line)
		}
	}

	return cmd.Wait()
}

// ---- config command -------------------------------------------------------

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage claude-signal configuration",
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

	fmt.Println("claude-signal configuration wizard")
	fmt.Println("===================================")
	fmt.Println()

	cfg.Signal.AccountNumber = prompt("Signal phone number (e.g. +12125551234)", cfg.Signal.AccountNumber)
	cfg.Signal.GroupID = prompt("Signal group ID (base64 from signal-cli listGroups)", cfg.Signal.GroupID)
	cfg.Hostname = prompt("Hostname (identifies this machine in Signal messages)", cfg.Hostname)
	cfg.Signal.DeviceName = prompt("Device name shown in Signal linked devices", cfg.Signal.DeviceName)
	cfg.Session.ClaudeCodeBin = prompt("claude-code binary path", cfg.Session.ClaudeCodeBin)

	if err := config.Save(cfg, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nConfiguration saved to %s\n\n", path)
	fmt.Println("Next steps:")
	fmt.Println("  1. Link your device (if not done yet): claude-signal link")
	fmt.Println("  2. Start the daemon: claude-signal start")
	fmt.Println("  3. Send 'help' in your Signal group to verify everything works")
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
			fmt.Printf("  max_sessions:     %d\n", cfg.Session.MaxSessions)
			fmt.Printf("  idle_timeout:     %ds\n", cfg.Session.InputIdleTimeout)
			fmt.Printf("  tail_lines:       %d\n", cfg.Session.TailLines)
			fmt.Printf("  claude_code_bin:  %s\n", cfg.Session.ClaudeCodeBin)
			return nil
		},
	}
}

// ---- session command ------------------------------------------------------

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage sessions locally (no daemon required)",
	}
	cmd.AddCommand(newSessionListCmd())
	return cmd
}

func newSessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sessions from the local store",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			idleTimeout := time.Duration(cfg.Session.InputIdleTimeout) * time.Second
			mgr, err := session.NewManager(cfg.Hostname, cfg.DataDir, cfg.Session.ClaudeCodeBin, idleTimeout)
			if err != nil {
				return err
			}
			sessions := mgr.ListSessions()
			if len(sessions) == 0 {
				fmt.Println("No sessions.")
				return nil
			}
			for _, s := range sessions {
				fmt.Printf("[%s] %-14s %s — %s\n  Task: %s\n\n",
					s.ID, s.State, s.Hostname, s.UpdatedAt.Format("2006-01-02 15:04:05"), s.Task)
			}
			return nil
		},
	}
}

// ---- version command ------------------------------------------------------

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("claude-signal v%s\n", Version)
		},
	}
}

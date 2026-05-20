// BL334 T43e — Operational data encryption CLI.
//
// Commands:
//
//	datawatch security encryption status   — show which files are encrypted
//	datawatch security encryption migrate  — encrypt all plaintext operational files
//	datawatch security wipe-plaintext      — 3-pass secure wipe + unlink of plaintext files
//	datawatch security logs                — decrypt and print daemon-app.log (T43h)

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/secfile"
)

func newSecurityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Operational data encryption and secure wipe",
	}
	enc := &cobra.Command{
		Use:   "encryption",
		Short: "Manage operational data encryption",
	}
	enc.AddCommand(newSecurityEncryptionStatusCmd())
	enc.AddCommand(newSecurityEncryptionMigrateCmd())
	cmd.AddCommand(enc)
	cmd.AddCommand(newSecurityWipePlaintextCmd())
	cmd.AddCommand(newSecurityLogsCmd())
	return cmd
}

// newSecurityEncryptionStatusCmd implements `datawatch security encryption status`.
func newSecurityEncryptionStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show encryption status of operational data files",
		RunE: func(_ *cobra.Command, _ []string) error {
			url := daemonURL() + "/api/security/encryption/status"
			resp, err := securityGet(url)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close() //nolint:errcheck
			var out map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}

			secure, _ := out["secure_mode"].(bool)
			summary, _ := out["summary"].(string)
			fmt.Printf("Secure mode : %v\n", secure)
			fmt.Printf("Summary     : %s\n\n", summary)

			if files, ok := out["files"].([]any); ok {
				fmt.Printf("%-60s  %-9s  %s\n", "File", "Exists", "Encrypted")
				fmt.Println(strings.Repeat("-", 80))
				for _, f := range files {
					fm, _ := f.(map[string]any)
					path, _ := fm["path"].(string)
					exists, _ := fm["exists"].(bool)
					enc, _ := fm["encrypted"].(bool)
					fmt.Printf("%-60s  %-9v  %v\n", path, exists, enc)
				}
			}
			return nil
		},
	}
}

// newSecurityEncryptionMigrateCmd implements `datawatch security encryption migrate`.
func newSecurityEncryptionMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Encrypt all plaintext operational files (idempotent)",
		RunE: func(_ *cobra.Command, _ []string) error {
			url := daemonURL() + "/api/security/encryption/migrate"
			resp, err := securityPost(url, nil)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close() //nolint:errcheck
			var out map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}
			if errMsg, ok := out["error"].(string); ok {
				return fmt.Errorf("migrate failed: %s", errMsg)
			}
			msg, _ := out["message"].(string)
			fmt.Println(msg)
			return nil
		},
	}
}

// newSecurityWipePlaintextCmd implements `datawatch security wipe-plaintext`.
func newSecurityWipePlaintextCmd() *cobra.Command {
	var confirm bool
	c := &cobra.Command{
		Use:   "wipe-plaintext",
		Short: "Secure-wipe plaintext operational data files (3-pass overwrite + unlink)",
		Long: `Overwrites each plaintext operational file with 3 passes (zeros, ones,
random bytes) then deletes it. Already-encrypted files are skipped.

WARNING: This is irreversible. On modern SSDs and copy-on-write filesystems
the overwrite may not reach the underlying storage. Use LUKS or an encrypted
home directory for stronger guarantees.

You must pass --confirm to proceed.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !confirm {
				return fmt.Errorf("--confirm required; read the warning and pass --confirm to proceed")
			}
			url := daemonURL() + "/api/security/wipe-plaintext"
			body, _ := json.Marshal(map[string]bool{"confirm": true})
			resp, err := securityPost(url, body)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close() //nolint:errcheck
			var out map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}
			if errMsg, ok := out["error"].(string); ok {
				return fmt.Errorf("wipe failed: %s", errMsg)
			}
			wiped, _ := out["wiped"].([]any)
			skipped, _ := out["skipped"].([]any)
			note, _ := out["note"].(string)
			fmt.Printf("Wiped   : %d file(s)\n", len(wiped))
			for _, f := range wiped {
				fmt.Printf("  - %v\n", f)
			}
			fmt.Printf("Skipped : %d file(s) (already encrypted)\n", len(skipped))
			if note != "" {
				fmt.Printf("Note    : %s\n", note)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&confirm, "confirm", false, "Required: confirms intent to irreversibly wipe plaintext files")
	return c
}

// newSecurityLogsCmd implements `datawatch security logs`.
// Decrypts daemon-app.log using the same Argon2id key as --secure mode.
func newSecurityLogsCmd() *cobra.Command {
	var tail int
	c := &cobra.Command{
		Use:   "logs",
		Short: "Decrypt and print the encrypted application log (daemon-app.log)",
		Long: `Reads daemon-app.log from the data directory, decrypts it using the
--secure password, and prints the log lines to stdout.

Boot messages written before key derivation remain in the plaintext daemon.log
and are not handled by this command.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, encKey, err := loadConfigAndDeriveKey()
			if err != nil {
				return fmt.Errorf("derive key: %w", err)
			}
			if encKey == nil {
				return fmt.Errorf("--secure mode not active; daemon-app.log is not encrypted")
			}
			defer zeroBytes(encKey)

			logPath := filepath.Join(expandHome(cfg.DataDir), "daemon-app.log")
			if _, err := os.Stat(logPath); os.IsNotExist(err) {
				return fmt.Errorf("daemon-app.log not found at %s (daemon may not have written any log lines yet)", logPath)
			}

			r, err := secfile.NewEncryptedLogReader(logPath, encKey)
			if err != nil {
				return fmt.Errorf("open log: %w", err)
			}
			defer r.Close() //nolint:errcheck

			data, err := r.ReadAll()
			if err != nil {
				return fmt.Errorf("read log: %w", err)
			}

			var lines []string
			sc := bufio.NewScanner(strings.NewReader(string(data)))
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}

			start := 0
			if tail > 0 && tail < len(lines) {
				start = len(lines) - tail
			}
			for _, l := range lines[start:] {
				fmt.Println(l)
			}
			return nil
		},
	}
	c.Flags().IntVar(&tail, "tail", 0, "Show last N lines (0 = all)")
	return c
}

// securityGet performs a GET against the daemon URL, honoring TLS config.
func securityGet(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return daemonClient().Do(req)
}

// securityPost performs a POST against the daemon URL, honoring TLS config.
func securityPost(url string, body []byte) (*http.Response, error) {
	var req *http.Request
	var err error
	if body == nil {
		req, err = http.NewRequest(http.MethodPost, url, nil)
	} else {
		req, err = http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return nil, err
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return daemonClient().Do(req)
}

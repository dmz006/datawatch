package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// addHostLoadAlertFlags registers the host load check flags on cmd.
func addHostLoadAlertFlags(cmd *cobra.Command) {
	cmd.Flags().String("host-load-alert-mode", "", "Host load check: off, alert (default) or reap (also ends proven-orphan processes of ended sessions)")
	cmd.Flags().Float64("host-load-alert-multiple", 0, "Host load check: load average above this multiple of the core count is a breach (0 = default 1.5)")
	cmd.Flags().Int("host-load-alert-minutes", 0, "Host load check: minutes a breach must last before it alerts (0 = default 10)")
}

// applyHostLoadAlertFlags collects the host load flags that were set into a
// host_load_alert object on body. An omitted flag leaves its field unchanged.
// A negative number or an unknown mode is rejected before anything is sent.
// Fields already in a JSON host_load_alert object on body are kept; a flag
// wins over the same field given in the JSON.
func applyHostLoadAlertFlags(cmd *cobra.Command, body map[string]any) error {
	h := map[string]any{}
	if cur, ok := body["host_load_alert"].(map[string]any); ok {
		h = cur
	}
	if cmd.Flags().Changed("host-load-alert-mode") {
		m, _ := cmd.Flags().GetString("host-load-alert-mode")
		switch m {
		case "off", "alert", "reap":
		default:
			return fmt.Errorf("--host-load-alert-mode must be off, alert or reap")
		}
		h["mode"] = m
	}
	if cmd.Flags().Changed("host-load-alert-multiple") {
		f, _ := cmd.Flags().GetFloat64("host-load-alert-multiple")
		if f < 0 {
			return fmt.Errorf("--host-load-alert-multiple must be >= 0")
		}
		h["multiple"] = f
	}
	if cmd.Flags().Changed("host-load-alert-minutes") {
		n, _ := cmd.Flags().GetInt("host-load-alert-minutes")
		if n < 0 {
			return fmt.Errorf("--host-load-alert-minutes must be >= 0")
		}
		h["minutes"] = n
	}
	if len(h) > 0 {
		body["host_load_alert"] = h
	}
	return nil
}

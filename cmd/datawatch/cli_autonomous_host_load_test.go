package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func hostLoadFlagCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "x"}
	addHostLoadAlertFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestHostLoadAlert_CLIFlagsBuildObject(t *testing.T) {
	cmd := hostLoadFlagCmd(t, "--host-load-alert-mode", "reap", "--host-load-alert-multiple", "2.5", "--host-load-alert-minutes", "15")
	body := map[string]any{}
	if err := applyHostLoadAlertFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	h := body["host_load_alert"].(map[string]any)
	if h["mode"] != "reap" || h["multiple"] != 2.5 || h["minutes"] != 15 {
		t.Fatalf("host_load_alert = %v", h)
	}
}

func TestHostLoadAlert_CLIOmittedFlagsLeaveBodyAlone(t *testing.T) {
	body := map[string]any{}
	if err := applyHostLoadAlertFlags(hostLoadFlagCmd(t), body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["host_load_alert"]; ok {
		t.Fatalf("object added with no flags: %v", body)
	}
}

func TestHostLoadAlert_CLIFlagWinsOverJSONAndKeepsOtherFields(t *testing.T) {
	body := map[string]any{"host_load_alert": map[string]any{"mode": "off", "minutes": float64(7)}}
	if err := applyHostLoadAlertFlags(hostLoadFlagCmd(t, "--host-load-alert-mode", "alert"), body); err != nil {
		t.Fatal(err)
	}
	h := body["host_load_alert"].(map[string]any)
	if h["mode"] != "alert" || h["minutes"] != float64(7) {
		t.Fatalf("host_load_alert = %v, want mode=alert and minutes kept", h)
	}
}

func TestHostLoadAlert_CLIRejectsBadValues(t *testing.T) {
	for _, args := range [][]string{
		{"--host-load-alert-mode", "kill"},
		{"--host-load-alert-multiple=-1"},
		{"--host-load-alert-minutes=-2"},
	} {
		body := map[string]any{}
		if err := applyHostLoadAlertFlags(hostLoadFlagCmd(t, args...), body); err == nil {
			t.Errorf("%v accepted", args)
		}
		if len(body) != 0 {
			t.Errorf("%v changed the body: %v", args, body)
		}
	}
}

func TestHostLoadAlert_CLIConfigSetHelpAndFlags(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	for _, want := range []string{"host_load_alert.mode", "host_load_alert.multiple", "host_load_alert.minutes", "--host-load-alert-mode"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("config-set help lacks %q", want)
		}
	}
	for _, f := range []string{"host-load-alert-mode", "host-load-alert-multiple", "host-load-alert-minutes"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("config-set lacks flag --%s", f)
		}
	}
}

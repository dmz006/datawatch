// BL330 — `datawatch push` CLI subcommands for UnifiedPush / mobile push
// registration management.
//
//	datawatch push list               → GET /api/push/register (list registrations)
//	datawatch push test [--id <id>]   → POST /api/push/notify (test push delivery)
//	datawatch push unregister         → DELETE /api/push/unregister (remove registration)

package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Manage UnifiedPush registrations and test push delivery (BL330)",
		Long: `Manage mobile push endpoint registrations and trigger test push
notifications via the datawatch UnifiedPush gateway.

Subcommands:
  list               List all registered push endpoints
  test [--id <id>]   Send a test push notification
  unregister         Remove a push registration by endpoint or id
  apns-test          Send a test push via APNs (BL397 Phase 4 / BL335)`,
	}
	cmd.AddCommand(
		newPushListCmd(),
		newPushTestCmd(),
		newPushUnregisterCmd(),
		newPushAPNsTestCmd(),
	)
	return cmd
}

// newPushAPNsTestCmd — BL397 Phase 4 / BL335. Separate from
// newPushTestCmd above: that one targets UnifiedPush registrations
// (internal/server/push.go's pushRegistration), this one targets APNs
// devices (internal/devices.Store, kind=apns) — different registries,
// different dispatch mechanism (native APNs HTTP/2 + JWT vs. a generic
// webhook POST).
func newPushAPNsTestCmd() *cobra.Command {
	var deviceID string
	cmd := &cobra.Command{
		Use:   "apns-test",
		Short: "Send a test push to one or all registered APNs (iOS) devices",
		RunE: func(*cobra.Command, []string) error {
			body := map[string]any{}
			if deviceID != "" {
				body["device_id"] = deviceID
			}
			return daemonJSON(http.MethodPost, "/api/push/apns/test", body)
		},
	}
	cmd.Flags().StringVar(&deviceID, "device-id", "", "Target one device (default: all registered APNs devices)")
	return cmd
}

func newPushListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered push endpoints",
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemonGet("/api/push/register")
		},
	}
}

func newPushTestCmd() *cobra.Command {
	var regID string
	var msg string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Send a test push notification",
		Long: `Sends a test push notification to all registered endpoints, or to a
specific registration when --id is specified.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if msg == "" {
				msg = "test from CLI"
			}
			body := map[string]any{
				"message": msg,
				"title":   "datawatch test",
			}
			if regID != "" {
				body["registration_id"] = regID
			}
			return daemonJSON(http.MethodPost, "/api/push/notify", body)
		},
	}
	cmd.Flags().StringVar(&regID, "id", "", "registration ID to target (empty = all registrations)")
	cmd.Flags().StringVar(&msg, "message", "", "custom message (default: \"test from CLI\")")
	return cmd
}

func newPushUnregisterCmd() *cobra.Command {
	var endpoint string
	var regID string
	cmd := &cobra.Command{
		Use:   "unregister",
		Short: "Remove a push registration",
		Long:  "Remove a push registration by --endpoint URL or --id. At least one is required.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if endpoint == "" && regID == "" {
				return fmt.Errorf("--endpoint or --id is required")
			}
			body := map[string]any{}
			if endpoint != "" {
				body["endpoint"] = endpoint
			}
			if regID != "" {
				body["id"] = regID
			}
			return daemonJSON(http.MethodDelete, "/api/push/unregister", body)
		},
	}
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "endpoint URL to unregister")
	cmd.Flags().StringVar(&regID, "id", "", "registration ID to unregister")
	return cmd
}

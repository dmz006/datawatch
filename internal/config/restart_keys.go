package config

import (
	"encoding/json"
	"sort"
	"strings"
)

// Live settings. A config key either takes effect the moment it is saved, or
// is read once at daemon start and needs a restart. restartRequiredKeys names
// the second kind: the one list PUT /api/config and the startup log line use.
// The reload reply keeps its own literal list in internal/server/reload.go,
// which two parity tests read from the source. An entry ending in ".*" matches every key below that prefix; any
// other entry matches that key exactly. A key not listed here is applied live.
var restartRequiredKeys = []string{
	"pin_check_seconds", // pin keepers start once at daemon boot
	"server.host", "server.port",
	"server.tls", "server.tls_auto_generate", "server.tls_cert", "server.tls_key",
	"acme.enabled", "acme.endpoint", "acme.domains", "acme.method", "acme.dns01.*", // the manager starts only at boot
	"mcp.enabled", "mcp.sse_enabled", "mcp.sse_host", "mcp.sse_port", "mcp.public_url",
	"mcp.tls_enabled", "mcp.resource_subscriptions",
	"dns_channel.enabled", "dns_channel.listen", "dns_channel.domain",
	"signal.account_number", "signal.group_id",
	"database.*", "agents.*",
	"delivery.enabled", "delivery.interval_seconds", // the delivery check reads them once at start
}

// RestartRequiredKeys returns a copy of the key list that needs a restart.
func RestartRequiredKeys() []string {
	return append([]string(nil), restartRequiredKeys...)
}

// KeyRequiresRestart reports whether a change to key only applies after a
// daemon restart.
func KeyRequiresRestart(key string) bool {
	for _, k := range restartRequiredKeys {
		if prefix, ok := strings.CutSuffix(k, ".*"); ok {
			if strings.HasPrefix(key, prefix+".") {
				return true
			}
			continue
		}
		if key == k {
			return true
		}
	}
	return false
}

// SplitRestartKeys sorts keys into those that apply immediately and those that
// need a restart. Both results are sorted.
func SplitRestartKeys(keys []string) (immediate, restart []string) {
	for _, k := range keys {
		if KeyRequiresRestart(k) {
			restart = append(restart, k)
		} else {
			immediate = append(immediate, k)
		}
	}
	sort.Strings(immediate)
	sort.Strings(restart)
	return immediate, restart
}

// PutConfigApplyNote reads a PUT /api/config reply body and describes which of
// the saved keys are live and which need a restart. It returns "" when the
// reply lists neither or is not the daemon's JSON. The CLI, the MCP config_set
// tool and the chat configure verb append it to their confirmation.
func PutConfigApplyNote(body []byte) string {
	var resp struct {
		Applied         []string `json:"applied"`
		RequiresRestart []string `json:"requires_restart"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return ""
	}
	switch {
	case len(resp.RequiresRestart) == 0 && len(resp.Applied) == 0:
		return ""
	case len(resp.RequiresRestart) == 0:
		return "applied now"
	case len(resp.Applied) == 0:
		return "saved; restart the daemon to apply: " + strings.Join(resp.RequiresRestart, ", ")
	}
	return "applied now: " + strings.Join(resp.Applied, ", ") + "; restart the daemon to apply: " + strings.Join(resp.RequiresRestart, ", ")
}

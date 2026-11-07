// plugins.services configure keys — the service-plugin watch settings
// (internal/config.PluginServicesConfig) are settable from chat with
// `configure <key>=<value>`. handleConfigure (internal/router/router.go, the
// configure-verb dispatcher) is generic — it passes the key/value pair to the
// daemon, which validates and stores it through the same PUT /api/config path
// that `datawatch config set` uses (applyConfigPatch in internal/server) — so
// this file owns what the router knows about these keys: the closed set and
// the help text, spliced into both the `configure help` and `configure list`
// output so an operator can discover them next to every other key.
package router

import "strings"

// pluginServiceConfigureEntry is one settable plugins.services.* key and the
// help line that documents it.
type pluginServiceConfigureEntry struct {
	Key  string // full dot-path key (the per_service entry names the <service> placeholder)
	Help string // one-line help shown in `configure help` and `configure list`
}

// pluginServicesConfigureEntries — the settable plugins.services keys, in
// help order. The values are validated by the daemon with the same rules a
// config load runs (config.PluginServicesConfig.Validate), and a 0 or empty
// value reads the documented default; the per_service entry is a prefix where
// <service> is a service-plugin manifest name.
var pluginServicesConfigureEntries = []pluginServiceConfigureEntry{
	{"plugins.services.health_interval_seconds", "whole seconds >= 5; 0 = the 60-second default"},
	{"plugins.services.backlog_growth_pulls", "whole number >= 1; 0 = the 3-pull default"},
	{"plugins.services.node_load_ratio", "number > 0; 0 = the 1.5 default"},
	{"plugins.services.small_model_max_params_billions", "number > 0; 0 = the 8-billion default"},
	{"plugins.services.small_model_context_tokens", "whole number >= 1; 0 = the 8192-token default"},
	{"plugins.services.per_service.<service>", "attach_only|managed; empty clears the service's override"},
}

// PluginServicesConfigureHelpLines renders the registry as the help lines
// handleConfigure splices into the `configure help` and `configure list`
// text; each line already carries the two-space indent the other key lines
// in that text have.
func PluginServicesConfigureHelpLines() []string {
	lines := make([]string, 0, len(pluginServicesConfigureEntries))
	for _, e := range pluginServicesConfigureEntries {
		lines = append(lines, "  "+e.Key+" ("+e.Help+")")
	}
	return lines
}

// PluginServicesConfigureKeyHelp reports the help text for one settable
// plugins.services key (exact match; the per_service placeholder key matches
// only the literal "<service>" spelling).
func PluginServicesConfigureKeyHelp(key string) (string, bool) {
	for _, e := range pluginServicesConfigureEntries {
		if e.Key == key {
			return e.Help, true
		}
	}
	return "", false
}

// PluginServicesConfigureIsKnown reports whether key is one of the settable
// plugins.services keys: an exact scalar key, or the per_service prefix with
// a non-empty service name.
func PluginServicesConfigureIsKnown(key string) bool {
	if _, ok := PluginServicesConfigureKeyHelp(key); ok {
		return true
	}
	if service, ok := strings.CutPrefix(key, "plugins.services.per_service."); ok {
		return service != ""
	}
	return key == "plugins.services.per_service" // the whole-map key (a JSON object value)
}

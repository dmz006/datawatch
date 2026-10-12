// BL403 — plugin widgets: PWA dashboard/session tiles a plugin declares in
// its manifest. Both automation and service plugins may declare widgets.

package plugins

import (
	"fmt"
	"strings"
	"time"
)

// Widget surfaces — where in the PWA a widget renders.
const (
	WidgetSurfaceDashboard = "dashboard"
	WidgetSurfaceSession   = "session"
)

// Widget kinds — how the PWA renders a widget's value.
const (
	WidgetKindStatTile    = "stat_tile"
	WidgetKindTable       = "table"
	WidgetKindList        = "list"
	WidgetKindSparkline   = "sparkline"
	WidgetKindStatusBadge = "status_badge"
	WidgetKindMarkdown    = "markdown"
)

// Widget data modes — how a widget's value reaches the daemon.
const (
	// WidgetDataPush: the plugin POSTs values to
	// /api/plugins/{name}/widgets/{widget_id}.
	WidgetDataPush = "push"
	// WidgetDataPull: the daemon fetches values on the widget's interval —
	// via the status_poll hook (kind: automation) or
	// GET <connection.base_url><pull_path> (kind: service).
	WidgetDataPull = "pull"
)

// DefaultWidgetPullInterval is the pull interval used when a pull-mode
// widget leaves interval_seconds unset (0).
const DefaultWidgetPullInterval = 30 * time.Second

// Widget is one entry of a manifest's widgets: block.
type Widget struct {
	// ID is unique within the manifest; it addresses the widget in the
	// push endpoint and in Manifest.Widget lookups.
	ID string `yaml:"id" json:"id"`
	// Surface is "dashboard" or "session".
	Surface string `yaml:"surface" json:"surface"`
	// Kind is one of the WidgetKind* constants.
	Kind string `yaml:"kind" json:"kind"`
	// DataMode is "push" or "pull".
	DataMode string `yaml:"data_mode" json:"data_mode"`
	// PullPath is required for pull mode and must be empty for push mode.
	// For kind: service it is a path appended to connection.base_url and
	// must start with "/". For kind: automation it is still required: it is
	// passed through to the plugin in the status_poll request payload so a
	// plugin with several pull widgets can tell them apart.
	PullPath string `yaml:"pull_path,omitempty" json:"pull_path,omitempty"`
	// IntervalSeconds only applies to pull mode. 0 means
	// DefaultWidgetPullInterval (30s); negative values are rejected by
	// Validate, so the effective floor is 1 second.
	IntervalSeconds int `yaml:"interval_seconds,omitempty" json:"interval_seconds,omitempty"`
}

// PullInterval returns how often the daemon should pull this widget's value:
// IntervalSeconds, or DefaultWidgetPullInterval when it is unset. A negative
// value (which Validate rejects) also falls back to the default.
func (w Widget) PullInterval() time.Duration {
	if w.IntervalSeconds <= 0 {
		return DefaultWidgetPullInterval
	}
	return time.Duration(w.IntervalSeconds) * time.Second
}

// Widget returns the manifest widget with the given ID.
func (m Manifest) Widget(id string) (Widget, bool) {
	for _, w := range m.Widgets {
		if w.ID == id {
			return w, true
		}
	}
	return Widget{}, false
}

var (
	validWidgetSurfaces = []string{WidgetSurfaceDashboard, WidgetSurfaceSession}
	validWidgetKinds    = []string{WidgetKindStatTile, WidgetKindTable, WidgetKindList,
		WidgetKindSparkline, WidgetKindStatusBadge, WidgetKindMarkdown}
)

// validateWidgets checks a manifest's widgets: block. pluginKind is the
// normalized manifest kind; it decides the pull_path shape for service
// plugins. Every error names the offending widget by index and ID.
func validateWidgets(pluginKind string, widgets []Widget) []error {
	var errs []error
	seen := map[string]int{}
	for i, w := range widgets {
		label := fmt.Sprintf("widgets[%d] (id %q)", i, w.ID)
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("%s: %s", label, fmt.Sprintf(format, args...)))
		}
		if strings.TrimSpace(w.ID) == "" {
			bad("id is required")
		} else if first, dup := seen[w.ID]; dup {
			bad("id duplicates widgets[%d]", first)
		} else {
			seen[w.ID] = i
		}
		if !containsString(validWidgetSurfaces, w.Surface) {
			bad("surface %q is invalid: must be %s", w.Surface, quoteList(validWidgetSurfaces))
		}
		if !containsString(validWidgetKinds, w.Kind) {
			bad("kind %q is invalid: must be %s", w.Kind, quoteList(validWidgetKinds))
		}
		switch w.DataMode {
		case WidgetDataPull:
			switch {
			case w.PullPath == "":
				bad("pull_path is required when data_mode is %q", WidgetDataPull)
			case pluginKind == KindService && !strings.HasPrefix(w.PullPath, "/"):
				bad("pull_path %q must start with \"/\" for kind %q", w.PullPath, KindService)
			}
		case WidgetDataPush:
			if w.PullPath != "" {
				bad("pull_path must be empty when data_mode is %q", WidgetDataPush)
			}
		default:
			bad("data_mode %q is invalid: must be %q or %q", w.DataMode, WidgetDataPush, WidgetDataPull)
		}
		if w.IntervalSeconds < 0 {
			bad("interval_seconds must be >= 0 (0 means the %s default)", DefaultWidgetPullInterval)
		}
	}
	return errs
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func quoteList(list []string) string {
	q := make([]string, len(list))
	for i, v := range list {
		q[i] = fmt.Sprintf("%q", v)
	}
	if len(q) == 2 {
		return q[0] + " or " + q[1]
	}
	return "one of " + strings.Join(q, ", ")
}

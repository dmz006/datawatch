// BL403 — plugin widgets manifest block tests.

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const svcWidgetBase = "name: x\nkind: service\nconnection:\n  base_url: http://h\n"

func TestWidgets_Valid(t *testing.T) {
	cases := []struct{ name, src string }{
		{"automation pull", "name: x\nentry: ./p\nhooks: [status_poll]\nwidgets:\n" +
			"  - id: queue\n    surface: dashboard\n    kind: stat_tile\n    data_mode: pull\n    pull_path: queue-depth\n    interval_seconds: 10\n"},
		{"service push", svcWidgetBase + "widgets:\n" +
			"  - id: inbox\n    surface: session\n    kind: list\n    data_mode: push\n"},
		{"service pull", svcWidgetBase + "widgets:\n" +
			"  - id: health\n    surface: dashboard\n    kind: status_badge\n    data_mode: pull\n    pull_path: /widgets/health\n"},
		{"every kind", "name: x\nwidgets:\n" +
			"  - {id: a, surface: dashboard, kind: stat_tile, data_mode: push}\n" +
			"  - {id: b, surface: dashboard, kind: table, data_mode: push}\n" +
			"  - {id: c, surface: dashboard, kind: list, data_mode: push}\n" +
			"  - {id: d, surface: session, kind: sparkline, data_mode: push}\n" +
			"  - {id: e, surface: session, kind: status_badge, data_mode: push}\n" +
			"  - {id: f, surface: session, kind: markdown, data_mode: push}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := parseManifest(t, tc.src).Validate(); err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
}

func TestWidgets_ValidateErrors(t *testing.T) {
	auto := func(widgets string) string { return "name: x\nwidgets:\n" + widgets }
	svc := func(widgets string) string { return svcWidgetBase + "widgets:\n" + widgets }
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"bad surface", auto("  - {id: w, surface: sidebar, kind: list, data_mode: push}\n"),
			[]string{`widgets[0] (id "w"): surface "sidebar" is invalid`}},
		{"bad kind", auto("  - {id: w, surface: dashboard, kind: pie_chart, data_mode: push}\n"),
			[]string{`widgets[0] (id "w"): kind "pie_chart" is invalid`}},
		{"bad data_mode", auto("  - {id: ok, surface: dashboard, kind: list, data_mode: push}\n" +
			"  - {id: inbox, surface: dashboard, kind: list, data_mode: poll}\n"),
			[]string{`widgets[1] (id "inbox"): data_mode "poll" is invalid: must be "push" or "pull"`}},
		{"reserved token id", auto("  - {id: token, surface: dashboard, kind: list, data_mode: push}\n"),
			[]string{`widgets[0] (id "token"): id "token" is reserved`}},
		{"pull without pull_path", auto("  - {id: w, surface: dashboard, kind: list, data_mode: pull}\n"),
			[]string{`widgets[0] (id "w"): pull_path is required`}},
		{"service pull without pull_path", svc("  - {id: w, surface: dashboard, kind: list, data_mode: pull}\n"),
			[]string{`widgets[0] (id "w"): pull_path is required`}},
		{"service pull relative path", svc("  - {id: w, surface: dashboard, kind: list, data_mode: pull, pull_path: stats}\n"),
			[]string{`widgets[0] (id "w"): pull_path "stats" must start with "/"`}},
		{"push with pull_path", auto("  - {id: w, surface: dashboard, kind: list, data_mode: push, pull_path: /x}\n"),
			[]string{`widgets[0] (id "w"): pull_path must be empty when data_mode is "push"`}},
		{"duplicate ids", auto("  - {id: w, surface: dashboard, kind: list, data_mode: push}\n" +
			"  - {id: w, surface: session, kind: table, data_mode: push}\n"),
			[]string{`widgets[1] (id "w"): id duplicates widgets[0]`}},
		{"missing id", auto("  - {surface: dashboard, kind: list, data_mode: push}\n"),
			[]string{`widgets[0] (id ""): id is required`}},
		{"negative interval", auto("  - {id: w, surface: dashboard, kind: list, data_mode: pull, pull_path: p, interval_seconds: -5}\n"),
			[]string{`widgets[0] (id "w"): interval_seconds must be >= 0`}},
		{"joins every problem", "widgets:\n  - {id: w, surface: x, kind: y, data_mode: z, interval_seconds: -1}\n",
			[]string{"name is required", "surface", "kind", "data_mode", "interval_seconds"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseManifest(t, tc.src).Validate()
			if err == nil {
				t.Fatalf("expected error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err.Error(), w)
				}
			}
		})
	}
}

func TestWidget_PullInterval(t *testing.T) {
	cases := []struct {
		secs int
		want time.Duration
	}{
		{0, DefaultWidgetPullInterval},
		{-3, DefaultWidgetPullInterval},
		{1, time.Second},
		{120, 2 * time.Minute},
	}
	if DefaultWidgetPullInterval != 30*time.Second {
		t.Fatalf("DefaultWidgetPullInterval = %v, want 30s", DefaultWidgetPullInterval)
	}
	for _, tc := range cases {
		if got := (Widget{IntervalSeconds: tc.secs}).PullInterval(); got != tc.want {
			t.Errorf("PullInterval(%d) = %v, want %v", tc.secs, got, tc.want)
		}
	}
}

func TestManifest_WidgetLookup(t *testing.T) {
	m := Manifest{Widgets: []Widget{{ID: "a", Kind: WidgetKindTable}, {ID: "b", Kind: WidgetKindList}}}
	if w, ok := m.Widget("b"); !ok || w.Kind != WidgetKindList {
		t.Fatalf("Widget(b) = %+v, %v", w, ok)
	}
	if _, ok := m.Widget("missing"); ok {
		t.Fatal("Widget(missing) should not be found")
	}
}

func TestRegistry_Discover_Widgets(t *testing.T) {
	root := t.TempDir()
	for dirName, src := range map[string]string{
		"svc": svcWidgetBase + "widgets:\n" +
			"  - id: inbox\n    surface: dashboard\n    kind: stat_tile\n    data_mode: pull\n    pull_path: /widgets/inbox\n    interval_seconds: 15\n" +
			"  - id: feed\n    surface: session\n    kind: markdown\n    data_mode: push\n",
		"bad": "name: bad\nwidgets:\n  - {id: w, surface: dashboard, kind: list, data_mode: poll}\n",
	} {
		dir := filepath.Join(root, dirName)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		writeManifest(t, dir, src)
	}
	r, err := NewRegistry(Config{Enabled: true, Dir: root})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, ok := r.Get("bad"); ok {
		t.Fatal("manifest with an invalid widget should be skipped")
	}
	p, ok := r.Get("x")
	if !ok {
		t.Fatal("service plugin with widgets should load")
	}
	if len(p.Widgets) != 2 {
		t.Fatalf("expected 2 widgets, got %+v", p.Widgets)
	}
	inbox, ok := p.Widget("inbox")
	if !ok {
		t.Fatal("inbox widget missing")
	}
	want := Widget{ID: "inbox", Surface: WidgetSurfaceDashboard, Kind: WidgetKindStatTile,
		DataMode: WidgetDataPull, PullPath: "/widgets/inbox", IntervalSeconds: 15}
	if inbox != want {
		t.Fatalf("inbox = %+v, want %+v", inbox, want)
	}
	if inbox.PullInterval() != 15*time.Second {
		t.Fatalf("PullInterval = %v", inbox.PullInterval())
	}
	if feed, _ := p.Widget("feed"); feed.DataMode != WidgetDataPush || feed.PullInterval() != DefaultWidgetPullInterval {
		t.Fatalf("feed = %+v", feed)
	}
}

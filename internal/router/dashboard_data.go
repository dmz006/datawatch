// dashboard_data.go — comm channel verbs for the dashboard data endpoints.
//
//	dashboard work-map                 GET /api/dashboard/work-map
//	dashboard lane-pulse [window]      GET /api/dashboard/lane-pulse
//	dashboard series <name> [value]    GET /api/dashboard/series/<name>
//
// The reply carries the endpoint body as compact JSON, the same data REST returns.

package router

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

// dashboardSeriesParam maps each series to the one query parameter its
// optional value feeds.
var dashboardSeriesParam = map[string]string{
	"lane-utilization":    "window",
	"throughput-forecast": "days",
	"verifier-health":     "window",
	"release-train":       "days",
	"review-backlog":      "limit",
	"defects-per-day":     "days",
}

const dashboardSeriesList = "lane-utilization, throughput-forecast, verifier-health, release-train, review-backlog, defects-per-day"

// parseDashboardData recognises the three dashboard data verbs; rest is the
// text after "dashboard ".
func parseDashboardData(lower string) (Command, bool) {
	f := strings.Fields(lower)
	if len(f) < 2 || f[0] != "dashboard" {
		return Command{}, false
	}
	switch f[1] {
	case "work-map", "work_map":
		if len(f) == 2 {
			return Command{Type: CmdDashboardData, Text: "work-map"}, true
		}
	case "lane-pulse", "lane_pulse":
		if len(f) <= 3 {
			return Command{Type: CmdDashboardData, Text: strings.Join(append([]string{"lane-pulse"}, f[2:]...), " ")}, true
		}
	case "series":
		if len(f) == 3 || len(f) == 4 {
			return Command{Type: CmdDashboardData, Text: strings.Join(f[1:], " ")}, true
		}
	}
	return Command{}, false
}

func isDashboardDataVerb(lower string) bool {
	_, ok := parseDashboardData(lower)
	return ok
}

func (r *Router) handleDashboardData(cmd Command) {
	f := strings.Fields(cmd.Text)
	var path string
	q := url.Values{}
	label := "dashboard " + cmd.Text + ":"
	switch f[0] {
	case "work-map":
		path = "/api/dashboard/work-map"
	case "lane-pulse":
		path = "/api/dashboard/lane-pulse"
		if len(f) > 1 {
			q.Set("window", f[1])
		}
	case "series":
		name := f[1]
		param, ok := dashboardSeriesParam[name]
		if !ok {
			r.reply("dashboard series failed", "unknown series "+name+": use one of "+dashboardSeriesList)
			return
		}
		path = "/api/dashboard/series/" + name
		if len(f) > 2 {
			q.Set(param, f[2])
		}
	}
	body, err := r.commGet(path, q)
	if err != nil {
		r.reply("dashboard "+f[0]+" failed", err.Error())
		return
	}
	var buf bytes.Buffer
	if json.Compact(&buf, []byte(body)) != nil {
		buf.Reset()
		buf.WriteString(strings.TrimSpace(body))
	}
	r.reply(label, buf.String())
}

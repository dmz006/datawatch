// Package metrics provides Prometheus metric registration and collection.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

var (
	// Session metrics
	SessionsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "datawatch_sessions_active",
			Help: "Number of currently active sessions",
		},
		[]string{"backend", "state"},
	)
	SessionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "datawatch_sessions_total",
			Help: "Total sessions created",
		},
		[]string{"backend"},
	)

	// System metrics
	CPUUsage = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_cpu_load_avg_1",
		Help: "1-minute load average",
	})
	MemoryUsed = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_memory_used_bytes",
		Help: "Used memory in bytes",
	})
	DiskUsed = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_disk_used_bytes",
		Help: "Used disk space in bytes",
	})
	DaemonRSS = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_daemon_rss_bytes",
		Help: "Daemon resident set size in bytes",
	})
	Goroutines = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_goroutines",
		Help: "Number of goroutines",
	})
	UptimeSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_uptime_seconds",
		Help: "Daemon uptime in seconds",
	})

	// Message metrics
	MessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "datawatch_messages_total",
			Help: "Total messages sent/received",
		},
		[]string{"channel", "direction"},
	)
	AlertsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "datawatch_alerts_total",
			Help: "Total alerts generated",
		},
		[]string{"level"},
	)

	// RTK metrics
	RTKTokensSaved = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_rtk_tokens_saved_total",
		Help: "Total tokens saved by RTK compression",
	})
	RTKSavingsPct = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_rtk_savings_percent",
		Help: "Average RTK savings percentage",
	})

	// BL366 — verifier git-diff grounding metrics.
	VerifierDiffInjectionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_verifier_diff_injections_total",
		Help: "Total verifier calls where a git diff was injected into the prompt",
	})

	// BL367 — autonomous quality gate metrics.
	QualityGateRunsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_quality_gate_runs_total",
		Help: "Total autonomous quality gate evaluations run",
	})
	QualityGatePassTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_quality_gate_pass_total",
		Help: "Quality gate evaluations with no regression",
	})
	QualityGateRegressionTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_quality_gate_regression_total",
		Help: "Quality gate evaluations that detected a test regression",
	})
)

// Register registers all Prometheus metrics.
func Register() {
	prometheus.MustRegister(
		SessionsActive, SessionsTotal,
		CPUUsage, MemoryUsed, DiskUsed, DaemonRSS, Goroutines, UptimeSeconds,
		MessagesTotal, AlertsTotal,
		RTKTokensSaved, RTKSavingsPct,
		VerifierDiffInjectionsTotal,
		QualityGateRunsTotal, QualityGatePassTotal, QualityGateRegressionTotal,
	)
}

// Handler returns an HTTP handler for the /metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Package metrics provides Prometheus metric registration and collection.
package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	// Capacity admission metrics
	CapacityHeld = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "datawatch_capacity_pool_used",
		Help: "Slots in use per capacity pool (leases plus external sessions)",
	}, []string{"pool"})
	CapacityLimit = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "datawatch_capacity_pool_limit",
		Help: "Configured limit per capacity pool (0 = unlimited)",
	}, []string{"pool"})
	CapacityWaiting = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_capacity_waiting",
		Help: "Tasks waiting for a capacity lease",
	})
	CapacityLeasesReaped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_capacity_leases_reaped_total",
		Help: "Leaked capacity leases reclaimed by the reaper",
	})

	// Verifier cost metrics
	VerifierRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "datawatch_verifier_runs_total",
		Help: "Verifier runs by outcome category",
	}, []string{"category"})
	VerifierWaitSeconds = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_verifier_wait_seconds_total",
		Help: "Total seconds verifiers waited for a capacity lease",
	})
	VerifierRunSeconds = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_verifier_run_seconds_total",
		Help: "Total seconds verifiers ran after their lease was granted",
	})

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

	// GH#201 — HTTP access / WS lifecycle / auth-failure log.
	AccessLogEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "datawatch_access_log_events_total",
			Help: "Total access-log events (http_access, auth_failure, ws_connect, ws_disconnect) by action and resolved principal kind",
		},
		[]string{"action", "principal_kind"},
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

	// BL369 — prompt injection guard metrics.
	InjectionGuardHitsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_injection_guard_hits_total",
		Help: "Total user-submitted texts flagged by the prompt injection guard (BL369)",
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

	// Agent tracing (tracing.*).
	TraceEventsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_trace_events_total",
		Help: "Trace events written to the trace store",
	})
	TraceAppendErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_trace_append_errors_total",
		Help: "Trace store writes that failed",
	})
	TraceStoredEvents = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_trace_stored_events",
		Help: "Events currently held in the trace store",
	})
	TraceStoredSessions = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_trace_stored_sessions",
		Help: "Sessions with a trace currently held in the trace store",
	})

	// Burn guard (usage.burn_guard).
	BurnGuardLevel = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_burn_guard_level",
		Help: "Burn guard ladder rungs in force (0 = none)",
	})
	BurnGuardProjectedPercent = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_burn_guard_projected_percent",
		Help: "Weekly usage percentage projected at the reset at the current burn",
	})
	BurnGuardOverByPoints = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_burn_guard_over_by_points",
		Help: "Projected percentage minus the target (negative when under)",
	})
	BurnGuardPointsPerHour = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_burn_guard_points_per_hour",
		Help: "Weekly points burned per hour over the trailing window",
	})
	BurnGuardTicksTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_burn_guard_ticks_total",
		Help: "Burn guard evaluations run",
	})
	BurnGuardStepsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "datawatch_burn_guard_steps_total",
		Help: "Burn guard ladder steps by direction (climb, step_back)",
	}, []string{"direction"})
	BurnGuardRefitsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_burn_guard_refits_total",
		Help: "Burn guard cost model refits attempted on a new usage reading",
	})
	BurnGuardErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_burn_guard_errors_total",
		Help: "Burn guard errors (readings, transcripts, admission, state save)",
	})

	// Supervisor watch gauges.
	SupervisorWatchFindings = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "datawatch_supervisor_watch_findings",
		Help: "Unresolved supervisor watch findings by standing (open includes would_alert and alerted)",
	}, []string{"state"})
	SupervisorWatchAgreedDays = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_supervisor_watch_agreed_days",
		Help: "Calendar days whose supervisor watch comparisons with the supervisor script all agreed",
	})
	SupervisorWatchLastPollAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_supervisor_watch_last_poll_age_seconds",
		Help: "Seconds since the coordinator last ran a supervisor watch pass (-1 when it never has)",
	})

	// Branch refresh gauges: Automata by the result of their latest refresh,
	// and the Automata whose refreshing a conflict stopped.
	BranchRefreshAutomata = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "datawatch_branch_refresh_automata",
		Help: "Automata by the result of their latest branch refresh (clean, conflict, error)",
	}, []string{"result"})
	BranchRefreshStopped = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_branch_refresh_stopped",
		Help: "Automata whose branch refreshing a merge conflict stopped",
	})

	// Supervisor loop gauges.
	SupervisorLoopRuns = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "datawatch_supervisor_loop_runs",
		Help: "Stored supervisor loop run records by outcome (ok, validator_failed, escalated, error)",
	}, []string{"outcome"})
	SupervisorLoopJobsEnabled = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_supervisor_loop_jobs_enabled",
		Help: "Supervisor loop jobs currently enabled (0 when the whole loop is off)",
	})
	SupervisorLoopLastRunAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_supervisor_loop_last_run_age_seconds",
		Help: "Seconds since the supervisor loop last ran a job (-1 when it never has)",
	})

	// Host load check (autonomous.host_load_alert): effective mode, the last
	// load reading against its threshold, whether the reading is over the
	// threshold, and the processes the reaper has ended (reap mode).
	HostLoadLastRead = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_host_load_last_reading",
		Help: "1-minute load average of the host load check's last successful reading (0 before the first reading, e.g. mode off)",
	})
	HostLoadLimit = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_host_load_limit",
		Help: "Threshold (multiple x core count) of the host load check's last successful reading",
	})
	HostLoadBreached = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_host_load_breached",
		Help: "1 while the host load check's last reading is over its threshold (a breach streak is in progress), else 0",
	})
	HostLoadOrphansReaped = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "datawatch_host_load_orphans_reaped_total",
		Help: "Leftover processes the host load check's reaper has ended for ended sessions (reap mode; the alert fires on the same tick as the reap)",
	})

	// BL397 — native ACME subsystem metrics.
	AcmeCertExpirySeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "datawatch_acme_cert_expiry_seconds",
		Help: "Seconds until the current ACME-issued cert expires (0 if never issued)",
	}, []string{"domain"})
	AcmeRenewalsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "datawatch_acme_renewals_total",
		Help: "ACME issue/renewal attempts by outcome",
	}, []string{"domain", "result"}) // result: success | failure

	// Web Search (SearXNG) metrics.
	WebSearchQueriesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_web_search_queries_total",
		Help: "Total web search queries proxied to SearXNG",
	})
	WebSearchErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_web_search_errors_total",
		Help: "Total web search queries that returned an error",
	})
	// Worker questions: what became of the DATAWATCH_QUESTION: marker lines
	// and of the tasks parked on a blocking one. outcome: raised | duplicate
	// | refused | parked | resumed.
	WorkerQuestionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "datawatch_worker_questions_total",
		Help: "Worker question marker outcomes (raised, duplicate, refused, parked, resumed)",
	}, []string{"outcome"})
	// Interstitial UI-prompt dismissals: what became of the safe dismiss key
	// the daemon pressed on a recognised interstitial prompt. outcome:
	// dismissed (the key cleared the prompt) | not_cleared (attempts cap
	// used up, the daemon gave up on keys for the occurrence) | refused (the
	// send-site guard refused the key, nothing was sent). kind is the
	// interstitial prompt kind (e.g. autocompact).
	UIPromptDismissalsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "datawatch_ui_prompt_dismissals_total",
		Help: "Interstitial UI-prompt dismissals by kind and outcome (dismissed, not_cleared, refused)",
	}, []string{"kind", "outcome"})
	// Session result endpoint metrics (scheduled-job structured results).
	// Posted = accepted and recorded; rejected = failed authz/identity/validation.
	SessionResultsPosted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_session_results_posted_total",
		Help: "Session result payloads accepted by the result endpoint",
	})
	SessionResultsRejected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "datawatch_session_results_rejected_total",
		Help: "Session result payloads rejected by the result endpoint",
	})
)

// Register registers all Prometheus metrics. Re-calling it is safe:
// the second call is a no-op.
var registerOnce sync.Once

func Register() {
	registerOnce.Do(func() {
		prometheus.MustRegister(
			CapacityHeld, CapacityLimit, CapacityWaiting, CapacityLeasesReaped,
			VerifierRuns, VerifierWaitSeconds, VerifierRunSeconds,
			SessionsActive, SessionsTotal,
			CPUUsage, MemoryUsed, DiskUsed, DaemonRSS, Goroutines, UptimeSeconds,
			MessagesTotal, AlertsTotal,
			AccessLogEventsTotal,
			SessionResultsPosted, SessionResultsRejected,
			RTKTokensSaved, RTKSavingsPct,
			VerifierDiffInjectionsTotal,
			InjectionGuardHitsTotal,
			QualityGateRunsTotal, QualityGatePassTotal, QualityGateRegressionTotal,
			WebSearchQueriesTotal, WebSearchErrorsTotal,
			AcmeCertExpirySeconds, AcmeRenewalsTotal,
			WorkerQuestionsTotal,
			UIPromptDismissalsTotal,
			SupervisorWatchFindings, SupervisorWatchAgreedDays, SupervisorWatchLastPollAge,
			SupervisorLoopRuns, SupervisorLoopJobsEnabled, SupervisorLoopLastRunAge,
			BranchRefreshAutomata, BranchRefreshStopped,
			HostLoadLastRead, HostLoadLimit, HostLoadBreached, HostLoadOrphansReaped,
			BurnGuardLevel, BurnGuardProjectedPercent, BurnGuardOverByPoints, BurnGuardPointsPerHour,
			BurnGuardTicksTotal, BurnGuardStepsTotal, BurnGuardRefitsTotal, BurnGuardErrorsTotal,
			TraceEventsTotal, TraceAppendErrorsTotal, TraceStoredEvents, TraceStoredSessions,
		)
	})
}

// Handler returns an HTTP handler for the /metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

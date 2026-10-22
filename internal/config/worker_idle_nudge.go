package config

// WorkerIdleNudgeDefaultSeconds is the idle window before a claude-code worker
// gets its single completion nudge, used when
// autonomous.worker_idle_nudge_seconds is unset or not positive.
const WorkerIdleNudgeDefaultSeconds = 180

// WorkerIdleNudgeStallDefaultSeconds is the window, counted from the nudge,
// after which a still-silent worker is declared stalled, used when
// autonomous.worker_idle_nudge_stall_seconds is unset or not positive.
const WorkerIdleNudgeStallDefaultSeconds = 600

// NormalizeWorkerIdleNudgeSeconds returns the effective nudge window in
// seconds: the configured value when positive, otherwise
// WorkerIdleNudgeDefaultSeconds.
func NormalizeWorkerIdleNudgeSeconds(v int) int {
	if v <= 0 {
		return WorkerIdleNudgeDefaultSeconds
	}
	return v
}

// NormalizeWorkerIdleNudgeStallSeconds returns the effective post-nudge stall
// window in seconds: the configured value when positive, otherwise
// WorkerIdleNudgeStallDefaultSeconds.
func NormalizeWorkerIdleNudgeStallSeconds(v int) int {
	if v <= 0 {
		return WorkerIdleNudgeStallDefaultSeconds
	}
	return v
}

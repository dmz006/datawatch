// Counters for the worker question marker: what became of each recognised
// DATAWATCH_QUESTION: line and of the tasks parked on a blocking one. The
// counts are since daemon start. They feed the stats snapshot
// (SystemStats.worker_questions_*), GET /api/questions/stats, the MCP
// question_stats tool and the datawatch_worker_questions_total metric.
package autonomous

import (
	"sync/atomic"

	"github.com/dmz006/datawatch/internal/metrics"
)

// Worker question outcomes, also the metric's outcome label values.
const (
	WorkerQuestionOutcomeRaised    = "raised"
	WorkerQuestionOutcomeDuplicate = "duplicate"
	WorkerQuestionOutcomeRefused   = "refused"
	WorkerQuestionOutcomeParked    = "parked"
	WorkerQuestionOutcomeResumed   = "resumed"
)

// WorkerQuestionCounts is a snapshot of the worker question counters.
type WorkerQuestionCounts struct {
	// Raised is marker lines stored as new questions.
	Raised int64 `json:"raised"`
	// Duplicate is marker lines that repeated a question still open.
	Duplicate int64 `json:"duplicate"`
	// Refused is marker lines that failed validation and stored nothing.
	Refused int64 `json:"refused"`
	// Parked is tasks parked on a blocking question.
	Parked int64 `json:"parked"`
	// Resumed is parked tasks resumed after the question was decided.
	Resumed int64 `json:"resumed"`
}

var workerQuestionCounters struct {
	raised, duplicate, refused, parked, resumed atomic.Int64
}

// recordWorkerQuestion counts one outcome in the snapshot and the metric.
func recordWorkerQuestion(outcome string) {
	switch outcome {
	case WorkerQuestionOutcomeRaised:
		workerQuestionCounters.raised.Add(1)
	case WorkerQuestionOutcomeDuplicate:
		workerQuestionCounters.duplicate.Add(1)
	case WorkerQuestionOutcomeRefused:
		workerQuestionCounters.refused.Add(1)
	case WorkerQuestionOutcomeParked:
		workerQuestionCounters.parked.Add(1)
	case WorkerQuestionOutcomeResumed:
		workerQuestionCounters.resumed.Add(1)
	default:
		return
	}
	metrics.WorkerQuestionsTotal.WithLabelValues(outcome).Inc()
}

// WorkerQuestionCountsSnapshot returns the counters now.
func WorkerQuestionCountsSnapshot() WorkerQuestionCounts {
	return WorkerQuestionCounts{
		Raised:    workerQuestionCounters.raised.Load(),
		Duplicate: workerQuestionCounters.duplicate.Load(),
		Refused:   workerQuestionCounters.refused.Load(),
		Parked:    workerQuestionCounters.parked.Load(),
		Resumed:   workerQuestionCounters.resumed.Load(),
	}
}

package router

import (
	"encoding/json"
	"fmt"
)

// parallelMetricsStatusLine renders the parallel execution line of an
// Automaton read for the chat status text: "parallel: <total> tasks,
// <read only> read only, speedup <n>x". It returns "" when the read records
// no tasks.
func parallelMetricsStatusLine(body string) string {
	var v struct {
		TotalTasks    int     `json:"total_tasks"`
		ReadOnlyTasks int     `json:"read_only_tasks"`
		Speedup       float64 `json:"speedup"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.TotalTasks <= 0 {
		return ""
	}
	return fmt.Sprintf("parallel: %d tasks, %d read only, speedup %.2fx", v.TotalTasks, v.ReadOnlyTasks, v.Speedup)
}

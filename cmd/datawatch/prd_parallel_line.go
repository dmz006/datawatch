package main

import (
	"encoding/json"
	"fmt"
)

// prdParallelLine renders the one-line parallel execution summary for prd-get
// from the daemon's reply: "parallel: <total> tasks, <read only> read only,
// speedup <n>x". It returns "" when the reply records no tasks.
func prdParallelLine(prdJSON []byte) string {
	var v struct {
		TotalTasks    int     `json:"total_tasks"`
		ReadOnlyTasks int     `json:"read_only_tasks"`
		Speedup       float64 `json:"speedup"`
	}
	if err := json.Unmarshal(prdJSON, &v); err != nil || v.TotalTasks <= 0 {
		return ""
	}
	return fmt.Sprintf("parallel: %d tasks, %d read only, speedup %.2fx", v.TotalTasks, v.ReadOnlyTasks, v.Speedup)
}

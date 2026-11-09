// Worker startup stats: the average startup token size per worker profile
// and per role, as the stats snapshot (GET /api/stats, the get_stats MCP
// tool and `datawatch stats`) reports them under the worker_startup block.
//
// The aggregation is the cost report's own (costreport.StartupTable with
// costreport.ProfileKey / costreport.RoleKey), so the stats block and the
// cost report's startup tables can never disagree about an average.
package stats

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/costreport"
)

// WorkerStartupStats is the worker_startup block of the stats snapshot.
// Both maps are always non-nil (JSON `{}` when there is nothing to
// average), so the block's shape never changes with the data.
type WorkerStartupStats struct {
	// AverageStartupTokensByProfile maps each worker profile (or
	// costreport.UnprofiledKey) to the mean startup token size over its
	// attempts that carry one. Keys whose attempts carry no size are
	// omitted, never reported as zero.
	AverageStartupTokensByProfile map[string]float64 `json:"average_startup_tokens_by_profile"`
	// AverageStartupTokensByRole maps each role (or costreport.NoRoleKey)
	// to the mean startup token size over its attempts that carry one.
	AverageStartupTokensByRole map[string]float64 `json:"average_startup_tokens_by_role"`
}

// BuildWorkerStartupStats aggregates the attempt startup records into the
// worker_startup block. A nil or empty attempt list yields the block with
// empty maps.
func BuildWorkerStartupStats(attempts []costreport.Attempt) WorkerStartupStats {
	return WorkerStartupStats{
		AverageStartupTokensByProfile: averagesByKey(costreport.StartupTable(attempts, costreport.ProfileKey)),
		AverageStartupTokensByRole:    averagesByKey(costreport.StartupTable(attempts, costreport.RoleKey)),
	}
}

// averagesByKey keeps the rows that carry an average, keyed by row key.
func averagesByKey(rows []costreport.StartupRow) map[string]float64 {
	out := make(map[string]float64, len(rows))
	for _, row := range rows {
		if row.AvgStartupTokens != nil {
			out[row.Key] = *row.AvgStartupTokens
		}
	}
	return out
}

// WorkerStartupText renders the worker_startup block as the CLI prints it:
// a profile section and a role section, one "key  average" line per entry
// in key order, and "(no attempts with a startup size)" for an empty
// section. It never panics on an empty or zero-value block.
func WorkerStartupText(w WorkerStartupStats) string {
	var b strings.Builder
	b.WriteString("worker startup (average startup tokens):\n")
	writeAverageSection(&b, "by profile", w.AverageStartupTokensByProfile)
	writeAverageSection(&b, "by role", w.AverageStartupTokensByRole)
	return strings.TrimRight(b.String(), "\n")
}

// writeAverageSection writes one labeled section of WorkerStartupText.
func writeAverageSection(b *strings.Builder, label string, m map[string]float64) {
	fmt.Fprintf(b, "  %s:\n", label)
	if len(m) == 0 {
		b.WriteString("    (no attempts with a startup size)\n")
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "    %s\t%s\n", k, strconv.FormatFloat(m[k], 'f', -1, 64))
	}
}

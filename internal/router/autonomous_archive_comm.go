package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const autonomousArchiveUsage = "usage: autonomous archive   (the dry-run report: which finished Automata would be archived and the bytes before and after; nothing is written. Archive for real with the CLI `datawatch autonomous archive --apply` or the autonomous_archive tool.)"

// handleAutonomousArchiveVerb serves "autonomous archive": the dry-run report
// of the finished-Automaton archive via POST /api/autonomous/archive with
// dry_run true. It never archives from chat; the reply is a short summary, not
// the whole list.
func (r *Router) handleAutonomousArchiveVerb(args []string) {
	if len(args) > 1 {
		r.reply("autonomous archive failed", autonomousArchiveUsage)
		return
	}
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/archive", `{"dry_run":true}`)
	if err != nil {
		r.reply("autonomous archive failed", err.Error())
		return
	}
	r.reply("autonomous archive", summarizeArchivePlan(out))
}

// summarizeArchivePlan reduces the dry-run report to a few lines.
func summarizeArchivePlan(body string) string {
	var plan struct {
		RetentionDays  int   `json:"retention_days"`
		BytesBefore    int64 `json:"bytes_before"`
		BytesAfter     int64 `json:"bytes_after"`
		ArchiveBytes   int64 `json:"archive_bytes"`
		AlreadyArchive int   `json:"already_archived"`
		Eligible       []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"eligible"`
		Skipped []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(body), &plan); err != nil {
		return prettyJSON(body)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Dry run (nothing written): %d finished Automata older than %d days would be archived.\n", len(plan.Eligible), plan.RetentionDays)
	fmt.Fprintf(&b, "Live records: %d bytes now, %d bytes after; the archive files would hold %d bytes.\n", plan.BytesBefore, plan.BytesAfter, plan.ArchiveBytes)
	fmt.Fprintf(&b, "Already archived: %d. Skipped because in use: %d.", plan.AlreadyArchive, len(plan.Skipped))
	for i, sk := range plan.Skipped {
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, "\n  skipped %s: %s", sk.ID, sk.Reason)
	}
	return b.String()
}

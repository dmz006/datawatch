// plans_migrate_comm.go — the plan-migrate verb over the chat channel:
//
//	autonomous plan-migrate [apply]
//
// Without "apply" it previews what would be created and linked and writes
// nothing; with it, the plans and links are written. It calls the loopback
// POST /api/plans/migrate, so the chat channel runs the same code as REST.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const planMigrateUsage = "usage: autonomous plan-migrate [apply]"

type planMigrateWire struct {
	Note   string `json:"note"`
	Result struct {
		Applied          bool              `json:"applied"`
		WouldCreate      []planMigrateItem `json:"would_create"`
		WouldLink        []planMigrateItem `json:"would_link"`
		SkippedExisting  []planMigrateItem `json:"skipped_existing"`
		Conflicts        []planMigrateItem `json:"conflicts"`
		TitlesDerived    int               `json:"titles_derived"`
		TitlesFromLinked int               `json:"titles_from_linked"`
		TitlesFellBack   int               `json:"titles_fell_back"`
	} `json:"result"`
}

type planMigrateItem struct {
	Title    string `json:"title"`
	LinkKind string `json:"link_kind"`
	LinkID   string `json:"link_id"`
	Reason   string `json:"reason"`
}

// handlePlanMigrateVerb answers "autonomous plan-migrate [apply]".
func (r *Router) handlePlanMigrateVerb(args []string) {
	apply := false
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "apply":
			apply = true
		case "":
		default:
			r.reply("autonomous plan-migrate failed", planMigrateUsage)
			return
		}
	}
	raw, _ := json.Marshal(map[string]any{"apply": apply})
	out, err := r.commJSON(http.MethodPost, "/api/plans/migrate", string(raw))
	if err != nil {
		r.reply("autonomous plan-migrate failed", err.Error())
		return
	}
	r.reply("autonomous plan-migrate", planMigrateText(out))
}

// planMigrateText summarises a migration result: the counts, then each
// conflict (the rest are in the REST answer).
func planMigrateText(body string) string {
	var res planMigrateWire
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return "the daemon sent a migration result I cannot read"
	}
	rs := res.Result
	head, create, link := "Preview", "would create", "would link"
	if rs.Applied {
		head, create, link = "Migration", "created", "linked"
	}
	lines := []string{fmt.Sprintf("%s: %s %d, %s %d, already there %d, conflicts %d, titles derived %d, from linked Automaton %d, fell back %d", head,
		create, len(rs.WouldCreate), link, len(rs.WouldLink), len(rs.SkippedExisting), len(rs.Conflicts),
		rs.TitlesDerived, rs.TitlesFromLinked, rs.TitlesFellBack)}
	for i, c := range rs.Conflicts {
		if i >= planRecordsMaxLines {
			lines = append(lines, fmt.Sprintf("… and %d more conflicts", len(rs.Conflicts)-i))
			break
		}
		lines = append(lines, "conflict: "+strings.Join(strings.Fields(c.Title), " ")+" — "+c.Reason)
	}
	if !rs.Applied {
		lines = append(lines, "To write these plans send: autonomous plan-migrate apply")
	}
	return strings.Join(lines, "\n")
}

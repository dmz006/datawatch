// autonomous repo CI watch over the chat channel: the repo CI watch
// read, so the operator can ask which watched CI runs are waiting for
// attention from Signal / Telegram without curl. The reply renders the
// same read view the GET route serves (internal/server/repo_ci_watch.go):
// the first line states the mode in effect (off, shadow or on); every
// further line is one record, the lines naming repository, ref,
// workflow, status, classification and run URL in that order. When the
// store is empty the reply says there are no repo CI watch records
// instead. A read — nothing is written and no rerun or classification
// is started by the verb.

package router

import (
	"encoding/json"
	"strings"
)

const (
	// repoCIWatchUsage is the usage line for the repo CI watch comm
	// verb. The read takes no arguments.
	repoCIWatchUsage = "usage: autonomous repo-ci-watch"
)

// RepoCIWatchCommVerbs lists the repo CI watch comm verbs for the
// parity test.
func RepoCIWatchCommVerbs() []string {
	return []string{"repo-ci-watch", "repo_ci_watch"}
}

// handleRepoCIWatchVerb answers the autonomous command's repo CI
// watch verbs.
func (r *Router) handleRepoCIWatchVerb(verb string, args []string) {
	switch verb {
	case "repo-ci-watch", "repo_ci_watch":
		r.replyRepoCIWatch(args)
	default:
		r.reply("autonomous repo-ci-watch failed", repoCIWatchUsage)
	}
}

// repoCIWatchRecordIn is the comm reply's slice of one repo CI watch
// record: the identity fields and the resolution the reply prints.
type repoCIWatchRecordIn struct {
	Repo           string `json:"repo"`
	RefKind        string `json:"ref_kind"`
	RefName        string `json:"ref_name"`
	Workflow       string `json:"workflow"`
	Status         string `json:"status"`
	Classification string `json:"classification"`
	RunURL         string `json:"run_url"`
}

// repoCIWatchStatusBody is the comm reply's slice of the repo CI
// watch read view: the mode in effect and the records, newest first.
type repoCIWatchStatusBody struct {
	Mode    string                `json:"mode"`
	Records []repoCIWatchRecordIn `json:"records"`
}

// renderRepoCIWatchText renders the comm reply body for the read view:
// the first line states the mode; each further line carries one
// record — repository, ref, workflow, status, classification and run
// URL, in that order; and a record-less store reads as none.
func renderRepoCIWatchText(body *repoCIWatchStatusBody) string {
	mode := strings.TrimSpace(body.Mode)
	if mode == "" {
		mode = "off"
	}
	lines := []string{"mode: " + mode}
	if len(body.Records) == 0 {
		lines = append(lines, "no repo CI watch records")
		return strings.Join(lines, "\n")
	}
	for _, rec := range body.Records {
		ref := strings.TrimSpace(rec.RefKind)
		name := strings.TrimSpace(rec.RefName)
		if name != "" && name != ref {
			ref = ref + " " + name
		}
		classification := strings.TrimSpace(rec.Classification)
		if classification == "" {
			classification = "none"
		}
		lines = append(lines, strings.Join([]string{
			strings.TrimSpace(rec.Repo),
			ref,
			strings.TrimSpace(rec.Workflow),
			strings.TrimSpace(rec.Status),
			classification,
			strings.TrimSpace(rec.RunURL),
		}, " | "))
	}
	return strings.Join(lines, "\n")
}

func (r *Router) replyRepoCIWatch(args []string) {
	if len(args) > 1 {
		r.reply("autonomous repo-ci-watch failed", repoCIWatchUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/repo-ci-watch", nil)
	if err != nil {
		r.reply("autonomous repo-ci-watch failed", err.Error())
		return
	}
	var body repoCIWatchStatusBody
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		r.reply("autonomous repo-ci-watch failed", "unparseable read view: "+err.Error())
		return
	}
	r.reply("autonomous repo-ci-watch", renderRepoCIWatchText(&body))
}

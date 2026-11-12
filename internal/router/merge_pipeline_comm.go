// autonomous merge-pipeline over the chat channel: the merge pipeline of a
// project, the record the daemon keeps for it, and a check of a pipeline. The
// replies render the same shared text the CLI and the MCP tools do
// (internal/mergepipeline view.go). Nothing here merges, deletes or pushes.
//
//	autonomous merge-pipeline [show] [project]
//	autonomous merge-pipeline set-record [project=NAME] <pipeline>
//	autonomous merge-pipeline clear-record [project]
//	autonomous merge-pipeline validate [project=NAME] <pipeline>
//	autonomous merge-pipeline validate [project=NAME] file[=NAME]
//	autonomous merge-pipeline release-readiness <patch|minor|major> [project]
//
// A pipeline is written inline as YAML or JSON (JSON is YAML), for example
// {"steps":[{"name":"tests","kind":"test","command":"make test"}]}. The
// settings of the block are changed with configure, for example
// configure autonomous.merge_pipeline.conflict_policy=stop_and_ask.
// The conflict settings are configure keys too: conflict_mode (shadow or on)
// and conflict_confirm_first_n (whole number, 0 turns confirmation off), for
// example configure autonomous.merge_pipeline.conflict_mode=on.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

const (
	mergePipelineCommPath = "/api/autonomous/merge-pipeline"
	// mergePipelineReadinessPath is the REST release readiness read.
	mergePipelineReadinessPath = "/api/autonomous/release-readiness"
	// mergePipelineCommMaxChars bounds one chat reply.
	mergePipelineCommMaxChars = 3000
	// mergePipelineCommMaxInput bounds the pipeline text a chat message carries.
	mergePipelineCommMaxInput = 16 * 1024

	mergePipelineUsage = "usage: autonomous merge-pipeline [show] [project] | set-record [project=NAME] <pipeline as YAML or JSON> | clear-record [project] | validate [project=NAME] <pipeline> | validate [project=NAME] file[=NAME] | release-readiness <patch|minor|major> [project]"
)

// MergePipelineCommVerbs lists the merge pipeline comm verbs, for the parity
// test.
func MergePipelineCommVerbs() []string { return []string{"merge-pipeline"} }

// MergePipelineConfigureKeys lists the configure keys of the block, for the
// parity test. Each also takes the form
// autonomous.merge_pipeline.projects.<project>.<key> for one project.
func MergePipelineConfigureKeys() []string {
	return []string{
		"autonomous.merge_pipeline.file_name",
		"autonomous.merge_pipeline.source_order",
		"autonomous.merge_pipeline.fallback_on_bad_file",
		"autonomous.merge_pipeline.conflict_policy",
		"autonomous.merge_pipeline.conflict_model",
		"autonomous.merge_pipeline.conflict_mode",
		"autonomous.merge_pipeline.conflict_confirm_first_n",
		"autonomous.merge_pipeline.version_files",
		"autonomous.merge_pipeline.release_size_map",
		"autonomous.merge_pipeline.major_trigger",
		"autonomous.merge_pipeline.release_cadence",
		"autonomous.merge_pipeline.version_scheme",
		"autonomous.merge_pipeline.release_approval.patch",
		"autonomous.merge_pipeline.release_approval.minor",
		"autonomous.merge_pipeline.release_approval.major",
		"autonomous.merge_pipeline.post_ship.enabled",
		"autonomous.merge_pipeline.post_ship.registry_file",
		"autonomous.merge_pipeline.post_ship.registry_row_pattern",
		"autonomous.merge_pipeline.post_ship.companion_repository",
		"autonomous.merge_pipeline.post_ship.issue_title_template",
		"autonomous.merge_pipeline.post_ship.notify_template",
		"autonomous.merge_pipeline.post_ship.hooks",
	}
}

// isMergePipelineReleaseApprovalKey reports whether key is a release approval
// key of the block, global or for one project
// (autonomous.merge_pipeline.projects.<project>.release_approval.<size>).
func isMergePipelineReleaseApprovalKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "autonomous.merge_pipeline.")
	if !ok {
		return false
	}
	if p, ok := strings.CutPrefix(rest, "projects."); ok {
		i := strings.Index(p, ".release_approval.")
		if i <= 0 {
			return false
		}
		rest = p[i+1:]
	}
	size, ok := strings.CutPrefix(rest, "release_approval.")
	return ok && (size == "patch" || size == "minor" || size == "major")
}

// mergePipelineConflictSetting reports which conflict setting key names,
// "conflict_mode" or "conflict_confirm_first_n", globally or for one project
// (autonomous.merge_pipeline.projects.<project>.<setting>).
func mergePipelineConflictSetting(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, "autonomous.merge_pipeline.")
	if !ok {
		return "", false
	}
	if p, ok := strings.CutPrefix(rest, "projects."); ok {
		i := strings.Index(p, ".")
		if i <= 0 {
			return "", false
		}
		rest = p[i+1:]
	}
	switch rest {
	case "conflict_mode", "conflict_confirm_first_n":
		return rest, true
	}
	return "", false
}

// checkMergePipelineConfigure refuses a value before the configure verb sends
// it to the daemon. A release approval value is checked with
// config.ValidateReleaseApprovalModeKey, and a conflict_mode or
// conflict_confirm_first_n value with config.ValidateConflictModeKey and
// config.ValidateConfirmFirstNKey: the same checks the daemon's config write
// runs, so the refusal names the same key and the same valid values. Any other
// key passes unchanged.
func checkMergePipelineConfigure(key, value string) error {
	if setting, ok := mergePipelineConflictSetting(key); ok {
		v := strings.TrimSpace(value)
		if setting == "conflict_mode" {
			return config.ValidateConflictModeKey(key, v)
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %q is invalid; use a whole number", key, v)
		}
		return config.ValidateConfirmFirstNKey(key, n)
	}
	if !isMergePipelineReleaseApprovalKey(key) {
		return nil
	}
	return config.ValidateReleaseApprovalModeKey(key, strings.TrimSpace(value))
}

// configureMergePipelineReleaseMode sends one release approval mode through
// PUT /api/config tagged with the comm surface, so the daemon records the change
// as a comm change. The daemon stores it through SetReleaseApprovalMode.
func (r *Router) configureMergePipelineReleaseMode(key, value string) error {
	b, err := json.Marshal(map[string]any{key: strings.TrimSpace(value)})
	if err != nil {
		return err
	}
	_, err = r.commJSON(http.MethodPut, "/api/config?surface=comm", string(b))
	return err
}

// replyMergePipeline answers the merge-pipeline verb. args[0] is the verb.
func (r *Router) replyMergePipeline(args []string) {
	const label = "autonomous merge-pipeline"
	words := args[1:]
	sub := "show"
	if len(words) > 0 {
		switch s := strings.ToLower(words[0]); s {
		case "show", "set-record", "set_record", "clear-record", "clear_record", "validate", "release-readiness", "release_readiness":
			sub, words = strings.ReplaceAll(s, "_", "-"), words[1:]
		}
	}
	fail := func(msg string) { r.reply(label+" failed", msg) }

	switch sub {
	case "release-readiness":
		r.replyReleaseReadiness(words)

	case "show", "clear-record":
		if len(words) > 1 {
			fail(mergePipelineUsage)
			return
		}
		q := url.Values{}
		if len(words) == 1 {
			q.Set("project", words[0])
		}
		if sub == "show" {
			out, err := r.commGet(mergePipelineCommPath, q)
			if err != nil {
				fail(err.Error())
				return
			}
			v, err := mergepipeline.ParseView([]byte(out))
			if err != nil {
				fail(err.Error())
				return
			}
			r.reply(label, cutMergePipelineChat(mergepipeline.RenderViewText(v)))
			return
		}
		path := mergePipelineCommPath + "/record"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		out, err := r.commJSON(http.MethodDelete, path, "")
		if err != nil {
			fail(err.Error())
			return
		}
		var done struct {
			Project string `json:"project"`
		}
		if err := json.Unmarshal([]byte(out), &done); err != nil {
			fail("the daemon's reply could not be read: " + err.Error())
			return
		}
		r.reply(label, strings.TrimRight(mergepipeline.RenderRecordClearedText(done.Project), "\n"))

	case "set-record", "validate":
		project := ""
		if len(words) > 0 && strings.HasPrefix(strings.ToLower(words[0]), "project=") {
			project = strings.TrimSpace(words[0][len("project="):])
			if project == "" {
				fail("project= is empty; " + mergePipelineUsage)
				return
			}
			words = words[1:]
		}
		body := map[string]any{}
		if project != "" {
			body["project"] = project
		}
		if sub == "validate" && len(words) == 1 && (strings.EqualFold(words[0], "file") || strings.HasPrefix(strings.ToLower(words[0]), "file=")) {
			name := "true"
			if i := strings.Index(words[0], "="); i >= 0 {
				name = strings.TrimSpace(words[0][i+1:])
				if name == "" {
					fail("file= is empty; use file for the configured file or file=NAME for a file inside the project")
					return
				}
			}
			if name == "true" {
				body["file"] = true
			} else {
				body["file"] = name
			}
		} else {
			text := strings.TrimSpace(strings.Join(words, " "))
			if text == "" {
				fail(mergePipelineUsage)
				return
			}
			if len(text) > mergePipelineCommMaxInput {
				fail("the pipeline is too long for a chat message; use the command line or the web page")
				return
			}
			body["yaml"] = text
		}
		payload, _ := json.Marshal(body)
		if sub == "set-record" {
			out, err := r.commJSON(http.MethodPut, mergePipelineCommPath+"/record", string(payload))
			if err != nil {
				fail(err.Error())
				return
			}
			rec, err := mergepipeline.ParseRecordReport([]byte(out))
			if err != nil {
				fail(err.Error())
				return
			}
			r.reply(label, cutMergePipelineChat(mergepipeline.RenderRecordText(rec)))
			return
		}
		out, err := r.commJSON(http.MethodPost, mergePipelineCommPath+"/validate", string(payload))
		if err != nil {
			fail(err.Error())
			return
		}
		rep, err := mergepipeline.ParseValidation([]byte(out))
		if err != nil {
			fail(err.Error())
			return
		}
		r.reply(label, cutMergePipelineChat(mergepipeline.RenderValidationText(rep)))
	}
}

// cutMergePipelineChat trims the trailing newline and cuts text to fit one
// chat message, saying that it was cut.
func cutMergePipelineChat(text string) string {
	text = strings.TrimRight(text, "\n")
	if len(text) <= mergePipelineCommMaxChars {
		return text
	}
	const note = "\n… (cut to fit one message; the command line shows all of it)"
	cut := text[:mergePipelineCommMaxChars-len(note)]
	// Do not end inside a multi-byte character.
	for len(cut) > 0 && cut[len(cut)-1]&0xC0 == 0x80 {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndex(cut, "\n"); i > 0 {
		cut = cut[:i]
	}
	return cut + note
}

// mergePipelineReadinessUsage is the usage line of the release readiness read.
const mergePipelineReadinessUsage = "usage: autonomous merge-pipeline release-readiness <patch|minor|major> [project]"

// replyReleaseReadiness answers release-readiness <size> [project]. The size is
// required and must be patch, minor or major (size= is accepted too); the
// project is optional, bare or project=NAME. The read goes through
// GET /api/autonomous/release-readiness, the same REST read the CLI and the
// MCP tool use, so the daemon computes the counts with ComputeReleaseReadiness.
func (r *Router) replyReleaseReadiness(words []string) {
	const label = "autonomous merge-pipeline release-readiness"
	fail := func(msg string) { r.reply(label+" failed", msg) }

	if len(words) == 0 || len(words) > 2 {
		fail(mergePipelineReadinessUsage)
		return
	}
	size := strings.ToLower(strings.TrimSpace(words[0]))
	size = strings.TrimPrefix(size, "size=")
	if !mergepipeline.ValidReadinessSize(size) {
		fail(fmt.Sprintf("size must be patch, minor or major; got %q. %s", words[0], mergePipelineReadinessUsage))
		return
	}
	q := url.Values{"size": {size}}
	if len(words) == 2 {
		project := strings.TrimSpace(words[1])
		if i := strings.Index(strings.ToLower(project), "project="); i == 0 {
			project = strings.TrimSpace(project[len("project="):])
		}
		if project == "" {
			fail("project is empty; " + mergePipelineReadinessUsage)
			return
		}
		q.Set("project", project)
	}
	out, err := r.commGet(mergePipelineReadinessPath, q)
	if err != nil {
		fail(err.Error())
		return
	}
	rep, err := mergepipeline.ParseReadiness([]byte(out))
	if err != nil {
		fail(err.Error())
		return
	}
	r.reply(label, cutMergePipelineChat(mergepipeline.RenderReadinessText(rep)))
}

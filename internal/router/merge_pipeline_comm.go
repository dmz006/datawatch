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
//
// A pipeline is written inline as YAML or JSON (JSON is YAML), for example
// {"steps":[{"name":"tests","kind":"test","command":"make test"}]}. The
// settings of the block are changed with configure, for example
// configure autonomous.merge_pipeline.conflict_policy=stop_and_ask.

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/mergepipeline"
)

const (
	mergePipelineCommPath = "/api/autonomous/merge-pipeline"
	// mergePipelineCommMaxChars bounds one chat reply.
	mergePipelineCommMaxChars = 3000
	// mergePipelineCommMaxInput bounds the pipeline text a chat message carries.
	mergePipelineCommMaxInput = 16 * 1024

	mergePipelineUsage = "usage: autonomous merge-pipeline [show] [project] | set-record [project=NAME] <pipeline as YAML or JSON> | clear-record [project] | validate [project=NAME] <pipeline> | validate [project=NAME] file[=NAME]"
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
		"autonomous.merge_pipeline.version_files",
		"autonomous.merge_pipeline.post_ship.enabled",
		"autonomous.merge_pipeline.post_ship.registry_file",
		"autonomous.merge_pipeline.post_ship.registry_row_pattern",
		"autonomous.merge_pipeline.post_ship.companion_repository",
		"autonomous.merge_pipeline.post_ship.issue_title_template",
		"autonomous.merge_pipeline.post_ship.notify_template",
		"autonomous.merge_pipeline.post_ship.hooks",
	}
}

// replyMergePipeline answers the merge-pipeline verb. args[0] is the verb.
func (r *Router) replyMergePipeline(args []string) {
	const label = "autonomous merge-pipeline"
	words := args[1:]
	sub := "show"
	if len(words) > 0 {
		switch s := strings.ToLower(words[0]); s {
		case "show", "set-record", "set_record", "clear-record", "clear_record", "validate":
			sub, words = strings.ReplaceAll(s, "_", "-"), words[1:]
		}
	}
	fail := func(msg string) { r.reply(label+" failed", msg) }

	switch sub {
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

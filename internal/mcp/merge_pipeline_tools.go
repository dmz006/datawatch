package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// The merge pipeline tools proxy the /api/autonomous/merge-pipeline routes:
//
//	autonomous_merge_pipeline_get          GET    .../merge-pipeline
//	autonomous_merge_pipeline_set_record   PUT    .../merge-pipeline/record (clear=true: DELETE)
//	autonomous_merge_pipeline_validate     POST   .../merge-pipeline/validate
//
// By default each answers in the same plain-language text the command line and
// the chat verbs print (internal/mergepipeline view.go); format "json" returns
// the daemon's body untouched. The pipeline settings are changed through
// autonomous_config_set (the merge_pipeline_* arguments).

const (
	mergePipelinePath         = "/api/autonomous/merge-pipeline"
	mergePipelineRecordPath   = mergePipelinePath + "/record"
	mergePipelineValidatePath = mergePipelinePath + "/validate"
)

// MergePipelineToolNames lists the merge pipeline tools, for the parity test.
func MergePipelineToolNames() []string {
	return []string{"autonomous_merge_pipeline_get", "autonomous_merge_pipeline_set_record", "autonomous_merge_pipeline_validate"}
}

// releaseReadinessPath is the REST read the autonomous_release_readiness tool
// proxies. The daemon computes the readiness there with the same
// ComputeReleaseReadiness path, so the tool returns that body unchanged.
const releaseReadinessPath = "/api/autonomous/release-readiness"

// releaseReadinessSizes are the release sizes the tool accepts; the daemon
// refuses any other size with the same rule.
var releaseReadinessSizes = map[string]bool{"patch": true, "minor": true, "major": true}

// toolAutonomousReleaseReadiness registers the read-only readiness tool for
// one release size: the same object as GET /api/autonomous/release-readiness.
func (s *Server) toolAutonomousReleaseReadiness() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_release_readiness",
		mcpsdk.WithDescription("Read the readiness of one release size (patch, minor or major), judged from the stored shadow-merge comparisons: how many plans held up against the landed state, how many would have conflicted, how many touched the PWA, and whether a blocking defect was found, with the bar each count must reach. Read-only; the same object as GET /api/autonomous/release-readiness. project optionally keeps only one project's comparisons."),
		mcpsdk.WithString("size", mcpsdk.Required(), mcpsdk.Enum("patch", "minor", "major"), mcpsdk.Description("The release size: patch, minor or major")),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — the project name; default is every project")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousReleaseReadiness(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_release_readiness"
	args := req.GetArguments()
	size, _ := args["size"].(string)
	size = strings.ToLower(strings.TrimSpace(size))
	if !releaseReadinessSizes[size] {
		return toolErrorf(op, "size is required and must be patch, minor or major; got %v", args["size"]), nil
	}
	q := url.Values{}
	q.Set("size", size)
	if v, ok := args["project"].(string); ok && strings.TrimSpace(v) != "" {
		q.Set("project", strings.TrimSpace(v))
	}
	out, err := s.proxyGet(releaseReadinessPath, q)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousMergePipelineGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_merge_pipeline_get",
		mcpsdk.WithDescription("Read the merge pipeline that would run for a project and why: the steps, where they came from (the repository file, the pipeline record the daemon keeps, or the built-in pipeline), the merge conflict policy and model, the release settings in effect (the release size by kind, the major release trigger, the release cadence and the version scheme), the release approval mode in effect for each release size (patch, minor, major), the post-ship hook block (the switch, the hooks it runs, and the targets the built-ins use), and the project's overrides. Read-only; the same object as GET /api/autonomous/merge-pipeline. project names the project (default: the merge base repository). format is text (the default, plain language) or json (the daemon's body)."),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — the project name; default is the merge base repository")),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolAutonomousMergePipelineSetRecord() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_merge_pipeline_set_record",
		mcpsdk.WithDescription("Set, or with clear=true remove, the merge pipeline record the daemon keeps for a project. The record is used when the project's repository has no pipeline file. Give the pipeline as yaml (text with a steps list) or as definition (an object with a steps list), not both; a pipeline that does not validate is refused with every problem named. Same as PUT / DELETE /api/autonomous/merge-pipeline/record. Nothing is merged."),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — the project name; default is the merge base repository")),
		mcpsdk.WithString("yaml", mcpsdk.Description("The pipeline as YAML text with a steps list")),
		mcpsdk.WithObject("definition", mcpsdk.Description("The pipeline as an object: {\"steps\": [{\"name\", \"kind\", \"command\" or \"action\", ...}]}")),
		mcpsdk.WithBoolean("clear", mcpsdk.Description("Optional — true removes the project's record instead of setting one")),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
	)
}

func (s *Server) toolAutonomousMergePipelineValidate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_merge_pipeline_validate",
		mcpsdk.WithDescription("Check a merge pipeline without saving it. Give yaml or definition to check a pipeline you wrote, or file (true for the configured file name, or a file name inside the project) to check the project's repository file. A pipeline that does not validate is a normal answer (valid false, every problem listed), not an error. Same as POST /api/autonomous/merge-pipeline/validate."),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — the project name; default is the merge base repository")),
		mcpsdk.WithString("yaml", mcpsdk.Description("The pipeline as YAML text with a steps list")),
		mcpsdk.WithObject("definition", mcpsdk.Description("The pipeline as an object with a steps list")),
		mcpsdk.WithString("file", mcpsdk.Description("Optional — \"true\" to check the project's configured pipeline file, or a file name inside the project")),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// mergePipelineProjectRequiredLine is the plain line both surfaces add when
// the daemon refuses a merge pipeline read because no project is known (no
// project argument and no merge base repository set).
const mergePipelineProjectRequiredLine = "a project is needed, pass the project argument or set the merge base repository"

const http400Prefix = "HTTP 400: "

// mergePipelineProjectRequired reports whether a non-2xx error from a merge
// pipeline read is the daemon's structured project_required refusal: an HTTP
// 400 whose body parses as JSON with code project_required. The second
// result is the body the error text carries. A body that is not JSON, every
// other status and every other code never match and keep today's error text.
func mergePipelineProjectRequired(err error) (body string, ok bool) {
	if err == nil {
		return "", false
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, http400Prefix) {
		return "", false
	}
	body = strings.TrimSpace(msg[len(http400Prefix):])
	var code struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(body), &code) != nil || code.Code != "project_required" {
		return "", false
	}
	return body, true
}

// mergePipelineProjectRequiredError builds the error both surfaces return for
// the project_required refusal: the daemon's JSON body followed by one plain
// line.
func mergePipelineProjectRequiredError(body string) error {
	return fmt.Errorf("%s\n%s", body, mergePipelineProjectRequiredLine)
}

// mergePipelineWantsJSON reads the format argument. Anything but text or json
// is refused so a typo is not silently read as the default.
func mergePipelineWantsJSON(args map[string]any) (bool, error) {
	v, ok := args["format"]
	if !ok || v == nil {
		return false, nil
	}
	f, isStr := v.(string)
	if !isStr {
		return false, fmt.Errorf("format must be text or json")
	}
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "", "text":
		return false, nil
	case "json":
		return true, nil
	}
	return false, fmt.Errorf("format %q is invalid; use text or json", f)
}

// mergePipelineBody builds the request body shared by set_record and
// validate from the tool arguments. It refuses a pipeline given twice and a
// definition that is not an object.
func mergePipelineBody(args map[string]any) (map[string]any, error) {
	body := map[string]any{}
	if v, ok := args["project"].(string); ok && strings.TrimSpace(v) != "" {
		body["project"] = strings.TrimSpace(v)
	}
	y, hasYAML := args["yaml"].(string)
	hasYAML = hasYAML && strings.TrimSpace(y) != ""
	def, hasDef := args["definition"]
	if hasDef && def == nil {
		hasDef = false
	}
	if hasYAML && hasDef {
		return nil, fmt.Errorf("give the pipeline as yaml or as definition, not both")
	}
	if hasYAML {
		body["yaml"] = y
	}
	if hasDef {
		obj, ok := def.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("definition must be an object with a steps list")
		}
		body["definition"] = obj
	}
	return body, nil
}

func (s *Server) handleAutonomousMergePipelineGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_merge_pipeline_get"
	args := req.GetArguments()
	asJSON, err := mergePipelineWantsJSON(args)
	if err != nil {
		return toolError(op, err), nil
	}
	q := url.Values{}
	if v, ok := args["project"].(string); ok && strings.TrimSpace(v) != "" {
		q.Set("project", strings.TrimSpace(v))
	}
	out, err := s.proxyGet(mergePipelinePath, q)
	if err != nil {
		if body, ok := mergePipelineProjectRequired(err); ok {
			return toolError(op, mergePipelineProjectRequiredError(body)), nil
		}
		return toolError(op, err), nil
	}
	// The post-ship detail is the read's one extra: the main pipeline answer is
	// the contract, so a settings lookup that fails leaves the read as it was.
	detail, dErr := s.postShipDetail()
	if dErr != nil {
		detail = nil
	}
	if asJSON {
		if detail == nil {
			return textOK(string(out)), nil
		}
		var obj map[string]any
		if err := json.Unmarshal(out, &obj); err != nil {
			return toolError(op, err), nil
		}
		var d map[string]any
		if err := json.Unmarshal(detail, &d); err != nil {
			return toolError(op, err), nil
		}
		obj["post_ship"] = d
		merged, err := json.Marshal(obj)
		if err != nil {
			return toolError(op, err), nil
		}
		return textOK(string(merged)), nil
	}
	v, err := mergepipeline.ParseView(out)
	if err != nil {
		return toolError(op, err), nil
	}
	if detail != nil {
		d, err := mergepipeline.ParsePostShipDetail(detail)
		if err != nil {
			return toolError(op, err), nil
		}
		return textOK(mergepipeline.RenderViewWithPostShip(v, d)), nil
	}
	return textOK(mergepipeline.RenderViewText(v)), nil
}

// postShipDetail reads the post-ship hook block from the daemon's settings
// (autonomous.merge_pipeline.post_ship) and returns its raw JSON, or nil when
// the daemon reports no block. The daemon answers both routes off the same
// process, so a read reaching here always answers the view route too.
func (s *Server) postShipDetail() ([]byte, error) {
	out, err := s.proxyGet("/api/config", nil)
	if err != nil {
		return nil, err
	}
	return mergepipeline.PostShipDetailFromBody(out)
}

func (s *Server) handleAutonomousMergePipelineSetRecord(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_merge_pipeline_set_record"
	args := req.GetArguments()
	asJSON, err := mergePipelineWantsJSON(args)
	if err != nil {
		return toolError(op, err), nil
	}
	body, err := mergePipelineBody(args)
	if err != nil {
		return toolError(op, err), nil
	}
	project, _ := body["project"].(string)
	doClear, _ := args["clear"].(bool)
	if doClear {
		if _, has := body["yaml"]; has {
			return toolError(op, fmt.Errorf("clear removes the record; do not give a pipeline with it")), nil
		}
		if _, has := body["definition"]; has {
			return toolError(op, fmt.Errorf("clear removes the record; do not give a pipeline with it")), nil
		}
		path := mergePipelineRecordPath
		if project != "" {
			path += "?" + url.Values{"project": {project}}.Encode()
		}
		out, err := s.proxyJSON(http.MethodDelete, path, nil)
		if err != nil {
			return toolError(op, err), nil
		}
		if asJSON {
			return textOK(string(out)), nil
		}
		var done struct {
			Project string `json:"project"`
		}
		_ = json.Unmarshal(out, &done)
		return textOK(mergepipeline.RenderRecordClearedText(done.Project)), nil
	}
	if _, has := body["yaml"]; !has {
		if _, has := body["definition"]; !has {
			return toolError(op, fmt.Errorf("give the pipeline as yaml or as definition, or clear=true to remove the record")), nil
		}
	}
	out, err := s.proxyJSON(http.MethodPut, mergePipelineRecordPath, body)
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	rec, err := mergepipeline.ParseRecordReport(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(mergepipeline.RenderRecordText(rec)), nil
}

func (s *Server) handleAutonomousMergePipelineValidate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_merge_pipeline_validate"
	args := req.GetArguments()
	asJSON, err := mergePipelineWantsJSON(args)
	if err != nil {
		return toolError(op, err), nil
	}
	body, err := mergePipelineBody(args)
	if err != nil {
		return toolError(op, err), nil
	}
	file, err := mergePipelineFileArg(args["file"])
	if err != nil {
		return toolError(op, err), nil
	}
	if file != nil {
		body["file"] = file
	}
	out, err := s.proxyJSON(http.MethodPost, mergePipelineValidatePath, body)
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	rep, err := mergepipeline.ParseValidation(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(mergepipeline.RenderValidationText(rep)), nil
}

// mergePipelineConfigFields are the settings of the autonomous.merge_pipeline
// block; each is an autonomous_config_set argument named merge_pipeline_<name>.
// The release approval fields are dotted in the daemon (release_approval.<size>),
// so mergePipelineConfigKey maps their underscore argument names to that form.
var mergePipelineConfigFields = []string{
	"file_name", "source_order", "fallback_on_bad_file", "conflict_policy", "conflict_model", "version_files",
	"release_size_map", "major_trigger", "release_cadence", "version_scheme",
	"release_approval_patch", "release_approval_minor", "release_approval_major",
}

// mergePipelineConfigKey is the daemon key for one field name: a
// release_approval_<size> field is release_approval.<size>, every other field
// keeps its name.
func mergePipelineConfigKey(field string) string {
	if rest := strings.TrimPrefix(field, "release_approval_"); rest != field {
		return "release_approval." + rest
	}
	return field
}

// mergePipelineConfigOptions are the autonomous_config_set arguments for the
// autonomous.merge_pipeline block. They are sent as dotted keys on PUT
// /api/config, where the daemon checks each value and names the valid ones in
// a refusal. merge_pipeline_project, when given, applies the others to that
// project alone (autonomous.merge_pipeline.projects.<project>.<name>).
func mergePipelineConfigOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString("merge_pipeline_project", mcpsdk.Description("Optional — a project name; the merge_pipeline_* arguments then apply to that project only")),
		mcpsdk.WithString("merge_pipeline_file_name", mcpsdk.Description("autonomous.merge_pipeline.file_name — the pipeline file in a project's repository, relative to the project (default .datawatch/merge-pipeline.yaml)")),
		mcpsdk.WithArray("merge_pipeline_source_order", mcpsdk.Description("autonomous.merge_pipeline.source_order — where the pipeline comes from, in the order tried: file, record, default. The value replaces the whole list."), mcpsdk.WithStringItems()),
		mcpsdk.WithString("merge_pipeline_fallback_on_bad_file", mcpsdk.Description("autonomous.merge_pipeline.fallback_on_bad_file — what happens when the repository file is unusable; the daemon names the valid values in its refusal")),
		mcpsdk.WithString("merge_pipeline_conflict_policy", mcpsdk.Description("autonomous.merge_pipeline.conflict_policy — what a merge does on a conflict; the daemon names the valid policies in its refusal")),
		mcpsdk.WithString("merge_pipeline_conflict_model", mcpsdk.Description("autonomous.merge_pipeline.conflict_model — the model that proposes conflict resolutions, a name from the LLM registry; empty means none")),
		mcpsdk.WithArray("merge_pipeline_version_files", mcpsdk.Description("autonomous.merge_pipeline.version_files — files that hold the version, relative to the project. The value replaces the whole list."), mcpsdk.WithStringItems()),
		mcpsdk.WithString("merge_pipeline_release_size_map", mcpsdk.Description("autonomous.merge_pipeline.release_size_map — the release size each Automaton kind adds, as kind=size pairs such as feature=minor,bug=patch, or a JSON object. Sizes are patch, minor, major or none; the pairs add to or override the built-in map (feature minor, bug patch). An empty value adds nothing.")),
		mcpsdk.WithString("merge_pipeline_major_trigger", mcpsdk.Enum("manual", "milestone", "never"), mcpsdk.Description("autonomous.merge_pipeline.major_trigger — when a major release step is raised: manual (the default, only when the operator names it), milestone (when the Automation the Automaton belongs to is a completed milestone) or never")),
		mcpsdk.WithString("merge_pipeline_release_cadence", mcpsdk.Description("autonomous.merge_pipeline.release_cadence — how often a release is cut: per_merge (the only value available today; the default). batch and manual are refused until they exist, and the refusal names the value to use.")),
		mcpsdk.WithString("merge_pipeline_version_scheme", mcpsdk.Description("autonomous.merge_pipeline.version_scheme — how versions are numbered: semver (the only value available today; the default). Other schemes are refused until they exist, and the refusal names the value to use.")),
		mcpsdk.WithString("merge_pipeline_release_approval_patch", mcpsdk.Description("autonomous.merge_pipeline.release_approval.patch — approval for a patch release: auto (ships without waiting), ask (asks the operator before it ships) or manual (left for the operator to ship). Default auto. Empty clears the size back to the global value or the default.")),
		mcpsdk.WithString("merge_pipeline_release_approval_minor", mcpsdk.Description("autonomous.merge_pipeline.release_approval.minor — approval for a minor release: auto, ask or manual. Default auto. Empty clears the size back to the global value or the default.")),
		mcpsdk.WithString("merge_pipeline_release_approval_major", mcpsdk.Description("autonomous.merge_pipeline.release_approval.major — approval for a major release: auto, ask or manual. Default ask. Empty clears the size back to the global value or the default.")),
	}
}

// mergePipelineConfigArgs maps the merge_pipeline_* arguments of
// autonomous_config_set into PUT /api/config entries. Every argument is
// presence-aware, so an explicit empty text or list is sent as given. The
// second result is an error when a project is named but no setting is, or a
// project name is not text.
func mergePipelineConfigArgs(req mcpsdk.CallToolRequest) (map[string]any, error) {
	out := map[string]any{}
	args := req.GetArguments()
	prefix := "autonomous.merge_pipeline."
	project := ""
	if raw, ok := args["merge_pipeline_project"]; ok && raw != nil {
		p, isStr := raw.(string)
		if !isStr || strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("merge_pipeline_project must be a project name")
		}
		project = strings.TrimSpace(p)
		prefix += "projects." + project + "."
	}
	for _, k := range mergePipelineConfigFields {
		v, ok := args["merge_pipeline_"+k]
		if !ok {
			continue
		}
		key := prefix + mergePipelineConfigKey(k)
		if strings.HasPrefix(k, "release_approval_") {
			if err := mergePipelineReleaseApprovalArg(key, v); err != nil {
				return nil, err
			}
		}
		out[key] = v
	}
	if project != "" && len(out) == 0 {
		return nil, fmt.Errorf("merge_pipeline_project was given with no merge_pipeline_* setting to apply to it")
	}
	return out, nil
}

// mergePipelineReleaseApprovalArg checks a release approval argument with the
// config package's validator, the same check the daemon runs on PUT
// /api/config, so a bad mode is refused here with its full key named and
// nothing is sent. null and the empty text clear the size; a value that is
// not text is refused.
func mergePipelineReleaseApprovalArg(key string, v any) error {
	if v == nil {
		return nil
	}
	s, isStr := v.(string)
	if !isStr {
		return fmt.Errorf("%s: %v is invalid; use auto, ask or manual", key, v)
	}
	return config.ValidateReleaseApprovalModeKey(key, strings.TrimSpace(s))
}

// mergePipelineFileArg reads the file argument of validate: true or the text
// "true" checks the configured file, other text names a file, false or absent
// checks nothing. Anything else is refused.
func mergePipelineFileArg(v any) (any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case bool:
		if t {
			return true, nil
		}
		return nil, nil
	case string:
		f := strings.TrimSpace(t)
		switch {
		case f == "":
			return nil, fmt.Errorf("file is empty; use true or a file name inside the project")
		case strings.EqualFold(f, "true"):
			return true, nil
		}
		return f, nil
	}
	return nil, fmt.Errorf("file must be true or a file name")
}

// datawatch config set for the merge pipeline release approval keys.
//
// The keys are autonomous.merge_pipeline.release_approval.<size> (the global
// value) and autonomous.merge_pipeline.projects.<project>.release_approval.<size>
// (one project's value), where <size> is patch, minor or major and the value
// is auto, ask or manual. The check runs here, before the value leaves the
// command, so a bad value is refused with the key and the valid values named.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// isMergePipelineReleaseApprovalKey reports whether key is a release approval
// key of the merge pipeline block, global or for one project, with a release
// size of patch, minor or major.
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
	return ok && (size == config.ReleaseSizePatch || size == config.ReleaseSizeMinor || size == config.ReleaseSizeMajor)
}

// checkMergePipelineReleaseApprovalValue refuses any value other than auto,
// ask or manual (surrounding spaces are ignored). An empty value is refused
// too: unsetting a size is not a config set.
func checkMergePipelineReleaseApprovalValue(key, value string) error {
	switch strings.TrimSpace(value) {
	case config.ReleaseApprovalAuto, config.ReleaseApprovalAsk, config.ReleaseApprovalManual:
		return nil
	}
	return fmt.Errorf("%s: %q is not a release approval mode; use %q, %q or %q",
		key, value, config.ReleaseApprovalAuto, config.ReleaseApprovalAsk, config.ReleaseApprovalManual)
}

// runMergePipelineReleaseApprovalSet validates a release approval key and value,
// then sends the key to the daemon as one JSON string.
func runMergePipelineReleaseApprovalSet(url, token, key, value string) error {
	if err := checkMergePipelineReleaseApprovalValue(key, value); err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	v := strings.TrimSpace(value)
	b, _ := json.Marshal(map[string]any{key: v})
	if err := putConfig(url, token, string(b)); err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	fmt.Printf("Set %s = %s\n", key, v)
	return nil
}

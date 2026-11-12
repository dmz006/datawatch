// The built-in post-ship hooks: the small fixed steps a project can run
// after a ship's deploy step without writing a shell line for each. Each
// built-in is one function here, takes its settings as a plain copy of the
// autonomous.merge_pipeline.post_ship block and returns a short account of
// what it did. They never run a shell string: the ones that leave the
// machine go through the ShipRunner, and every error they return is
// redacted before it is handed back.

package mergepipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PostShipSettings is the autonomous.merge_pipeline.post_ship block copied
// into this package by the caller, so a built-in never reads a setting of its
// own: an unset target is empty, and a built-in whose target is empty fails
// rather than guess.
type PostShipSettings struct {
	// Enabled turns the whole block on; the caller runs built-ins only
	// when it is set and the hook is listed and enabled.
	Enabled bool
	// Hooks lists the steps, run in order after a ship's deploy step.
	Hooks []PostShipHook
	// RegistryFile is the registry file the registry_row hook writes,
	// relative to the project. May contain the {project} placeholder.
	RegistryFile string
	// RegistryRowPattern is the line-matching text template the
	// registry_row hook uses. May contain {project} and {version}.
	RegistryRowPattern string
	// CompanionRepository is the owner/name the parity_issue hook files
	// its issue in.
	CompanionRepository string
	// IssueTitleTemplate is the parity_issue hook's issue title. May
	// contain {project} and {version}.
	IssueTitleTemplate string
	// NotifyTemplate is the notify hook's message body. May contain
	// {project} and {version}.
	NotifyTemplate string
	// WriteThrough is autonomous.plan_records.write_through. When set and
	// the Runner has a RecordWriter, the registry_row hook records the row
	// as a plan record and leaves the registry file alone.
	WriteThrough bool
}

// PostShipHook is one entry of the post-ship hook list. Exactly one of
// Builtin and Command is set: a built-in runs against the target setting
// named by its name, a command runs a shell line through the ShipRunner.
type PostShipHook struct {
	// Name identifies the hook inside the list; required and unique.
	Name string
	// Builtin is one of the built-in names. Empty when Command is set.
	Builtin string
	// Command is a shell command line. Empty when Builtin is set.
	Command string
	// Enabled turns the hook on. False by default.
	Enabled bool
}

// render substitutes the {project} and {version} placeholders in s and
// leaves any other {placeholder} exactly as written.
func render(s, project, version string) string {
	s = strings.ReplaceAll(s, "{project}", project)
	s = strings.ReplaceAll(s, "{version}", version)
	return s
}

// builtinErr redacts a built-in's error text before it is returned, so a
// failing step never leaks a credential into the run record or a log.
func builtinErr(format string, a ...any) error {
	return errors.New(redact(fmt.Sprintf(format, a...)))
}

// RunRegistryRow is the registry_row built-in: it finds the project's row
// in the registry file and marks the status cell shipped. The registry
// file must stay inside the project, the row pattern must match exactly
// one line, and the write goes through a temporary file in the same
// directory plus a rename, so a crash never leaves a half-written registry.
func RunRegistryRow(ctx context.Context, dir, project, version string, s PostShipSettings) (string, error) {
	if strings.TrimSpace(s.RegistryFile) == "" {
		return "", builtinErr("the registry_row hook has no registry_file setting")
	}
	rel := render(s.RegistryFile, project, version)
	if err := checkDir(rel); err != nil {
		return "", builtinErr("registry_file %q: %s", rel, err)
	}
	pattern := render(s.RegistryRowPattern, project, version)
	if strings.TrimSpace(pattern) == "" {
		return "", builtinErr("the registry_row hook has no registry_row_pattern setting")
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path) // #nosec G304 -- relative path validated, joined under the project
	if err != nil {
		return "", builtinErr("reading %s: %s", rel, err)
	}
	lines := strings.Split(string(data), "\n")
	var matched []int
	for i, l := range lines {
		if strings.Contains(l, pattern) {
			matched = append(matched, i)
		}
	}
	if len(matched) != 1 {
		return "", builtinErr("the row pattern matched %d lines in %s; exactly one is required", len(matched), rel)
	}
	row := matched[0]
	idx, err := statusCellIndex(lines, row)
	if err != nil {
		return "", builtinErr("the row in %s: %s", rel, err)
	}
	cells := strings.Split(lines[row], "|")
	cells[idx] = " Shipped " + version + " "
	lines[row] = strings.Join(cells, "|")

	// Write through a temporary file in the same directory and rename,
	// so the registry file is never observed half-written.
	dirName := filepath.Dir(path)
	tmp, err := os.CreateTemp(dirName, ".registry-row-*.tmp")
	if err != nil {
		return "", builtinErr("temp file in %s: %s", dirName, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if fi, serr := os.Stat(path); serr == nil {
		_ = os.Chmod(tmpName, fi.Mode().Perm())
	}
	if _, werr := tmp.WriteString(strings.Join(lines, "\n")); werr != nil {
		_ = tmp.Close()
		return "", builtinErr("writing %s: %s", rel, werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return "", builtinErr("closing %s: %s", rel, cerr)
	}
	if rerr := os.Rename(tmpName, path); rerr != nil {
		return "", builtinErr("replacing %s: %s", rel, rerr)
	}
	return "updated the registry row", nil
}

// registryStatuses are the words a registry status cell starts with,
// longest first so "in progress" wins over a shorter prefix.
var registryStatuses = []string{
	"in progress", "in review", "open", "planned", "done", "shipped",
	"blocked", "paused", "deferred", "frozen", "partial",
}

// looksLikeStatus reports whether cell reads as a registry status value
// ("open", "**done**, shipped in 9.5.35", "In progress — waiting").
func looksLikeStatus(cell string) bool {
	t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(cell), "*", ""))
	for _, w := range registryStatuses {
		if t == w || strings.HasPrefix(t, w+" ") || strings.HasPrefix(t, w+",") {
			return true
		}
	}
	return false
}

// statusCellIndex finds the status cell of the pipe-table row at lines[row].
// It prefers the table header's "status" column when the header sits above
// the row, and falls back to recognizing the status value itself.
func statusCellIndex(lines []string, row int) (int, error) {
	cells := strings.Split(lines[row], "|")
	if h := tableHeader(lines, row); h != "" {
		for i, c := range strings.Split(h, "|") {
			if strings.Contains(strings.ToLower(strings.TrimSpace(c)), "status") {
				if i < len(cells) {
					return i, nil
				}
				break
			}
		}
	}
	for i, c := range cells {
		if looksLikeStatus(c) {
			return i, nil
		}
	}
	return -1, errors.New("no status cell was found in the row")
}

// tableHeader returns the table's header row above the data row at row, or
// empty when the table starts at the top of the file.
func tableHeader(lines []string, row int) string {
	for j := row - 1; j >= 0; j-- {
		if !strings.Contains(lines[j], "|") {
			if j+1 < row {
				return lines[j+1]
			}
			return ""
		}
	}
	return ""
}

// RunParityIssue is the parity_issue built-in: it files an issue in the
// companion repository recording the shipped version, through the
// ShipRunner so no shell string is ever built.
func RunParityIssue(ctx context.Context, ship ShipRunner, dir, project, version string, s PostShipSettings) (string, error) {
	if ship == nil {
		return "", builtinErr("no ship runner is wired, so the parity_issue hook was not run")
	}
	repo := strings.TrimSpace(render(s.CompanionRepository, project, version))
	if repo == "" {
		return "", builtinErr("the parity_issue hook has no companion_repository setting")
	}
	title := strings.TrimSpace(render(s.IssueTitleTemplate, project, version))
	if title == "" {
		return "", builtinErr("the parity_issue hook has no issue_title_template setting")
	}
	body := strings.TrimSpace(render(s.NotifyTemplate, project, version))
	if body == "" {
		body = "Shipped " + version + " of " + project +
			": parity is required on the other client surfaces for this release."
	}
	if err := ship.FileIssue(ctx, dir, repo, title, body); err != nil {
		return "", builtinErr("filing the parity issue in %s: %s", repo, err)
	}
	return "filed the parity issue in " + repo, nil
}

// RunTag is the tag built-in: it tags the release, skipping rather than
// failing when the release tag already exists. The presence check is a
// plain `git rev-parse --verify` on the tag ref; no shell string is built.
func RunTag(ctx context.Context, ship ShipRunner, dir, version string) (string, error) {
	if ship == nil {
		return "", builtinErr("no ship runner is wired, so the tag hook was not run")
	}
	if strings.TrimSpace(version) == "" {
		return "", builtinErr("the tag hook has no version to tag")
	}
	tag := "v" + version
	m := &Merger{Dir: dir}
	res, err := m.run(ctx, dir, "git", "rev-parse", "-q", "--verify", "refs/tags/"+tag)
	if err != nil {
		return "", builtinErr("checking for tag %s: %s", tag, err)
	}
	if res.Code == 0 {
		return "tag " + tag + " already exists; skipped", nil
	}
	if terr := ship.Tag(ctx, dir, tag, "Release "+tag); terr != nil {
		return "", builtinErr("tagging %s: %s", tag, terr)
	}
	return "tagged " + tag, nil
}

// Notifier sends the notify built-in's message. A nil Notifier makes the
// built-in fail rather than drop the notification.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// RunNotify is the notify built-in: it renders the notify template and
// hands it to the Notifier.
func RunNotify(ctx context.Context, n Notifier, project, version string, s PostShipSettings) (string, error) {
	if n == nil {
		return "", builtinErr("no notifier is wired, so the notify hook was not run")
	}
	text := render(s.NotifyTemplate, project, version)
	if strings.TrimSpace(text) == "" {
		return "", builtinErr("the notify hook has no notify_template setting")
	}
	if err := n.Notify(ctx, text); err != nil {
		return "", builtinErr("sending the notification: %s", err)
	}
	return "sent the notification", nil
}

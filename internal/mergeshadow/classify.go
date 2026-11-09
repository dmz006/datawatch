package mergeshadow

import (
	"path"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// FileClass says how a conflicted path is resolved. The class decides who
// may resolve the conflict: mechanical files have one correct rewrite, code
// and document files need a model or a person, and an unknown path is never
// guessed at.
type FileClass string

// File classes a conflicted path can take.
const (
	// FileClassMechanical covers files whose conflict is a fixed rewrite: the
	// changelog, the version line of each version file, the README line,
	// locale JSON files and generated indexes.
	FileClassMechanical FileClass = "mechanical"
	// FileClassCode covers source files.
	FileClassCode FileClass = "code"
	// FileClassDocument covers markdown and other documentation files.
	FileClassDocument FileClass = "document"
	// FileClassUnknown is any path no rule matches.
	FileClassUnknown FileClass = "unknown"
)

// DefaultChangelogFile and DefaultReadmeFile name the changelog and README
// the merge classifier treats as mechanical. The merge pipeline config has
// no setting for either path, so these are the values used for every project.
const (
	DefaultChangelogFile = "CHANGELOG.md"
	DefaultReadmeFile    = "README.md"
)

// codeExtensions are the source-file extensions classed as code.
var codeExtensions = map[string]bool{
	".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true,
	".swift": true, ".c": true, ".h": true, ".cc": true, ".cpp": true,
	".hpp": true, ".cs": true, ".php": true, ".sh": true, ".sql": true,
}

// documentExtensions are the documentation-file extensions classed as document.
var documentExtensions = map[string]bool{
	".md": true, ".markdown": true, ".rst": true, ".adoc": true, ".txt": true,
}

// localeDirs are the directory names that hold locale JSON files.
var localeDirs = map[string]bool{"locales": true, "locale": true, "i18n": true}

// Classifier maps a conflicted path to its FileClass. Its mechanical paths
// come from the merge pipeline settings, so a project's version files are
// read from config and never named in code.
type Classifier struct {
	// VersionFiles are the project files whose version line is mechanical.
	VersionFiles []string
	// ChangelogFile is the changelog path; mechanical when matched.
	ChangelogFile string
	// ReadmeFile is the README path; mechanical when matched.
	ReadmeFile string
}

// NewClassifier builds the classifier for project from the merge pipeline
// config: the version files come from the config's effective list for that
// project; the changelog and README take their defaults.
func NewClassifier(cfg config.MergePipelineConfig, project string) Classifier {
	return Classifier{
		VersionFiles:  cfg.EffectiveVersionFiles(project),
		ChangelogFile: DefaultChangelogFile,
		ReadmeFile:    DefaultReadmeFile,
	}
}

// Classify returns the class of the conflicted path p. Paths are compared
// after cleaning, so "./go.mod" and "go.mod" are the same file. An empty path,
// an absolute path or one that leaves the project is unknown.
func (c Classifier) Classify(p string) FileClass {
	clean := cleanRelPath(p)
	if clean == "" {
		return FileClassUnknown
	}
	if c.isMechanicalPath(clean) {
		return FileClassMechanical
	}
	ext := strings.ToLower(path.Ext(clean))
	base := path.Base(clean)
	if ext == ".json" && hasLocaleDir(clean) {
		return FileClassMechanical
	}
	if isGeneratedIndex(base, ext) {
		return FileClassMechanical
	}
	if codeExtensions[ext] {
		return FileClassCode
	}
	if documentExtensions[ext] || strings.HasPrefix(clean, "docs/") {
		return FileClassDocument
	}
	return FileClassUnknown
}

// isMechanicalPath reports whether clean names the changelog, the README or a
// version file, comparing each configured path after cleaning it too.
func (c Classifier) isMechanicalPath(clean string) bool {
	if clean == cleanRelPath(c.ChangelogFile) || clean == cleanRelPath(c.ReadmeFile) {
		return true
	}
	for _, v := range c.VersionFiles {
		if clean == cleanRelPath(v) {
			return true
		}
	}
	return false
}

// cleanRelPath cleans a project-relative slash path. It returns "" for an
// empty path, an absolute path, or one that climbs out of the project.
func cleanRelPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return ""
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

// hasLocaleDir reports whether any directory of clean is a locale directory.
func hasLocaleDir(clean string) bool {
	dirs := strings.Split(path.Dir(clean), "/")
	for _, d := range dirs {
		if localeDirs[d] {
			return true
		}
	}
	return false
}

// isGeneratedIndex reports whether base is a generated index file: a JSON or
// YAML file whose name is "index" or ends in ".index".
func isGeneratedIndex(base, ext string) bool {
	if ext != ".json" && ext != ".yaml" && ext != ".yml" {
		return false
	}
	stem := strings.TrimSuffix(base, path.Ext(base))
	return stem == "index" || strings.HasSuffix(stem, ".index")
}

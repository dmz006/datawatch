package mergepipeline

import (
	"path"
	"strings"
)

// FileClass names the kind of file a conflict is in, which decides how the
// conflict is handled.
type FileClass string

const (
	ClassCode        FileClass = "code"
	ClassDocument    FileClass = "document"
	ClassChangelog   FileClass = "changelog"
	ClassVersionLine FileClass = "version_line"
	ClassReadmeLine  FileClass = "readme_line"
	ClassLocale      FileClass = "locale"
	ClassGenerated   FileClass = "generated"
)

const (
	localeDir    = "internal/server/web/locales/"
	docsAssetDir = "internal/docsindex/assets/"
	webDocsDir   = "internal/server/web/docs/"
)

// ClassifyConflictFile classifies a cleaned, slash-separated, repository
// relative path. An absolute path or one that climbs out of the repository
// is ClassCode, the most careful class.
func ClassifyConflictFile(rel string, versionFiles []string) FileClass {
	if rel == "" || strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") {
		return ClassCode
	}
	if rel == "CHANGELOG.md" {
		return ClassChangelog
	}
	for _, v := range versionFiles {
		if rel == v {
			return ClassVersionLine
		}
	}
	if rel == "README.md" {
		return ClassReadmeLine
	}
	if strings.HasPrefix(rel, localeDir) && path.Ext(rel) == ".json" &&
		!strings.Contains(strings.TrimPrefix(rel, localeDir), "/") {
		return ClassLocale
	}
	if strings.HasPrefix(rel, docsAssetDir) || strings.HasPrefix(rel, webDocsDir) {
		return ClassGenerated
	}
	if path.Ext(rel) == ".md" || strings.HasPrefix(rel, "docs/") {
		return ClassDocument
	}
	return ClassCode
}

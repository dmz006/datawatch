package packs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadPackFolder loads a pack from a directory. It parses the folder's
// pack.yaml through ParseManifest, verifies that every task's prompt file
// exists inside the folder — refusing prompt paths that are absolute or
// escape the folder and prompt files that are missing or unreadable — and
// records a content digest over the folder's files in sorted order on the
// returned Pack, for version records.
//
// Tasks without a prompt path are accepted: they simply ship no prompt file.
func LoadPackFolder(dir string) (*Pack, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("pack folder: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("pack folder: %s is not a directory", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("pack folder: %w", err)
	}

	data, err := os.ReadFile(filepath.Join(abs, "pack.yaml"))
	if err != nil {
		return nil, fmt.Errorf("pack folder: %w", err)
	}
	p, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}

	for _, t := range p.Tasks {
		rel := strings.TrimSpace(t.Prompt)
		if rel == "" {
			continue
		}
		if filepath.IsAbs(rel) {
			return nil, fmt.Errorf("pack folder: task %q: prompt path %q must be relative to the pack folder", t.Title, t.Prompt)
		}
		target := filepath.Join(abs, rel)
		if !strings.HasPrefix(target, abs+string(filepath.Separator)) {
			return nil, fmt.Errorf("pack folder: task %q: prompt path %q escapes the pack folder", t.Title, t.Prompt)
		}
		info, err := os.Stat(target)
		if err != nil {
			return nil, fmt.Errorf("pack folder: task %q: prompt file %q: %w", t.Title, rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("pack folder: task %q: prompt file %q is not a regular file", t.Title, rel)
		}
		if _, err := os.ReadFile(target); err != nil {
			return nil, fmt.Errorf("pack folder: task %q: reading prompt file %q: %w", t.Title, rel, err)
		}
	}

	digest, err := folderDigest(abs)
	if err != nil {
		return nil, fmt.Errorf("pack folder: %w", err)
	}
	p.Digest = digest
	p.Dir = abs

	return p, nil
}

// folderDigest returns a hex-encoded SHA-256 digest over every regular file
// under dir, visited in sorted relative-path order. Each file contributes its
// relative path and its content, so the digest changes when any file is
// added, removed, renamed, or edited.
func folderDigest(dir string) (string, error) {
	var rels []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type().IsRegular() {
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				return relErr
			}
			rels = append(rels, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(rels)

	h := sha256.New()
	for _, rel := range rels {
		content, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		io.WriteString(h, rel)
		io.WriteString(h, "\n")
		h.Write(content)
		io.WriteString(h, "\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

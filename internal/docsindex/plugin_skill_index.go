// BL274 Sprint 4, v6.19.0 — plugin + skill docs indexer + fsnotify watchers.
//
// Per BL274 design:
//   Q9 — plugin manifest must declare `docs:files: [...]` (required) and
//        may declare `docs:howtos:[{file,topic,params}]` metadata. Each
//        plugin gets its OWN per-plugin index (Q1 hybrid: core+skills
//        unified, plugins isolated).
//   Q10 — skill SKILL.md auto-indexes by default; optional `docs:` block
//        in the manifest extends the indexed file set.
//   Q6  — all opt-in. Skill / plugin source is added to PendingQueue
//        when first seen; only indexed once an operator trusts the source.
//   Q8  — fsnotify watchers on ~/.datawatch/skills/ and
//        ~/.datawatch/plugins/ + explicit reload hooks (belt + suspenders).
//
// This file is the in-process indexer. main.go wires the watcher
// goroutines + the trust-acceptance callback at startup.

package docsindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// yamlUnmarshal is a thin wrapper so the rest of the file can stay
// dependency-name-agnostic.
func yamlUnmarshal(data []byte, out any) error { return yaml.Unmarshal(data, out) }

// PluginSkillIndexer walks the operator's skills + plugins directories,
// parses manifests, and adds their docs to the runtime's BM25 index under
// per-source tiers (skill:<name> / plugin:<name>). New sources land in the
// PendingQueue for operator approval; index entries materialize only after
// the source becomes trusted.
type PluginSkillIndexer struct {
	rt          *Runtime
	skillsDir   string // e.g. ~/.datawatch/skills
	pluginsDir  string // e.g. ~/.datawatch/plugins
	addedHook   func(source string, chunkCount int)
}

// NewPluginSkillIndexer returns an indexer rooted at the operator's
// data dirs. Both root dirs may be missing — the indexer no-ops cleanly.
func NewPluginSkillIndexer(rt *Runtime, dataDir string) *PluginSkillIndexer {
	return &PluginSkillIndexer{
		rt:         rt,
		skillsDir:  filepath.Join(dataDir, "skills"),
		pluginsDir: filepath.Join(dataDir, "plugins"),
	}
}

// SetAddedHook installs a callback fired each time a new source's docs
// land in the index (post-trust). Used by main.go to log + emit alerts.
func (p *PluginSkillIndexer) SetAddedHook(fn func(source string, chunkCount int)) {
	p.addedHook = fn
}

// IndexAll walks both roots and indexes every trusted source's docs;
// untrusted sources land in the pending queue for operator approval.
// Returns counts (sourcesSeen, sourcesIndexed, chunksAdded).
func (p *PluginSkillIndexer) IndexAll(ctx context.Context) (seen, indexed, chunks int) {
	if p == nil || p.rt == nil {
		return
	}
	// Skills.
	if entries, err := os.ReadDir(p.skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			seen++
			source := "skill:" + e.Name()
			if !p.rt.Trust().IsTrusted(source) {
				_ = p.rt.Pending().Add(source, "auto-discovered skill at "+filepath.Join(p.skillsDir, e.Name()))
				continue
			}
			added := p.indexSkill(filepath.Join(p.skillsDir, e.Name()), source)
			if added > 0 {
				indexed++
				chunks += added
				if p.addedHook != nil {
					p.addedHook(source, added)
				}
			}
		}
	}
	// Plugins.
	if entries, err := os.ReadDir(p.pluginsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			seen++
			source := "plugin:" + e.Name()
			if !p.rt.Trust().IsTrusted(source) {
				_ = p.rt.Pending().Add(source, "auto-discovered plugin at "+filepath.Join(p.pluginsDir, e.Name()))
				continue
			}
			added := p.indexPlugin(filepath.Join(p.pluginsDir, e.Name()), source)
			if added > 0 {
				indexed++
				chunks += added
				if p.addedHook != nil {
					p.addedHook(source, added)
				}
			}
		}
	}
	return
}

// indexSkill auto-indexes SKILL.md (Q10 default) plus any extra files
// declared via the manifest's `docs:files:` extension.
func (p *PluginSkillIndexer) indexSkill(skillDir, source string) int {
	var added int
	// Default: SKILL.md.
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if body, err := os.ReadFile(skillPath); err == nil {
		rel := "SKILL.md"
		chunks := ChunkDoc(rel, string(body))
		for i := range chunks {
			chunks[i].Source = source
		}
		added += p.rt.AddChunks(chunks)
	}
	// Optional extension via manifest `docs:files:`.
	manifestPath := filepath.Join(skillDir, "manifest.yaml")
	docs := readManifestDocs(manifestPath)
	if docs == nil {
		return added
	}
	for _, f := range docs.Files {
		if f == "SKILL.md" {
			continue // already handled above
		}
		full := filepath.Join(skillDir, f)
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		chunks := ChunkDoc(f, string(body))
		for i := range chunks {
			chunks[i].Source = source
		}
		added += p.rt.AddChunks(chunks)
	}
	return added
}

// indexPlugin requires `docs:files:` in the manifest (Q9 — plugins are
// isolated, so they MUST opt in). No SKILL.md fallback.
func (p *PluginSkillIndexer) indexPlugin(pluginDir, source string) int {
	manifestPath := filepath.Join(pluginDir, "manifest.yaml")
	docs := readManifestDocs(manifestPath)
	if docs == nil || len(docs.Files) == 0 {
		return 0
	}
	var added int
	for _, f := range docs.Files {
		full := filepath.Join(pluginDir, f)
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		chunks := ChunkDoc(f, string(body))
		for i := range chunks {
			chunks[i].Source = source
		}
		added += p.rt.AddChunks(chunks)
	}
	return added
}

// Watch starts fsnotify watchers on both roots. Re-indexes the affected
// source on directory create/remove/rename or manifest.yaml/SKILL.md
// modify. Long-lived; meant to run in a goroutine.
func (p *PluginSkillIndexer) Watch(ctx context.Context) error {
	if p == nil || p.rt == nil {
		return fmt.Errorf("docsindex: nil indexer")
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("docsindex: fsnotify: %w", err)
	}
	defer w.Close()
	for _, root := range []string{p.skillsDir, p.pluginsDir} {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			_ = os.MkdirAll(root, 0o755)
		}
		_ = w.Add(root)
		// Also watch existing children.
		if children, err := os.ReadDir(root); err == nil {
			for _, c := range children {
				if c.IsDir() {
					_ = w.Add(filepath.Join(root, c.Name()))
				}
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			// On any change inside a watched root, re-index that root's children.
			if strings.HasPrefix(ev.Name, p.skillsDir) || strings.HasPrefix(ev.Name, p.pluginsDir) {
				p.IndexAll(ctx)
				if ev.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						_ = w.Add(ev.Name) // watch new subdir
					}
				}
			}
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
			// Non-fatal: continue watching.
		}
	}
}

// readManifestDocs reads a manifest.yaml and returns its docs:files /
// docs:howtos block (best-effort; nil on any error).
func readManifestDocs(path string) *manifestDocs {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var wrap struct {
		Docs *manifestDocs `yaml:"docs"`
	}
	if err := yamlUnmarshal(body, &wrap); err != nil {
		return nil
	}
	return wrap.Docs
}

type manifestDocs struct {
	Files  []string            `yaml:"files,omitempty"`
	Howtos []manifestDocsHowto `yaml:"howtos,omitempty"`
}

type manifestDocsHowto struct {
	File   string   `yaml:"file"`
	Topic  string   `yaml:"topic,omitempty"`
	Params []string `yaml:"params,omitempty"`
}

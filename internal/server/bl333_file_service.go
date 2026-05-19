// BL333 (v8.3.0 T43) — Federated File Service.
//
// Adds subdirectory-isolated file operations on top of the existing /api/files
// GET+POST (list/mkdir) handlers:
//
//   POST   /api/files          multipart/form-data → upload a file
//   DELETE /api/files          JSON {path} → delete a file
//   GET    /api/files/peers/{name}        → list peer-specific subdirectory
//   GET    /api/files/discussions/{id}    → list discussion-specific subdirectory
//   GET    /api/files/meta                → storage overview (root, peer counts, disk usage)

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
)

// fileServiceRoot resolves the effective root for the file service.
// Priority: FileServiceRoot → RootPath → user home directory.
func (s *Server) fileServiceRoot() string {
	if s.cfg != nil && s.cfg.Session.FileServiceRoot != "" {
		root := s.cfg.Session.FileServiceRoot
		if len(root) > 0 && root[0] == '~' {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, root[1:])
		}
		return filepath.Clean(root)
	}
	if s.cfg != nil && s.cfg.Session.RootPath != "" {
		root := s.cfg.Session.RootPath
		if len(root) > 0 && root[0] == '~' {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, root[1:])
		}
		return filepath.Clean(root)
	}
	home, _ := os.UserHomeDir()
	return home
}

// checkPathTraversal returns an error if target escapes root.
func checkPathTraversal(root, target string) error {
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != cleanRoot &&
		!strings.HasPrefix(cleanTarget+string(filepath.Separator), cleanRoot+string(filepath.Separator)) {
		return fmt.Errorf("path outside service root")
	}
	return nil
}

// handleFilesJSONUpload handles POST /api/files/upload with JSON body
// {path, content} — used by the MCP files_upload tool.
func (s *Server) handleFilesJSONUpload(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	target := req.Path
	if len(target) > 0 && target[0] == '~' {
		home, _ := os.UserHomeDir()
		target = filepath.Join(home, target[1:])
	}
	root := s.fileServiceRoot()
	if err := checkPathTraversal(root, target); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		http.Error(w, "mkdir parent: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(target, []byte(req.Content), 0644); err != nil {
		http.Error(w, "write: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"path":  target,
		"bytes": len(req.Content),
	})
}

// handleFilesUpload handles multipart file upload via POST /api/files when
// Content-Type starts with "multipart/". Form fields: path (destination path).
func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "parse multipart: "+err.Error(), http.StatusBadRequest)
		return
	}
	destPath := r.FormValue("path")
	if destPath == "" {
		http.Error(w, "path field required", http.StatusBadRequest)
		return
	}
	if len(destPath) > 0 && destPath[0] == '~' {
		home, _ := os.UserHomeDir()
		destPath = filepath.Join(home, destPath[1:])
	}
	root := s.fileServiceRoot()
	if err := checkPathTraversal(root, destPath); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file field required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close() //nolint:errcheck

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		http.Error(w, "mkdir parent: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "create: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close() //nolint:errcheck

	n, err := io.Copy(out, f)
	if err != nil {
		http.Error(w, "write: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"path":  destPath,
		"bytes": n,
	})
}

// handleFilesDelete handles DELETE /api/files with JSON body {path}.
func (s *Server) handleFilesDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	target := req.Path
	if len(target) > 0 && target[0] == '~' {
		home, _ := os.UserHomeDir()
		target = filepath.Join(home, target[1:])
	}
	root := s.fileServiceRoot()
	if err := checkPathTraversal(root, target); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "remove: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"deleted": target,
	})
}

// handleFilesPeer handles GET /api/files/peers/{name} — lists files in
// <fileServiceRoot>/peers/<name>/.
func (s *Server) handleFilesPeer(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/files/peers/")
	name = strings.Trim(name, "/")
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		http.Error(w, "invalid peer name", http.StatusBadRequest)
		return
	}
	root := s.fileServiceRoot()
	dir := filepath.Join(root, "peers", name)
	s.listFilesDir(w, dir)
}

// handleFilesDiscussion handles GET /api/files/discussions/{id} — lists files
// in <fileServiceRoot>/discussions/<id>/.
func (s *Server) handleFilesDiscussion(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/files/discussions/")
	id = strings.Trim(id, "/")
	if id == "" || strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		http.Error(w, "invalid discussion id", http.StatusBadRequest)
		return
	}
	root := s.fileServiceRoot()
	dir := filepath.Join(root, "discussions", id)
	s.listFilesDir(w, dir)
}

// listFilesDir is a shared helper: creates dir if missing, then lists it.
func (s *Server) listFilesDir(w http.ResponseWriter, dir string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "mkdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "readdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type Entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Path  string `json:"path"`
		Bytes int64  `json:"bytes,omitempty"`
	}
	result := []Entry{}
	for _, e := range entries {
		entryPath := filepath.Join(dir, e.Name())
		var sz int64
		if !e.IsDir() {
			if fi, err := e.Info(); err == nil {
				sz = fi.Size()
			}
		}
		result = append(result, Entry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Path:  entryPath,
			Bytes: sz,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"path":    dir,
		"entries": result,
	})
}

// fileSubdirMeta walks a subdirectory (peers/ or discussions/) and returns
// per-entry metadata.
type fileSubdirMeta struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	FileCount int    `json:"file_count"`
	Bytes     int64  `json:"bytes"`
}

func collectSubdirMeta(base string) []fileSubdirMeta {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	out := make([]fileSubdirMeta, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(base, e.Name())
		var count int
		var total int64
		_ = filepath.Walk(sub, func(_ string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			count++
			total += fi.Size()
			return nil
		})
		out = append(out, fileSubdirMeta{
			Name:      e.Name(),
			Path:      sub,
			FileCount: count,
			Bytes:     total,
		})
	}
	return out
}

// handleFilesMeta handles GET /api/files/meta — storage overview.
func (s *Server) handleFilesMeta(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := s.fileServiceRoot()
	peers := collectSubdirMeta(filepath.Join(root, "peers"))
	discussions := collectSubdirMeta(filepath.Join(root, "discussions"))
	if peers == nil {
		peers = []fileSubdirMeta{}
	}
	if discussions == nil {
		discussions = []fileSubdirMeta{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"root":        root,
		"peers":       peers,
		"discussions": discussions,
	})
}

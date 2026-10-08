// SEC-021 (docs/plans/historical-plans/2026-09-02-sec-design-c-audit-config.md
// §C3, remainder) — the file service's default root (neither
// file_service_root nor session.root_path configured) used to fall back to
// the operator's own home directory (and RootPath, the operator's project
// checkout, ranked above that) -- so an in-repo absolute path like
// "docs/plans/x.md" sent to the upload endpoint landed inside the
// operator's actual repo by default, the write-scope half of the stored-XSS
// chain described there. These tests pin: the default now resolves to
// <data_dir>/files, and the deny-list blocks writes into internal/server/web
// or docs/ even when the configured root does include them.

package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestFileServiceRoot_DefaultsToDataDirSubpath_NotHome(t *testing.T) {
	dataDir := t.TempDir()
	s := &Server{cfg: &config.Config{DataDir: dataDir}}

	root := s.fileServiceRoot()
	want := filepath.Clean(filepath.Join(dataDir, "files"))
	if root != want {
		t.Fatalf("fileServiceRoot() = %q, want %q", root, want)
	}
	home, _ := os.UserHomeDir()
	if root == filepath.Clean(home) {
		t.Fatal("fileServiceRoot() must never default to the operator's home directory")
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		t.Fatalf("expected fileServiceRoot() to create %q, stat err: %v", root, err)
	}
}

func TestFileServiceRoot_ExplicitFileServiceRootStillWins(t *testing.T) {
	explicit := t.TempDir()
	s := &Server{cfg: &config.Config{
		DataDir: t.TempDir(),
		Session: config.SessionConfig{FileServiceRoot: explicit},
	}}
	if got := s.fileServiceRoot(); got != filepath.Clean(explicit) {
		t.Fatalf("fileServiceRoot() = %q, want explicit %q", got, explicit)
	}
}

func TestIsDenyListedFileServicePath(t *testing.T) {
	root := "/srv/files"
	cases := []struct {
		target string
		want   bool
	}{
		{"/srv/files/internal/server/web/app.js", true},
		{"/srv/files/internal/server/web", true},
		{"/srv/files/docs/plans/x.md", true},
		{"/srv/files/docs", true},
		{"/srv/files/documents/notes.md", false}, // "documents" must not match "docs"
		{"/srv/files/attachments/photo.png", false},
		{"/srv/files/internal/other/web", false},
	}
	for _, c := range cases {
		if got := isDenyListedFileServicePath(root, c.target); got != c.want {
			t.Errorf("isDenyListedFileServicePath(%q, %q) = %v, want %v", root, c.target, got, c.want)
		}
	}
}

func TestHandleFilesJSONUpload_DeniesAppDocsTree(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	body, _ := json.Marshal(map[string]string{
		"path":    filepath.Join(root, "docs", "plans", "sec-test.md"),
		"content": `<img src=x onerror=alert(1)>`,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleFilesJSONUpload(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "plans", "sec-test.md")); err == nil {
		t.Fatal("file must not have been written into the deny-listed docs/ subtree")
	}
}

func TestHandleFilesUpload_DeniesAppWebTree(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("path", filepath.Join(root, "internal", "server", "web", "evil.js"))
	fw, _ := mw.CreateFormFile("file", "evil.js")
	_, _ = fw.Write([]byte("alert(1)"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	s.handleFilesUpload(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleFilesDelete_DeniesAppDocsTree(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)
	// A real file the deny-list should still protect even if it somehow
	// already existed there (e.g. shipped with the install).
	docsDir := filepath.Join(root, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	target := filepath.Join(docsDir, "README.md")
	_ = os.WriteFile(target, []byte("important docs"), 0644)

	req := httptest.NewRequest(http.MethodDelete, "/api/files?path="+target, nil)
	rr := httptest.NewRecorder()
	s.handleFilesDelete(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("deny-listed file must not have been deleted")
	}
}

func TestHandleFilesJSONUpload_AllowsNonDenyListedPath(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	body, _ := json.Marshal(map[string]string{
		"path":    filepath.Join(root, "attachments", "notes.txt"),
		"content": "hello",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleFilesJSONUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// BL374 — files-as-links: GET /api/files/download serves a file for viewing/download.
//
//	TS-720: text file served inline with correct content
//	TS-721: binary file served as attachment (no inline param)
//	TS-722: missing path param returns 400
//	TS-723: non-existent file returns 404
//	TS-724: directory path returns 400
//	TS-725: path outside root returns 403 when root is configured
package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func bl374Server(t *testing.T, rootPath string) *Server {
	t.Helper()
	s := &Server{}
	if rootPath != "" {
		s.cfg = &config.Config{
			Session: config.SessionConfig{RootPath: rootPath},
		}
	}
	return s
}

// TS-720 — text file returned inline when ?inline=1.
func TestFilesDownload_InlineText(t *testing.T) {
	dir := t.TempDir()
	content := "hello from BL374\n"
	fpath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(fpath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	s := bl374Server(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/files/download?path="+fpath+"&inline=1", nil)
	rec := httptest.NewRecorder()
	s.handleFilesDownload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "inline") {
		t.Errorf("expected inline disposition, got: %q", rec.Header().Get("Content-Disposition"))
	}
	if !strings.Contains(rec.Body.String(), "hello from BL374") {
		t.Errorf("body missing expected content: %q", rec.Body.String())
	}
}

// TS-721 — file without inline param returns attachment disposition.
func TestFilesDownload_Attachment(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(fpath, []byte{0x00, 0x01, 0x02}, 0600); err != nil {
		t.Fatal(err)
	}

	s := bl374Server(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/files/download?path="+fpath, nil)
	rec := httptest.NewRecorder()
	s.handleFilesDownload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("expected attachment disposition, got: %q", rec.Header().Get("Content-Disposition"))
	}
}

// TS-722 — missing path query param returns 400.
func TestFilesDownload_MissingPath(t *testing.T) {
	s := bl374Server(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/files/download", nil)
	rec := httptest.NewRecorder()
	s.handleFilesDownload(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TS-723 — non-existent file returns 404.
func TestFilesDownload_NotFound(t *testing.T) {
	s := bl374Server(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/files/download?path=/tmp/definitely-does-not-exist-bl374.txt", nil)
	rec := httptest.NewRecorder()
	s.handleFilesDownload(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TS-724 — directory path returns 400.
func TestFilesDownload_Directory(t *testing.T) {
	dir := t.TempDir()
	s := bl374Server(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/files/download?path="+dir, nil)
	rec := httptest.NewRecorder()
	s.handleFilesDownload(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TS-725 — path outside configured root returns 403.
func TestFilesDownload_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	fpath := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(fpath, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	s := bl374Server(t, root)
	req := httptest.NewRequest(http.MethodGet, "/api/files/download?path="+fpath, nil)
	rec := httptest.NewRecorder()
	s.handleFilesDownload(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

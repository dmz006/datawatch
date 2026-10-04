package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/messaging"
)

// ---------------------------------------------------------------------
// Unit tests: decodeImageURL directly.
// ---------------------------------------------------------------------

func TestDecodeImageURL_DataURI_UnaffectedByImageDir(t *testing.T) {
	// BL394 regression check: the data: URI path never touches the
	// filesystem and must work identically whether image_dir is set,
	// unset, or anything else -- this is the pre-existing, unchanged
	// behavior the fix must not disturb.
	png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	for _, imageDir := range []string{"", t.TempDir()} {
		b := &Backend{imageDir: imageDir}
		att, err := b.decodeImageURL("data:image/png;base64," + png1x1)
		if err != nil {
			t.Fatalf("imageDir=%q: data: URI should always work, got: %v", imageDir, err)
		}
		if att.ContentType != "image/png" {
			t.Errorf("imageDir=%q: content type = %q, want image/png", imageDir, att.ContentType)
		}
		os.Remove(att.FilePath) //nolint:errcheck
	}
}

func TestDecodeImageURL_LocalPath_DisabledByDefault(t *testing.T) {
	b := &Backend{imageDir: ""} // default / unset
	tmpFile := filepath.Join(t.TempDir(), "x.png")
	os.WriteFile(tmpFile, []byte("fake-png"), 0644) //nolint:errcheck

	_, err := b.decodeImageURL(tmpFile)
	if err == nil {
		t.Fatal("expected local file path to be rejected when webhook.image_dir is unset")
	}
}

func TestDecodeImageURL_LocalPath_AllowedWithinImageDir(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(imgPath, []byte("fake-png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	b := &Backend{imageDir: dir}

	att, err := b.decodeImageURL(imgPath)
	if err != nil {
		t.Fatalf("expected a path inside image_dir to be allowed, got: %v", err)
	}
	if att.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", att.ContentType)
	}
	if att.Size != int64(len("fake-png-bytes")) {
		t.Errorf("size = %d, want %d", att.Size, len("fake-png-bytes"))
	}
	os.Remove(att.FilePath) //nolint:errcheck
}

func TestDecodeImageURL_LocalPath_RelativeJoinedUnderImageDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shot.gif"), []byte("gif-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	b := &Backend{imageDir: dir}

	att, err := b.decodeImageURL("shot.gif") // relative, not absolute
	if err != nil {
		t.Fatalf("expected a relative path to resolve under image_dir, got: %v", err)
	}
	if att.ContentType != "image/gif" {
		t.Errorf("content type = %q, want image/gif", att.ContentType)
	}
	os.Remove(att.FilePath) //nolint:errcheck
}

func TestDecodeImageURL_LocalPath_RejectsTraversalOutsideImageDir(t *testing.T) {
	dir := t.TempDir()
	b := &Backend{imageDir: dir}

	// A real file that exists, but outside image_dir -- must still be
	// rejected even though it's readable by the process.
	outside := filepath.Join(filepath.Dir(dir), "outside-secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside) //nolint:errcheck

	cases := []string{
		outside,
		filepath.Join(dir, "../outside-secret.png"),
		filepath.Join(dir, "..", "..", "etc", "passwd"),
	}
	for _, c := range cases {
		if _, err := b.decodeImageURL(c); err == nil {
			t.Errorf("expected %q to be rejected as outside image_dir", c)
		}
	}
}

func TestDecodeImageURL_LocalPath_RejectsSiblingDirectoryBypass(t *testing.T) {
	// The classic naive-prefix-check bypass: image_dir=/x/allowed,
	// attempted path=/x/allowed-evil/file -- a bare strings.HasPrefix
	// without the trailing separator would wrongly allow this.
	parent := t.TempDir()
	allowedDir := filepath.Join(parent, "allowed")
	evilDir := filepath.Join(parent, "allowed-evil")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evilDir, 0755); err != nil {
		t.Fatal(err)
	}
	evilFile := filepath.Join(evilDir, "x.png")
	if err := os.WriteFile(evilFile, []byte("evil"), 0644); err != nil {
		t.Fatal(err)
	}

	b := &Backend{imageDir: allowedDir}
	if _, err := b.decodeImageURL(evilFile); err == nil {
		t.Fatal("sibling-directory bypass must be rejected")
	}
}

// ---------------------------------------------------------------------
// E2E-style tests: the real HTTP handler, end to end (JSON decode, auth,
// image decode, channel delivery) -- no network socket needed since we
// call the handler directly with a real *http.Request/httptest.Recorder,
// same pattern the stdlib itself recommends for testing http.HandlerFunc.
// ---------------------------------------------------------------------

func postTask(t *testing.T, b *Backend, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(data))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	b.handleTask(rec, req)
	return rec
}

func newTestBackend(imageDir string) *Backend {
	return &Backend{msgs: make(chan messaging.Message, 4), imageDir: imageDir}
}

func TestHandleTask_NoTokenConfigured_WorksWithoutAuth(t *testing.T) {
	b := newTestBackend("")
	rec := postTask(t, b, "", map[string]any{"task": "write tests"})
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (no token configured means no auth required)", rec.Code)
	}
	select {
	case msg := <-b.msgs:
		if msg.Text != "write tests" || msg.Backend != "webhook" {
			t.Errorf("unexpected message: %+v", msg)
		}
		if len(msg.Attachments) != 0 {
			t.Errorf("expected no attachments, got %+v", msg.Attachments)
		}
	default:
		t.Fatal("expected a message to be delivered to the channel")
	}
}

func TestHandleTask_TokenConfigured_RejectsMissingOrWrongToken(t *testing.T) {
	b := newTestBackend("")
	b.token = "secret123"

	rec := postTask(t, b, "", map[string]any{"task": "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing token: status = %d, want 401", rec.Code)
	}
	rec = postTask(t, b, "wrong-token", map[string]any{"task": "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", rec.Code)
	}
	rec = postTask(t, b, "secret123", map[string]any{"task": "x"})
	if rec.Code != 200 {
		t.Errorf("correct token: status = %d, want 200", rec.Code)
	}
}

func TestHandleTask_ProjectDirPrefixedIntoText(t *testing.T) {
	// Pre-existing behavior, unrelated to this fix -- regression check
	// that it survived the refactor to a Backend method.
	b := newTestBackend("")
	postTask(t, b, "", map[string]any{"task": "do the thing", "project_dir": "/opt/myapp"})
	msg := <-b.msgs
	if msg.Text != "/opt/myapp: do the thing" {
		t.Errorf("text = %q, want %q", msg.Text, "/opt/myapp: do the thing")
	}
}

func TestHandleTask_MissingTaskField_Returns400(t *testing.T) {
	b := newTestBackend("")
	rec := postTask(t, b, "", map[string]any{"project_dir": "/x"})
	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleTask_ImageURL_DataURI_AttachesSuccessfully(t *testing.T) {
	png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	b := newTestBackend("") // image_dir unset -- must not matter for data: URIs
	rec := postTask(t, b, "", map[string]any{
		"task":      "look at this",
		"image_url": "data:image/png;base64," + png1x1,
	})
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	msg := <-b.msgs
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	defer os.Remove(msg.Attachments[0].FilePath) //nolint:errcheck
	if msg.Attachments[0].ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", msg.Attachments[0].ContentType)
	}
}

func TestHandleTask_ImageURL_LocalPathWithinImageDir_AttachesSuccessfully(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imgPath, []byte("png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	b := newTestBackend(dir)
	rec := postTask(t, b, "", map[string]any{"task": "x", "image_url": imgPath})
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	msg := <-b.msgs
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment for an in-scope local path, got %d", len(msg.Attachments))
	}
	os.Remove(msg.Attachments[0].FilePath) //nolint:errcheck
}

// This is the actual regression this whole fix exists for: a traversal
// attempt against a real, readable file outside image_dir must not be
// attached -- end to end, through the real HTTP handler, not just the
// decodeImageURL unit above.
func TestHandleTask_ImageURL_TraversalAttempt_SilentlyNotAttached(t *testing.T) {
	allowedDir := t.TempDir()
	secret := filepath.Join(filepath.Dir(allowedDir), "e2e-secret.txt")
	if err := os.WriteFile(secret, []byte("do not leak this"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(secret) //nolint:errcheck

	b := newTestBackend(allowedDir)
	traversal := filepath.Join(allowedDir, "..", filepath.Base(secret))
	rec := postTask(t, b, "", map[string]any{"task": "exfiltrate", "image_url": traversal})
	// The request itself still succeeds -- image decode failure doesn't
	// fail the whole task, it just omits the attachment (pre-existing
	// behavior, unchanged by this fix).
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	msg := <-b.msgs
	if len(msg.Attachments) != 0 {
		t.Fatalf("traversal attempt must not produce an attachment, got %+v", msg.Attachments)
	}
}

func TestHandleTask_ImageURL_LocalPath_DisabledByDefault_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "x.png")
	if err := os.WriteFile(imgPath, []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}
	b := newTestBackend("") // image_dir unset
	rec := postTask(t, b, "", map[string]any{"task": "x", "image_url": imgPath})
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	msg := <-b.msgs
	if len(msg.Attachments) != 0 {
		t.Fatalf("expected no attachment when image_dir is unset, got %+v", msg.Attachments)
	}
}

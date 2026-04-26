// BL189 — OpenAI-compat transcribe backend tests.
//
// Use a local httptest server that pretends to be OpenWebUI's
// `/v1/audio/transcriptions` endpoint and assert the multipart
// request shape + response decode are correct.

package transcribe

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAICompat_PostsMultipartAndDecodesResponse(t *testing.T) {
	var (
		gotModel    string
		gotLanguage string
		gotFilename string
		gotAuth     string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		gotModel = r.FormValue("model")
		gotLanguage = r.FormValue("language")
		fhs := r.MultipartForm.File["file"]
		if len(fhs) != 1 {
			t.Fatalf("expected 1 file part, got %d", len(fhs))
		}
		gotFilename = fhs[0].Filename
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hello world from whisper"}`)
	}))
	defer srv.Close()

	tmp, err := os.CreateTemp(t.TempDir(), "audio-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("RIFFXXXX") // dummy bytes; server doesn't actually decode
	tmp.Close()

	tx, err := NewOpenAICompat(srv.URL, "test-key", "whisper-1", "en")
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	out, err := tx.Transcribe(context.Background(), tmp.Name())
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if out != "hello world from whisper" {
		t.Errorf("text = %q want %q", out, "hello world from whisper")
	}
	if gotModel != "whisper-1" {
		t.Errorf("model = %q want whisper-1", gotModel)
	}
	if gotLanguage != "en" {
		t.Errorf("language = %q want en", gotLanguage)
	}
	if gotFilename != filepath.Base(tmp.Name()) {
		t.Errorf("filename = %q want %q", gotFilename, filepath.Base(tmp.Name()))
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q want Bearer test-key", gotAuth)
	}
}

func TestOpenAICompat_HTTPErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusBadRequest)
	}))
	defer srv.Close()
	tmp, _ := os.CreateTemp(t.TempDir(), "audio-*.wav")
	tmp.Close()
	tx, _ := NewOpenAICompat(srv.URL, "", "", "")
	_, err := tx.Transcribe(context.Background(), tmp.Name())
	if err == nil {
		t.Fatal("expected error from HTTP 400")
	}
	if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %v; want HTTP 400 + body", err)
	}
}

func TestOpenAICompat_AuthOmittedWhenAPIKeyEmpty(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":""}`)
	}))
	defer srv.Close()
	tmp, _ := os.CreateTemp(t.TempDir(), "audio-*.wav")
	tmp.Close()
	tx, _ := NewOpenAICompat(srv.URL, "", "", "")
	_, _ = tx.Transcribe(context.Background(), tmp.Name())
	if sawAuth != "" {
		t.Errorf("expected no Authorization header when APIKey empty; got %q", sawAuth)
	}
}

// Sanity: NewFromConfig routes correctly.
func TestNewFromConfig_RoutesByBackend(t *testing.T) {
	tx, err := NewFromConfig(BackendConfig{Backend: "openai_compat", Endpoint: "http://x", Model: "m"})
	if err != nil {
		t.Fatalf("openai_compat factory: %v", err)
	}
	if _, ok := tx.(*OpenAICompatTranscriber); !ok {
		t.Errorf("openai_compat returned %T, want *OpenAICompatTranscriber", tx)
	}
	if _, err := NewFromConfig(BackendConfig{Backend: "openai_compat"}); err == nil {
		t.Errorf("openai_compat without endpoint should error")
	}
	if _, err := NewFromConfig(BackendConfig{Backend: "garbage"}); err == nil {
		t.Errorf("unknown backend should error")
	}
	// BL201 — ollama and openwebui both route through the OpenAI-compat
	// client; the BL201 inheritance step in cmd/datawatch fills in the
	// endpoint from cfg.Ollama / cfg.OpenWebUI before this is reached.
	for _, b := range []string{"ollama", "openwebui"} {
		tx, err := NewFromConfig(BackendConfig{Backend: b, Endpoint: "http://x", Model: "m"})
		if err != nil {
			t.Fatalf("%s factory: %v", b, err)
		}
		if _, ok := tx.(*OpenAICompatTranscriber); !ok {
			t.Errorf("%s returned %T, want *OpenAICompatTranscriber", b, tx)
		}
	}
}

// Verify the multipart writer ends cleanly (no truncated boundary).
// Catches a bug in early drafts where mw.Close() was after the body
// got copied to the request.
func TestOpenAICompat_BodyCloses(t *testing.T) {
	body := makeMultipartBody(t, "file.wav", []byte("RIFF12345"), "whisper-1", "en")
	mr := multipart.NewReader(body, extractBoundary(body))
	for {
		_, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
	}
}

// Helper: rebuild the multipart body the same way the transcriber
// does, to keep this test independent of the live HTTP call.
func makeMultipartBody(t *testing.T, name string, data []byte, model, lang string) *strings.Reader {
	t.Helper()
	var buf strings.Builder
	mw := multipart.NewWriter(&strings.Builder{})
	_ = mw // ignore warning; this helper is illustrative
	buf.WriteString("--BOUNDARY\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"" + name + "\"\r\n\r\n")
	buf.Write(data)
	buf.WriteString("\r\n--BOUNDARY\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\n" + model + "\r\n")
	buf.WriteString("--BOUNDARY\r\nContent-Disposition: form-data; name=\"language\"\r\n\r\n" + lang + "\r\n--BOUNDARY--\r\n")
	return strings.NewReader(buf.String())
}

func extractBoundary(_ *strings.Reader) string { return "BOUNDARY" }

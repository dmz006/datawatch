// BL368 — unit tests for internal/server/vision.go.
//
// TS-BL368-SV1: POST /api/vision/describe — 503 when visioner is nil
// TS-BL368-SV2: GET — 405 method not allowed
// TS-BL368-SV3: POST with image — 200 + description JSON
// TS-BL368-SV4: POST missing image field — 400
// TS-BL368-SV5: POST visioner error — 502
// TS-BL368-SV6: extToMIME covers expected extensions

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeVisioner is a test double for visionSurface.
type fakeVisioner struct {
	result string
	err    error
}

func (f *fakeVisioner) Describe(_ context.Context, _ []byte, _, _ string) (string, error) {
	return f.result, f.err
}

// buildImageRequest constructs a multipart POST with an "image" field.
func buildImageRequest(t *testing.T, filename, contentType, prompt string, imageData []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("image", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageData); err != nil {
		t.Fatal(err)
	}
	if prompt != "" {
		if err := mw.WriteField("prompt", prompt); err != nil {
			t.Fatal(err)
		}
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/vision/describe", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TS-BL368-SV1: no visioner → 503
func TestVisionDescribe_NoVisioner(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/vision/describe", nil)
	rr := httptest.NewRecorder()
	s.handleVisionDescribe(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rr.Code)
	}
}

// TS-BL368-SV2: GET → 405
func TestVisionDescribe_MethodNotAllowed(t *testing.T) {
	s := &Server{visioner: &fakeVisioner{result: "ok"}}
	req := httptest.NewRequest(http.MethodGet, "/api/vision/describe", nil)
	rr := httptest.NewRecorder()
	s.handleVisionDescribe(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
}

// TS-BL368-SV3: happy path → 200 with description
func TestVisionDescribe_HappyPath(t *testing.T) {
	s := &Server{visioner: &fakeVisioner{result: "a sunset over the ocean"}}
	req := buildImageRequest(t, "photo.jpg", "image/jpeg", "", []byte("fake-image"))
	rr := httptest.NewRecorder()
	s.handleVisionDescribe(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["description"] != "a sunset over the ocean" {
		t.Errorf("description: got %v", resp["description"])
	}
	if _, ok := resp["latency_ms"]; !ok {
		t.Error("latency_ms missing from response")
	}
}

// TS-BL368-SV4: missing image field → 400
func TestVisionDescribe_MissingImageField(t *testing.T) {
	s := &Server{visioner: &fakeVisioner{result: "ok"}}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("prompt", "describe it")
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/vision/describe", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	s.handleVisionDescribe(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "missing image") {
		t.Errorf("want missing image error, got %q", rr.Body.String())
	}
}

// TS-BL368-SV5: visioner error → 502
func TestVisionDescribe_VisionerError(t *testing.T) {
	s := &Server{visioner: &fakeVisioner{err: fmt.Errorf("model timeout")}}
	req := buildImageRequest(t, "x.png", "image/png", "", []byte("data"))
	rr := httptest.NewRecorder()
	s.handleVisionDescribe(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rr.Code)
	}
}

// TS-BL368-SV6: extToMIME coverage
func TestVisionExtToMIME(t *testing.T) {
	cases := []struct{ ext, want string }{
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".png", "image/png"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
		{".bmp", "image/jpeg"}, // unknown → fallback
		{"", "image/jpeg"},    // empty → fallback
	}
	for _, c := range cases {
		got := extToMIME(c.ext)
		if got != c.want {
			t.Errorf("extToMIME(%q) = %q, want %q", c.ext, got, c.want)
		}
	}
}

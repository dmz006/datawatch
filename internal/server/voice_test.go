// Issue #2 — voice transcription endpoint tests.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeTranscriber returns canned output without touching Whisper.
type fakeTranscriber struct {
	out string
	err error
}

func (f *fakeTranscriber) Transcribe(_ context.Context, _ string) (string, error) {
	return f.out, f.err
}

func newAudioRequest(t *testing.T, body string, extra map[string]string) *http.Request {
	t.Helper()
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	part, _ := w.CreateFormFile("audio", "clip.ogg")
	_, _ = part.Write([]byte(body))
	for k, v := range extra {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestVoice_NoTranscriber_503(t *testing.T) {
	s := &Server{}
	req := newAudioRequest(t, "fake-opus", nil)
	rr := httptest.NewRecorder()
	s.handleVoiceTranscribe(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

func TestVoice_HappyPath_NoAction(t *testing.T) {
	s := &Server{transcriber: &fakeTranscriber{out: "hello world"}}
	req := newAudioRequest(t, "fake-opus", nil)
	rr := httptest.NewRecorder()
	s.handleVoiceTranscribe(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Transcript string `json:"transcript"`
		Action     string `json:"action"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Transcript != "hello world" {
		t.Errorf("transcript=%q want hello world", got.Transcript)
	}
	if got.Action != "none" {
		t.Errorf("action=%q want none (auto_exec not set)", got.Action)
	}
}

func TestVoice_AutoExec_RecognisesPrefixes(t *testing.T) {
	cases := []struct {
		transcript string
		want       string
	}{
		{"new: add a login page", "new"},
		{"reply: yes please continue", "reply"},
		{"status", "status"},
		{"status of the build", "status"},
		{"noop", "none"},
	}
	for _, c := range cases {
		s := &Server{transcriber: &fakeTranscriber{out: c.transcript}}
		req := newAudioRequest(t, "fake", map[string]string{"auto_exec": "1"})
		rr := httptest.NewRecorder()
		s.handleVoiceTranscribe(rr, req)
		var got struct{ Action string }
		_ = json.NewDecoder(rr.Body).Decode(&got)
		if got.Action != c.want {
			t.Errorf("%q → %q want %q", c.transcript, got.Action, c.want)
		}
	}
}

func TestVoice_TranscribeError_502(t *testing.T) {
	s := &Server{transcriber: &fakeTranscriber{err: errors.New("boom")}}
	req := newAudioRequest(t, "x", nil)
	rr := httptest.NewRecorder()
	s.handleVoiceTranscribe(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status=%d want 502", rr.Code)
	}
}

func TestVoice_MissingAudioField_400(t *testing.T) {
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	_ = w.WriteField("session_id", "x")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	s := &Server{transcriber: &fakeTranscriber{}}
	rr := httptest.NewRecorder()
	s.handleVoiceTranscribe(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestClassifyVoiceAction_AllPrefixes(t *testing.T) {
	if classifyVoiceAction("NEW: something") != "new" {
		t.Error("new prefix case-insensitive")
	}
	if classifyVoiceAction("") != "none" {
		t.Error("empty transcript")
	}
}

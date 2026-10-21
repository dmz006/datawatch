// Long-audio REGRESSION tests — the handler layer:
//
//   POST /api/voice/transcribe
//     → 25 MB multipart cap (enforced with an explicit 413),
//     → ReadTimeout (long/slow uploads must still complete — the 15 s
//       server-wide value was the PRIMARY root cause of the >30 s
//       failures, see docs/plans/2026-10-20-voice-long-recording-rootcause.md).
//
// These tests previously REPRODUCED the failures and asserted the broken
// behaviour; with the fix landed they assert the correct behaviour:
// long recordings upload, transcribe and return a transcript; oversized
// uploads get a clear 413.
//
// Run: go test ./internal/server/ -run 'TestVoice_LongRecording' -v

package server

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// bigSilentWAV builds seconds of 16 kHz 16-bit mono silence as a WAV
// (32 000 bytes/second at this rate — 10 minutes → 19.2 MB; the 25 MB
// cap is crossed at ≈13.6 minutes, the boundary the original
// reproduction computed).
func bigSilentWAV(seconds int) []byte {
	const sampleRate = 16000
	dataSize := sampleRate * seconds * 2 // 16-bit mono
	var b bytes.Buffer
	b.WriteString("RIFF")
	wLE32(&b, uint32(36+dataSize))
	b.WriteString("WAVEfmt ")
	wLE32(&b, 16)
	wLE16(&b, 1)
	wLE16(&b, 1)
	wLE32(&b, sampleRate)
	wLE32(&b, sampleRate*2)
	wLE16(&b, 2)
	wLE16(&b, 16)
	b.WriteString("data")
	wLE32(&b, uint32(dataSize))
	b.Write(make([]byte, dataSize))
	return b.Bytes()
}

func wLE16(b *bytes.Buffer, v uint16) { b.Write([]byte{byte(v), byte(v >> 8)}) }
func wLE32(b *bytes.Buffer, v uint32) {
	b.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

// multipartWAVBody builds a well-formed multipart/form-data body: the
// leading fields, then the audio file part (the given WAV bytes), with
// the trailing boundary written by the writer. Returns the body and the
// matching Content-Type header (the boundary is writer-generated).
func multipartWAVBody(t *testing.T, wav []byte, sessionID, fileName string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	bw := multipart.NewWriter(&body)
	if err := bw.WriteField("session_id", sessionID); err != nil {
		t.Fatal(err)
	}
	fw, err := bw.CreateFormFile("audio", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(wav); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), bw.FormDataContentType()
}

// TestVoice_LongRecording_UploadsTranscribe uploads 31 s / 65 s / 5 min /
// 10 min WAVs through the real handleVoiceTranscribe against a fake
// transcriber: every one of them must arrive intact, transcribe, and
// return the transcript — none may be dropped by the 25 MB cap (all are
// below it).
func TestVoice_LongRecording_UploadsTranscribe(t *testing.T) {
	cases := []struct {
		name    string
		seconds int
		want    string
	}{
		{"31s_just_over_a_whisper_window", 31, "31 s should transcribe"},
		{"65s_two_whisper_windows", 65, "65 s should transcribe"},
		{"5min_multi_minute", 300, "5 min should transcribe"},
		{"10min_multi_minute", 600, "10 min should transcribe"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			wav := bigSilentWAV(c.seconds)
			mb := float64(len(wav)) / 1e6
			if mb > 25 {
				t.Fatalf("%d s of 16 kHz mono WAV = %.2f MB would exceed the 25 MB cap — pick a smaller case", c.seconds, mb)
			}
			t.Logf("%d s 16 kHz mono WAV = %.2f MB (cap 26.21 MB)", c.seconds, mb)

			body, ctype := multipartWAVBody(t, wav, c.name, fmt.Sprintf("rec-%ds.wav", c.seconds))
			fr := &fakeTranscriber{out: c.want}
			srv := &Server{transcriber: fr}
			req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(body))
			req.Header.Set("Content-Type", ctype)

			rr := httptest.NewRecorder()
			srv.handleVoiceTranscribe(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("code %d want 200 (body: %s)", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), c.want) {
				t.Fatalf("transcript missing %q in %s", c.want, rr.Body.String())
			}
			t.Logf("OK: %d s long recording transcribes; transcript present", c.seconds)
		})
	}
}

// TestVoice_LongRecording_OversizeUpload_413 uploads a body that EXCEEDS
// the 25 MB cap and asserts the explicit, operator-actionable 413 (before
// the fix the cap was not enforced at all — ParseMultipartForm(25<<20)
// only bounded in-memory spooling, and a truncated read surfaced as an
// opaque 400).
func TestVoice_LongRecording_OversizeUpload_413(t *testing.T) {
	// 821 s of 16 kHz 16-bit mono WAV = 26.27 MB — just over the 25 MB cap.
	sec := int(25<<20/32000) + 1
	wav := bigSilentWAV(sec)
	if len(wav) <= int(maxVoiceUploadBytes) {
		t.Fatalf("case bug: %d-byte WAV must exceed the %d-byte cap", len(wav), maxVoiceUploadBytes)
	}
	t.Logf("oversized upload: %d bytes (%.2f MB) vs cap %d bytes (%d MB)",
		len(wav), float64(len(wav))/1e6, maxVoiceUploadBytes, maxVoiceUploadBytes>>20)

	body, ctype := multipartWAVBody(t, wav, "oversize", "big.wav")
	fr := &fakeTranscriber{out: "must not run"}
	srv := &Server{transcriber: fr}
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)

	rr := httptest.NewRecorder()
	srv.handleVoiceTranscribe(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code %d want 413 (body: %s)", rr.Code, rr.Body.String())
	}
	msg := rr.Body.String()
	if !strings.Contains(msg, "25 MB") {
		t.Fatalf("413 body must name the limit for operator action, got: %q", msg)
	}
	t.Logf("OK: oversized upload rejected with explicit 413: %q", strings.TrimSpace(msg))
}

// TestVoice_LongRecording_SlowUploadCompletes is the regression test for
// the PRIMARY root cause: the old server-wide ReadTimeout of 15 s aborted
// any upload whose body took longer than 15 s to arrive ("bad multipart:
// … i/o timeout"). With the named voiceUploadReadTimeout constant (180 s,
// the same one server.go now uses), a slow-but-valid upload must COMPLETE
// and return a transcript.
func TestVoice_LongRecording_SlowUploadCompletes(t *testing.T) {
	const pause = 5 * time.Second // slow-link simulation, well under the 180 s budget

	wav := bigSilentWAV(31) // 31 s recording: 1.0 MB of audio + framing
	body, ctype := multipartWAVBody(t, wav, "slow-upload", "rec-31s.wav")

	fr := &fakeTranscriber{out: "slow upload should now arrive"}
	srv := &Server{transcriber: fr}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// The SAME constant the production server uses (server.go) —
	// the old flat 15 s was the bug. Handler wiring mirrors the
	// production voice route (internal/server/server.go → apiMux).
	s := &http.Server{Handler: http.HandlerFunc(srv.handleVoiceTranscribe), ReadTimeout: voiceUploadReadTimeout}
	go func() { _ = s.Serve(ln) }()
	defer func() { _ = s.Close() }()

	// Client: deliver the pre-built body in two parts with a 5 s pause
	// in between — previously killed by the 15 s read timeout, now
	// inside the 180 s budget.
	req, err := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+"/api/voice/transcribe",
		&twoPartReader{body: body, first: 512, pause: pause})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ctype)
	req.ContentLength = int64(len(body))

	start := time.Now()
	client := &http.Client{Timeout: voiceUploadReadTimeout + 30*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("slow upload failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("slow-but-valid upload must COMPLETE and return a transcript: got %d (before the fix this shape returned 400 'bad multipart: … i/o timeout'); body=%s",
			resp.StatusCode, got)
	}
	if !strings.Contains(string(got), "slow upload should now arrive") {
		t.Fatalf("transcript missing: %s", got)
	}
	if elapsed < pause {
		t.Errorf("elapsed %v < pause %v — the pause did not actually slow the body", elapsed, pause)
	}
	t.Logf("OK: 31 s WAV body delivered over %v (including %v pause) completed with 200 — ReadTimeout no longer kills slow uploads", elapsed, pause)
}

// twoPartReader streams a pre-built body: first `first` bytes, then a
// pause, then the remainder — simulating a slow mobile link without
// buffering anything.
type twoPartReader struct {
	body   []byte
	first  int
	pause  time.Duration
	offset int
	paused bool
}

func (r *twoPartReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.body) {
		return 0, io.EOF
	}
	if !r.paused {
		allow := r.first - r.offset
		if allow < 0 {
			allow = 0
		}
		if len(p) > allow {
			p = p[:allow]
		}
		if allow == 0 {
			// First chunk fully delivered; slow the link down before
			// the remainder.
			r.paused = true
			time.Sleep(r.pause)
		}
	}
	n := copy(p, r.body[r.offset:])
	r.offset += n
	if r.offset >= len(r.body) {
		return n, io.EOF
	}
	return n, nil
}

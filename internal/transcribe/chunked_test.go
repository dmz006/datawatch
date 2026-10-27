package transcribe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// constantWAV16k builds seconds of mono 16 kHz 16-bit PCM WAV at a constant
// non-silent level, so the silence-aware splitter cuts on exact window
// boundaries.
func constantWAV16k(seconds int) []byte {
	const sampleRate = 16000
	data := make([]byte, sampleRate*seconds*2)
	for i := 0; i < len(data); i += 2 {
		binary.LittleEndian.PutUint16(data[i:], 8000)
	}
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	buf.Write(le32(uint32(36 + len(data))))
	buf.WriteString("WAVEfmt ")
	buf.Write(le32(16))
	buf.Write(le16(1))
	buf.Write(le16(1))
	buf.Write(le32(sampleRate))
	buf.Write(le32(sampleRate * 2))
	buf.Write(le16(2))
	buf.Write(le16(16))
	buf.WriteString("data")
	buf.Write(le32(uint32(len(data))))
	buf.Write(data)
	return buf.Bytes()
}

// stubInner records every path it is given and the highest number of
// concurrent calls. Call i returns texts[i] (or "slice-i" past the list).
type stubInner struct {
	mu        sync.Mutex
	paths     []string
	durations []float64
	texts     []string
	failAt    int // 1-based call number that fails; 0 = never
	failErr   error
	onCall    func(call int)

	active, maxActive int32
}

func (s *stubInner) Transcribe(ctx context.Context, audioPath string) (string, error) {
	cur := atomic.AddInt32(&s.active, 1)
	defer atomic.AddInt32(&s.active, -1)
	for {
		m := atomic.LoadInt32(&s.maxActive)
		if cur <= m || atomic.CompareAndSwapInt32(&s.maxActive, m, cur) {
			break
		}
	}
	s.mu.Lock()
	s.paths = append(s.paths, audioPath)
	call := len(s.paths)
	dur := -1.0
	if b, err := os.ReadFile(audioPath); err == nil {
		dur = wavDurationSeconds(b)
	}
	s.durations = append(s.durations, dur)
	s.mu.Unlock()
	if s.onCall != nil {
		s.onCall(call)
	}
	if s.failAt == call {
		return "", s.failErr
	}
	if call-1 < len(s.texts) {
		return s.texts[call-1], nil
	}
	return fmt.Sprintf("slice-%d", call-1), nil
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logSink) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "")
}

func newChunkedForTest(inner Transcriber, chunk time.Duration) (*ChunkedTranscriber, *logSink) {
	sink := &logSink{}
	c := NewChunked(inner, chunk)
	c.Logf = sink.logf
	return c, sink
}

func TestChunked_SplitsLongWAVSequentially(t *testing.T) {
	path := writeWavFile(t, "clip.wav", constantWAV16k(100))
	stub := &stubInner{texts: []string{" one ", "two", "", "four\n"}}
	c, sink := newChunkedForTest(stub, 30*time.Second)

	got, err := c.Transcribe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "one two four"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if len(stub.paths) != 4 {
		t.Fatalf("calls = %d, want 4 (%v)", len(stub.paths), stub.paths)
	}
	if stub.maxActive != 1 {
		t.Fatalf("max concurrent calls = %d, want 1", stub.maxActive)
	}
	for i, d := range stub.durations {
		if d <= 0 || d > 30.001 {
			t.Errorf("slice %d duration = %.3fs, want in (0, 30]", i, d)
		}
		if stub.paths[i] == path {
			t.Errorf("slice %d was the original file", i)
		}
	}
	if !strings.Contains(sink.joined(), "[voice] chunked: 4 slices of <=30s") {
		t.Errorf("missing chunked log, got %q", sink.joined())
	}
	if n := strings.Count(sink.joined(), "chunked:"); n != 1 {
		t.Errorf("chunked log lines = %d, want 1", n)
	}
}

func TestChunked_ShortAudioSendsOriginal(t *testing.T) {
	path := writeWavFile(t, "short.wav", constantWAV16k(10))
	stub := &stubInner{texts: []string{"hello"}}
	c, _ := newChunkedForTest(stub, 30*time.Second)

	got, err := c.Transcribe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("text = %q", got)
	}
	if len(stub.paths) != 1 || stub.paths[0] != path {
		t.Fatalf("calls = %v, want one call with %q", stub.paths, path)
	}
}

func TestChunked_ZeroChunkDelegates(t *testing.T) {
	path := writeWavFile(t, "long.wav", constantWAV16k(100))
	stub := &stubInner{texts: []string{"whole"}}
	c, sink := newChunkedForTest(stub, 0)

	got, err := c.Transcribe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "whole" || len(stub.paths) != 1 || stub.paths[0] != path {
		t.Fatalf("got %q, calls %v", got, stub.paths)
	}
	if sink.joined() != "" {
		t.Errorf("unexpected log output %q", sink.joined())
	}
}

func TestChunked_NilInnerIsAnError(t *testing.T) {
	c, _ := newChunkedForTest(nil, 30*time.Second)
	if _, err := c.Transcribe(context.Background(), "x.wav"); err == nil {
		t.Fatal("want an error when there is no inner transcriber")
	}
}

func TestChunked_NonWAVWithoutFFmpegFallsBack(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "clip.webm")
	if err := os.WriteFile(path, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := &stubInner{texts: []string{"whole file"}}
	c, sink := newChunkedForTest(stub, 30*time.Second)

	got, err := c.Transcribe(context.Background(), path)
	if err != nil {
		t.Fatalf("a missing ffmpeg must not fail the request: %v", err)
	}
	if got != "whole file" || len(stub.paths) != 1 || stub.paths[0] != path {
		t.Fatalf("got %q, calls %v", got, stub.paths)
	}
	if !strings.Contains(sink.joined(), "chunking skipped") {
		t.Errorf("missing skip log, got %q", sink.joined())
	}
}

// writeFakeFFmpeg installs a shell script named ffmpeg that creates three
// segment files by filling %05d in its last argument.
func writeFakeFFmpeg(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
for a in "$@"; do last="$a"; done
for i in 0 1 2; do
  f=$(printf "$last" "$i")
  echo "segment $i" > "$f"
done
`
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestChunked_NonWAVUsesFFmpegSegments(t *testing.T) {
	writeFakeFFmpeg(t)
	path := filepath.Join(t.TempDir(), "clip.webm")
	if err := os.WriteFile(path, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := &stubInner{texts: []string{"a", "b", "c"}}
	c, _ := newChunkedForTest(stub, 30*time.Second)

	got, err := c.Transcribe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a b c" {
		t.Fatalf("text = %q", got)
	}
	if len(stub.paths) != 3 {
		t.Fatalf("calls = %d, want 3", len(stub.paths))
	}
	for i, p := range stub.paths {
		if want := fmt.Sprintf("seg-%05d.webm", i); filepath.Base(p) != want {
			t.Errorf("call %d got %q, want %q", i, filepath.Base(p), want)
		}
	}
	if stub.maxActive != 1 {
		t.Errorf("max concurrent calls = %d, want 1", stub.maxActive)
	}
}

var errSliceBackend = errors.New("backend exploded")

func TestChunked_SliceFailureKeepsCauseAndStops(t *testing.T) {
	path := writeWavFile(t, "clip.wav", constantWAV16k(100))
	stub := &stubInner{failAt: 2, failErr: errSliceBackend}
	c, _ := newChunkedForTest(stub, 30*time.Second)

	_, err := c.Transcribe(context.Background(), path)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "slice 2 of 4") {
		t.Errorf("error %q does not name the failed slice", err)
	}
	if !errors.Is(err, errSliceBackend) {
		t.Errorf("errors.Is lost the original cause: %v", err)
	}
	if len(stub.paths) != 2 {
		t.Errorf("calls = %d, want 2 (no slice after the failure)", len(stub.paths))
	}
}

func TestChunked_CancelledContextStopsBetweenSlices(t *testing.T) {
	path := writeWavFile(t, "clip.wav", constantWAV16k(100))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stub := &stubInner{onCall: func(call int) {
		if call == 2 {
			cancel()
		}
	}}
	c, _ := newChunkedForTest(stub, 30*time.Second)

	_, err := c.Transcribe(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(stub.paths) != 2 {
		t.Errorf("calls = %d, want 2", len(stub.paths))
	}
}

func TestChunked_RemovesChunkTempDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	path := writeWavFile(t, "clip.wav", constantWAV16k(100))

	stub := &stubInner{failAt: 3, failErr: errSliceBackend}
	c, _ := newChunkedForTest(stub, 30*time.Second)
	_, _ = c.Transcribe(context.Background(), path) // failure path must clean up too
	if _, err := c.Transcribe(context.Background(), path); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("leftover temp entry %q", e.Name())
	}
}

type preflightInner struct {
	stubInner
	err    error
	called bool
}

func (p *preflightInner) Preflight(ctx context.Context) error {
	p.called = true
	return p.err
}

func TestChunked_PreflightDelegates(t *testing.T) {
	want := errors.New("unreachable")
	inner := &preflightInner{err: want}
	c, _ := newChunkedForTest(inner, 30*time.Second)
	if err := c.Preflight(context.Background()); !errors.Is(err, want) || !inner.called {
		t.Fatalf("err = %v, called = %v", err, inner.called)
	}

	plain, _ := newChunkedForTest(&stubInner{}, 30*time.Second)
	if err := plain.Preflight(context.Background()); err != nil {
		t.Fatalf("no-preflight inner returned %v", err)
	}
}

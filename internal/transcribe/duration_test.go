package transcribe

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pcmWAVWithSamples builds a mono 16-bit PCM WAV of exactly the given number of
// samples at the given rate.
func pcmWAVWithSamples(sampleRate uint32, samples int) []byte {
	data := make([]byte, samples*2)
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

func writeDurationTestFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeFFprobe puts a stub ffprobe that prints out first on PATH.
func fakeFFprobe(t *testing.T, out string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "ffprobe"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestAudioDuration_PCMWav(t *testing.T) {
	p := writeDurationTestFile(t, "clip.wav", pcmWAVWithSamples(16000, 200000)) // 12.5 s
	d, ok := AudioDuration(context.Background(), p)
	if !ok || d != 12500*time.Millisecond {
		t.Fatalf("got (%v, %v), want (12.5s, true)", d, ok)
	}
}

func TestAudioDuration_PCMWavNeedsNoFFprobe(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	p := writeDurationTestFile(t, "clip.wav", pcmWAVWithSamples(16000, 200000))
	if d, ok := AudioDuration(context.Background(), p); !ok || d != 12500*time.Millisecond {
		t.Fatalf("got (%v, %v), want (12.5s, true)", d, ok)
	}
}

func TestAudioDuration_TruncatedAndGarbage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	wav := pcmWAVWithSamples(16000, 200000)
	cases := map[string][]byte{
		"truncated.wav": wav[:30],
		"garbage.bin":   []byte("this is not audio at all"),
		"empty.wav":     {},
	}
	for name, b := range cases {
		p := writeDurationTestFile(t, name, b)
		if d, ok := AudioDuration(context.Background(), p); ok || d != 0 {
			t.Errorf("%s: got (%v, %v), want (0, false)", name, d, ok)
		}
	}
	if d, ok := AudioDuration(context.Background(), filepath.Join(t.TempDir(), "missing.wav")); ok || d != 0 {
		t.Errorf("missing file: got (%v, %v), want (0, false)", d, ok)
	}
}

func TestAudioDuration_FFprobeNonWAV(t *testing.T) {
	fakeFFprobe(t, "42.25")
	p := writeDurationTestFile(t, "clip.webm", []byte("not a wav container"))
	d, ok := AudioDuration(context.Background(), p)
	if !ok || d != 42250*time.Millisecond {
		t.Fatalf("got (%v, %v), want (42.25s, true)", d, ok)
	}
}

func TestAudioDuration_FFprobeUnknownValues(t *testing.T) {
	for _, out := range []string{"N/A", "", "0", "-3.5", "abc"} {
		fakeFFprobe(t, out)
		p := writeDurationTestFile(t, "clip.webm", []byte("not a wav container"))
		if d, ok := AudioDuration(context.Background(), p); ok || d != 0 {
			t.Errorf("ffprobe %q: got (%v, %v), want (0, false)", out, d, ok)
		}
	}
}

func TestAudioDuration_NoFFprobeOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	p := writeDurationTestFile(t, "clip.webm", []byte("not a wav container"))
	if d, ok := AudioDuration(context.Background(), p); ok || d != 0 {
		t.Fatalf("got (%v, %v), want (0, false)", d, ok)
	}
}

func TestAudioDuration_CancelledContext(t *testing.T) {
	fakeFFprobe(t, "42.25")
	p := writeDurationTestFile(t, "clip.webm", []byte("not a wav container"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d, ok := AudioDuration(ctx, p); ok || d != 0 {
		t.Fatalf("got (%v, %v), want (0, false)", d, ok)
	}
}

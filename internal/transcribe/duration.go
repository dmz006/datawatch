package transcribe

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// AudioDuration returns the length of the clip at path when it can be
// determined cheaply, and (0, false) otherwise. It never fails the
// request: any read, parse or tool error simply means "unknown".
//
// A PCM WAV (the 16 kHz mono file the voice handler produces after its
// ffmpeg transcode) is measured straight from its header and data chunk,
// with no external tool. Any other container is measured with ffprobe
// when it is on PATH. Browser MediaRecorder webm often reports N/A for
// its duration; that, like an empty or non-positive answer, is unknown.
func AudioDuration(ctx context.Context, path string) (time.Duration, bool) {
	if ctx.Err() != nil {
		return 0, false
	}
	if d, ok := wavDuration(path); ok {
		return d, true
	}
	return ffprobeDuration(ctx, path)
}

// wavDuration measures a PCM WAV file from its fmt and data chunks.
// Non-WAV files are rejected after reading only the RIFF header so a
// large webm/ogg is never loaded into memory.
func wavDuration(path string) (time.Duration, bool) {
	f, err := os.Open(path) // #nosec G304 -- caller-supplied clip path, read-only
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	var head [12]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return 0, false
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return 0, false
	}
	b, err := os.ReadFile(path) // #nosec G304 -- same path as above
	if err != nil {
		return 0, false
	}
	p, ok := parsePCM16WAV(b)
	if !ok {
		return 0, false
	}
	bytesPerSecond := int64(p.sampleRate) * int64(p.bytesPerFrame)
	if bytesPerSecond <= 0 {
		return 0, false
	}
	d := time.Duration(int64(p.dataSize) * int64(time.Second) / bytesPerSecond)
	if d <= 0 {
		return 0, false
	}
	return d, true
}

// ffprobeDuration asks ffprobe for the container duration in seconds.
func ffprobeDuration(ctx context.Context, path string) (time.Duration, bool) {
	bin, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, false
	}
	// #nosec G204 -- fixed binary found on PATH; path is a single argv element
	out, err := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		path,
	).Output()
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(out))
	if s == "" || strings.EqualFold(s, "N/A") {
		return 0, false
	}
	secs, err := strconv.ParseFloat(s, 64)
	if err != nil || secs <= 0 || secs != secs || secs > float64(1<<40) {
		return 0, false
	}
	return time.Duration(secs * float64(time.Second)), true
}

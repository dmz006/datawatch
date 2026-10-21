// Long-audio window splitting (root-cause fix, see
// docs/plans/2026-10-20-voice-long-recording-rootcause.md).
//
// Whisper is trained on 30 s windows. Full-length OpenAI-style backends
// (OpenAI, OpenWebUI) accept multi-minute audio in one request, but a
// bare windowed engine (whisper.cpp server mode, a raw
// faster-whisper-server, or a proxy that caps request duration) rejects
// anything over its window with HTTP 400. Rather than hard-failing,
// openai_compat.go splits the recording locally into ≤ window chunks,
// transcribes each chunk in order, and joins the texts deterministically.
//
// Splitting strategy:
//   - 30 s windows (or the window the server reports in its rejection,
//     when it says so explicitly).
//   - Silence-aware boundaries: within the last quarter of each window
//     the splitter prefers the first near-silent sample as the cut
//     point, instead of always cutting dead-centre of a word. Cutting
//     at silence is strictly better than overlapping chunks (overlap
//     makes Whisper transcribe the shared audio twice and duplicates
//     words in the joined text), so no overlap is applied.
//   - Deterministic: same input bytes → identical chunk cut points and
//     file contents.
//
// PCM WAV input (the shape internal/server/voice.go hands to
// transcribers after its ffmpeg transcode step) is split in pure Go.
// Any other container is split with ffmpeg (`-f segment`), when present.

package transcribe

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultAudioWindow is Whisper's native window size. Backends that
// reject long audio almost always enforce exactly this.
const DefaultAudioWindow = time.Duration(DefaultAudioWindowSeconds) * time.Second

// DefaultAudioWindowSeconds — 30 s (Whisper's training window).
const DefaultAudioWindowSeconds = 30

// silenceCutFraction bounds how early within a window a silence-aware
// cut may land: cuts are searched in
// [prevCut + f*window, prevCut + window], which keeps every chunk at
// ≥ 3/4 of the window (no pathologically short tail chunks) and ≤ the
// window (backend compliance).
const silenceCutFraction = 3.0 / 4.0

// silenceThresholdFraction — a sample is "silent" when its magnitude is
// below this fraction of full scale (≈1 %), i.e. effectively noise floor.
const silenceThresholdFraction = 1.0 / 100.0

// splitAudioLong splits the audio at audioPath into chunks of at most
// `window` duration.
//
// Returns:
//   - files: chunk temp-file paths in chronological order (nil when no
//     split was needed — caller sends the original file);
//   - cleanup: removes the chunk temp dir (nil when there is nothing to
//     remove);
//   - err: non-nil only when the audio COULD NOT be split (not a PCM
//     WAV and no ffmpeg on PATH). The caller must surface the original
//     backend rejection in that case, never swallow it.
func splitAudioLong(ctx context.Context, audioPath string, window time.Duration) (files []string, cleanup func(), err error) {
	if window <= 0 {
		window = DefaultAudioWindow
	}
	raw, rerr := os.ReadFile(audioPath)
	if rerr != nil {
		return nil, nil, fmt.Errorf("transcribe(split): read audio: %w", rerr)
	}

	chunks, ok := splitWAVPCM(raw, window)
	if !ok {
		// Not a splittable PCM WAV — try ffmpeg for the real container
		// (webm/opus, m4a, …). ffmpeg itself returns (nil, nil, nil)
		// when there was only one segment (nothing to split).
		return splitWithFFmpeg(ctx, audioPath, window)
	}

	dir, derr := os.MkdirTemp("", "dw-audio-chunks-")
	if derr != nil {
		return nil, nil, fmt.Errorf("transcribe(split): temp dir: %w", derr)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	for i, c := range chunks {
		p := filepath.Join(dir, fmt.Sprintf("chunk-%05d.wav", i))
		if werr := os.WriteFile(p, c, 0o644); werr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("transcribe(split): write chunk: %w", werr)
		}
		files = append(files, p)
	}
	return files, cleanup, nil
}

// wavParams is the subset of the WAV fmt chunk needed to split.
type wavParams struct {
	sampleRate    uint32
	channels      uint16
	bitsPerSample uint16
	dataOffset    int // byte offset of the first data byte
	dataSize      int // length of the data chunk
	bytesPerFrame int // channels * bitsPerSample/8
}

// parsePCM16WAV parses a 16-bit or 32-bit integer PCM mono/stereo WAV
// (the exact shapes our stack produces). Returns false when the bytes
// are not a PCM WAV we can safely re-contain.
func parsePCM16WAV(b []byte) (wavParams, bool) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return wavParams{}, false
	}
	var p wavParams
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		sz := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := pos + 8
		switch id {
		case "fmt ":
			if body+16 <= len(b) && sz >= 16 {
				code := binary.LittleEndian.Uint16(b[body : body+2])
				if code != 1 { // PCM only — float/IMA-ADPCM/…. aren't re-containable
					return wavParams{}, false
				}
				p.channels = binary.LittleEndian.Uint16(b[body+2 : body+4])
				p.sampleRate = binary.LittleEndian.Uint32(b[body+4 : body+8])
				p.bitsPerSample = binary.LittleEndian.Uint16(b[body+14 : body+16])
			} else {
				return wavParams{}, false
			}
		case "data":
			if p.dataSize == 0 && body <= len(b) {
				p.dataOffset = body
				if body+sz > len(b) {
					sz = len(b) - body
				}
				p.dataSize = sz
			}
		}
		next := sz
		if next&1 == 1 {
			next++ // chunks are word-aligned
		}
		pos += 8 + next
		if next == 0 { // malformed; stop
			break
		}
	}
	if p.sampleRate == 0 || p.channels == 0 || p.bitsPerSample == 0 || p.dataSize == 0 ||
		p.dataOffset+p.dataSize > len(b) {
		return wavParams{}, false
	}
	switch p.bitsPerSample {
	case 8, 16, 24, 32:
	default:
		return wavParams{}, false
	}
	p.bytesPerFrame = int(p.channels) * int(p.bitsPerSample) / 8
	if p.bytesPerFrame == 0 || p.dataSize%p.bytesPerFrame != 0 {
		return wavParams{}, false
	}
	return p, true
}

func frameIsSilent(data []byte, byteOffset int, bits uint16, threshold int) bool {
	if byteOffset+int(bits/8) > len(data) {
		return true
	}
	// One representative sample (first channel) — comparing every
	// interleaved channel adds no signal for a near-silence check.
	switch bits {
	case 8:
		v := int(int8(data[byteOffset]))
		return abs(v) <= threshold
	case 16:
		return int(binary.LittleEndian.Uint16(data[byteOffset:byteOffset+2])) <= threshold
	case 24:
		v := int32(int8(data[byteOffset]))<<16 |
			int32(data[byteOffset+1])<<8 |
			int32(data[byteOffset+2])
		return abs(int(v)) <= threshold
	case 32:
		v := int64(int32(binary.LittleEndian.Uint32(data[byteOffset : byteOffset+4])))
		return v <= int64(threshold)
	}
	return false
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// splitWAVPCM splits a PCM WAV into chunks of at most `window`, with
// silence-aware cut points (see package comment). Returns (nil, true)
// when the audio already fits within one window — nothing to split.
func splitWAVPCM(raw []byte, window time.Duration) ([][]byte, bool) {
	p, ok := parsePCM16WAV(raw)
	if !ok {
		return nil, false
	}
	data := raw[p.dataOffset : p.dataOffset+p.dataSize]
	totalFrames := p.dataSize / p.bytesPerFrame
	windowFrames := int(float64(window.Seconds()) * float64(p.sampleRate))
	if windowFrames <= 0 || totalFrames <= windowFrames {
		return nil, true // fits in one window — no split needed
	}
	threshold := int(float64(uint64(1)<<uint(p.bitsPerSample-1)) * silenceThresholdFraction)
	if threshold < 1 {
		threshold = 1
	}

	type cut struct{ start, end int } // frame offsets
	var cuts []cut
	start := 0
	for totalFrames-start > windowFrames {
		upper := start + windowFrames // chunk end may not exceed this
		lower := int(float64(start) + silenceCutFraction*float64(windowFrames))
		target := upper
		// Silence-aware: first near-silent sample in the last quarter
		// of the window, else the exact window boundary.
		for f := lower; f < upper; f++ {
			if frameIsSilent(data, f*p.bytesPerFrame, p.bitsPerSample, threshold) {
				target = f
				break
			}
		}
		cuts = append(cuts, cut{start, target})
		start = target
		if start >= totalFrames {
			break
		}
	}
	cuts = append(cuts, cut{start, totalFrames}) // final (possibly short) chunk

	out := make([][]byte, 0, len(cuts))
	for _, c := range cuts {
		chunkData := data[c.start*p.bytesPerFrame : c.end*p.bytesPerFrame]
		out = append(out, buildWAV(raw[:p.dataOffset], p, chunkData))
	}
	return out, true
}

// buildWAV assembles a minimal canonical RIFF/WAVE (one fmt + one data
// chunk) carrying the given PCM data. The original fmt parameters are
// preserved so the chunk audibly matches the source sample exactly.
func buildWAV(fmtHeader []byte, p wavParams, chunkData []byte) []byte {
	_ = fmtHeader // original extra chunks (LIST/INFO) don't travel
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	buf.Write(wavLE32(uint32(36 + len(chunkData))))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	buf.Write(wavLE32(16))
	buf.Write(wavLE16(1)) // PCM
	buf.Write(wavLE16(p.channels))
	buf.Write(wavLE32(p.sampleRate))
	buf.Write(wavLE32(p.sampleRate * uint32(p.bytesPerFrame)))
	buf.Write(wavLE16(uint16(p.bytesPerFrame)))
	buf.Write(wavLE16(p.bitsPerSample))
	buf.WriteString("data")
	buf.Write(wavLE32(uint32(len(chunkData))))
	buf.Write(chunkData)
	return buf.Bytes()
}

func wavLE16(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }

func wavLE32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

// splitWithFFmpeg segments a non-PCM container (webm/opus, m4a, …)
// into ≤ window files using ffmpeg's segment muxer. Stream-copy first;
// re-encode if the container doesn't support it. Returns (nil, nil,
// nil) when ffmpeg produced a single segment (nothing to split).
func splitWithFFmpeg(ctx context.Context, audioPath string, window time.Duration) ([]string, func(), error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, nil, fmt.Errorf("transcribe(split): %q is not a PCM WAV and ffmpeg is not on PATH — cannot split into ≤%v chunks", audioPath, window)
	}
	dir, err := os.MkdirTemp("", "dw-audio-segments-")
	if err != nil {
		return nil, nil, fmt.Errorf("transcribe(split): temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	ext := strings.TrimPrefix(filepath.Ext(audioPath), ".")
	if ext == "" {
		ext = "webm"
	}
	pattern := filepath.Join(dir, "seg-%05d."+ext)
	secs := max(1, int(window.Seconds()))

	run := func(args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y")
		c.Args = append(c.Args, args...)
		return c.CombinedOutput()
	}
	if out, err := run("-i", audioPath, "-f", "segment", "-segment_time", strconv.Itoa(secs),
		"-reset_timestamps", "1", "-c", "copy", pattern); err != nil {
		if out2, err2 := run("-i", audioPath, "-f", "segment", "-segment_time", strconv.Itoa(secs),
			"-reset_timestamps", "1", pattern); err2 != nil {
			cleanup()
			return nil, nil, fmt.Errorf("transcribe(split): ffmpeg: %w: %s; retry: %s", err2, out, out2)
		}
	}

	entries, lerr := os.ReadDir(dir)
	if lerr != nil {
		cleanup()
		return nil, nil, fmt.Errorf("transcribe(split): read segments: %w", lerr)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "seg-") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	if len(files) <= 1 {
		cleanup()
		return nil, nil, nil // single segment — nothing to split
	}
	return files, cleanup, nil
}

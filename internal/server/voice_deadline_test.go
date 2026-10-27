package server

import (
	"context"
	"testing"
	"time"
)

func TestVoiceDeadline_TranscribeTimeout(t *testing.T) {
	cases := []struct {
		name  string
		audio time.Duration
		known bool
		want  time.Duration
	}{
		{"unknown duration", 0, false, 5 * time.Minute},
		{"unknown ignores length", 3 * time.Hour, false, 5 * time.Minute},
		{"10 seconds", 10 * time.Second, true, 5 * time.Minute},
		{"150 seconds is the crossover", 150 * time.Second, true, 5 * time.Minute},
		{"180 seconds doubles", 180 * time.Second, true, 360 * time.Second},
		{"3 hours is capped", 3 * time.Hour, true, 30 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := voiceTranscribeTimeout(tc.audio, tc.known); got != tc.want {
				t.Errorf("voiceTranscribeTimeout(%v, %v) = %v, want %v", tc.audio, tc.known, got, tc.want)
			}
		})
	}
}

// recordedDeadline serves one upload through a stub transcriber and returns
// how far ahead of now the transcribe context deadline was.
func recordedDeadline(t *testing.T, body string) time.Duration {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no ffmpeg/ffprobe: the upload reaches the transcriber unchanged
	var deadline time.Time
	var hasDeadline bool
	s := &Server{transcriber: funcTranscriber(func(ctx context.Context, _ string) (string, error) {
		deadline, hasDeadline = ctx.Deadline()
		return "ok", nil
	})}
	rr := serveVoice(t, s, newAudioRequest(t, body, nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !hasDeadline {
		t.Fatal("transcribe context has no deadline")
	}
	return time.Until(deadline)
}

func within(got, want, slack time.Duration) bool {
	return got <= want && got >= want-slack
}

func TestVoiceDeadline_HandlerScalesWithKnownDuration(t *testing.T) {
	got := recordedDeadline(t, string(pcmWAV(200)))
	if want := 400 * time.Second; !within(got, want, 30*time.Second) {
		t.Errorf("deadline in %v, want about %v", got, want)
	}
}

func TestVoiceDeadline_HandlerUnparseableUploadGetsBase(t *testing.T) {
	got := recordedDeadline(t, "not audio at all")
	if want := 5 * time.Minute; !within(got, want, 30*time.Second) {
		t.Errorf("deadline in %v, want about %v", got, want)
	}
}

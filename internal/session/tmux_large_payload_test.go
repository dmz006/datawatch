// Found live 2026-10-11: a ~160KB decompose prompt (BL406's rules_file/
// context_file content, never this large before that fix) split into
// ~20 chunks at the old flat 10ms inter-chunk delay corrupted
// claude-code's own input rendering — the session sat forever showing
// unexpanded "[Pasted text #N]" placeholders instead of running. This
// chunking path (SendKeysWithSettle for a payload over tmuxSendChunk
// bytes) had zero prior test coverage; every existing test uses a
// payload under one chunk.

package session

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSettleForChunks_SingleChunkUnchanged(t *testing.T) {
	if got := settleForChunks(defaultSendSettle, 1); got != defaultSendSettle {
		t.Errorf("1 chunk: got %v, want unchanged %v", got, defaultSendSettle)
	}
	if got := settleForChunks(defaultSendSettle, 0); got != defaultSendSettle {
		t.Errorf("0 chunks (empty payload): got %v, want unchanged %v", got, defaultSendSettle)
	}
}

func TestSettleForChunks_ScalesWithChunkCount(t *testing.T) {
	base := settleForChunks(defaultSendSettle, 1)
	ten := settleForChunks(defaultSendSettle, 10)
	twenty := settleForChunks(defaultSendSettle, 20)
	if ten <= base {
		t.Errorf("10 chunks should settle longer than 1 chunk: got %v, base %v", ten, base)
	}
	if twenty <= ten {
		t.Errorf("20 chunks should settle longer than 10 chunks: got %v, ten %v", twenty, ten)
	}
}

func TestSettleForChunks_CapsAtMaximum(t *testing.T) {
	if got := settleForChunks(defaultSendSettle, 10000); got != maxTrailingSettle {
		t.Errorf("pathologically large chunk count: got %v, want capped at %v", got, maxTrailingSettle)
	}
}

func TestSendKeysWithSettle_MultiChunkPayload_SendsEveryByteThenEnter(t *testing.T) {
	pathVal, logFile := fakeTmuxOnPath(t)
	t.Setenv("PATH", pathVal)

	// 3 full chunks + a partial 4th, like the real ~160KB prompt that
	// triggered this bug (proportionally: tmuxSendChunk*3.5).
	payload := strings.Repeat("A", tmuxSendChunk*3+tmuxSendChunk/2)

	tm := &TmuxManager{}
	start := time.Now()
	if err := tm.SendKeysWithSettle("s", payload, defaultSendSettle); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	b, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	// 4 literal chunk calls + 1 final Enter call.
	if len(lines) != 5 {
		t.Fatalf("want 5 tmux calls (4 chunks + Enter), got %d:\n%s", len(lines), string(b))
	}
	for i := 0; i < 4; i++ {
		if !strings.Contains(lines[i], "-l") {
			t.Errorf("call %d should be a literal -l push: %q", i, lines[i])
		}
	}
	if !strings.Contains(lines[4], "Enter") {
		t.Errorf("final call should be Enter, got %q", lines[4])
	}
	// Reassemble the chunks' payload length by decoding argv length
	// from the fake tmux log is awkward (shell-quoted); instead just
	// confirm the scaled settle actually elapsed — the real bug was
	// the Enter firing too early relative to a multi-chunk send.
	wantMinSettle := settleForChunks(defaultSendSettle, 4)
	if elapsed < wantMinSettle {
		t.Errorf("elapsed %v is less than the scaled settle %v for a 4-chunk payload — Enter fired too early", elapsed, wantMinSettle)
	}
}

func TestSendKeysWithSettle_MultiChunkPayload_ExactChunkCountMatchesSize(t *testing.T) {
	for _, nChunks := range []int{2, 5, 13, 20} {
		t.Run(strconv.Itoa(nChunks), func(t *testing.T) {
			pathVal, logFile := fakeTmuxOnPath(t)
			t.Setenv("PATH", pathVal)

			payload := strings.Repeat("B", tmuxSendChunk*nChunks)
			tm := &TmuxManager{}
			if err := tm.SendKeysWithSettle("s", payload, 5*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(logFile)
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			want := nChunks + 1 // + the trailing Enter call
			if len(lines) != want {
				t.Fatalf("payload of exactly %d chunks: want %d tmux calls, got %d", nChunks, want, len(lines))
			}
		})
	}
}

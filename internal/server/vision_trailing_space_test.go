package server

import (
	"fmt"
	"testing"
	"time"
)

// TestExpandImageTags_TrailingSpace covers the single trailing space that the
// bare '@<path>' form gets when its [image:...] tag is the last thing in the
// input. Position cases run with no backend (bare form). Fallback cases use a
// lone trailing tag so every failure path must still end in '@<path> '. The
// backend-success case must keep the description form with no trailing space.
func TestExpandImageTags_TrailingSpace(t *testing.T) {
	// Position cases: no backend, so the output is always the bare form.
	position := []struct {
		name string
		in   func(p string) string
		want func(p string) string
	}{
		{
			name: "tag at end of text",
			in:   func(p string) string { return "look [image:" + p + "]" },
			want: func(p string) string { return "look @" + p + " " },
		},
		{
			name: "tag in middle",
			in:   func(p string) string { return "see [image:" + p + "] now" },
			want: func(p string) string { return "see @" + p + " now" },
		},
		{
			name: "same tag twice, only last gets space",
			in:   func(p string) string { return "[image:" + p + "] and [image:" + p + "]" },
			want: func(p string) string { return "@" + p + " and @" + p + " " },
		},
		{
			name: "tag alone",
			in:   func(p string) string { return "[image:" + p + "]" },
			want: func(p string) string { return "@" + p + " " },
		},
	}
	for _, tc := range position {
		tc := tc
		t.Run("position/"+tc.name, func(t *testing.T) {
			root := t.TempDir()
			p := writeTempImage(t, root, []byte("fake-image-data"))
			s := expandImageTagsServer(t, root, nil)
			in := tc.in(p)
			if got, want := s.expandImageTags(in), tc.want(p); got != want {
				t.Errorf("got %q want %q", got, want)
			}
		})
	}

	// Fallback cases: a lone trailing tag must end in '@<path> ' whatever failed.
	fallback := []struct {
		name string
		make func(t *testing.T, s *Server)
	}{
		{
			name: "timeout",
			make: func(t *testing.T, s *Server) {
				s.visioner = &ctxIgnoringSlowVisioner{wait: 3 * time.Second, desc: "stale description"}
				s.cfg.Vision.DescribeTimeoutSeconds = 1
			},
		},
		{
			name: "error",
			make: func(t *testing.T, s *Server) {
				s.visioner = &fakeVisioner{err: fmt.Errorf("backend down")}
			},
		},
		{
			name: "empty description",
			make: func(t *testing.T, s *Server) {
				s.visioner = &fakeVisioner{result: ""}
			},
		},
		{
			name: "no backend",
			make: func(t *testing.T, s *Server) {
				s.visioner = nil
			},
		},
	}
	for _, tc := range fallback {
		tc := tc
		t.Run("fallback/"+tc.name, func(t *testing.T) {
			root := t.TempDir()
			p := writeTempImage(t, root, []byte("fake-image-data"))
			s := expandImageTagsServer(t, root, nil)
			tc.make(t, s)
			start := time.Now()
			got := s.expandImageTags("[image:" + p + "]")
			elapsed := time.Since(start)
			if want := "@" + p + " "; got != want {
				t.Errorf("got %q want %q", got, want)
			}
			if elapsed > 2*time.Second {
				t.Errorf("took %s, want under 2s", elapsed)
			}
		})
	}

	// Success case: backend returns a description, tag is last, no trailing space.
	t.Run("success/description form has no trailing space", func(t *testing.T) {
		root := t.TempDir()
		p := writeTempImage(t, root, []byte("fake-image-data"))
		s := expandImageTagsServer(t, root, &fakeVisioner{result: "desc"})
		want := "[image: desc | path: " + p + "]"
		if got := s.expandImageTags("[image:" + p + "]"); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
}

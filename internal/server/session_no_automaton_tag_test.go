package server

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSessionNoAutomatonTag covers sessions started without an Automaton
// through the REST start path (POST /api/sessions/start): neither a plain
// start nor a supervisor-role start may carry an automaton-axis system tag,
// and both must still carry their compute tag.
func TestSessionNoAutomatonTag(t *testing.T) {
	const automatonAxis = "automaton"

	assertNoAutomatonAxisTag := func(t *testing.T, tags []string) {
		t.Helper()
		for _, tag := range tags {
			if strings.HasPrefix(tag, automatonAxis) {
				t.Errorf("system tags %v carry %q, want no %s-axis tag", tags, tag, automatonAxis)
			}
		}
	}

	t.Run("PlainStartHasNoAutomatonTagButHasCompute", func(t *testing.T) {
		s, _, _ := fedCapFixture(t)
		dir := t.TempDir()
		fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+dir+`"}`)

		sess, ok := s.manager.GetSession(fullID)
		if !ok {
			t.Fatalf("session %s not found", fullID)
		}
		assertNoAutomatonAxisTag(t, sess.SystemTags)
		if want := "compute:" + launchLocalNode; !launchTagPresent(sess.SystemTags, want) {
			t.Errorf("system tags %v missing %q", sess.SystemTags, want)
		}
	})

	t.Run("SupervisorStartHasNoAutomatonTagButHasCompute", func(t *testing.T) {
		s, _, _ := fedCapFixture(t)
		dir := t.TempDir()
		fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+dir+`","launch_role":"supervisor"}`)

		sess, ok := s.manager.GetSession(fullID)
		if !ok {
			t.Fatalf("session %s not found", fullID)
		}
		assertNoAutomatonAxisTag(t, sess.SystemTags)
		if want := "compute:" + launchLocalNode; !launchTagPresent(sess.SystemTags, want) {
			t.Errorf("system tags %v missing %q", sess.SystemTags, want)
		}
		if want := "project:" + filepath.Base(dir); !launchTagPresent(sess.SystemTags, want) {
			t.Errorf("system tags %v missing %q", sess.SystemTags, want)
		}
	})
}

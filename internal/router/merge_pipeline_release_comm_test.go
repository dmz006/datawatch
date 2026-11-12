package router

// The release settings of the merge pipeline in the chat channel: configure
// carries release_size_map, major_trigger, release_cadence and version_scheme,
// globally and for one project, exactly like conflict_policy; the daemon's
// refusal of an unavailable cadence or scheme is shown unchanged; the help and
// list texts name the four keys.

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

var releaseSettingNames = []string{"release_size_map", "major_trigger", "release_cadence", "version_scheme"}

type releaseCommHarness struct {
	mu   sync.Mutex
	wg   sync.WaitGroup
	sent []string
	got  map[string]string
	fail error
	r    *Router
}

func newReleaseCommHarness() *releaseCommHarness {
	h := &releaseCommHarness{got: map[string]string{}}
	h.r = &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) {
		h.mu.Lock()
		h.sent = append(h.sent, s)
		h.mu.Unlock()
		h.wg.Done()
	}}}
	h.r.SetConfigureFunc(func(k, v string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.fail != nil {
			return h.fail
		}
		h.got[k] = v
		return nil
	})
	return h
}

func (h *releaseCommHarness) say(text string) string {
	h.wg.Add(1)
	h.r.handleConfigure(Command{Type: CmdConfigure, Text: text})
	h.wg.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sent[len(h.sent)-1]
}

func TestMergePipelineReleaseComm_ConfigureGlobalAndProject(t *testing.T) {
	h := newReleaseCommHarness()
	values := map[string]string{
		"release_size_map": "feature=minor,bug=patch",
		"major_trigger":    "milestone",
		"release_cadence":  "per_merge",
		"version_scheme":   "semver",
	}
	for _, name := range releaseSettingNames {
		for _, key := range []string{
			"autonomous.merge_pipeline." + name,
			"autonomous.merge_pipeline.projects.demo." + name,
		} {
			reply := h.say(key + "=" + values[name])
			if !strings.Contains(reply, "Set "+key+" = "+values[name]) {
				t.Errorf("%s: reply lacks the confirmation: %q", key, reply)
			}
			if h.got[key] != values[name] {
				t.Errorf("%s: configure got %q, want %q", key, h.got[key], values[name])
			}
		}
	}
}

func TestMergePipelineReleaseComm_JSONMapPassedUnchanged(t *testing.T) {
	h := newReleaseCommHarness()
	key := "autonomous.merge_pipeline.release_size_map"
	val := `{"feature":"major","bug":"patch"}`
	h.say(key + "=" + val)
	if h.got[key] != val {
		t.Fatalf("value: %q", h.got[key])
	}
}

func TestMergePipelineReleaseComm_RefusalShown(t *testing.T) {
	cases := []struct{ key, val, refusal string }{
		{"autonomous.merge_pipeline.release_cadence", "batch", "autonomous.merge_pipeline.release_cadence: release cadence batch is not available yet (available: per_merge)"},
		{"autonomous.merge_pipeline.release_cadence", "manual", "autonomous.merge_pipeline.release_cadence: release cadence manual is not available yet (available: per_merge)"},
		{"autonomous.merge_pipeline.version_scheme", "calver", "autonomous.merge_pipeline.version_scheme: version scheme calver is not available yet (available: semver)"},
		{"autonomous.merge_pipeline.projects.demo.release_cadence", "batch", "release cadence batch is not available yet (available: per_merge)"},
		{"autonomous.merge_pipeline.projects.demo.version_scheme", "calver", "version scheme calver is not available yet (available: semver)"},
		{"autonomous.merge_pipeline.release_size_map", "feature=huge", `release_size_map.feature: "huge" is not a release size (patch, minor, major or none)`},
	}
	for _, c := range cases {
		h := newReleaseCommHarness()
		h.fail = errors.New(c.refusal)
		reply := h.say(c.key + "=" + c.val)
		if !strings.Contains(reply, "Configure failed") || !strings.Contains(reply, c.refusal) {
			t.Errorf("%s=%s: refusal not shown: %q", c.key, c.val, reply)
		}
		if strings.Contains(reply, "Set "+c.key) {
			t.Errorf("%s=%s: a refused value must not be confirmed: %q", c.key, c.val, reply)
		}
	}
}

func TestMergePipelineReleaseComm_KeysListed(t *testing.T) {
	keys := MergePipelineConfigureKeys()
	for _, name := range releaseSettingNames {
		want := "autonomous.merge_pipeline." + name
		found := false
		for _, k := range keys {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing key %q", want)
		}
	}
}

func TestMergePipelineReleaseComm_HelpAndListMentionKeys(t *testing.T) {
	h := newReleaseCommHarness()
	for _, text := range []string{"help", "list"} {
		reply := h.say(text)
		for _, name := range releaseSettingNames {
			if !strings.Contains(reply, "autonomous.merge_pipeline."+name) {
				t.Errorf("%s reply lacks %s", text, name)
			}
		}
		for _, want := range []string{"not available yet", "autonomous.merge_pipeline.projects.<project>.<key>"} {
			if !strings.Contains(reply, want) {
				t.Errorf("%s reply lacks %q", text, want)
			}
		}
		for _, bad := range []string{"PRD"} {
			if i := strings.Index(reply, "release_size_map"); i >= 0 && strings.Contains(reply[i:i+700], bad) {
				t.Errorf("%s: release text carries %q", text, bad)
			}
		}
	}
}

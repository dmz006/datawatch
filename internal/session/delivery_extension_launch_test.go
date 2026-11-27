package session

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

type extensionRecordingBackend struct {
	nodeRecordingBackend
	extMu     sync.Mutex
	extension string
	loaded    map[string]string // tmux session -> extension at launch
}

func (b *extensionRecordingBackend) SetDeliveryExtension(path string) {
	b.extMu.Lock()
	b.extension = path
	b.extMu.Unlock()
}

func (b *extensionRecordingBackend) Launch(_ context.Context, _, tmuxSession, _, _ string) error {
	b.extMu.Lock()
	defer b.extMu.Unlock()
	b.loaded[tmuxSession] = b.extension
	return nil
}

// What the pre-launch hook wired for one session reaches that launch, and a
// session it did not wire loads nothing left over on the shared backend.
func TestTheDeliveryExtensionIsPerLaunch(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	backend := &extensionRecordingBackend{loaded: map[string]string{}}
	backend.launched = map[string]string{}
	wire := true
	mgr.SetOnPreLaunch(func(s *Session) {
		if wire {
			s.DeliveryExtension = "/data/pi-delivery/ext-" + s.ID + ".ts"
		}
	})
	start := func() *Session {
		sess, err := mgr.Start(context.Background(), "task", "", t.TempDir(),
			&StartOptions{BackendObj: backend, LaunchFn: backend.Launch})
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}
	a := start()
	wire = false
	b := start()
	if got := backend.loaded[a.TmuxSession]; got != "/data/pi-delivery/ext-"+a.ID+".ts" {
		t.Errorf("the wired launch loaded %q", got)
	}
	if got := backend.loaded[b.TmuxSession]; got != "" {
		t.Errorf("the unwired launch loaded %q", got)
	}
	// Both ways a session is launched hand it over: a start and a restart.
	src, err := os.ReadFile("manager.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "setLaunchDeliveryExtension(backendObj, sess)"); n != 2 {
		t.Errorf("a start and a restart must both set it: %d places", n)
	}
}

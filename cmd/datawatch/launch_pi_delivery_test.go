package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm/backends/pi"
	"github.com/dmz006/datawatch/internal/session"
)

type piLaunchFx struct {
	dataDir string
	env     map[string]string
}

func (f *piLaunchFx) wire(t *testing.T, sess *session.Session, token, ca string) error {
	t.Helper()
	return wirePiDelivery(sess, piDeliveryLaunch{DataDir: f.dataDir, APIURL: "http://127.0.0.1:1", CACertPath: ca, Token: token,
		SetSessionEnv: func(tmux, key, value string) error { f.env[tmux+" "+key] = value; return nil }})
}

func (f *piLaunchFx) credential(t *testing.T, sess *session.Session) (string, pi.DeliveryCredential) {
	t.Helper()
	path := f.env[sess.TmuxSession+" "+pi.DeliveryEnv]
	var c pi.DeliveryCredential
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no credential file at %q: %v", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return path, c
}

func TestReqPiLaunchWritesAPrivateCredential(t *testing.T) {
	f := &piLaunchFx{dataDir: t.TempDir(), env: map[string]string{}}
	sess := launchMCPSession("aa01", "pi", t.TempDir(), false)
	const token = "tok-SECRET-5150"
	// Nothing here depends on Claude's channel: this is the whole wiring.
	if err := f.wire(t, sess, token, "/some/ca.pem"); err != nil {
		t.Fatal(err)
	}
	path, c := f.credential(t, sess)
	if c.Token != token || c.APIURL != "http://127.0.0.1:1" || c.CACertPath != "/some/ca.pem" ||
		!strings.HasPrefix(path, f.dataDir) || filepath.Dir(c.StateFile) != filepath.Dir(path) {
		t.Fatalf("%s %+v", path, c)
	}
	// The operator's decision: after the current task, with the note that the
	// message is real.
	if c.DeliverAs != pi.DeliverFollowUp || !c.Guidance {
		t.Fatalf("not follow-up with the note: %+v", c)
	}
	for p, mode := range map[string]fs.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != mode {
			t.Errorf("%s: %v %v", p, err, st.Mode())
		}
	}
	for k, v := range f.env {
		if strings.Contains(k+v, token) {
			t.Errorf("the token is in the environment: %s=%s", k, v)
		}
	}
	if got := filesHolding(t, f.dataDir, token); len(got) != 1 || got[0] != path {
		t.Errorf("the token is in %v", got)
	}
	if got := filesHolding(t, sess.ProjectDir, token); len(got) != 0 {
		t.Errorf("the token is in the project: %v", got)
	}
	ext := filepath.Join(f.dataDir, "pi-delivery", "datawatch-delivery.ts")
	if st, err := os.Stat(ext); err != nil || st.Mode().Perm() != 0o400 {
		t.Errorf("the extension: %v", err)
	}

	// A restart mints a new token: the same file, replaced.
	if err := f.wire(t, sess, "tok-two", ""); err != nil {
		t.Fatal(err)
	}
	again, c2 := f.credential(t, sess)
	if again != path || c2.Token != "tok-two" || len(filesHolding(t, f.dataDir, token)) != 0 {
		t.Fatalf("%s %+v", again, c2)
	}
}

func TestReqPiLaunchEndKeepsOnlyOwedReports(t *testing.T) {
	f := &piLaunchFx{dataDir: t.TempDir(), env: map[string]string{}}
	owes := launchMCPSession("aa01", "pi", t.TempDir(), false)
	clear := launchMCPSession("aa02", "pi-prompt", t.TempDir(), false)
	other := launchMCPSession("aa03", "pi", t.TempDir(), false)
	for _, s := range []*session.Session{owes, clear, other} {
		if err := f.wire(t, s, "tok-"+s.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	path, c := f.credential(t, owes)
	if err := os.WriteFile(c.StateFile, []byte(`{"reports":{"r-1":"abc"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clearPath, cc := f.credential(t, clear)
	if err := os.WriteFile(cc.StateFile, []byte(`{"reports":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	endSessionMCPFiles(owes, f.dataDir)
	endSessionMCPFiles(clear, f.dataDir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the ended session keeps its credential: %v", err)
	}
	if _, err := os.Stat(c.StateFile); err != nil {
		t.Errorf("an owed report is lost at the end: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(clearPath)); !os.IsNotExist(err) {
		t.Errorf("a session that owes nothing keeps a folder: %v", err)
	}
	if _, c3 := f.credential(t, other); c3.Token != "tok-aa03" {
		t.Error("another session's file was touched")
	}
	// Started again under the same ID, it finds what it owes.
	if err := f.wire(t, owes, "tok-new", ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(c.StateFile); err != nil || !strings.Contains(string(data), "r-1") {
		t.Errorf("%v %s", err, data)
	}
}

func TestReqPiLaunchRefusesWhatItCannotKeepPrivate(t *testing.T) {
	f := &piLaunchFx{dataDir: t.TempDir(), env: map[string]string{}}
	good := launchMCPSession("aa01", "pi", t.TempDir(), false)
	if err := f.wire(t, good, "", ""); err == nil {
		t.Error("no token")
	}
	for _, id := range []string{"", "..", ".", "a/b", `a\b`} {
		bad := launchMCPSession("aa02", "pi", t.TempDir(), false)
		bad.FullID = id
		if err := f.wire(t, bad, "tok", ""); err == nil {
			t.Errorf("a session ID %q was used as a folder", id)
		}
		endPiDelivery(bad, f.dataDir) // and removes nothing
	}
	if err := wirePiDelivery(nil, piDeliveryLaunch{DataDir: f.dataDir, Token: "t"}); err == nil {
		t.Error("no session")
	}
	if len(f.env) != 0 {
		t.Errorf("%v", f.env)
	}
	if _, err := os.Stat(f.dataDir); err != nil {
		t.Fatal(err)
	}
}

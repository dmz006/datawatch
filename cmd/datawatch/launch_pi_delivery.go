// The launch wiring of the datawatch pi extension (delivery job 4). Owner:
// part C.
//
// Part A calls wirePiDelivery from the pre-launch hook for a pi-family
// session, with a token minted for that session whether or not Claude's channel
// is on. It writes the built-in extension and the session's private delivery
// credential, and puts the credential's path (never the token) in the session's
// environment. No launch command loads the extension yet.

// A busy pi is handed a request after its current task (follow-up), and the
// extension adds a short note that tells pi's model such messages are real: the
// operator's choice, 2026-11-27. The two were proven together.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/llm/backends/pi"
	"github.com/dmz006/datawatch/internal/session"
)

// piDeliveryDir is the folder, under the data folder, of the extension and of
// one private folder per pi session.
const piDeliveryDir = "pi-delivery"

// piDeliveryLaunch is what wiring one pi session needs.
type piDeliveryLaunch struct {
	DataDir    string // the daemon's expanded data directory
	APIURL     string // loopback base URL
	CACertPath string // empty without TLS
	Token      string // this session's own token
	// SetSessionEnv sets a variable in the session's tmux environment. The
	// variable carries a file path, never a token.
	SetSessionEnv func(tmuxSession, key, value string) error
}

// piSessionDir is one session's private folder. It is kept by the session's
// ID, so a session started again under that ID finds the reports it owes.
func piSessionDir(dataDir string, sess *session.Session) (string, error) {
	if sess == nil || dataDir == "" {
		return "", errors.New("no session or data folder")
	}
	id := sess.FullID
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("session ID %q cannot name a folder", id)
	}
	return filepath.Join(dataDir, piDeliveryDir, id), nil
}

const (
	piCredentialFile = "credential.json"
	piReportsFile    = "reports.json"
)

// wirePiDelivery writes the session's delivery credential file and names it in
// the session's environment.
func wirePiDelivery(sess *session.Session, in piDeliveryLaunch) error {
	dir, err := piSessionDir(in.DataDir, sess)
	if err != nil {
		return err
	}
	if in.Token == "" || in.SetSessionEnv == nil {
		return errors.New("a pi session needs its own token and an environment to name its credential in")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := pi.WriteDeliveryExtension(filepath.Dir(dir)); err != nil {
		return err
	}
	path := filepath.Join(dir, piCredentialFile)
	if err := pi.WriteDeliveryCredential(path, pi.DeliveryCredential{
		APIURL: in.APIURL, Token: in.Token, CACertPath: in.CACertPath,
		StateFile: filepath.Join(dir, piReportsFile),
		DeliverAs: pi.DeliverFollowUp, Guidance: true,
	}); err != nil {
		return err
	}
	return in.SetSessionEnv(sess.TmuxSession, pi.DeliveryEnv, path)
}

// endPiDelivery removes an ended session's credential. A report datawatch has
// not recorded yet (request IDs and digests only) stays, for a session started
// again under the same ID; with none, the folder goes too.
func endPiDelivery(sess *session.Session, dataDir string) {
	dir, err := piSessionDir(dataDir, sess)
	if err != nil {
		return
	}
	if err := os.Remove(filepath.Join(dir, piCredentialFile)); err != nil && !os.IsNotExist(err) {
		fmt.Printf("[warn] session %s: remove its pi delivery credential: %v\n", sess.FullID, err)
	}
	reports := filepath.Join(dir, piReportsFile)
	if data, err := os.ReadFile(reports); err == nil { // #nosec G304 -- under the daemon's data folder
		var owed struct {
			Reports map[string]json.RawMessage `json:"reports"`
		}
		if json.Unmarshal(bytes.TrimSpace(data), &owed) == nil && len(owed.Reports) == 0 {
			_ = os.Remove(reports)
		}
	}
	_ = os.Remove(dir) // only when empty
}

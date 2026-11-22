package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// FinaliseSessionFolders finalises the saved state of session folders under
// <dataDir>/sessions/<id>/ whose session.json still says running,
// waiting_input, stalled or rate_limited. It reads the same layout as
// ReconcileSessions and takes one terminal snapshot; when that listing fails
// it changes nothing and reports TerminalListOK=false.
//
// Each folder is judged one of two ways:
//
//   - The daemon holds a record for the folder's id. The record decides: a
//     live, foreign or still-gone record leaves the folder alone (the record
//     pass owns those), and a final record has its state, ended_at and
//     end_note copied into the folder.
//   - The daemon holds no record (never stored, or archived). The folder is
//     judged by its own saved fields: it is left alone while its terminal
//     session exists or its recorded subprocess pid still runs the task;
//     otherwise its state comes from GoneFinalState over the tail of the
//     folder's output log, with ended_at set to now and end_note set to
//     GoneEndNote.
//
// Only session.json is rewritten, through a temporary file and a rename; every
// other file in the folder is left as it was. A session.json that cannot be
// parsed is skipped and counted in Unparseable, and never stops the pass.
// Finalised holds the folder ids that were rewritten.
func (m *Manager) FinaliseSessionFolders(now time.Time) FinaliseResult {
	res := FinaliseResult{At: now}
	terminals, ok := m.TerminalSnapshot()
	if !ok {
		return res
	}
	res.TerminalListOK = true

	root := filepath.Join(m.dataDir, "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		return res
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		dir := filepath.Join(root, id)
		raw, err := os.ReadFile(filepath.Join(dir, folderStateFile))
		if err != nil {
			if !os.IsNotExist(err) {
				res.Skipped++
				res.Unparseable++
			}
			continue // no session.json: not a session folder
		}
		var saved Session
		if err := json.Unmarshal(raw, &saved); err != nil {
			res.Skipped++
			res.Unparseable++
			continue
		}
		if !finalisableState(saved.State) {
			res.Skipped++
			continue
		}

		update, done := m.folderUpdate(id, dir, &saved, terminals, now)
		if !done {
			res.Skipped++
			continue
		}
		if err := writeFolderState(dir, raw, update); err != nil {
			res.Skipped++
			continue
		}
		res.Finalised = append(res.Finalised, id)
		if saved.ProjectDir != "" {
			if info, err := os.Stat(saved.ProjectDir); err == nil && info.IsDir() {
				res.Folders++
			}
		}
	}
	return res
}

// folderStateFile is the per-session saved state file inside a session folder.
const folderStateFile = "session.json"

// folderEnd is the end-of-life data written into a folder's session.json.
type folderEnd struct {
	state   State
	endedAt *time.Time
	note    string
}

// folderUpdate decides what, if anything, to write into the folder id. The
// second return value is false when the folder must be left as it is.
func (m *Manager) folderUpdate(id, dir string, saved *Session, terminals map[string]bool, now time.Time) (folderEnd, bool) {
	if rec, held := m.store.Get(id); held && !m.store.IsArchived(id) {
		if rec.Hostname != m.hostname || !rec.State.IsTerminal() {
			return folderEnd{}, false // live, foreign or still gone: not final
		}
		end := folderEnd{state: rec.State, note: rec.EndNote}
		if rec.EndedAt != nil {
			t := *rec.EndedAt
			end.endedAt = &t
		}
		if folderMatches(saved, end) {
			return folderEnd{}, false
		}
		return end, true
	}

	// No record: judge the folder by its own saved fields.
	if saved.Hostname != "" && saved.Hostname != m.hostname {
		return folderEnd{}, false // another host's session: not judged here
	}
	if saved.TmuxSession != "" && terminals[saved.TmuxSession] {
		return folderEnd{}, false
	}
	own := &Session{LogFile: filepath.Join(dir, "output.log")}
	if pid, ok := readSubprocessPID(own); ok && subprocessAlive(pid, saved.Task) {
		return folderEnd{}, false
	}
	// Re-check right before writing: a terminal may have appeared since the
	// snapshot was taken.
	if saved.TmuxSession != "" && m.tmux.SessionExists(saved.TmuxSession) {
		return folderEnd{}, false
	}
	state, note := GoneFinalState(ReadOutputTail(own.LogFile, goneTailBytes))
	ended := now
	return folderEnd{state: state, endedAt: &ended, note: note}, true
}

// folderMatches reports whether the folder already carries end exactly.
func folderMatches(saved *Session, end folderEnd) bool {
	if saved.State != end.state || saved.EndNote != end.note {
		return false
	}
	switch {
	case saved.EndedAt == nil && end.endedAt == nil:
		return true
	case saved.EndedAt == nil || end.endedAt == nil:
		return false
	}
	return saved.EndedAt.Equal(*end.endedAt)
}

// writeFolderState rewrites <dir>/session.json with the end data applied. Every
// other field of the saved document is carried over untouched. The new content
// goes to a temporary file in the same folder and is renamed over the old one.
func writeFolderState(dir string, raw []byte, end folderEnd) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	set := func(key string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		doc[key] = b
		return nil
	}
	if err := set("state", end.state); err != nil {
		return err
	}
	if end.endedAt != nil {
		if err := set("ended_at", end.endedAt); err != nil {
			return err
		}
	} else {
		delete(doc, "ended_at")
	}
	if end.note != "" {
		if err := set("end_note", end.note); err != nil {
			return err
		}
	} else {
		delete(doc, "end_note")
	}
	out, err := marshalFolderDoc(doc)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, folderStateFile)
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// marshalFolderDoc renders the document with indented output, the same layout
// the session tracker writes.
func marshalFolderDoc(doc map[string]json.RawMessage) ([]byte, error) {
	compact, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", "  "); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

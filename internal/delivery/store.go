// The delivery records on disk: the list and the start mark, sealed with the
// delivery store key on a secure install, and the plain heartbeat.

package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// Record file names inside Options.Dir.
const (
	stateFileName = "state.rec"
	startMarkFile = "startmark.rec"
	diskTestFile  = ".disktest"
)

const recordsVersion = 1

// track is how long a source has reported one thing in one state, in observed
// time.
type track struct {
	Source string        `json:"source"`
	Age    time.Duration `json:"age"`
}

type records struct {
	Version  int               `json:"version"`
	Pass     int               `json:"pass"`
	LastPass time.Time         `json:"last_pass"`
	Tracks   map[string]*track `json:"tracks"`
	Entries  map[string]*Entry `json:"entries"`
	// NonOperator counts actions by a session or peer.
	NonOperator int `json:"non_operator"`
	// AllFailed: every channel refused the last send.
	AllFailed bool `json:"all_failed"`
	// Guarded are the keys the mass-expiry guard holds back. Release lets
	// them through on the next pass.
	Guarded    []string       `json:"guarded,omitempty"`
	Release    bool           `json:"release,omitempty"`
	Unrecorded int            `json:"unrecorded,omitempty"`
	Sources    []SourceStatus `json:"sources,omitempty"`
}

// sourceMark is what one source held when it was first read.
type sourceMark struct {
	At time.Time `json:"at"`
	// Open are the things it reported then; True are the keys of those
	// already breaking a rule, which form the backlog.
	Open []string `json:"open,omitempty"`
	True []string `json:"true,omitempty"`
}

// startMark is saved once, at first enable. Nothing older than it is treated
// as new.
type startMark struct {
	Version int                    `json:"version"`
	At      time.Time              `json:"at"`
	Sources map[string]*sourceMark `json:"sources"`
	Slots   map[string]time.Time   `json:"schedule_slots,omitempty"`
}

func newRecords() *records {
	return &records{Version: recordsVersion, Tracks: map[string]*track{}, Entries: map[string]*Entry{}}
}

// whole reports that no part of a record that was read is missing.
func (r *records) whole() bool {
	for _, en := range r.Entries {
		if en == nil {
			return false
		}
	}
	for _, t := range r.Tracks {
		if t == nil {
			return false
		}
	}
	return true
}

func (m *startMark) whole() bool {
	for _, s := range m.Sources {
		if s == nil {
			return false
		}
	}
	return true
}

func cloneJSON[T any](v *T) *T {
	data, err := json.Marshal(v)
	if err != nil {
		panic("delivery: record cannot be marshalled: " + err.Error())
	}
	out := new(T)
	if err := json.Unmarshal(data, out); err != nil {
		panic("delivery: record cannot be copied: " + err.Error())
	}
	return out
}

func (r *records) clone() *records {
	c := cloneJSON(r)
	if c.Tracks == nil {
		c.Tracks = map[string]*track{}
	}
	if c.Entries == nil {
		c.Entries = map[string]*Entry{}
	}
	return c
}

func (m *startMark) clone() *startMark {
	if m == nil {
		return nil
	}
	c := cloneJSON(m)
	if c.Sources == nil {
		c.Sources = map[string]*sourceMark{}
	}
	return c
}

// pointers returns every saved entry, oldest listed first.
func (r *records) pointers() []*Entry {
	out := make([]*Entry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ListedAt.Equal(out[j].ListedAt) {
			return out[i].ListedAt.Before(out[j].ListedAt)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// writeRecordFile replaces path durably, sealed when key is set.
func writeRecordFile(path string, data, key []byte) error {
	return secfile.WriteFileDurable(path, data, 0o600, key)
}

// readRecord reads one record. On a secure install a plain file is a foreign
// write unless adopt is set; adopted reports that one was read.
func readRecord(path string, key []byte, adopt bool, v any) (adopted bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	switch {
	case secfile.IsEncrypted(data):
		if key == nil {
			return false, errors.New("the record is sealed and datawatch runs without a store key")
		}
		if data, err = secfile.Decrypt(data, key); err != nil {
			return false, err
		}
	case key != nil && !adopt:
		return false, errors.New("a plain record in a sealed store")
	case key != nil:
		adopted = true
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("parse record: %w", err)
	}
	return adopted, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// load reads the records. It writes nothing. A record that cannot be read is
// never taken to mean there is nothing there.
func (e *Engine) load() {
	e.broken = nil
	e.markDirty = false
	statePath := filepath.Join(e.opts.Dir, stateFileName)
	markPath := filepath.Join(e.opts.Dir, startMarkFile)

	rec := newRecords()
	adopted, err := readRecord(statePath, e.opts.Key, e.opts.AdoptPlain, rec)
	switch {
	case errors.Is(err, fs.ErrNotExist) && exists(filepath.Join(e.opts.Dir, HeartbeatFile)):
		// A heartbeat follows a saved list: this one was lost.
		e.broken = append(e.broken, RedRecords)
		rec = newRecords()
	case errors.Is(err, fs.ErrNotExist):
		rec = newRecords()
	case err != nil || !rec.whole():
		e.broken = append(e.broken, RedRecords)
		rec = newRecords()
	}

	mark := &startMark{}
	markAdopted, err := readRecord(markPath, e.opts.Key, e.opts.AdoptPlain, mark)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		mark = nil
		// After the first enable there is always a start mark.
		if exists(statePath) || exists(filepath.Join(e.opts.Dir, HeartbeatFile)) {
			e.broken = append(e.broken, RedStartMark)
		}
	case err != nil || mark.At.IsZero() || !mark.whole():
		mark = nil
		e.broken = append(e.broken, RedStartMark)
	}
	e.rec, e.mark = rec.clone(), mark.clone()
	e.markDirty = adopted || markAdopted
}

// persistLocked saves the start mark, when it changed, and the list. Callers
// hold e.mu. On success they become the engine's records.
func (e *Engine) persistLocked(rec *records, mark *startMark, markDirty bool) error {
	if err := os.MkdirAll(e.opts.Dir, 0o700); err != nil {
		return err
	}
	write := func(name string, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return e.opts.writeFile(filepath.Join(e.opts.Dir, name), data, e.opts.Key)
	}
	// The mark goes first: a list with no start mark stops the check.
	if (markDirty || e.markDirty) && mark != nil {
		if err := write(startMarkFile, mark); err != nil {
			return err
		}
	}
	if err := write(stateFileName, rec); err != nil {
		return err
	}
	e.rec, e.mark, e.markDirty = rec, mark, false
	// From here a plain record in a sealed store is a foreign write.
	e.opts.AdoptPlain = false
	return nil
}

// diskTest writes, syncs and reads back a file in the store folder.
func (e *Engine) diskTest(now time.Time) error {
	path := filepath.Join(e.opts.Dir, diskTestFile)
	want := []byte("disk test " + now.UTC().Format(time.RFC3339Nano) + "\n")
	if err := e.opts.writeFile(path, want, nil); err != nil {
		return err
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("the disk test read back something else")
	}
	return nil
}

func (e *Engine) writeHeartbeat(r Report, now time.Time) error {
	hb := Heartbeat{
		At: now.UTC(), Unix: now.Unix(), IntervalSeconds: int(e.opts.Interval / time.Second),
		State: r.Heartbeat, Pass: r.Pass, Open: len(r.Items) + len(r.Backlog),
	}
	data, err := json.Marshal(hb)
	if err != nil {
		return err
	}
	return e.opts.writeFile(filepath.Join(e.opts.Dir, HeartbeatFile), append(data, '\n'), nil)
}

// ReadHeartbeat reads the heartbeat file at path.
func ReadHeartbeat(path string) (Heartbeat, error) {
	var hb Heartbeat
	data, err := os.ReadFile(path)
	if err != nil {
		return hb, err
	}
	err = json.Unmarshal(data, &hb)
	return hb, err
}

// collect reads one source. A panic in it is an unreadable source, not an
// empty one.
func collect(ctx context.Context, s Source, now time.Time) (cands []Candidate, err error) {
	defer func() {
		if r := recover(); r != nil {
			cands, err = nil, fmt.Errorf("source %s failed: %v", s.Name, r)
		}
	}()
	return s.Collect(ctx, now)
}

// syncFolder makes a new name in dir survive a power loss.
func syncFolder(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

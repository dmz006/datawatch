// Repair of a damaged request store. It is for rare damage, and does four
// things: rebuild a damaged or missing manifest from the intact record files;
// on a secure install put back a readable index that differs from the sealed
// copy; set aside a record that cannot be believed, so the others work; and,
// with a key that opens nothing, change nothing.
//
// Every file is first copied into a folder of the repair, and nothing there is
// deleted. A repair never marks anything done. A request rebuilt while open is
// closed as failed with the reason "repaired": it is listed to the operator
// and is not run again until the operator reissues it. When any of this cannot
// be done safely, the store is not used: datawatch runs with delivery requests
// off.

package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/secfile"
)

// ErrCannotRepair: the store cannot be repaired safely. Nothing in it was
// changed.
var ErrCannotRepair = errors.New("requests: the store cannot be repaired safely")

// RepairReport says what a repair did. It holds file names and request IDs
// only.
type RepairReport struct {
	// Folder keeps a copy of every file as it was, and the files set aside.
	Folder string
	// Manifest: the manifest was rebuilt from the intact record files.
	Manifest bool
	// Restored requests got their readable index back from the sealed copy.
	Restored []string
	// SetAside files could not be believed. They are in Folder.
	SetAside []string
	// Missing files were named by the manifest and are gone: not repaired.
	Missing []string
	// Held requests were open and either rebuilt or waiting for one that could
	// not be kept. They are closed as failed, with the reason "repaired", until
	// the operator reissues or drops them.
	Held []string
}

// OpenRequestsRepaired opens the store and repairs it when it must. The report
// is nil when nothing needed repair. With ErrCannotRepair there is no store.
func OpenRequestsRepaired(opts RequestOptions) (*Requests, *RepairReport, error) {
	s, err := OpenRequests(opts)
	if err == nil && s.faultError() == nil {
		return s, nil, nil
	}
	if opts.Dir == "" {
		return nil, nil, err
	}
	rep, err := repairRequests(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrCannotRepair, err)
	}
	if s, err = OpenRequests(opts); err == nil {
		err = s.faultError()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: it is still damaged after the repair, whose files are in %s: %w", ErrCannotRepair, rep.Folder, err)
	}
	return s, rep, nil
}

const (
	repairPrefix = "repair-"
	originalsDir = "originals"
	setAsideDir  = "set-aside"
)

// storeFiles are the record files as the repair found them. A file that cannot
// be read has no bytes.
type storeFiles struct {
	data  map[string][]byte
	names []string // sorted
}

func readStoreFiles(dir string) (storeFiles, error) {
	out := storeFiles{data: map[string][]byte{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), recordSuffix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- the daemon's own data folder
		if err != nil {
			data = nil
		} else if data == nil {
			data = []byte{}
		}
		out.data[e.Name()] = data
		out.names = append(out.names, e.Name())
	}
	sort.Strings(out.names)
	return out, nil
}

// safeToRepair refuses what a repair must not touch: a key that opens none of
// the sealed files (or no key for them), and files of another version.
func (s *Requests) safeToRepair(files storeFiles) error {
	sealed, opened := 0, 0
	for _, name := range files.names {
		data := files.data[name]
		inner := data
		if !secfile.IsEncrypted(inner) {
			inner = bodyOf(data)
		}
		plain := data
		if secfile.IsEncrypted(inner) {
			sealed++
			if s.opts.Key == nil {
				continue
			}
			got, err := secfile.Decrypt(inner, s.opts.Key)
			if err != nil {
				continue
			}
			opened++
			if name == metaFileName {
				plain = got
			}
		}
		var head struct {
			Version int `json:"version"` // the manifest
			V       int `json:"v"`       // a request record
		}
		if json.Unmarshal(plain, &head) == nil && (name == metaFileName && head.Version > recordVersion ||
			strings.HasPrefix(name, RequestIDPrefix) && head.V > recordVersion) {
			return fmt.Errorf("%s was written by a newer datawatch", name)
		}
	}
	switch {
	case sealed > 0 && s.opts.Key == nil:
		return errors.New("the files are sealed and datawatch runs without the store key")
	case sealed > 0 && opened == 0:
		return errors.New("the store key opens none of the sealed files (a wrong key)")
	}
	return nil
}

// fromSealed reads a request record whose readable index differs from its
// sealed copy, as the sealed copy has it.
func (s *Requests) fromSealed(name string, data []byte) (*storedRequest, bool) {
	var d diskRecord
	if s.opts.Key == nil || json.Unmarshal(data, &d) != nil || d.V != recordVersion || d.Sealed == "" {
		return nil, false
	}
	plain, err := secfile.Decrypt([]byte(d.Sealed), s.opts.Key)
	if err != nil {
		return nil, false
	}
	var part sealedPart
	if json.Unmarshal(plain, &part) != nil || sameJSON(part.Index, d.Index) {
		return nil, false
	}
	st := &storedRequest{content: part.Content}
	if json.Unmarshal(part.Index, &st.index) != nil || st.index.ID+recordSuffix != name {
		return nil, false
	}
	return st, true
}

// rebuild takes every intact record file into a new manifest. It returns the
// requests it took.
func (s *Requests) rebuild(files storeFiles, rep *RepairReport) (rebuilt []string) {
	s.meta = newStoreMeta()
	take := func(name string, data []byte) {
		s.meta.Seq++
		s.meta.Files[name] = fileSum{Seq: s.meta.Seq, Hash: hashOf(data)}
	}
	var highest uint64
	for _, name := range files.names {
		data := files.data[name]
		switch {
		case name == metaFileName:
		case data == nil:
			rep.SetAside = append(rep.SetAside, name)
		case strings.HasPrefix(name, queuePrefix):
			plain, _, err := s.openSealed(data, s.opts.AdoptPlain)
			if err != nil || !json.Valid(plain) {
				rep.SetAside = append(rep.SetAside, name)
				continue
			}
			take(name, data)
			s.queue[strings.TrimSuffix(strings.TrimPrefix(name, queuePrefix), recordSuffix)] = plain
		default:
			st, _, _, err := s.decodeRecord(data)
			if err != nil || st.index.ID+recordSuffix != name {
				var ok bool
				if st, ok = s.fromSealed(name, data); !ok {
					rep.SetAside = append(rep.SetAside, name)
					continue
				}
				rep.Restored = append(rep.Restored, st.index.ID)
			}
			take(name, data)
			highest = max(highest, st.index.Seq)
			s.reqs[st.index.ID] = st
			s.keys[st.index.KeyHash] = st.index.ID
			rebuilt = append(rebuilt, st.index.ID)
		}
	}
	s.meta.Seq = max(s.meta.Seq, highest)
	return rebuilt
}

// mend keeps a sound manifest and deals with the files it cannot believe. It
// returns the requests it rebuilt.
func (s *Requests) mend(files storeFiles, rep *RepairReport) (rebuilt []string) {
	var bad []string
	for _, f := range s.faults {
		if !slices.Contains(bad, f.name) {
			bad = append(bad, f.name)
		}
	}
	s.faults = nil
	for _, name := range bad {
		delete(s.meta.Pending, name)
		data, there := files.data[name]
		if !there {
			delete(s.meta.Files, name)
			rep.Missing = append(rep.Missing, name)
			continue
		}
		if st, ok := s.fromSealed(name, data); ok {
			// Recorded as it is now, so that the write below replaces it.
			s.meta.Files[name] = fileSum{Seq: st.index.Seq, Hash: hashOf(data)}
			s.reqs[st.index.ID] = st
			s.keys[st.index.KeyHash] = st.index.ID
			rep.Restored = append(rep.Restored, st.index.ID)
			rebuilt = append(rebuilt, st.index.ID)
			continue
		}
		delete(s.meta.Files, name)
		rep.SetAside = append(rep.SetAside, name)
	}
	return rebuilt
}

// keepOriginals copies every readable record file into a new repair folder and
// reads each copy back.
func (s *Requests) keepOriginals(files storeFiles) (folder string, err error) {
	folder = filepath.Join(s.opts.Dir, repairPrefix+s.opts.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Mkdir(folder, 0o700); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(folder) // our own unfinished copy
		}
	}()
	copies := filepath.Join(folder, originalsDir)
	if err := os.Mkdir(copies, 0o700); err != nil {
		return folder, err
	}
	for _, name := range files.names {
		data := files.data[name]
		if data == nil {
			if name == metaFileName {
				return folder, fmt.Errorf("%s cannot be read, so it cannot be kept", name)
			}
			continue // it is moved aside as it is
		}
		path := filepath.Join(copies, name)
		if err := s.opts.writeFile(path, data); err != nil {
			return folder, fmt.Errorf("keep the original of %s: %w", name, err)
		}
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) { // #nosec G304 -- the copy just written
			return folder, fmt.Errorf("the copy of %s did not read back", name)
		}
	}
	return folder, syncFolder(copies)
}

// repairRequests repairs the files of a store that did not open sound.
func repairRequests(opts RequestOptions) (*RepairReport, error) {
	s := newRequests(opts)
	files, err := readStoreFiles(opts.Dir)
	if err != nil {
		return nil, err
	}
	if err := s.safeToRepair(files); err != nil {
		return nil, err
	}
	_, manifest := files.data[metaFileName]
	sound := manifest
	if sound {
		if err := s.load(); err != nil {
			if !errors.Is(err, ErrRecords) {
				return nil, err
			}
			sound, s = false, newRequests(opts)
		}
	}
	rep := &RepairReport{Manifest: !sound}
	var rebuilt []string
	if sound {
		rebuilt = s.mend(files, rep)
	} else {
		rebuilt = s.rebuild(files, rep)
	}
	if rep.Folder, err = s.keepOriginals(files); err != nil {
		return nil, err
	}
	// What waited for a request that could not be kept is held too: it must not
	// start as if that work were done.
	lost := func(id string) bool {
		name := id + recordSuffix
		return id != "" && (slices.Contains(rep.SetAside, name) || slices.Contains(rep.Missing, name))
	}
	// One write: the manifest, and every such open request closed for the
	// operator. Newest first, so that one waiting on another is held itself and
	// not only failed with it.
	err = s.update(func(w *work) error {
		ids := w.ids()
		slices.Reverse(ids)
		for _, id := range ids {
			cur := &w.peek(id).index
			if cur.State.Closed() || !slices.Contains(rebuilt, id) && !lost(cur.After) {
				continue
			}
			st := w.edit(id)
			w.close(&st.index, StateFailed, WhyRepaired, "datawatch")
			st.index.MayRunTwice = true
			rep.Held = append(rep.Held, id)
		}
		w.t.meta = append(w.t.meta, func(*storeMeta) {})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Only now, with a manifest that no longer names them, are they moved.
	if len(rep.SetAside) > 0 {
		aside := filepath.Join(rep.Folder, setAsideDir)
		if err := os.Mkdir(aside, 0o700); err != nil {
			return nil, err
		}
		for _, name := range rep.SetAside {
			if err := os.Rename(filepath.Join(opts.Dir, name), filepath.Join(aside, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("set %s aside: %w", name, err)
			}
		}
		if err := errors.Join(syncFolder(aside), syncFolder(opts.Dir)); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

// Text tells the operator what was repaired and what was not. It holds file
// names and request IDs only.
func (r *RepairReport) Text() string {
	list := func(in []string) string {
		if len(in) > 20 {
			return strings.Join(in[:20], ", ") + fmt.Sprintf(" and %d more", len(in)-20)
		}
		return strings.Join(in, ", ")
	}
	out := []string{"datawatch repaired the delivery request store. Every file as it was is kept in " + r.Folder + "."}
	if r.Manifest {
		out = append(out, "Rebuilt: the manifest, from the intact record files. Role holders and reminder counts "+
			"were only in it and are lost: give the roles again.")
	}
	if len(r.Restored) > 0 {
		out = append(out, "Restored from the sealed copy: "+list(r.Restored)+".")
	}
	if len(r.SetAside) > 0 {
		out = append(out, "Not repaired, set aside and kept: "+list(r.SetAside)+".")
	}
	if len(r.Missing) > 0 {
		out = append(out, "Not repaired, gone from the disk: "+list(r.Missing)+".")
	}
	if len(r.Held) > 0 {
		out = append(out, "Held, listed as failed (repaired) and not run again until you reissue or drop them: "+
			list(r.Held)+".")
	}
	out = append(out, "Nothing was marked done by the repair. Older work queue items are as their files were.")
	return strings.Join(out, " ")
}

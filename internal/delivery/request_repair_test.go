// The operator's decision of 2026-11-27: repair a damaged request store, keep
// the originals, never mark anything done, and do not run a rebuilt request
// again until the operator decides.

package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type repairFx struct {
	*reqFx
	waiting, accepted, reported, done string
}

// repairStore has one request in each of four states and one older queue item.
func repairStore(t *testing.T, key []byte) *repairFx {
	t.Helper()
	f := &repairFx{reqFx: newReqFx(t, key)}
	f.supervise("a", "claude-code")
	f.done = f.create(ReqAsk).ID
	f.take(f.done, "a")
	f.accept(f.done, "a")
	f.report(f.done, "a")
	f.ver.set(f.done, Verdict{Status: "done", Closes: true})
	f.tick(time.Minute)
	f.want(f.done, StateDone)
	f.reported = f.create(ReqAsk).ID
	f.take(f.reported, "a")
	f.accept(f.reported, "a")
	f.report(f.reported, "a")
	f.accepted = f.create(ReqAsk).ID
	f.take(f.accepted, "a")
	f.accept(f.accepted, "a")
	f.waiting = f.create(ReqAsk).ID
	if err := f.s.SaveQueueItem("item1", []byte(`{"id":"item1","role":"r"}`)); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *repairFx) path(name string) string { return filepath.Join(f.data, "queue.d", name) }

func (f *repairFx) read(name string) []byte {
	f.t.Helper()
	b, err := os.ReadFile(f.path(name))
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (f *repairFx) write(name string, b []byte) {
	f.t.Helper()
	if err := os.WriteFile(f.path(name), b, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// repaired opens the store as the daemon does, and requires a repair.
func (f *repairFx) repaired() *RepairReport {
	f.t.Helper()
	s, rep, err := OpenRequestsRepaired(f.options())
	if err != nil {
		f.t.Fatalf("repair: %v", err)
	}
	if rep == nil {
		f.t.Fatal("no repair report")
	}
	f.s = s
	return rep
}

// keptAsItWas requires every file of before to be, byte for byte, in the repair
// folder.
func keptAsItWas(t *testing.T, folder string, before map[string]string) {
	t.Helper()
	for name, body := range before {
		base, ok := strings.CutPrefix(name, "queue.d"+string(filepath.Separator))
		if !ok || !strings.HasSuffix(base, recordSuffix) || strings.ContainsRune(base, filepath.Separator) {
			continue
		}
		got, err := os.ReadFile(filepath.Join(folder, "originals", base))
		if err != nil || string(got) != body {
			t.Errorf("the original of %s is not kept: %v", base, err)
		}
	}
}

// held requires a request closed as failed "repaired", listed, and not to be
// run: it cannot be taken and no check closes it.
func (f *repairFx) held(rep *RepairReport, ids ...string) {
	f.t.Helper()
	for _, id := range ids {
		r := f.want(id, StateFailed)
		if r.ReasonCode != WhyRepaired || !r.MayRunTwice || !slices.Contains(rep.Held, id) {
			f.t.Fatalf("%s: %+v, held %v", id, r, rep.Held)
		}
		f.ver.set(id, Verdict{Status: "done", Closes: true})
		if _, err := f.s.Take(bg, sessCaller("a"), id, TakeInput{Via: ViaTool}); err == nil {
			f.t.Fatalf("%s was handed out again", id)
		}
	}
	f.tick(time.Minute)
	listed := f.listed()
	for _, id := range ids {
		f.want(id, StateFailed)
		if c, ok := listed[id]; !ok || c.Problem != WhyRepaired {
			f.t.Fatalf("%s is not shown to the operator as repaired: %+v", id, c)
		}
	}
}

func TestReqRepairRebuildsADamagedManifest(t *testing.T) {
	for name, key := range map[string][]byte{"plain": nil, "secure": testKey(3)} {
		t.Run(name, func(t *testing.T) {
			f := repairStore(t, key)
			f.write(metaFileName, []byte("not a manifest"))
			before := f.files()
			if _, err := OpenRequests(f.options()); !errors.Is(err, ErrRecords) {
				t.Fatalf("the plain open: %v", err)
			}
			rep := f.repaired()
			if !rep.Manifest || len(rep.SetAside)+len(rep.Missing)+len(rep.Restored) != 0 || rep.Folder == "" {
				t.Fatalf("%+v", rep)
			}
			keptAsItWas(t, rep.Folder, before)
			if r := f.want(f.done, StateDone); r.ProofAs != ProofDone {
				t.Fatalf("done: %+v", r)
			}
			if len(rep.Held) != 3 {
				t.Fatalf("held %v", rep.Held)
			}
			// The roles were in the manifest: they are gone, so give one again.
			f.supervise("a", "claude-code")
			f.held(rep, f.waiting, f.accepted, f.reported)
			items, err := f.s.LoadQueue()
			if err != nil || len(items) != 1 {
				t.Fatalf("queue: %v %v", items, err)
			}
			// The operator decides: a reissue needs "it may run twice".
			if _, err := f.s.Reissue(bg, opCaller, f.waiting, ReasonInput{ReasonCode: "again"}); !errors.Is(err, ErrMayRunTwice) {
				t.Fatalf("reissue without saying so: %v", err)
			}
			v, err := f.s.Reissue(bg, opCaller, f.waiting, ReasonInput{ReasonCode: "again", RepeatOK: true})
			if err != nil || v.State != StateWaiting {
				t.Fatalf("reissue: %+v %v", v, err)
			}
			// The repaired store stays repaired.
			f.reopen()
			f.want(f.done, StateDone)
			if key != nil {
				for p, body := range f.files() {
					if strings.Contains(body, "do the thing") {
						t.Fatalf("%s holds plain content", p)
					}
				}
			}
		})
	}
}

func TestReqRepairAMissingManifest(t *testing.T) {
	f := repairStore(t, testKey(3))
	if err := os.Remove(f.path(metaFileName)); err != nil {
		t.Fatal(err)
	}
	rep := f.repaired()
	if !rep.Manifest || len(rep.Held) != 3 || len(rep.SetAside) != 0 {
		t.Fatalf("%+v", rep)
	}
	f.want(f.done, StateDone)
}

// editIndex rewrites the readable index of a record, leaving the sealed copy.
func (f *repairFx) editIndex(id string, edit func(map[string]any)) {
	f.t.Helper()
	var d diskRecord
	if err := json.Unmarshal(f.read(id+recordSuffix), &d); err != nil {
		f.t.Fatal(err)
	}
	var index map[string]any
	if err := json.Unmarshal(d.Index, &index); err != nil {
		f.t.Fatal(err)
	}
	edit(index)
	d.Index, _ = json.Marshal(index)
	out, _ := json.Marshal(d)
	f.write(id+recordSuffix, out)
}

func TestReqRepairRestoresADifferingIndexFromTheSealedCopy(t *testing.T) {
	f := repairStore(t, testKey(3))
	f.editIndex(f.waiting, func(m map[string]any) { m["state"], m["proof_as"] = "done", "done" })
	before := f.files()
	rep := f.repaired()
	if rep.Manifest || !slices.Equal(rep.Restored, []string{f.waiting}) || !slices.Equal(rep.Held, []string{f.waiting}) ||
		len(rep.SetAside) != 0 {
		t.Fatalf("%+v", rep)
	}
	keptAsItWas(t, rep.Folder, before)
	if r := f.get(f.waiting); r.ProofAs != "" {
		t.Fatalf("the edited index was believed: %+v", r)
	}
	// Only the restored one is held; the others are as they were.
	f.want(f.accepted, StateAccepted)
	f.want(f.reported, StateReported)
	if len(f.s.Bindings()) != 1 {
		t.Fatal("an intact manifest keeps the roles")
	}
	f.held(rep, f.waiting)
}

func TestReqRepairSetsAnUnreadableRecordAside(t *testing.T) {
	for name, key := range map[string][]byte{"plain": nil, "secure": testKey(3)} {
		t.Run(name, func(t *testing.T) {
			f := repairStore(t, key)
			garbage := []byte("\x00\x01 half a record")
			f.write(f.waiting+recordSuffix, garbage)
			f.write(queuePrefix+"item1"+recordSuffix, []byte("\x00 half an item"))
			before := f.files()
			rep := f.repaired()
			want := []string{queuePrefix + "item1" + recordSuffix, f.waiting + recordSuffix}
			slices.Sort(want)
			got := slices.Clone(rep.SetAside)
			slices.Sort(got)
			if rep.Manifest || !slices.Equal(got, want) || len(rep.Held) != 0 {
				t.Fatalf("%+v", rep)
			}
			keptAsItWas(t, rep.Folder, before)
			kept, err := os.ReadFile(filepath.Join(rep.Folder, "set-aside", f.waiting+recordSuffix))
			if err != nil || !bytes.Equal(kept, garbage) {
				t.Fatalf("set aside: %v %q", err, kept)
			}
			if _, ok := f.s.Request(f.waiting); ok {
				t.Fatal("the record set aside is still believed")
			}
			// The others work.
			f.want(f.accepted, StateAccepted)
			f.report(f.accepted, "a")
			f.want(f.accepted, StateReported)
			if items, err := f.s.LoadQueue(); err != nil || len(items) != 0 {
				t.Fatalf("queue: %v %v", items, err)
			}
			f.reopen()
		})
	}
}

func TestReqRepairAMissingRecordIsReportedNotRepaired(t *testing.T) {
	f := repairStore(t, nil)
	if err := os.Remove(f.path(f.waiting + recordSuffix)); err != nil {
		t.Fatal(err)
	}
	rep := f.repaired()
	if !slices.Equal(rep.Missing, []string{f.waiting + recordSuffix}) || len(rep.SetAside)+len(rep.Held) != 0 {
		t.Fatalf("%+v", rep)
	}
	f.want(f.accepted, StateAccepted)
}

func TestReqRepairADamagedManifestAndADamagedRecord(t *testing.T) {
	f := repairStore(t, testKey(3))
	f.write(metaFileName, []byte("junk"))
	f.write(f.accepted+recordSuffix, []byte("junk too"))
	f.editIndex(f.reported, func(m map[string]any) { m["state"] = "done" })
	rep := f.repaired()
	if !rep.Manifest || !slices.Equal(rep.SetAside, []string{f.accepted + recordSuffix}) ||
		!slices.Equal(rep.Restored, []string{f.reported}) || len(rep.Held) != 2 {
		t.Fatalf("%+v", rep)
	}
	f.want(f.reported, StateFailed)
	f.want(f.done, StateDone)
}

// unchanged requires a refusal that left every file as it was.
func (f *repairFx) cannot(o RequestOptions) {
	f.t.Helper()
	before := f.files()
	s, rep, err := OpenRequestsRepaired(o)
	if !errors.Is(err, ErrCannotRepair) || s != nil || rep != nil {
		f.t.Fatalf("store=%v report=%+v err=%v", s != nil, rep, err)
	}
	same(f.t, before, f.files())
}

func TestReqRepairChangesNothingWithAWrongKey(t *testing.T) {
	f := repairStore(t, testKey(3))
	for _, key := range [][]byte{testKey(9), nil} {
		o := f.options()
		o.Key = key
		f.cannot(o)
	}
}

func TestReqRepairChangesNothingWhenTheOriginalsCannotBeKept(t *testing.T) {
	f := repairStore(t, nil)
	f.write(metaFileName, []byte("junk"))
	f.fail = func(path string) error {
		if strings.Contains(path, "originals") {
			return errors.New("disk full")
		}
		return nil
	}
	before := f.files()
	if _, _, err := OpenRequestsRepaired(f.options()); !errors.Is(err, ErrCannotRepair) {
		t.Fatalf("%v", err)
	}
	for name, body := range before {
		if f.files()[name] != body {
			t.Fatalf("%s changed", name)
		}
	}
}

func TestReqRepairLeavesAnotherVersionsStoreAlone(t *testing.T) {
	f := repairStore(t, nil)
	f.write(metaFileName, []byte(`{"version":2,"seq":1,"files":{}}`))
	f.cannot(f.options())
}

func TestReqRepairDoesNothingToASoundStore(t *testing.T) {
	f := repairStore(t, testKey(3))
	before := f.files()
	s, rep, err := OpenRequestsRepaired(f.options())
	if err != nil || rep != nil || s == nil {
		t.Fatalf("%+v %v", rep, err)
	}
	same(t, before, f.files())
}

// A request that waits for one the repair could not keep must not start as if
// that work were done.
func TestReqRepairHoldsWhatWaitedForALostRequest(t *testing.T) {
	f := repairStore(t, nil)
	child := f.create(ReqAsk, func(in *CreateInput) { in.After = f.accepted }).ID
	f.write(f.accepted+recordSuffix, []byte("junk"))
	rep := f.repaired()
	if !slices.Equal(rep.Held, []string{child}) {
		t.Fatalf("%+v", rep)
	}
	f.held(rep, child)
}

// Found by the live proof: a closed request whose readable index differs must
// be written back too, or the store stays damaged. It reads as the sealed copy
// has it.
func TestReqRepairRestoresAClosedRequestsIndexToo(t *testing.T) {
	for name, alsoManifest := range map[string]bool{"sound manifest": false, "damaged manifest": true} {
		t.Run(name, func(t *testing.T) {
			f := repairStore(t, testKey(3))
			gone := f.create(ReqAsk).ID
			if _, err := f.s.Drop(bg, opCaller, gone, ReasonInput{ReasonCode: "not_needed"}); err != nil {
				t.Fatal(err)
			}
			f.editIndex(gone, func(m map[string]any) { m["state"], m["proof_as"] = "done", "done" })
			f.editIndex(f.done, func(m map[string]any) { m["state"] = "waiting" })
			if alsoManifest {
				f.write(metaFileName, []byte("junk"))
			}
			rep := f.repaired()
			got := slices.Clone(rep.Restored)
			slices.Sort(got)
			want := []string{gone, f.done}
			slices.Sort(want)
			if !slices.Equal(got, want) || slices.Contains(rep.Held, gone) || slices.Contains(rep.Held, f.done) {
				t.Fatalf("%+v", rep)
			}
			if r := f.want(gone, StateCancelled); r.ProofAs != "" {
				t.Fatalf("the edited 'done' was believed: %+v", r)
			}
			if r := f.want(f.done, StateDone); r.ProofAs != ProofDone {
				t.Fatalf("%+v", r)
			}
			f.reopen()
			if err := f.s.faultError(); err != nil {
				t.Fatalf("still damaged: %v", err)
			}
		})
	}
}

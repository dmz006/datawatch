package usagecalc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func autoLanesLogTestEntry(i int) AutoLanesLogEntry {
	return AutoLanesLogEntry{
		At:           time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute),
		From:         i,
		To:           i + 1,
		Direction:    AutoLanesUp,
		Reason:       "entry " + strconv.Itoa(i),
		Policy:       "policy-" + strconv.Itoa(i),
		LanesRunning: 2,
		SpecialItems: 1,
		ReadingID:    "reading-" + strconv.Itoa(i),
	}
}

func TestAutoLanesLogAppends(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenAutoLanesLog(dir)
	if err != nil {
		t.Fatalf("OpenAutoLanesLog: %v", err)
	}
	if got := l.Len(); got != 0 {
		t.Fatalf("fresh log Len() = %d, want 0", got)
	}
	first, second, third := autoLanesLogTestEntry(1), autoLanesLogTestEntry(2), autoLanesLogTestEntry(3)
	for _, e := range []AutoLanesLogEntry{first, second, third} {
		if err := l.Append(e); err != nil {
			t.Fatalf("Append(%+v): %v", e, err)
		}
	}
	want := []AutoLanesLogEntry{first, second, third}
	if got := l.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	if !reflect.DeepEqual(l.Entries(), want) {
		t.Fatalf("Entries() = %+v, want appended order %+v", l.Entries(), want)
	}
	// The file was persisted with each append.
	raw, err := os.ReadFile(filepath.Join(dir, storeDirName, AutoLanesLogName))
	if err != nil {
		t.Fatalf("log file not written: %v", err)
	}
	var fromFile []AutoLanesLogEntry
	if err := json.Unmarshal(raw, &fromFile); err != nil {
		t.Fatalf("log file does not parse: %v", err)
	}
	if !reflect.DeepEqual(fromFile, want) {
		t.Fatalf("log file = %+v, want %+v", fromFile, want)
	}
}

func TestAutoLanesLogKeepsOnlyNewest500(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenAutoLanesLog(dir)
	if err != nil {
		t.Fatalf("OpenAutoLanesLog: %v", err)
	}
	const n = 505
	for i := 1; i <= n; i++ {
		if err := l.Append(autoLanesLogTestEntry(i)); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	if got := l.Len(); got != autoLanesLogMaxEntries {
		t.Fatalf("Len() = %d, want %d", got, autoLanesLogMaxEntries)
	}
	entries := l.Entries()
	if !reflect.DeepEqual(entries[0], autoLanesLogTestEntry(n-autoLanesLogMaxEntries+1)) {
		t.Fatalf("oldest kept entry = %+v, want entry %d", entries[0], n-autoLanesLogMaxEntries+1)
	}
	if !reflect.DeepEqual(entries[len(entries)-1], autoLanesLogTestEntry(n)) {
		t.Fatalf("newest kept entry = %+v, want entry %d", entries[0], n)
	}

	// The persisted file holds the same bounded log.
	l2, err := OpenAutoLanesLog(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := l2.Len(); got != autoLanesLogMaxEntries {
		t.Fatalf("reopened Len() = %d, want %d", got, autoLanesLogMaxEntries)
	}
	if !reflect.DeepEqual(l2.Entries(), entries) {
		t.Fatal("reopened log differs from the bounded in-memory log")
	}
}

func TestAutoLanesLogRoundTripsThroughTempDir(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenAutoLanesLog(dir)
	if err != nil {
		t.Fatalf("OpenAutoLanesLog: %v", err)
	}
	want := make([]AutoLanesLogEntry, 0, 7)
	for i := 1; i <= 7; i++ {
		e := autoLanesLogTestEntry(i)
		if err := l.Append(e); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
		want = append(want, e)
	}

	l2, err := OpenAutoLanesLog(dir)
	if err != nil {
		t.Fatalf("reopen after writes: %v", err)
	}
	if !reflect.DeepEqual(l2.Entries(), want) {
		t.Fatalf("round-tripped log = %+v, want %+v", l2.Entries(), want)
	}

	// A fresh log for a directory with no file starts empty.
	empty, err := OpenAutoLanesLog(filepath.Join(dir, "fresh"))
	if err != nil {
		t.Fatalf("OpenAutoLanesLog(empty dir): %v", err)
	}
	if got := empty.Len(); got != 0 {
		t.Fatalf("fresh log Len() = %d, want 0", got)
	}

	// A corrupt file never panics: empty log plus the parse error.
	corrupt := filepath.Join(dir, "corrupt")
	if err := os.MkdirAll(filepath.Join(corrupt, storeDirName), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, storeDirName, AutoLanesLogName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	c, err := OpenAutoLanesLog(corrupt)
	if err == nil {
		t.Fatal("corrupt file: expected an error, got nil")
	}
	if c == nil || c.Len() != 0 {
		t.Fatalf("corrupt file: want a non-nil empty log, got %+v", c)
	}
}

func TestAutoLanesLogWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenAutoLanesLog(dir)
	if err != nil {
		t.Fatalf("OpenAutoLanesLog: %v", err)
	}
	sub := filepath.Join(dir, storeDirName)
	for i := 1; i <= 3; i++ {
		if err := l.Append(autoLanesLogTestEntry(i)); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	// The atomic dance leaves exactly the log file: the temp file was renamed
	// into place, never left or left behind.
	entries, err := os.ReadDir(sub)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != AutoLanesLogName {
		t.Fatalf("directory after Appends = %v, want [%s]", names, AutoLanesLogName)
	}

	// Each persisted state is a complete, valid log — never a half-written
	// one — and matches the in-memory log byte-for-byte in content.
	raw, err := os.ReadFile(filepath.Join(sub, AutoLanesLogName))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var fromFile []AutoLanesLogEntry
	if err := json.Unmarshal(raw, &fromFile); err != nil {
		t.Fatalf("persisted log is not a complete JSON document: %v", err)
	}
	if !reflect.DeepEqual(fromFile, l.Entries()) {
		t.Fatalf("persisted log = %+v, want %+v", fromFile, l.Entries())
	}

	// A log whose write fails keeps its previous contents: a failed append
	// must not clear the entries held in memory.
	failing := &AutoLanesLog{path: filepath.Join(dir, "missing-dir", AutoLanesLogName), entries: []AutoLanesLogEntry{autoLanesLogTestEntry(1)}}
	if err := failing.Append(autoLanesLogTestEntry(2)); err == nil {
		t.Fatal("append with an unwritable directory: expected an error")
	}
	if got := failing.Len(); got != 1 {
		t.Fatalf("after failed Append, Len() = %d, want the previous 1", got)
	}
}

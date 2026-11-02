package burnguard

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultWorkerMarker is the part of a transcript directory name that marks a session run in
// a task worktree (a worker or a verifier). Claude names a project directory after the
// session's working directory with the separators turned into dashes, so a worktree under
// .../prd-worktrees/... carries this text.
const DefaultWorkerMarker = "prd-worktrees"

// Tokens is a count of tokens by kind, as recorded in a transcript's usage block.
type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheWrite int64 `json:"cache_write"`
	CacheRead  int64 `json:"cache_read"`
}

// Add returns t plus o.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{t.Input + o.Input, t.Output + o.Output, t.CacheWrite + o.CacheWrite, t.CacheRead + o.CacheRead}
}

// Bucket is the tokens of one clock hour, split between worker/verifier sessions and the
// rest. Hour is the start of the hour in UTC.
type Bucket struct {
	Hour   time.Time `json:"hour"`
	Worker Tokens    `json:"worker"`
	Other  Tokens    `json:"other"`
}

// Total is the worker and other tokens together.
func (b Bucket) Total() Tokens { return b.Worker.Add(b.Other) }

// Usage is the result of reading a transcript directory.
type Usage struct {
	// Buckets are the hourly buckets, oldest first.
	Buckets []Bucket `json:"buckets"`
	// Through is the moment of the newest assistant turn read. The newest bucket may cover
	// only part of its hour; Through tells the fit and the projection where the data ends.
	Through time.Time `json:"through"`
	// Files is the number of transcript files read.
	Files int `json:"files"`
	// Turns is the number of assistant turns counted.
	Turns int `json:"turns"`
	// Duplicates is the number of turns skipped because their request id was already seen.
	Duplicates int `json:"duplicates"`
	// Malformed is the number of lines skipped because they were not usable JSON, or an
	// assistant turn carried no usable timestamp.
	Malformed int `json:"malformed"`
}

// ReadOptions tunes ReadDir. The zero value reads everything and uses DefaultWorkerMarker.
type ReadOptions struct {
	// Since drops turns before this moment, and files not modified since then.
	Since time.Time
	// WorkerMarker is the text in a project directory name that marks a worker or verifier
	// session. Empty means DefaultWorkerMarker.
	WorkerMarker string
}

// transcriptLine is the part of a transcript line the reader needs.
type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   *struct {
		ID    string `json:"id"`
		Usage *struct {
			Input       int64 `json:"input_tokens"`
			Output      int64 `json:"output_tokens"`
			CacheCreate int64 `json:"cache_creation_input_tokens"`
			CacheRead   int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ReadDir reads the transcripts under dir: the files dir/<project>/*.jsonl, the layout of
// ~/.claude/projects. The directory is always passed in; the package never looks in the
// home directory itself. A turn counts once per request id (or message id when there is no
// request id): Claude writes one transcript line per content block of a response, and
// resumed sessions copy earlier turns, so the same request appears many times.
//
// A line that is not JSON, or an assistant turn with no readable timestamp, is counted in
// Malformed and skipped. A directory that cannot be read is an error; a file that cannot be
// read is skipped.
func ReadDir(dir string, opts ReadOptions) (*Usage, error) {
	if dir == "" {
		return nil, errors.New("burnguard: transcript directory not set")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	sort.Strings(files)
	marker := opts.WorkerMarker
	if marker == "" {
		marker = DefaultWorkerMarker
	}
	u := &Usage{}
	seen := map[string]bool{}
	hours := map[time.Time]*Bucket{}
	for _, f := range files {
		if !opts.Since.IsZero() {
			if st, err := os.Stat(f); err != nil || st.ModTime().Before(opts.Since) {
				continue
			}
		}
		worker := strings.Contains(filepath.Base(filepath.Dir(f)), marker)
		if err := readFile(f, worker, opts.Since, u, seen, hours); err != nil {
			continue
		}
		u.Files++
	}
	u.Buckets = make([]Bucket, 0, len(hours))
	for _, b := range hours {
		u.Buckets = append(u.Buckets, *b)
	}
	sort.Slice(u.Buckets, func(i, j int) bool { return u.Buckets[i].Hour.Before(u.Buckets[j].Hour) })
	return u, nil
}

func readFile(path string, worker bool, since time.Time, u *Usage, seen map[string]bool, hours map[time.Time]*Bucket) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = fh.Close() }()
	// Lines can be megabytes (tool output), so a Scanner's token limit is not used.
	r := bufio.NewReaderSize(fh, 1<<20)
	for {
		raw, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(raw))) > 0 {
			countLine(raw, worker, since, u, seen, hours)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func countLine(raw []byte, worker bool, since time.Time, u *Usage, seen map[string]bool, hours map[time.Time]*Bucket) {
	var l transcriptLine
	if err := json.Unmarshal(raw, &l); err != nil {
		u.Malformed++
		return
	}
	if l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		u.Malformed++
		return
	}
	id := l.RequestID
	if id == "" {
		id = l.Message.ID
	}
	if id != "" {
		if seen[id] {
			u.Duplicates++
			return
		}
		seen[id] = true
	}
	if !since.IsZero() && at.Before(since) {
		return
	}
	at = at.UTC()
	hour := at.Truncate(time.Hour)
	b := hours[hour]
	if b == nil {
		b = &Bucket{Hour: hour}
		hours[hour] = b
	}
	use := l.Message.Usage
	t := Tokens{Input: use.Input, Output: use.Output, CacheWrite: use.CacheCreate, CacheRead: use.CacheRead}
	if worker {
		b.Worker = b.Worker.Add(t)
	} else {
		b.Other = b.Other.Add(t)
	}
	u.Turns++
	if at.After(u.Through) {
		u.Through = at
	}
}

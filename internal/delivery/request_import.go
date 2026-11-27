// Moving queue.json into the sealed store, and going back. Owner: part A.
//
// Import writes each item sealed, reads every one back, and only then replaces
// queue.json with QueueMarker, which an older program cannot parse: it stops
// with "parse queue store" instead of starting with an empty queue. No plain
// backup is kept. Overwriting is not reliable on an SSD or a copy-on-write
// disk: the migration message says so.
//
// While queue.json holds a queue, it is the truth: an older datawatch may have
// run after an export or an interrupted import. Stored items it does not hold
// are removed.

package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// QueueMarker replaces queue.json after the import. It is deliberately not
// JSON.
const QueueMarker = "datawatch moved this queue into queue.d. This file is deliberately not JSON: an older datawatch must stop here, not start with an empty queue. To go back, use: datawatch queue export\n"

// ErrExportPlain: on a secure install an export is plain content.
var ErrExportPlain = errors.New("this install is secure and queue.json is plain content: say --plain to write it anyway")

func parseQueueFile(data []byte) (map[string]json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse queue store: %w", err)
	}
	out := map[string]json.RawMessage{}
	for _, raw := range items {
		var head struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil || !queueIDRe.MatchString(head.ID) {
			return nil, errors.New("parse queue store: an item has no usable ID; nothing was imported")
		}
		out[head.ID] = raw
	}
	return out, nil
}

// ImportQueue moves the items of the queue.json at path into the store. With
// no queue.json, or after an earlier import, it only keeps the marker there.
func (s *Requests) ImportQueue(path string) (imported int, err error) {
	data, err := os.ReadFile(path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return 0, fmt.Errorf("read queue store: %w", err)
	}
	if string(data) == QueueMarker {
		return 0, nil
	}
	if !missing {
		items, err := parseQueueFile(data)
		if err != nil {
			return 0, err
		}
		ids := make([]string, 0, len(items))
		for id := range items {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if err := s.SaveQueueItem(id, items[id]); err != nil {
				return 0, err
			}
			if err := s.readBack(id, items[id]); err != nil {
				return 0, err
			}
		}
		stored, err := s.LoadQueue()
		if err != nil {
			return 0, err
		}
		for _, raw := range stored {
			var head struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &head) == nil {
				if _, ok := items[head.ID]; !ok {
					if err := s.DeleteQueueItem(head.ID); err != nil {
						return 0, err
					}
				}
			}
		}
		imported = len(items)
	}
	if err := s.Healthy(); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrCannotRecord, err)
	}
	if err := secfile.WriteFileDurable(path, []byte(QueueMarker), 0o600, nil); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrCannotRecord, err)
	}
	return imported, nil
}

// readBack opens one item's file again and compares it.
func (s *Requests) readBack(id string, want []byte) error {
	data, err := os.ReadFile(filepath.Join(s.opts.Dir, queuePrefix+id+recordSuffix))
	if err != nil {
		return fmt.Errorf("%w: read back item %s: %v", ErrCannotRecord, id, err)
	}
	got, _, err := s.openSealed(data, s.opts.Key == nil)
	if err != nil || !sameJSON(got, want) {
		return fmt.Errorf("%w: item %s did not read back", ErrCannotRecord, id)
	}
	return nil
}

// ExportQueue writes the queue items back as queue.json. On a secure install
// the file is plain content, so plain must be true.
func (s *Requests) ExportQueue(path string, plain bool) (exported int, err error) {
	if s.opts.Key != nil && !plain {
		return 0, ErrExportPlain
	}
	items, err := s.LoadQueue()
	if err != nil {
		return 0, err
	}
	// Oldest first, as the older program writes it.
	made := func(raw json.RawMessage) time.Time {
		var head struct {
			CreatedAt time.Time `json:"created_at"`
		}
		_ = json.Unmarshal(raw, &head)
		return head.CreatedAt
	}
	sort.SliceStable(items, func(i, j int) bool { return made(items[i]).Before(made(items[j])) })
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := secfile.WriteFileDurable(path, data, 0o600, nil); err != nil {
		return 0, err
	}
	return len(items), nil
}

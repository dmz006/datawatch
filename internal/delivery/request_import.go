// Moving queue.json into the sealed store, and going back. Owner: part A. STUB.
//
// Import writes each item sealed, reads every one back, and only then replaces
// queue.json with QueueMarker, which an older program cannot parse: it stops
// with "parse queue store" instead of starting with an empty queue. No plain
// backup is kept. Overwriting is not reliable on an SSD or a copy-on-write
// disk: the migration message says so.

package delivery

// QueueMarker replaces queue.json after the import. It is deliberately not
// JSON.
const QueueMarker = "datawatch moved this queue into queue.d. This file is deliberately not JSON: an older datawatch must stop here, not start with an empty queue. To go back, use: datawatch queue export\n"

// ImportQueue moves the items of the queue.json at path into the store.
func (s *Requests) ImportQueue(string) (imported int, err error) { return 0, ErrRequestsNotBuilt }

// ExportQueue writes the queue items back as queue.json. On a secure install
// the file is plain content, so plain must be true.
func (s *Requests) ExportQueue(string, bool) (exported int, err error) {
	return 0, ErrRequestsNotBuilt
}

// An .mcp.json belongs to whoever wrote it. datawatch changes only the server
// entries it means to, keeps everything else, and never writes a file it could
// not read in full.

package channel

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dmz006/datawatch/internal/toolsettings"
)

// mcpFile is one .mcp.json read in full: doc is the whole file and servers its
// "mcpServers" object, part of doc.
type mcpFile struct {
	real    string // where to write it: a link stays a link
	doc     map[string]any
	servers map[string]any
}

// readMCPFile reads path. A missing file gives an empty one.
func readMCPFile(path string) (*mcpFile, error) {
	real, doc, _, err := toolsettings.ReadThroughLink(path)
	if err != nil {
		return nil, err
	}
	f := &mcpFile{real: real, doc: doc}
	if doc["mcpServers"] != nil {
		servers, ok := doc["mcpServers"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: \"mcpServers\" is not an object, so the file is left as it is", path)
		}
		f.servers = servers
	}
	return f, nil
}

func (f *mcpFile) has(name string) bool {
	_, ok := f.servers[name]
	return ok
}

// spec is the entry called name as datawatch reads it.
func (f *mcpFile) spec(name string) (MCPServerSpec, bool) {
	var s MCPServerSpec
	raw, err := json.Marshal(f.servers[name])
	if !f.has(name) || err != nil || json.Unmarshal(raw, &s) != nil {
		return MCPServerSpec{}, false
	}
	return s, true
}

func (f *mcpFile) set(name string, spec MCPServerSpec) {
	if f.servers == nil {
		f.servers = map[string]any{}
		f.doc["mcpServers"] = f.servers
	}
	f.servers[name] = spec
}

// write replaces the file, readable by its owner only: an entry can hold a
// token.
func (f *mcpFile) write() error {
	return toolsettings.Write(f.real, f.doc, 0o600)
}

// removeEntry removes the entry called name from path. With dropEmpty the file
// is deleted when nothing but an empty server list would be left. A file that
// cannot be read is left alone.
func removeEntry(path, name string, dropEmpty bool) error {
	if _, err := os.Lstat(path); err != nil {
		return nil // no file, nothing to remove
	}
	f, err := readMCPFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[channel] %v\n", err)
		return nil
	}
	if !f.has(name) {
		return nil
	}
	delete(f.servers, name)
	if dropEmpty && len(f.servers) == 0 && len(f.doc) == 1 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return f.write()
}

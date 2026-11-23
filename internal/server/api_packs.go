package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/packs"
	"github.com/dmz006/datawatch/internal/skills"
)

// Pack REST surface:
//
//	GET    /api/packs/config                        — packs.enabled and packs.active
//	PUT    /api/packs/config                        — set either; enabling checks the connected registry sources
//	GET    /api/packs                               — installed packs with their trust state
//	POST   /api/packs/install                       — {"source_dir": "..."} install a pack folder
//	POST   /api/packs/accept                        — {"name","version"} record operator acceptance
//	DELETE /api/packs/{name}/{version}              — remove an installed pack
//	GET    /api/packs/inputs?level=&pack=[&key=]    — read stored inputs (secrets stay references)
//	PUT    /api/packs/inputs?level=&pack=[&key=]    — {"inputs": {"name": "value"}} replace one scope
//
// level is global, project or automaton; key is the project directory or the
// Automaton id and is required for the latter two.

var packsMu sync.Mutex

const packsInputsFile = "inputs.json"

// packInputStore is the on-disk shape of inputs.json.
type packInputStore struct {
	Global    map[string]map[string]string            `json:"global"`
	Projects  map[string]map[string]map[string]string `json:"projects"`
	Automaton map[string]map[string]map[string]string `json:"automata"`
}

func (s *Server) packsRoot() string { return s.statePath("packs") }

func (s *Server) loadPackInputs() (*packInputStore, error) {
	st := &packInputStore{}
	data, err := os.ReadFile(filepath.Join(s.packsRoot(), packsInputsFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, st); err != nil {
			return nil, err
		}
	}
	if st.Global == nil {
		st.Global = map[string]map[string]string{}
	}
	if st.Projects == nil {
		st.Projects = map[string]map[string]map[string]string{}
	}
	if st.Automaton == nil {
		st.Automaton = map[string]map[string]map[string]string{}
	}
	return st, nil
}

func (s *Server) savePackInputs(st *packInputStore) error {
	root := s.packsRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".inputs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(root, packsInputsFile))
}

// scopeMap returns the (possibly nil) input map for one pack at one level.
func (st *packInputStore) scopeMap(level, key, pack string) map[string]string {
	switch level {
	case "global":
		return st.Global[pack]
	case "project":
		return st.Projects[key][pack]
	default:
		return st.Automaton[key][pack]
	}
}

func (st *packInputStore) setScope(level, key, pack string, vals map[string]string) {
	switch level {
	case "global":
		st.Global[pack] = vals
	case "project":
		if st.Projects[key] == nil {
			st.Projects[key] = map[string]map[string]string{}
		}
		st.Projects[key][pack] = vals
	default:
		if st.Automaton[key] == nil {
			st.Automaton[key] = map[string]map[string]string{}
		}
		st.Automaton[key][pack] = vals
	}
}

// installedPacks loads every pack folder under the install root.
func (s *Server) installedPacks() ([]*packs.Pack, error) {
	root := s.packsRoot()
	names, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*packs.Pack
	for _, n := range names {
		if !n.IsDir() || strings.HasPrefix(n.Name(), ".") {
			continue
		}
		vers, err := os.ReadDir(filepath.Join(root, n.Name()))
		if err != nil {
			return nil, err
		}
		for _, v := range vers {
			if !v.IsDir() {
				continue
			}
			p, err := packs.LoadPackFolder(filepath.Join(root, n.Name(), v.Name()))
			if err != nil {
				continue // a damaged folder is not listed
			}
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

func (s *Server) installedPackNames() []string {
	list, _ := s.installedPacks()
	seen := map[string]bool{}
	var names []string
	for _, p := range list {
		if !seen[p.Name] {
			seen[p.Name] = true
			names = append(names, p.Name)
		}
	}
	return names
}

// connectedRegistries returns the skills registry sources, by value.
func (s *Server) connectedRegistries() []skills.Registry {
	if s.skillsMgr == nil {
		return nil
	}
	var out []skills.Registry
	for _, r := range s.skillsMgr.Store().ListRegistries() {
		if r != nil {
			out = append(out, *r)
		}
	}
	return out
}

func packsErr(w http.ResponseWriter, msg string, code int) {
	http.Error(w, msg, code)
}

func (s *Server) handlePacks(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/packs"), "/")
	read := r.Method == http.MethodGet
	capName := federation.CapConfigWrite
	if read {
		capName = federation.CapConfigRead
	}
	if !s.fedCap(w, r, capName) {
		return
	}
	switch {
	case rest == "" && read:
		s.packsList(w)
	case rest == "config":
		s.packsConfig(w, r)
	case rest == "install" && r.Method == http.MethodPost:
		s.packsInstall(w, r)
	case rest == "accept" && r.Method == http.MethodPost:
		s.packsAccept(w, r)
	case rest == "inputs":
		s.packsInputs(w, r)
	case r.Method == http.MethodDelete && strings.Count(rest, "/") == 1:
		parts := strings.SplitN(rest, "/", 2)
		s.packsRemove(w, parts[0], parts[1])
	default:
		packsErr(w, "not found or method not allowed", http.StatusNotFound)
	}
}

func (s *Server) packsList(w http.ResponseWriter) {
	packsMu.Lock()
	defer packsMu.Unlock()
	list, err := s.installedPacks()
	if err != nil {
		packsErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	store, err := packs.LoadTrustStore(s.packsRoot())
	if err != nil {
		packsErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"name": p.Name, "version": p.Version, "description": p.Description,
			"digest": p.Digest, "accepted": store.Runnable(p) == nil,
			"summary": packs.Summarize(p),
		})
	}
	writeJSONOK(w, map[string]any{"packs": out})
}

func (s *Server) packsConfig(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		packsErr(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	packsMu.Lock()
	defer packsMu.Unlock()
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, s.cfg.Packs)
	case http.MethodPut:
		var req struct {
			Enabled *bool     `json:"enabled"`
			Active  *[]string `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			packsErr(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		next := s.cfg.Packs
		if req.Enabled != nil {
			next.Enabled = *req.Enabled
		}
		if req.Active != nil {
			next.Active = append([]string(nil), (*req.Active)...)
		}
		if err := next.Validate(s.installedPackNames()); err != nil {
			packsErr(w, err.Error(), http.StatusBadRequest)
			return
		}
		if next.Enabled && !s.cfg.Packs.Enabled {
			if err := packs.CheckEnable(s.connectedRegistries()); err != nil {
				packsErr(w, err.Error(), http.StatusConflict)
				return
			}
		}
		prev := s.cfg.Packs
		s.cfg.Packs = next
		if s.cfgPath != "" {
			if err := s.saveConfig(); err != nil {
				s.cfg.Packs = prev
				packsErr(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSONOK(w, s.cfg.Packs)
	default:
		packsErr(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) packsInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceDir string `json:"source_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SourceDir == "" {
		packsErr(w, "bad request: source_dir is required", http.StatusBadRequest)
		return
	}
	packsMu.Lock()
	defer packsMu.Unlock()
	if err := os.MkdirAll(s.packsRoot(), 0o700); err != nil {
		packsErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inst, err := packs.Install(req.SourceDir, s.packsRoot())
	if err != nil {
		packsErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONOK(w, map[string]any{"name": inst.Name, "version": inst.Version, "digest": inst.Digest, "dir": inst.Dir})
}

func (s *Server) packsAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Version == "" {
		packsErr(w, "bad request: name and version are required", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(req.Name+req.Version, `/\`) || req.Name == ".." || req.Version == ".." {
		packsErr(w, "bad request: unsafe name or version", http.StatusBadRequest)
		return
	}
	packsMu.Lock()
	defer packsMu.Unlock()
	p, err := packs.LoadPackFolder(filepath.Join(s.packsRoot(), req.Name, req.Version))
	if err != nil {
		packsErr(w, "pack is not installed: "+err.Error(), http.StatusNotFound)
		return
	}
	store, err := packs.LoadTrustStore(s.packsRoot())
	if err != nil {
		packsErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rec, err := store.Accept(p, "operator", time.Now())
	if err != nil {
		packsErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONOK(w, rec)
}

func (s *Server) packsRemove(w http.ResponseWriter, name, version string) {
	packsMu.Lock()
	defer packsMu.Unlock()
	if err := packs.Remove(name, version, s.packsRoot()); err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not installed") {
			code = http.StatusNotFound
		}
		packsErr(w, err.Error(), code)
		return
	}
	writeJSONOK(w, map[string]any{"removed": name + "/" + version})
}

// declaredInputs returns the input declarations of a pack: the built-in pack,
// or the newest-sorted installed version of a named pack.
func (s *Server) declaredInputs(name string) ([]packs.Input, bool) {
	if name == "builtin" {
		p, err := packs.LoadBuiltin()
		if err != nil {
			return nil, false
		}
		return p.Inputs, true
	}
	list, _ := s.installedPacks()
	var found *packs.Pack
	for _, p := range list {
		if p.Name == name {
			found = p
		}
	}
	if found == nil {
		return nil, false
	}
	return found.Inputs, true
}

func (s *Server) packsInputs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	level, key, pack := q.Get("level"), q.Get("key"), q.Get("pack")
	if level != "global" && level != "project" && level != "automaton" {
		packsErr(w, "level must be global, project or automaton", http.StatusBadRequest)
		return
	}
	if level != "global" && key == "" {
		packsErr(w, "key is required for the "+level+" level", http.StatusBadRequest)
		return
	}
	if pack == "" {
		packsErr(w, "pack is required", http.StatusBadRequest)
		return
	}
	decls, ok := s.declaredInputs(pack)
	if !ok {
		packsErr(w, "pack "+pack+" is not available", http.StatusNotFound)
		return
	}
	packsMu.Lock()
	defer packsMu.Unlock()
	st, err := s.loadPackInputs()
	if err != nil {
		packsErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		vals := packs.RedactInputs(st.scopeMap(level, key, pack), decls)
		writeJSONOK(w, map[string]any{"level": level, "key": key, "pack": pack, "inputs": vals})
	case http.MethodPut:
		var req struct {
			Inputs map[string]string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			packsErr(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		byName := map[string]packs.Input{}
		for _, d := range decls {
			byName[d.Name] = d
		}
		for name, v := range req.Inputs {
			d, ok := byName[name]
			if !ok {
				packsErr(w, "pack "+pack+" declares no input "+name, http.StatusBadRequest)
				return
			}
			if err := packs.CheckSecretInput(d, v); err != nil {
				packsErr(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if req.Inputs == nil {
			req.Inputs = map[string]string{}
		}
		st.setScope(level, key, pack, req.Inputs)
		if err := s.savePackInputs(st); err != nil {
			packsErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"level": level, "key": key, "pack": pack, "inputs": packs.RedactInputs(req.Inputs, decls)})
	default:
		packsErr(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

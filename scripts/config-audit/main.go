// Command config-audit checks that every configuration key reaches every
// surface the configuration accessibility rule names, prints a table of the
// gaps, and exits non-zero when a key is missing without a recorded
// exemption.
//
// This is datawatch build and lint tooling, not a product feature.
//
// Keys come from the Config struct in internal/config, read with go/ast (no
// reflection, no compile of the daemon). Each key is then checked against:
//
//	ref  docs/config-reference.yaml (the annotated config.yaml reference)
//	web  the Settings rows in internal/server/web/app.js
//	get  the map handleGetConfig builds in internal/server/api.go
//	put  the keys handlePutConfig and applyConfigPatch accept
//
// The comm channel `configure` verb, `datawatch config set` and the MCP
// config_set tool all forward the key to PUT /api/config, so a key is as
// reachable there as it is on put. The audit checks that those three
// forwarders are still generic (they reach PUT /api/config and carry no key
// list of their own) instead of repeating the put column three times.
//
// The program reads only the named files and directories; it never walks the
// repository. Exit codes: 0 no unexempted gap, 1 a gap or a broken forwarder,
// 2 a usage, input or exemptions-file error.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

// surfaces are the per-key columns, in table order.
var surfaces = []string{"ref", "web", "get", "put"}

// Paths of the inputs, relative to the repository root.
const (
	configDir    = "internal/config"
	serverDir    = "internal/server"
	webFile      = "internal/server/web/app.js"
	refFile      = "docs/config-reference.yaml"
	cliMainFile  = "cmd/datawatch/main.go"
	mcpToolsFile = "internal/mcp/memory_tools.go"
	defaultExemp = "scripts/config-audit-exemptions.txt"
)

// keySet records which keys a surface covers.
type keySet struct {
	exact    map[string]bool
	parents  map[string]bool // every proper dotted ancestor of an exact key
	prefixes []string        // each ends in "."; covers every key beneath it
}

func newKeySet() *keySet {
	return &keySet{exact: map[string]bool{}, parents: map[string]bool{}}
}

func (k *keySet) add(key string) {
	if key == "" || k.exact[key] {
		return
	}
	k.exact[key] = true
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			k.parents[key[:i]] = true
		}
	}
}

func (k *keySet) addPrefix(p string) {
	if p == "" {
		return
	}
	if !strings.HasSuffix(p, ".") {
		p += "."
	}
	for _, have := range k.prefixes {
		if have == p {
			return
		}
	}
	k.prefixes = append(k.prefixes, p)
}

// covers reports whether the surface reaches key: the key itself, a deeper
// key under it (a map or list shown through its entries), or a prefix
// handler that owns it.
func (k *keySet) covers(key string) bool {
	if k == nil {
		return false
	}
	if k.exact[key] || k.parents[key] {
		return true
	}
	for _, p := range k.prefixes {
		if strings.HasPrefix(key, p) || strings.HasPrefix(p, key+".") {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Go package loading (non-recursive, one named directory)
// ---------------------------------------------------------------------------

type goPackage struct {
	files   map[string]*ast.File
	structs map[string]*ast.StructType
	funcs   map[string][]*ast.FuncDecl
	consts  map[string]ast.Expr
	vars    map[string]ast.Expr // package-level var initialisers
	order   []string            // file names, sorted, for deterministic walks
}

// loadPackage parses the non-test .go files directly inside dir.
func loadPackage(dir string) (*goPackage, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	p := &goPackage{
		files:   map[string]*ast.File{},
		structs: map[string]*ast.StructType{},
		funcs:   map[string][]*ast.FuncDecl{},
		consts:  map[string]ast.Expr{},
		vars:    map[string]ast.Expr{},
	}
	fset := token.NewFileSet()
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, n), err)
		}
		p.files[n] = f
		p.order = append(p.order, n)
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				p.funcs[d.Name.Name] = append(p.funcs[d.Name.Name], d)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if st, ok := s.Type.(*ast.StructType); ok {
							p.structs[s.Name.Name] = st
						}
					case *ast.ValueSpec:
						for i, nm := range s.Names {
							if i >= len(s.Values) {
								continue
							}
							switch d.Tok {
							case token.CONST:
								p.consts[nm.Name] = s.Values[i]
							case token.VAR:
								p.vars[nm.Name] = s.Values[i]
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(p.order)
	return p, nil
}

// constString resolves a string constant expression: a literal, a named
// constant (here, or in config for a config.X selector), or a + of those.
func constString(e ast.Expr, pkgs ...*goPackage) (string, bool) {
	return constStringDepth(e, 0, pkgs)
}

func constStringDepth(e ast.Expr, depth int, pkgs []*goPackage) (string, bool) {
	if depth > 6 {
		return "", false
	}
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.ParenExpr:
		return constStringDepth(e.X, depth+1, pkgs)
	case *ast.Ident:
		for _, p := range pkgs {
			if v, ok := p.consts[e.Name]; ok {
				return constStringDepth(v, depth+1, pkgs)
			}
		}
	case *ast.SelectorExpr:
		for _, p := range pkgs {
			if v, ok := p.consts[e.Sel.Name]; ok {
				return constStringDepth(v, depth+1, pkgs)
			}
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			a, ok1 := constStringDepth(e.X, depth+1, pkgs)
			b, ok2 := constStringDepth(e.Y, depth+1, pkgs)
			return a + b, ok1 && ok2
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// keys: the Config struct
// ---------------------------------------------------------------------------

// configKeys lists every leaf key of the Config struct in dir as a dotted
// path, sorted and de-duplicated, using yaml.v3's naming: the tag name, else
// the lower-cased field name. yaml:"-" and unexported fields are skipped,
// ",inline" flattens a nested struct into its parent, nested structs and
// pointers to structs recurse, and a map, slice, interface or foreign type
// is one leaf.
func configKeys(dir string) ([]string, error) {
	p, err := loadPackage(dir)
	if err != nil {
		return nil, err
	}
	root, ok := p.structs["Config"]
	if !ok {
		return nil, fmt.Errorf("%s: no struct Config", dir)
	}
	seen := map[string]bool{}
	walkStruct(p, root, "", seen, map[string]bool{}, 0)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func walkStruct(p *goPackage, st *ast.StructType, prefix string, seen map[string]bool, onPath map[string]bool, depth int) {
	if depth > 16 || st.Fields == nil {
		return
	}
	for _, f := range st.Fields.List {
		tag := ""
		if f.Tag != nil {
			if raw, err := strconv.Unquote(f.Tag.Value); err == nil {
				tag = reflect.StructTag(raw).Get("yaml")
			}
		}
		parts := strings.Split(tag, ",")
		name, inline := parts[0], false
		for _, o := range parts[1:] {
			if o == "inline" {
				inline = true
			}
		}
		if name == "-" {
			continue
		}
		fieldNames := []string{}
		for _, n := range f.Names {
			fieldNames = append(fieldNames, n.Name)
		}
		if len(fieldNames) == 0 { // embedded: named after its type
			fieldNames = []string{typeName(f.Type)}
		}
		for _, fn := range fieldNames {
			if fn == "" || !ast.IsExported(fn) {
				continue
			}
			key := name
			if key == "" {
				key = strings.ToLower(fn)
			}
			tn := typeName(f.Type)
			sub, isStruct := p.structs[tn]
			if isStruct && isPlainType(f.Type) {
				if onPath[tn] {
					continue
				}
				onPath[tn] = true
				next := join(prefix, key)
				if inline {
					next = prefix
				}
				walkStruct(p, sub, next, seen, onPath, depth+1)
				delete(onPath, tn)
				continue
			}
			if inline { // an inline non-struct is not nestable; ignore it
				continue
			}
			seen[join(prefix, key)] = true
		}
	}
}

// typeName returns the identifier of a plain or pointer-to-plain type ("" for
// everything else); isPlainType is true for T and *T only (not slices/maps).
func typeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return typeName(e.X)
	case *ast.SelectorExpr:
		return "" // a type from another package is a leaf
	}
	return ""
}

func isPlainType(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident:
		return true
	case *ast.StarExpr:
		return isPlainType(e.X)
	}
	return false
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// ---------------------------------------------------------------------------
// surface: ref (docs/config-reference.yaml)
// ---------------------------------------------------------------------------

type refFrame struct {
	indent int
	name   string
}

var (
	// a comment that starts with a dotted key: `# autonomous.decisions.enabled — type bool`
	refDottedComment = regexp.MustCompile(`^#\s*([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)(?:\s|:|,|$)`)
	// a commented-out YAML line: `#   enabled: true` or `# decisions:`
	refSampleComment = regexp.MustCompile(`^#\s?( *)([a-z][a-z0-9_]*):(?:\s|$)`)
)

// refComment reads the two ways the reference documents a key inside a
// comment: a leading dotted key name, or a commented-out sample line. A
// commented sample nests by the column of the text after the '#'.
func refComment(ks *keySet, trim string, indent int, stack *[]refFrame, skip *int) {
	if m := refDottedComment.FindStringSubmatch(trim); m != nil {
		ks.add(m[1])
		return
	}
	m := refSampleComment.FindStringSubmatch(trim)
	if m == nil {
		return
	}
	col := indent + len(m[1])
	for len(*stack) > 0 && (*stack)[len(*stack)-1].indent >= col {
		*stack = (*stack)[:len(*stack)-1]
	}
	*stack = append(*stack, refFrame{col, m[2]})
	names := make([]string, len(*stack))
	for i, fr := range *stack {
		names[i] = fr.name
	}
	ks.add(strings.Join(names, "."))
	_ = skip
}

var yamlKeyLine = regexp.MustCompile(`^( *)([A-Za-z0-9_][A-Za-z0-9_\-]*):(.*)$`)

// refKeys reads the dotted key paths of a YAML file by indentation, line by
// line, including keys the reference documents in comments (see refComment).
// List items (and what is nested in them) and block scalars are skipped.
func refKeys(path string) (*keySet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	ks := newKeySet()
	var stack []refFrame
	skipDeeperThan := -1 // ignore lines indented deeper than this (list item or block scalar body)
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			trim := strings.TrimLeft(line, " ")
			indent := len(line) - len(trim)
			switch {
			case trim == "":
			case strings.HasPrefix(trim, "#"):
				refComment(ks, trim, indent, &stack, &skipDeeperThan)
			case skipDeeperThan >= 0 && indent > skipDeeperThan:
			case strings.HasPrefix(trim, "- ") || trim == "-":
				skipDeeperThan = indent
			default:
				skipDeeperThan = -1
				if m := yamlKeyLine.FindStringSubmatch(line); m != nil {
					for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
						stack = stack[:len(stack)-1]
					}
					stack = append(stack, refFrame{indent, m[2]})
					names := make([]string, len(stack))
					for i, fr := range stack {
						names[i] = fr.name
					}
					ks.add(strings.Join(names, "."))
					v := strings.TrimSpace(m[3])
					if strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
						skipDeeperThan = indent
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	return ks, nil
}

// ---------------------------------------------------------------------------
// surface: web (Settings rows in app.js)
// ---------------------------------------------------------------------------

var (
	webKeyRe = regexp.MustCompile(`\bkey:\s*['"]([A-Za-z0-9][A-Za-z0-9_.\-]*)['"]`)
	// saveGeneralField('a.b', ...), putMergePipelineSetting('a.b', ...) and the like
	webSaveRe = regexp.MustCompile(`\b(?:save|put)[A-Za-z]*\(\s*['"]([a-z][a-z0-9_.]*)['"]`)
	webAttrRe = regexp.MustCompile(`data-cfg-key="([a-z][a-z0-9_.]*)"`)
	// a PUT body or patch built by hand: {'a.b': v} or patch['a.b'] = v (dotted keys only)
	webPatchRe = regexp.MustCompile(`(?:patch\[\s*['"]([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)['"]\s*\]|['"]([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)['"]\s*:)`)
)

// webKeys collects every Settings row key in app.js: an object literal with a
// key: '<name>' pair together with a label, labelKey, type or desc on the same
// line. Keys that start with an underscore are internal rows, not settings.
// That covers the three *_CONFIG_FIELDS arrays and the rows built beside them.
// Hand-built controls count as well: a save/put call whose first argument is
// the key (saveGeneralField('a.b', ...)), a data-cfg-key="a.b" attribute, and
// a hand-built PUT body or patch ({'a.b': v}, patch['a.b'] = v).
func webKeys(path string) (*keySet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	ks := newKeySet()
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadString('\n')
		if strings.Contains(line, "key:") &&
			(strings.Contains(line, "label") || strings.Contains(line, "type:") || strings.Contains(line, "desc:")) {
			for _, m := range webKeyRe.FindAllStringSubmatch(line, -1) {
				ks.add(m[1])
			}
		}
		for _, m := range webSaveRe.FindAllStringSubmatch(line, -1) {
			ks.add(m[1])
		}
		for _, m := range webAttrRe.FindAllStringSubmatch(line, -1) {
			ks.add(m[1])
		}
		for _, m := range webPatchRe.FindAllStringSubmatch(line, -1) {
			ks.add(m[1] + m[2])
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	return ks, nil
}

// ---------------------------------------------------------------------------
// surface: get (handleGetConfig)
// ---------------------------------------------------------------------------

// varInfo is a local map variable: its literal plus index assignments.
type varInfo struct {
	lit   *ast.CompositeLit
	extra []kv
}

type kv struct {
	key   ast.Expr
	value ast.Expr
}

type getReader struct {
	server *goPackage
	config *goPackage
	ks     *keySet
	budget int
}

// getKeys reads the keys GET /api/config emits: the map literal handleGetConfig
// builds, nested map literals flattened to dotted paths, helper calls
// followed into the server (and config) package, and out["k"] = v assignments.
// A value it cannot see into (a struct, a call it cannot resolve) is recorded
// as the whole subtree under its key.
func getKeys(dir string) (*keySet, error) {
	srv, err := loadPackage(dir)
	if err != nil {
		return nil, err
	}
	cfg, _ := loadPackage(filepath.Join(filepath.Dir(dir), "config"))
	if cfg == nil {
		cfg = &goPackage{funcs: map[string][]*ast.FuncDecl{}, consts: map[string]ast.Expr{}, vars: map[string]ast.Expr{}}
	}
	fns := srv.funcs["handleGetConfig"]
	if len(fns) == 0 {
		return nil, fmt.Errorf("%s: no func handleGetConfig", dir)
	}
	g := &getReader{server: srv, config: cfg, ks: newKeySet(), budget: 20000}
	vars := collectMapVars(fns[0].Body)
	root := vars["out"]
	if root == nil {
		for _, v := range vars { // fall back to any map variable, deterministic enough for one root
			root = v
			break
		}
	}
	if root == nil {
		return nil, fmt.Errorf("handleGetConfig builds no map")
	}
	g.flattenVar(root, "", vars, 0)
	return g.ks, nil
}

// collectMapVars finds `name := map[...]...{...}` declarations and
// `name["k"] = v` assignments anywhere in body (flow-insensitive).
func collectMapVars(body *ast.BlockStmt) map[string]*varInfo {
	vars := map[string]*varInfo{}
	if body == nil {
		return vars
	}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			if i >= len(as.Rhs) {
				break
			}
			switch l := lhs.(type) {
			case *ast.Ident:
				if cl, ok := as.Rhs[i].(*ast.CompositeLit); ok && isStringMap(cl) {
					vars[l.Name] = &varInfo{lit: cl}
				}
			case *ast.IndexExpr:
				if id, ok := l.X.(*ast.Ident); ok {
					if v := vars[id.Name]; v != nil {
						v.extra = append(v.extra, kv{l.Index, as.Rhs[i]})
					}
				}
			}
		}
		return true
	})
	return vars
}

func isStringMap(cl *ast.CompositeLit) bool {
	mt, ok := cl.Type.(*ast.MapType)
	if !ok {
		return false
	}
	id, ok := mt.Key.(*ast.Ident)
	return ok && id.Name == "string"
}

func (g *getReader) flattenVar(v *varInfo, prefix string, vars map[string]*varInfo, depth int) {
	for _, e := range v.lit.Elts {
		if p, ok := e.(*ast.KeyValueExpr); ok {
			g.entry(p.Key, p.Value, prefix, vars, depth)
		}
	}
	for _, e := range v.extra {
		g.entry(e.key, e.value, prefix, vars, depth)
	}
}

func (g *getReader) entry(keyExpr, value ast.Expr, prefix string, vars map[string]*varInfo, depth int) {
	k, ok := constString(keyExpr, g.server, g.config)
	if !ok || k == "" {
		return
	}
	g.budget--
	if g.budget < 0 {
		return
	}
	full := join(prefix, k)
	if g.expand(value, full, vars, depth+1) {
		return
	}
	g.ks.add(full)
	g.ks.addPrefix(full) // a value the audit cannot see into covers its whole subtree
}

// expand flattens value under prefix when it is a map the audit can read.
func (g *getReader) expand(value ast.Expr, prefix string, vars map[string]*varInfo, depth int) bool {
	if depth > 8 {
		return false
	}
	switch v := value.(type) {
	case *ast.CompositeLit:
		if isStringMap(v) {
			g.flattenVar(&varInfo{lit: v}, prefix, vars, depth)
			return true
		}
	case *ast.Ident:
		if vi := vars[v.Name]; vi != nil {
			g.flattenVar(vi, prefix, vars, depth)
			return true
		}
	case *ast.CallExpr:
		if fl, ok := v.Fun.(*ast.FuncLit); ok {
			return g.expandReturns(fl.Body, prefix, depth)
		}
		name := calleeName(v.Fun)
		if name == "" {
			return false
		}
		for _, pk := range []*goPackage{g.server, g.config} {
			for _, fd := range pk.funcs[name] {
				if fd.Body != nil && g.expandReturns(fd.Body, prefix, depth) {
					return true
				}
			}
		}
	}
	return false
}

// expandReturns flattens the first returned map literal (or local map
// variable) of a function body.
func (g *getReader) expandReturns(body *ast.BlockStmt, prefix string, depth int) bool {
	vars := collectMapVars(body)
	done := false
	ast.Inspect(body, func(n ast.Node) bool {
		if done {
			return false
		}
		rs, ok := n.(*ast.ReturnStmt)
		if !ok || len(rs.Results) == 0 {
			return true
		}
		if g.expand(rs.Results[0], prefix, vars, depth+1) {
			done = true
		}
		return true
	})
	return done
}

func calleeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

// ---------------------------------------------------------------------------
// surface: put (handlePutConfig + applyConfigPatch)
// ---------------------------------------------------------------------------

var keyLike = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[A-Za-z0-9_<>\-]*)*$`)

type putReader struct {
	server  *goPackage
	config  *goPackage
	ks      *keySet
	visited map[*ast.FuncDecl]bool
}

// putKeys reads the keys PUT /api/config accepts: the case labels of every
// switch over the key variable, key == "x" comparisons, patch["x"] lookups
// and HasPrefix/CutPrefix handlers inside handlePutConfig and applyConfigPatch,
// following helpers that are handed the key or the patch (two levels deep).
// A switch over a CutPrefix remainder adds its labels under that prefix.
func putKeys(dir string) (*keySet, error) {
	srv, err := loadPackage(dir)
	if err != nil {
		return nil, err
	}
	cfg, _ := loadPackage(filepath.Join(filepath.Dir(dir), "config"))
	if cfg == nil {
		cfg = &goPackage{funcs: map[string][]*ast.FuncDecl{}, consts: map[string]ast.Expr{}, vars: map[string]ast.Expr{}}
	}
	pr := &putReader{server: srv, config: cfg, ks: newKeySet(), visited: map[*ast.FuncDecl]bool{}}
	found := false
	for _, name := range []string{"handlePutConfig", "applyConfigPatch"} {
		for _, fd := range srv.funcs[name] {
			found = true
			pr.scan(fd, map[string]string{"k": "", "key": ""}, map[string]bool{"patch": true}, 0)
		}
	}
	if !found {
		return nil, fmt.Errorf("%s: no func handlePutConfig or applyConfigPatch", dir)
	}
	return pr.ks, nil
}

func (pr *putReader) addKey(s string) {
	if keyLike.MatchString(s) {
		if strings.HasSuffix(s, ".") {
			pr.ks.addPrefix(s)
		} else {
			pr.ks.add(s)
		}
	}
}

// scan visits one function. keyVars maps a variable that holds the patch key
// (or a remainder of it) to the prefix already stripped; patchVars names
// map-typed patch variables.
func (pr *putReader) scan(fd *ast.FuncDecl, keyVars map[string]string, patchVars map[string]bool, depth int) {
	if fd == nil || fd.Body == nil || pr.visited[fd] || depth > 2 {
		return
	}
	pr.visited[fd] = true
	pkgs := []*goPackage{pr.server, pr.config}
	isKey := func(e ast.Expr) (string, bool) {
		if id, ok := e.(*ast.Ident); ok {
			p, ok := keyVars[id.Name]
			return p, ok
		}
		return "", false
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.RangeStmt:
			id, ok := n.X.(*ast.Ident)
			if !ok {
				break
			}
			if patchVars[id.Name] {
				if kid, ok := n.Key.(*ast.Ident); ok {
					keyVars[kid.Name] = ""
				}
				break
			}
			// for _, k := range someKeyList: a package-level list of key names
			if cl, ok := pr.server.vars[id.Name].(*ast.CompositeLit); ok {
				for _, el := range cl.Elts {
					if s, ok := constString(el, pkgs...); ok {
						pr.addKey(s)
					}
				}
				if vid, ok := n.Value.(*ast.Ident); ok {
					keyVars[vid.Name] = ""
				}
			}
		case *ast.AssignStmt:
			// rest, ok := strings.CutPrefix(k, P) / x := strings.TrimPrefix(k, P)
			if len(n.Rhs) == 1 {
				if ce, ok := n.Rhs[0].(*ast.CallExpr); ok && len(ce.Args) == 2 {
					fn := calleeName(ce.Fun)
					if fn == "CutPrefix" || fn == "TrimPrefix" {
						if base, ok := isKey(ce.Args[0]); ok {
							if pfx, ok := constString(ce.Args[1], pkgs...); ok {
								if id, ok := n.Lhs[0].(*ast.Ident); ok {
									keyVars[id.Name] = base + pfx
								}
								pr.addKey(base + pfx)
								if !strings.HasSuffix(base+pfx, ".") {
									pr.ks.addPrefix(base + pfx)
								}
							}
						}
					}
				}
			}
		case *ast.SwitchStmt:
			if base, ok := isKey(n.Tag); ok {
				for _, st := range n.Body.List {
					cc, ok := st.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, e := range cc.List {
						if s, ok := constString(e, pkgs...); ok {
							pr.addKey(base + s)
						}
					}
				}
			}
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				for _, pair := range [][2]ast.Expr{{n.X, n.Y}, {n.Y, n.X}} {
					if base, ok := isKey(pair[0]); ok {
						if s, ok := constString(pair[1], pkgs...); ok {
							pr.addKey(base + s)
						}
					}
				}
			}
		case *ast.IndexExpr:
			if id, ok := n.X.(*ast.Ident); ok && patchVars[id.Name] {
				if s, ok := constString(n.Index, pkgs...); ok {
					pr.addKey(s)
				}
			}
		case *ast.CallExpr:
			name := calleeName(n.Fun)
			if (name == "HasPrefix" || name == "CutPrefix" || name == "TrimPrefix") && len(n.Args) == 2 {
				if base, ok := isKey(n.Args[0]); ok {
					if pfx, ok := constString(n.Args[1], pkgs...); ok {
						pr.ks.addPrefix(base + pfx)
					}
				}
				return true
			}
			// a helper handed the key or the patch is read the same way
			for i, a := range n.Args {
				base, isK := isKey(a)
				pid, _ := a.(*ast.Ident)
				isP := pid != nil && patchVars[pid.Name]
				if !isK && !isP {
					continue
				}
				for _, callee := range pr.server.funcs[name] {
					params := paramNames(callee)
					if i >= len(params) {
						continue
					}
					kvs, pvs := map[string]string{}, map[string]bool{}
					if isK {
						kvs[params[i]] = base
					}
					if isP {
						pvs[params[i]] = true
					}
					pr.scan(callee, kvs, pvs, depth+1)
				}
			}
		}
		return true
	})
}

func paramNames(fd *ast.FuncDecl) []string {
	var out []string
	if fd.Type.Params == nil {
		return out
	}
	for _, f := range fd.Type.Params.List {
		if len(f.Names) == 0 {
			out = append(out, "_")
		}
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// channels: configure, cli and mcp must stay generic forwarders to PUT
// ---------------------------------------------------------------------------

type channelCheck struct {
	name  string
	file  string
	fn    string   // function whose body is checked; "" = whole file
	must  []string // substrings the body must contain
	extra string   // a substring the whole file must contain
}

var channelChecks = []channelCheck{
	{"configure", cliMainFile, "commConfigureFunc", []string{"/api/config", "http.MethodPut"}, "SetConfigureFunc(commConfigureFunc("},
	{"cli", cliMainFile, "runConfigSet", []string{"/api/config"}, ""},
	{"mcp", mcpToolsFile, "handleConfigSet", []string{"/api/config", "MethodPut"}, ""},
}

// funcBody returns the text of `func ... name(` through the closing brace at
// column 0, streaming the file.
func funcBody(path, name string) (body, whole string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close() //nolint:errcheck
	var b, all strings.Builder
	in := false
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, rerr := r.ReadString('\n')
		if len(line) > 0 {
			all.WriteString(line)
			if !in && strings.HasPrefix(line, "func ") && funcNameRe(name).MatchString(line) {
				in = true
			}
			if in {
				b.WriteString(line)
				if strings.HasPrefix(line, "}") {
					in = false
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return "", "", rerr
		}
	}
	return b.String(), all.String(), nil
}

func funcNameRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^func (\([^)]*\) )?` + regexp.QuoteMeta(name) + `\(`)
}

// checkChannels returns one status line per channel and whether all are ok.
func checkChannels(root string) ([]string, bool, error) {
	var lines []string
	allOK := true
	for _, c := range channelChecks {
		body, whole, err := funcBody(filepath.Join(root, c.file), c.fn)
		if err != nil {
			return nil, false, err
		}
		var problems []string
		if strings.TrimSpace(body) == "" {
			problems = append(problems, "func "+c.fn+" not found in "+c.file)
		}
		for _, m := range c.must {
			if body != "" && !strings.Contains(body, m) {
				problems = append(problems, c.fn+" no longer contains "+m)
			}
		}
		if c.extra != "" && !strings.Contains(whole, c.extra) {
			problems = append(problems, c.file+" no longer contains "+c.extra)
		}
		if len(problems) == 0 {
			lines = append(lines, c.name+": ok (forwards to PUT /api/config, so it reaches what put reaches)")
		} else {
			allOK = false
			lines = append(lines, c.name+": BROKEN: "+strings.Join(problems, "; "))
		}
	}
	return lines, allOK, nil
}

// ---------------------------------------------------------------------------
// exemptions
// ---------------------------------------------------------------------------

type exemption struct {
	key      string
	prefix   bool
	surfaces map[string]bool
	reason   string
	line     int
	used     bool
}

func (e *exemption) matches(key string) bool {
	if e.prefix {
		return strings.HasPrefix(key, e.key)
	}
	return key == e.key
}

// loadExemptions parses `key | surfaces | reason` lines. The reason is
// required and the surface names are checked.
func loadExemptions(path string) ([]*exemption, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	var out []*exemption
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("%s:%d: want `key | surfaces | reason`", path, n)
		}
		key, surf, reason := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if key == "" {
			return nil, fmt.Errorf("%s:%d: empty key", path, n)
		}
		if reason == "" {
			return nil, fmt.Errorf("%s:%d: an exemption needs a reason", path, n)
		}
		e := &exemption{key: key, surfaces: map[string]bool{}, reason: reason, line: n}
		if strings.HasSuffix(key, ".*") {
			e.prefix = true
			e.key = strings.TrimSuffix(key, "*")
		}
		for _, s := range strings.Split(surf, ",") {
			s = strings.TrimSpace(s)
			switch {
			case s == "*":
				for _, all := range surfaces {
					e.surfaces[all] = true
				}
			case contains(surfaces, s):
				e.surfaces[s] = true
			default:
				return nil, fmt.Errorf("%s:%d: unknown surface %q (use %s or *)", path, n, s, strings.Join(surfaces, ", "))
			}
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// audit
// ---------------------------------------------------------------------------

type row struct {
	key     string
	missing map[string]bool // surface -> missing and not exempt
	exempt  map[string]bool // surface -> missing but exempt
}

type result struct {
	keys  int
	rows  []row // keys with at least one missing surface (exempt or not), sorted
	gaps  int   // missing and not exempt
	exemp int   // missing and exempt
	stale []*exemption
}

// audit compares every key against the surface sets and the exemptions.
func audit(keys []string, sets map[string]*keySet, ex []*exemption) result {
	res := result{keys: len(keys)}
	for _, k := range keys {
		r := row{key: k, missing: map[string]bool{}, exempt: map[string]bool{}}
		for _, s := range surfaces {
			if sets[s].covers(k) {
				continue
			}
			ok := false
			for _, e := range ex {
				if e.surfaces[s] && e.matches(k) {
					e.used = true
					ok = true
				}
			}
			if ok {
				r.exempt[s] = true
				res.exemp++
			} else {
				r.missing[s] = true
				res.gaps++
			}
		}
		if len(r.missing)+len(r.exempt) > 0 {
			res.rows = append(res.rows, r)
		}
	}
	for _, e := range ex {
		if !e.used {
			res.stale = append(res.stale, e)
		}
	}
	return res
}

func cell(r row, s string) string {
	switch {
	case r.missing[s]:
		return "MISSING"
	case r.exempt[s]:
		return "exempt"
	}
	return "ok"
}

func feature(key string) string {
	p := strings.Split(key, ".")
	if p[0] == "autonomous" && len(p) > 2 {
		return p[0] + "." + p[1]
	}
	return p[0]
}

func writeTable(w io.Writer, res result, all bool, keys []string, sets map[string]*keySet, ex []*exemption) {
	width := 3
	// without -all only keys with a gap that is not exempt are listed
	var rows []row
	for _, r := range res.rows {
		if len(r.missing) > 0 {
			rows = append(rows, r)
		}
	}
	if all {
		rows = nil
		byKey := map[string]row{}
		for _, r := range res.rows {
			byKey[r.key] = r
		}
		for _, k := range keys {
			if r, ok := byKey[k]; ok {
				rows = append(rows, r)
			} else {
				rows = append(rows, row{key: k})
			}
		}
	}
	for _, r := range rows {
		if len(r.key) > width {
			width = len(r.key)
		}
	}
	fmt.Fprintf(w, "%-*s  %-7s %-7s %-7s %-7s\n", width, "key", "ref", "web", "get", "put") //nolint:errcheck
	for _, r := range rows {
		fmt.Fprintf(w, "%-*s  %-7s %-7s %-7s %-7s\n", width, r.key, cell(r, "ref"), cell(r, "web"), cell(r, "get"), cell(r, "put")) //nolint:errcheck
	}
	_ = sets
	_ = ex
}

// writeGapTable writes the markdown gap table: every key missing from a
// surface (exemptions are not applied), grouped by surface and by feature.
func writeGapTable(path string, res result, channels []string, keys int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	w := bufio.NewWriter(f)
	bySurface := map[string][]string{}
	byFeature := map[string][]row{}
	keysWithGap := 0
	for _, r := range res.rows {
		any := false
		for _, s := range surfaces {
			if r.missing[s] || r.exempt[s] {
				bySurface[s] = append(bySurface[s], r.key)
				any = true
			}
		}
		if any {
			keysWithGap++
			byFeature[feature(r.key)] = append(byFeature[feature(r.key)], r)
		}
	}
	fmt.Fprintf(w, "# Configuration audit gap table\n\n")                                                                                                             //nolint:errcheck
	fmt.Fprintf(w, "Generated by `make config-audit` (scripts/config-audit), exemptions not applied.\n\n")                                                            //nolint:errcheck
	fmt.Fprintf(w, "## Summary\n\n- keys checked: %d\n- keys with at least one gap: %d\n- gaps (key and surface pairs): %d\n", keys, keysWithGap, res.gaps+res.exemp) //nolint:errcheck
	for _, s := range surfaces {
		fmt.Fprintf(w, "- %s missing: %d\n", s, len(bySurface[s])) //nolint:errcheck
	}
	fmt.Fprintf(w, "\nThe comm `configure` verb, `datawatch config set` and the MCP `config_set` tool forward every key to PUT /api/config, so their gaps are the put gaps. Forwarder check:\n\n") //nolint:errcheck
	for _, c := range channels {
		fmt.Fprintf(w, "- %s\n", c) //nolint:errcheck
	}
	fmt.Fprintf(w, "\n## Gaps by surface\n") //nolint:errcheck
	for _, s := range surfaces {
		fmt.Fprintf(w, "\n### %s (%d)\n\n", s, len(bySurface[s])) //nolint:errcheck
		for _, k := range bySurface[s] {
			fmt.Fprintf(w, "- `%s`\n", k) //nolint:errcheck
		}
	}
	fmt.Fprintf(w, "\n## Gaps by feature\n") //nolint:errcheck
	feats := make([]string, 0, len(byFeature))
	for k := range byFeature {
		feats = append(feats, k)
	}
	sort.Strings(feats)
	fmt.Fprintf(w, "\n| feature | keys with a gap | ref | web | get | put |\n|---|---|---|---|---|---|\n") //nolint:errcheck
	for _, ft := range feats {
		var n [4]int
		for _, r := range byFeature[ft] {
			for i, sf := range surfaces {
				if r.missing[sf] || r.exempt[sf] {
					n[i]++
				}
			}
		}
		fmt.Fprintf(w, "| %s | %d | %d | %d | %d | %d |\n", ft, len(byFeature[ft]), n[0], n[1], n[2], n[3]) //nolint:errcheck
	}
	for _, ft := range feats {
		fmt.Fprintf(w, "\n### %s (%d keys)\n\n| key | missing from |\n|---|---|\n", ft, len(byFeature[ft])) //nolint:errcheck
		for _, r := range byFeature[ft] {
			var m []string
			for _, s := range surfaces {
				if r.missing[s] || r.exempt[s] {
					m = append(m, s)
				}
			}
			fmt.Fprintf(w, "| `%s` | %s |\n", r.key, strings.Join(m, ", ")) //nolint:errcheck
		}
	}
	return w.Flush()
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root (only the named input files are read)")
	exFile := fs.String("exemptions", "", "exemptions file (default <root>/"+defaultExemp+")")
	noEx := fs.Bool("no-exemptions", false, "ignore the exemptions file")
	all := fs.Bool("all", false, "list every key, not only keys with a gap")
	gapTable := fs.String("gap-table", "", "also write the gap table (markdown, grouped by surface and feature) to this file")
	stats := fs.Bool("stats", false, "print elapsed time and peak memory to stderr")
	timeout := fs.Duration("timeout", 50*time.Second, "give up after this long")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	debug.SetMemoryLimit(350 << 20)
	start := time.Now()
	if *timeout > 0 {
		t := time.AfterFunc(*timeout, func() {
			fmt.Fprintf(stderr, "config-audit: timed out after %s\n", *timeout) //nolint:errcheck
			os.Exit(2)
		})
		defer t.Stop()
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "config-audit: %v\n", err) //nolint:errcheck
		return 2
	}

	keys, err := configKeys(filepath.Join(*root, configDir))
	if err != nil {
		return fail(err)
	}
	sets := map[string]*keySet{}
	if sets["ref"], err = refKeys(filepath.Join(*root, refFile)); err != nil {
		return fail(err)
	}
	if sets["web"], err = webKeys(filepath.Join(*root, webFile)); err != nil {
		return fail(err)
	}
	if sets["get"], err = getKeys(filepath.Join(*root, serverDir)); err != nil {
		return fail(err)
	}
	if sets["put"], err = putKeys(filepath.Join(*root, serverDir)); err != nil {
		return fail(err)
	}
	channels, chOK, err := checkChannels(*root)
	if err != nil {
		return fail(err)
	}

	var ex []*exemption
	if !*noEx {
		path := *exFile
		if path == "" {
			path = filepath.Join(*root, defaultExemp)
		}
		if ex, err = loadExemptions(path); err != nil {
			if os.IsNotExist(err) && *exFile == "" {
				ex = nil
			} else {
				return fail(err)
			}
		}
	}

	res := audit(keys, sets, ex)
	writeTable(stdout, res, *all, keys, sets, ex)
	fmt.Fprintln(stdout) //nolint:errcheck
	for _, c := range channels {
		fmt.Fprintln(stdout, c) //nolint:errcheck
	}
	fmt.Fprintf(stdout, "%d keys checked, %d gaps, %d exempt\n", res.keys, res.gaps, res.exemp) //nolint:errcheck
	for _, e := range res.stale {
		fmt.Fprintf(stderr, "config-audit: stale exemption (line %d): %s matches no gap\n", e.line, e.key) //nolint:errcheck
	}
	if *gapTable != "" {
		if err := writeGapTable(*gapTable, res, channels, res.keys); err != nil {
			return fail(err)
		}
	}
	if *stats {
		fmt.Fprintf(stderr, "config-audit: %s elapsed, peak memory %d MB\n", time.Since(start).Round(time.Millisecond), peakRSSMB()) //nolint:errcheck
	}
	if res.gaps > 0 || !chOK {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

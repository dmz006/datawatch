package genartifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
)

// Marker is the first line of every generated file.
const Marker = "<!-- generated: datawatch change record; edit freely, a person edit is kept -->"

// EditedMarker, as an extra line right after Marker, tells Write a person has
// taken the file over.
const EditedMarker = "<!-- edited -->"

// ErrDisabled is returned by Render for a kind switched off in the config.
var ErrDisabled = errors.New("genartifacts: artefact disabled")

// Options configure Render and Write.
type Options struct {
	// Root is the project directory. Relative paths and the override
	// templates resolve against it. Empty means the current directory.
	Root string
	// Settings are the per-artefact switches. Nil means every kind is on.
	Settings *config.EffectiveGeneratedArtefactsSettings
	// Paths overrides the default tracked path of a kind; the placeholder
	// <automaton-id> is replaced with the record's automaton id.
	Paths map[Kind]string
	// RowNumber is the registry row number for RegistryRow; empty prints
	// "NNN".
	RowNumber string
}

// Status is the outcome of a Write.
type Status string

// Write outcomes.
const (
	Written  Status = "written"
	Skipped  Status = "skipped"
	Disabled Status = "disabled"
)

// Result is what Write did.
type Result struct {
	Status   Status
	Path     string
	Reason   string
	Warnings []string
}

// Enabled reports whether the kind is switched on.
func (o Options) Enabled(k Kind) bool {
	if o.Settings == nil {
		return true
	}
	s := o.Settings
	switch k {
	case GateReport:
		return s.GateReport
	case ChangelogDraft:
		return s.ChangelogDraft
	case ParityDraft:
		return s.ParityDraft
	case TrackerRows:
		return s.TrackerRows
	case RegistryRow:
		return s.RegistryRow
	}
	return false
}

func (o Options) root() string {
	if o.Root == "" {
		return "."
	}
	return o.Root
}

// TrackedPath is the path of the kind's file for a record, relative to the
// project root.
func TrackedPath(k Kind, rec *changerecord.ChangeRecord, o Options) string {
	p := o.Paths[k]
	if p == "" {
		p = DefaultPaths()[k]
	}
	id := "unknown"
	if rec != nil && rec.AutomatonID != "" {
		id = rec.AutomatonID
	}
	return strings.ReplaceAll(p, IDPlaceholder, safeID(id))
}

func safeID(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, s)
}

// Render renders the kind for the record. A nil record renders as an empty
// one. The returned warnings name a broken project override that fell back to
// the default. A disabled kind returns ErrDisabled.
func Render(kind Kind, rec *changerecord.ChangeRecord, o Options) (string, []string, error) {
	if !kind.Valid() {
		return "", nil, fmt.Errorf("genartifacts: unknown kind %q", kind)
	}
	if !o.Enabled(kind) {
		return "", nil, ErrDisabled
	}
	if rec == nil {
		rec = &changerecord.ChangeRecord{}
	}
	data := buildData(kind, rec, o)
	var warnings []string
	if OverridePath(kind) == "" {
		return "", nil, fmt.Errorf("genartifacts: unknown kind %q", kind)
	}
	if b, err := os.ReadFile(filepath.Join(o.root(), filepath.FromSlash(OverridePath(kind)))); err == nil { // #nosec G304 -- fixed path under the project root
		out, err := execute(string(b), data)
		if err == nil {
			return out, nil, nil
		}
		warnings = append(warnings, fmt.Sprintf("override template %s is invalid, using the default: %v", OverridePath(kind), err))
	} else if !errors.Is(err, os.ErrNotExist) {
		warnings = append(warnings, fmt.Sprintf("override template %s unreadable, using the default: %v", OverridePath(kind), err))
	}
	text, err := DefaultTemplate(kind)
	if err != nil {
		return "", warnings, err
	}
	out, err := execute(text, data)
	if err != nil {
		return "", warnings, fmt.Errorf("genartifacts: render %s: %w", kind, err)
	}
	return out, warnings, nil
}

func execute(text string, data any) (string, error) {
	t, err := parseTemplate("artefact", text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ---- template data ----

type surface struct {
	Feature, Conditions, Description, TestType, Tested string
	Tests                                              []string
}

type view struct {
	ID, Title, Summary string
	RowNumber          string

	CommandsRun, CommandsPassed, CommandsFailed int
	TestsPassed, TestsFailed                    int
	FilesChanged, PackagesTouched, TestsAdded   int
	SettingsAdded, RoutesAdded, LocaleKeysAdded int

	Settings   []string
	Routes     []string
	LocaleKeys []changerecord.LocaleKey
	Screens    []string
	Bullets    []string
	Surfaces   []surface

	// Cost is the ledger-backed cost section; nil renders no section.
	Cost *costView
}

type costView struct {
	Sessions, TokensIn, TokensOut, CacheRead, CacheCreation, ToolCalls int
	Span                                                               string
	SessionIDs                                                         []string
	Estimate, Label                                                    string
	WindowNote, UnknownNote                                            string
}

func costOf(c *changerecord.CostSummary) *costView {
	if c == nil {
		return nil
	}
	v := &costView{Sessions: c.Sessions, TokensIn: c.TokensIn, TokensOut: c.TokensOut,
		CacheRead: c.CacheRead, CacheCreation: c.CacheCreation, ToolCalls: c.ToolCalls,
		SessionIDs: c.SessionIDs, Span: (time.Duration(c.SpanSeconds) * time.Second).String(), WindowNote: c.WindowNote, UnknownNote: c.UnknownNote}
	if c.APIEquivalentUSD != nil {
		v.Estimate = fmt.Sprintf("$%.2f", *c.APIEquivalentUSD)
		v.Label = c.USDLabel
		if v.Label == "" {
			v.Label = "API-equivalent estimate, not billed"
		}
	}
	return v
}

func buildData(kind Kind, rec *changerecord.ChangeRecord, o Options) view {
	id := rec.AutomatonID
	if id == "" {
		id = "unknown"
	}
	v := view{ID: id, RowNumber: o.RowNumber}
	if v.RowNumber == "" {
		v.RowNumber = "NNN"
	}
	for _, c := range rec.Commands {
		v.CommandsRun++
		if c.ExitCode == 0 {
			v.CommandsPassed++
		} else {
			v.CommandsFailed++
		}
		v.TestsPassed += c.Pass
		v.TestsFailed += c.Fail
	}
	v.FilesChanged = len(rec.Files)
	v.PackagesTouched = len(rec.Packages)
	v.TestsAdded = len(rec.TestsAdded)
	v.SettingsAdded = len(rec.SettingsAdded)
	v.RoutesAdded = len(rec.RoutesAdded)
	v.LocaleKeysAdded = len(rec.LocaleKeysAdded)
	v.Settings = rec.SettingsAdded
	v.Routes = rec.RoutesAdded
	v.LocaleKeys = rec.LocaleKeysAdded

	v.Title = "Automaton " + id
	v.Summary = fmt.Sprintf("Change from Automaton %s: %d files in %d packages.", id, v.FilesChanged, v.PackagesTouched)
	v.Screens = screensOf(rec)
	v.Bullets = bulletsOf(rec)
	v.Surfaces = surfacesOf(rec)
	v.Cost = costOf(rec.Cost)
	return v
}

func bulletsOf(rec *changerecord.ChangeRecord) []string {
	var b []string
	if n := len(rec.SettingsAdded); n > 0 {
		b = append(b, fmt.Sprintf("%d new settings: %s.", n, codeList(rec.SettingsAdded)))
	}
	if n := len(rec.RoutesAdded); n > 0 {
		b = append(b, fmt.Sprintf("%d new routes: %s.", n, codeList(rec.RoutesAdded)))
	}
	if n := len(rec.LocaleKeysAdded); n > 0 {
		b = append(b, fmt.Sprintf("%d new locale keys.", n))
	}
	if n := len(rec.Packages); n > 0 {
		b = append(b, fmt.Sprintf("Touches %d packages: %s.", n, codeList(rec.Packages)))
	}
	if n := len(rec.TestsAdded); n > 0 {
		b = append(b, fmt.Sprintf("%d tests added.", n))
	}
	return b
}

func codeList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "`" + x + "`"
	}
	return strings.Join(q, ", ")
}

// surfaceOf maps a changed file to the surface it belongs to.
func surfaceOf(p string) string {
	switch {
	case strings.HasPrefix(p, "internal/server/web/"):
		return "Web UI"
	case strings.HasPrefix(p, "internal/server/"), strings.HasPrefix(p, "docs/api/"):
		return "REST API"
	case strings.HasPrefix(p, "internal/mcp/"):
		return "MCP"
	case strings.HasPrefix(p, "cmd/"):
		return "CLI"
	case strings.HasPrefix(p, "internal/config/"), strings.HasPrefix(p, "docs/config-reference"):
		return "Config"
	case strings.HasPrefix(p, "internal/router/"), strings.HasPrefix(p, "internal/messaging/"):
		return "Comm channel"
	}
	return ""
}

func screensOf(rec *changerecord.ChangeRecord) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range rec.Files {
		if strings.HasPrefix(f.Path, "internal/server/web/") && !strings.Contains(f.Path, "/locales/") {
			s := "`" + f.Path + "`"
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

func surfacesOf(rec *changerecord.ChangeRecord) []surface {
	order := []string{}
	files := map[string][]string{}
	for _, f := range rec.Files {
		s := surfaceOf(f.Path)
		if s == "" {
			continue
		}
		if _, ok := files[s]; !ok {
			order = append(order, s)
		}
		files[s] = append(files[s], f.Path)
	}
	sort.Strings(order)
	var out []surface
	for _, s := range order {
		tested := "**No**"
		if len(rec.TestsAdded) > 0 {
			tested = "**Yes**"
		}
		out = append(out, surface{
			Feature:     s + ": " + path.Dir(files[s][0]),
			Tests:       rec.TestsAdded,
			Conditions:  "go test ./... -count=1",
			Description: fmt.Sprintf("%d files changed on this surface.", len(files[s])),
			TestType:    "Go unit",
			Tested:      tested,
		})
	}
	return out
}

// ---- writing ----

func bodyHash(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

func sidecarPath(p string) string {
	return filepath.Join(filepath.Dir(p), "."+filepath.Base(p)+".genhash")
}

// splitMarker returns the body of a generated file and whether it carries the
// marker and the edited line.
func splitMarker(s string) (body string, marked, edited bool) {
	first, rest, _ := strings.Cut(s, "\n")
	if strings.TrimRight(first, "\r") != Marker {
		return s, false, false
	}
	if second, after, _ := strings.Cut(rest, "\n"); strings.TrimRight(second, "\r") == EditedMarker {
		return after, true, true
	}
	return rest, true, false
}

// Write writes content for kind at path (relative to Options.Root unless
// absolute), with the marker as its first line. A disabled kind writes
// nothing and returns Disabled. An existing file is kept, and Write returns
// Skipped with a reason, when it lacks the marker, carries the edited line, or
// its body hash differs from the one recorded in its sidecar.
func Write(kind Kind, content, p string, o Options) (Result, error) {
	res := Result{Path: p}
	if !kind.Valid() {
		return res, fmt.Errorf("genartifacts: unknown kind %q", kind)
	}
	if !o.Enabled(kind) {
		res.Status = Disabled
		res.Reason = string(kind) + " is switched off in the generated-artefact settings"
		return res, nil
	}
	full, err := o.resolve(p)
	if err != nil {
		return res, err
	}
	if b, err := os.ReadFile(full); err == nil { // #nosec G304 -- path resolved under the project root
		body, marked, edited := splitMarker(string(b))
		switch {
		case !marked:
			return skip(res, "existing file has no generated marker"), nil
		case edited:
			return skip(res, "existing file carries the edited marker"), nil
		}
		if rec, err := os.ReadFile(sidecarPath(full)); err == nil { // #nosec G304 -- sidecar next to the target
			if strings.TrimSpace(string(rec)) != bodyHash(body) {
				return skip(res, "existing file body differs from the recorded generated hash"), nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("genartifacts: read %s: %w", p, err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return res, fmt.Errorf("genartifacts: %w", err)
	}
	if err := os.WriteFile(full, []byte(Marker+"\n"+content), 0o644); err != nil { // #nosec G306 -- tracked doc
		return res, fmt.Errorf("genartifacts: write %s: %w", p, err)
	}
	if err := os.WriteFile(sidecarPath(full), []byte(bodyHash(content)+"\n"), 0o644); err != nil { // #nosec G306 -- tracked sidecar
		return res, fmt.Errorf("genartifacts: write sidecar for %s: %w", p, err)
	}
	res.Status = Written
	return res, nil
}

func skip(r Result, reason string) Result {
	r.Status = Skipped
	r.Reason = reason
	return r
}

func (o Options) resolve(p string) (string, error) {
	if p == "" {
		return "", errors.New("genartifacts: empty path")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("genartifacts: path %q escapes the project root", p)
	}
	return filepath.Join(o.root(), clean), nil
}

// Generate renders a kind and writes it to its tracked path in one step.
func Generate(kind Kind, rec *changerecord.ChangeRecord, o Options) (Result, error) {
	content, warns, err := Render(kind, rec, o)
	if errors.Is(err, ErrDisabled) {
		return Result{Status: Disabled, Path: TrackedPath(kind, rec, o), Reason: string(kind) + " is switched off in the generated-artefact settings"}, nil
	}
	if err != nil {
		return Result{Path: TrackedPath(kind, rec, o)}, err
	}
	res, err := Write(kind, content, TrackedPath(kind, rec, o), o)
	res.Warnings = append(res.Warnings, warns...)
	return res, err
}

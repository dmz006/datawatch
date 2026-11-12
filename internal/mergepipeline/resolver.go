package mergepipeline

import (
	"errors"
	"fmt"
)

// Source says where the pipeline in effect came from.
type Source string

const (
	SourceFile    Source = "file"    // the project's own file in its repository
	SourceRecord  Source = "record"  // the record the daemon keeps
	SourceDefault Source = "default" // the built-in pipeline
)

// Values of Settings.FallbackOnBadFile.
const (
	// FallbackRecord reports the bad file and uses the daemon record.
	FallbackRecord = "record"
	// FallbackDefault reports the bad file and uses the built-in pipeline.
	FallbackDefault = "default"
	// FallbackNone reports the bad file and substitutes nothing.
	FallbackNone = "none"
)

// Settings are the resolution rules for one project.
type Settings struct {
	// FileName is the pipeline file, relative to the project directory.
	FileName string
	// SourceOrder lists the sources tried first to last ("file", "record").
	// The built-in default always applies after them.
	SourceOrder []string
	// FallbackOnBadFile is "record" (default), "default" or "none".
	FallbackOnBadFile string
	// MaxFileBytes caps the file size; zero uses DefaultMaxFileBytes.
	MaxFileBytes int64
}

// DefaultSettings returns the rules used when none are supplied: the file
// first, then the record, a bad file falling back to the record.
func DefaultSettings() Settings {
	return Settings{
		FileName:          DefaultFileName,
		SourceOrder:       []string{string(SourceFile), string(SourceRecord)},
		FallbackOnBadFile: FallbackRecord,
	}
}

// Resolution is the pipeline in effect for a project and why.
type Resolution struct {
	Definition Definition
	Source     Source
	// Reason is a plain-language account of the choice.
	Reason string
	// FileError is the problem found with the project's file, if any, even
	// when another source was used in its place.
	FileError error
}

// Resolver picks the pipeline for a project.
type Resolver struct {
	// Records is the daemon's record store; nil means there are no records.
	Records *RecordStore
	// ProjectDir returns the project's repository directory.
	ProjectDir func(project string) (string, bool)
	// Settings returns the rules for a project; nil uses DefaultSettings.
	Settings func(project string) Settings
}

// Resolve returns the definition in effect for project. A project file wins
// over the daemon record, the record is used when there is no file, and the
// built-in default applies when there is neither. A malformed file is never
// passed over silently: it is returned in Resolution.FileError, and the
// record (or the default) stands in only as Settings.FallbackOnBadFile allows.
// The error is non-nil only when no definition could be chosen.
func (r *Resolver) Resolve(project string) (Resolution, error) {
	set := DefaultSettings()
	if r.Settings != nil {
		set = r.Settings(project)
	}
	if set.FileName == "" {
		set.FileName = DefaultFileName
	}
	if len(set.SourceOrder) == 0 {
		set.SourceOrder = DefaultSettings().SourceOrder
	}

	var fileErr error
	var notes []string
	for _, name := range set.SourceOrder {
		switch Source(name) {
		case SourceFile:
			def, err := r.loadFile(project, set)
			if err == nil {
				return Resolution{Definition: def, Source: SourceFile,
					Reason: fmt.Sprintf("the project's own file %s is present and valid", set.FileName)}, nil
			}
			var nf *ErrNotFound
			if errors.As(err, &nf) {
				notes = append(notes, fmt.Sprintf("no file %s in the project", set.FileName))
				continue
			}
			return r.badFile(project, set, err)
		case SourceRecord:
			if res, ok := r.fromRecord(project, notes, fileErr); ok {
				return res, nil
			}
			notes = append(notes, "no daemon record for the project")
		default:
			notes = append(notes, fmt.Sprintf("unknown source %q ignored", name))
		}
	}
	return Resolution{Definition: DefaultDefinition(), Source: SourceDefault,
		Reason: joinNotes(notes) + "; using the built-in pipeline"}, nil
}

func (r *Resolver) loadFile(project string, set Settings) (Definition, error) {
	if r.ProjectDir == nil {
		return Definition{}, &ErrNotFound{Path: set.FileName}
	}
	dir, ok := r.ProjectDir(project)
	if !ok || dir == "" {
		return Definition{}, &ErrNotFound{Path: set.FileName}
	}
	return LoadFile(dir, set.FileName, set.MaxFileBytes)
}

func (r *Resolver) fromRecord(project string, notes []string, fileErr error) (Resolution, bool) {
	if r.Records == nil {
		return Resolution{}, false
	}
	rec, err := r.Records.Get(project)
	if err != nil {
		return Resolution{}, false
	}
	reason := "the daemon's record for the project is in effect"
	if len(notes) > 0 {
		reason = joinNotes(notes) + "; " + reason
	}
	return Resolution{Definition: rec.Definition, Source: SourceRecord, Reason: reason, FileError: fileErr}, true
}

// badFile handles a file that exists but cannot be used.
func (r *Resolver) badFile(project string, set Settings, fileErr error) (Resolution, error) {
	switch set.FallbackOnBadFile {
	case FallbackNone:
		return Resolution{Source: SourceFile, FileError: fileErr,
			Reason: "the project's file is unusable and the setting allows no substitute"}, fileErr
	case FallbackDefault:
		return Resolution{Definition: DefaultDefinition(), Source: SourceDefault, FileError: fileErr,
			Reason: "the project's file is unusable (" + fileErr.Error() + "); using the built-in pipeline as the setting allows"}, nil
	}
	if r.Records != nil {
		if rec, err := r.Records.Get(project); err == nil {
			return Resolution{Definition: rec.Definition, Source: SourceRecord, FileError: fileErr,
				Reason: "the project's file is unusable (" + fileErr.Error() + "); using the daemon's record as the setting allows"}, nil
		}
	}
	return Resolution{Source: SourceFile, FileError: fileErr,
		Reason: "the project's file is unusable and there is no daemon record to fall back on"}, fileErr
}

func joinNotes(notes []string) string {
	out := ""
	for i, n := range notes {
		if i > 0 {
			out += "; "
		}
		out += n
	}
	if out == "" {
		return "no source configured"
	}
	return out
}

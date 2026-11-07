// Package resources resolves the host-derived memory ceiling.
package resources

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// OverrideNone is the word that opts out of a ceiling explicitly.
const OverrideNone = "none"

// Override is a parsed ceiling override. The zero value means "not set".
type Override struct {
	// Set is true when an override was given.
	Set bool
	// None is true when the operator opted out of a ceiling.
	None bool
	// MB is the override in MB; meaningful only when Set && !None.
	MB int
}

// ParseOverride parses an override string: a positive MB number or the
// word "none". An empty string is an unset override.
func ParseOverride(s string) (Override, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Override{}, nil
	}
	if strings.EqualFold(s, OverrideNone) {
		return Override{Set: true, None: true}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return Override{}, fmt.Errorf("invalid memory ceiling override %q: want a positive MB number or %q", s, OverrideNone)
	}
	return Override{Set: true, MB: n}, nil
}

// PickOverride applies the precedence order: the per-task-kind override,
// then the per-project override. When neither is set it returns the zero
// Override, which means the global default applies.
func PickOverride(taskKind, project Override) Override {
	if taskKind.Set {
		return taskKind
	}
	if project.Set {
		return project
	}
	return Override{}
}

// Source says where a resolved ceiling came from.
type Source string

const (
	// SourceDefault is the global default (host total times share, clamped).
	SourceDefault Source = "default"
	// SourceOverride is an explicit MB override.
	SourceOverride Source = "override"
	// SourceNone is an explicit operator opt-out: no ceiling is set.
	SourceNone Source = "none"
)

// Ceiling is a resolved memory ceiling.
type Ceiling struct {
	// MB is the ceiling in MB; zero when Source is SourceNone.
	MB int
	// Source says how the ceiling was reached.
	Source Source
}

// NoCeiling reports whether the operator explicitly opted out, so a status
// can say "no ceiling set by the operator".
func (c Ceiling) NoCeiling() bool { return c.Source == SourceNone }

// ResolveCeiling returns the ceiling for a host with totalMB of memory. The
// share, floor and cap come from cfg. Without an override the ceiling is
// total times share, clamped between floor and cap. An override replaces the
// computed value; "none" returns a distinct SourceNone result.
func ResolveCeiling(totalMB int, cfg config.ResourceLimitsConfig, ov Override) Ceiling {
	if ov.Set {
		if ov.None {
			return Ceiling{Source: SourceNone}
		}
		return Ceiling{MB: ov.MB, Source: SourceOverride}
	}
	mb := totalMB * cfg.MemorySharePercent / 100
	if mb > cfg.MemoryCapMB {
		mb = cfg.MemoryCapMB
	}
	if mb < cfg.MemoryFloorMB {
		mb = cfg.MemoryFloorMB
	}
	return Ceiling{MB: mb, Source: SourceDefault}
}

// ErrMemTotalUnavailable is returned when host memory cannot be read.
var ErrMemTotalUnavailable = errors.New("host total memory unavailable")

const procMeminfo = "/proc/meminfo"

// HostTotalMB reads MemTotal from /proc/meminfo, in MB.
func HostTotalMB() (int, error) { return hostTotalMBFrom(procMeminfo) }

func hostTotalMBFrom(path string) (int, error) {
	f, err := os.Open(path) // #nosec G304 -- fixed path or test path
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrMemTotalUnavailable, err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || kb <= 0 {
				return 0, fmt.Errorf("%w: bad MemTotal line %q", ErrMemTotalUnavailable, sc.Text())
			}
			return int(kb / 1024), nil
		}
	}
	return 0, fmt.Errorf("%w: no MemTotal in %s", ErrMemTotalUnavailable, path)
}

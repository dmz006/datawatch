package mergeshadow

import (
	"fmt"
	"sort"
	"strings"
)

// The version planner is pure. It derives the planned version from the default
// branch version and the release size alone. The trial worktree's own version
// is only ever compared against the result, never used to compute it, so a
// trial that already carries a bump cannot bump again.

// VersionPlan is the result of planning one version bump.
type VersionPlan struct {
	// Base is the default branch version the bump starts from.
	Base string
	// Size is the release size applied to Base.
	Size ReleaseSize
	// Planned is the version the bump produces, as X.Y.Z.
	Planned string
	// AlreadyAtPlanned is true when every version file in the trial tree
	// already carries Planned, so the bump must be skipped.
	AlreadyAtPlanned bool
	// Stale lists the version file paths, sorted, whose value differs from
	// Planned. It is empty when AlreadyAtPlanned is true. A file that holds
	// no version is stale.
	Stale []string
}

// PlanVersion computes the planned version for a bump of the given size over
// baseVersion. It is a pure function of its two inputs: the same inputs always
// give the same result. Size must be patch, minor or major; a base that is not
// a plain X.Y.Z version is an error, so nothing is guessed.
func PlanVersion(baseVersion string, size ReleaseSize) (string, error) {
	v, ok := parseSemver(baseVersion)
	if !ok {
		return "", fmt.Errorf("mergeshadow: base version %q is not X.Y.Z", baseVersion)
	}
	switch size {
	case ReleaseSizePatch:
		v[2]++
	case ReleaseSizeMinor:
		v[1]++
		v[2] = 0
	case ReleaseSizeMajor:
		v[0]++
		v[1] = 0
		v[2] = 0
	default:
		return "", fmt.Errorf("mergeshadow: release size %q has no version step", size)
	}
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]), nil
}

// PlanVersionBump plans the bump from baseVersion and then checks it against
// the version files read from the trial tree. treeVersions maps each version
// file path to the value it holds, with "" for a file that holds no version.
// The planned version does not depend on treeVersions, so planning twice over
// the same base and size gives the same Planned, and a second run over a tree
// that already carries the bump reports AlreadyAtPlanned and no Stale files.
func PlanVersionBump(baseVersion string, size ReleaseSize, treeVersions map[string]string) (VersionPlan, error) {
	planned, err := PlanVersion(baseVersion, size)
	if err != nil {
		return VersionPlan{}, err
	}
	plan := VersionPlan{Base: baseVersion, Size: size, Planned: planned}
	for path, value := range treeVersions {
		if strings.TrimSpace(value) != planned {
			plan.Stale = append(plan.Stale, path)
		}
	}
	sort.Strings(plan.Stale)
	plan.AlreadyAtPlanned = len(treeVersions) > 0 && len(plan.Stale) == 0
	return plan, nil
}

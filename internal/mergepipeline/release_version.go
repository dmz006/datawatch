package mergepipeline

import (
	"fmt"
	"strconv"
	"strings"
)

// ReleaseSize is how much a release raises the version: a patch, a minor or
// a major step.
type ReleaseSize string

const (
	// ReleaseSizePatch raises the patch number by one.
	ReleaseSizePatch ReleaseSize = "patch"
	// ReleaseSizeMinor raises the minor number by one and resets patch to 0.
	ReleaseSizeMinor ReleaseSize = "minor"
	// ReleaseSizeMajor raises the major number by one and resets minor and
	// patch to 0.
	ReleaseSizeMajor ReleaseSize = "major"
)

// NextVersion returns the version that follows current for a release of the
// given size. current must be X.Y.Z with non-negative decimal numbers. An
// empty size is treated as a patch release. An unknown size is an error.
func NextVersion(current string, size ReleaseSize) (string, error) {
	major, minor, patch, err := parseReleaseVersion(current)
	if err != nil {
		return "", err
	}
	switch size {
	case "", ReleaseSizePatch:
		patch++
	case ReleaseSizeMinor:
		minor++
		patch = 0
	case ReleaseSizeMajor:
		major++
		minor = 0
		patch = 0
	default:
		return "", fmt.Errorf("unknown release size %q", string(size))
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}

// parseReleaseVersion splits an X.Y.Z version into its three numbers.
func parseReleaseVersion(v string) (int, int, int, error) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("version %q is not X.Y.Z", v)
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || (len(p) > 1 && p[0] == '+') {
			return 0, 0, 0, fmt.Errorf("version %q is not X.Y.Z", v)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}

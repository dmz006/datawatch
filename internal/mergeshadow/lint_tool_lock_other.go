//go:build !unix

package mergeshadow

import "os"

// lockFile is a no-op off unix; the in-process mutex still serialises installs.
func lockFile(*os.File) error { return nil }

func unlockFile(*os.File) {}

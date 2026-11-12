//go:build windows

package autonomous

import "os"

// fileOwnedBy has no /proc to read on Windows, so nothing is ever owned and the
// finder matches nothing there.
func fileOwnedBy(fi os.FileInfo, uid int) bool { return false }

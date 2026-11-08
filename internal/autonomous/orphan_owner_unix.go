//go:build !windows

package autonomous

import (
	"os"
	"syscall"
)

// fileOwnedBy reports whether fi is owned by uid.
func fileOwnedBy(fi os.FileInfo, uid int) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

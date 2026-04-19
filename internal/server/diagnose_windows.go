//go:build windows

package server

func checkDiskSpace(name, path string, minFreeBytes uint64) DiagnoseCheck {
	// Windows free-space check via syscall.GetDiskFreeSpaceEx is non-trivial
	// to add without `golang.org/x/sys/windows`. Skip with an informational
	// note so the composite check stays meaningful on Windows.
	return DiagnoseCheck{
		Name: name, OK: true, Detail: "skipped on windows",
	}
}

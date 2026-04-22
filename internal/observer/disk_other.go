//go:build !linux

package observer

func readDiskUsagePlatform() []Disk { return nil }

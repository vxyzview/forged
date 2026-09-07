//go:build !windows

// free-disk reporting via statfs. Split out so windows builds keep compiling
// (the doctor gracefully skips the disk check there).
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package cli

import (
	"syscall"
)

// freeDiskBytes returns the number of free bytes on the filesystem holding
// dir, or an error when the filesystem probe fails.
func freeDiskBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

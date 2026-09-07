//go:build windows

// Windows stub for the doctor disk-space probe: statfs is not available, so
// the doctor simply reports the check as unavailable.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package cli

import "fmt"

// freeDiskBytes always errors on Windows; diskSpaceCheck degrades gracefully.
func freeDiskBytes(dir string) (uint64, error) {
	return 0, fmt.Errorf("disk space check not supported on windows")
}

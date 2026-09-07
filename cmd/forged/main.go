// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
//
// Command forged is the entry point: prints the banner and dispatches to the
// cobra CLI, propagating the exit code.
package main

import (
	"os"

	"github.com/vxyzview/forged/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}

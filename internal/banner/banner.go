// Package banner renders the FORGED startup banner and farewell panel
// using lipgloss styling.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package banner

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/vxyzview/forged/internal/config"
)

// Banner is the FORGED ASCII art (figlet "ANSI Shadow" style).
const Banner = `
 ▄████  ▒█████   ██▀███    ▄████ ▓█████ ▓█████▄
 ██▒ ▀█▒▒██▒  ██▒▓██ ▒ ██▒ ██▒ ▀█▒▓█   ▀ ▒██▀ ██▌
▒██░▄▄▄░▒██░  ██▒▓██ ░▄█ ▒▒██░▄▄▄░▒███   ░██   █▌
░▓█  ██▓▒██   ██░▒██▀▀█▄  ░▓█  ██▓▒▓█  ▄ ░▓█▄   ▌
░▒▓███▀▒░ ████▓▒░░██▓ ▒██▒░▒▓███▀▒░▒████▒░▒████▓
 ░▒   ▒ ░ ▒░▒░▒░ ░ ▒▓ ░▒▓░ ░▒   ▒ ░░ ░  ░ ░ ▒  ▒
  ░   ░   ░ ▒ ▒░   ░▒ ░ ▒░  ░   ░    ░    ░ ░  ░
░ ░   ░ ░ ░ ░ ▒    ░░   ░ ░ ░   ░    ░      ░
      ░     ░ ░     ░           ░    ░  ░`

const (
	tagline   = "Android Kernel Builder  ·  AnyKernel3 Ready  ·  LLVM / Clang"
	copyright = "© 2026 vxyzview  —  Made with love"
)

var badges = []string{"AOSP Clang", "AnyKernel3", "arm64 · arm", "LTO", "ccache"}

// Print renders the FORGED startup banner to stdout.
// Minimalist: the wordmark, one tagline line, one quiet badge row.
// Solid colours only — no gradients.
func Print() {
	artStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff7c00"))
	version := lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")).Render("v" + config.Version)
	tag := lipgloss.NewStyle().Foreground(lipgloss.Color("#a8b2c0")).Render(tagline)
	copyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")).Render(copyright)

	fmt.Println()
	fmt.Println(artStyle.Render(Banner))
	fmt.Println()
	fmt.Println("  " + version + "  " + tag)
	fmt.Println("  " + copyStyle)

	// Feature badges: one dim, separated row — no boxes.
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#7c7c7c"))
	fmt.Println(dim.Render("  " + strings.Join(badges, "  ·  ")))
	fmt.Println()
}

// PrintFarewell renders the final build status panel.
// One bordered line, nothing else — solid colour, no gradient.
func PrintFarewell(success bool, elapsed float64, zipOutputDir string) {
	border := lipgloss.RoundedBorder()
	if success {
		ok := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#39d353")).Render("✓ BUILD COMPLETE")
		info := lipgloss.NewStyle().Foreground(lipgloss.Color("#a8b2c0")).
			Render(fmt.Sprintf("  %.1fs  ·  flashable ZIP in %s/", elapsed, zipOutputDir))
		panel := lipgloss.NewStyle().
			Border(border).
			BorderForeground(lipgloss.Color("#39d353")).
			Padding(0, 1).
			Render(ok + info)
		fmt.Println()
		fmt.Println(panel)
	} else {
		fail := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff4444")).Render("✗ BUILD FAILED")
		info := lipgloss.NewStyle().Foreground(lipgloss.Color("#a8b2c0")).
			Render(fmt.Sprintf("  %.1fs  ·  check the log for errors", elapsed))
		panel := lipgloss.NewStyle().
			Border(border).
			BorderForeground(lipgloss.Color("#ff4444")).
			Padding(0, 1).
			Render(fail + info)
		fmt.Println()
		fmt.Println(panel)
	}
	fmt.Println()
}

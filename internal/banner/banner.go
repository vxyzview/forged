// Package banner renders the FORGED ASCII startup banner and farewell panel
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

// Banner is the FORGED ASCII art.
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

var badges = []struct {
	label  string
	colour lipgloss.Color
}{
	{"  AOSP Clang  ", lipgloss.Color("#ff7c00")},
	{"  AnyKernel3  ", lipgloss.Color("#ffbe00")},
	{"  arm64 · arm  ", lipgloss.Color("#a8b2c0")},
	{"   LTO Ready   ", lipgloss.Color("#39d353")},
	{"   ccache ⚡   ", lipgloss.Color("#ff9f2f")},
}

// Print renders the FORGED startup banner to stdout.
func Print() {
	artStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff7c00"))
	version := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff9f2f")).Render("v" + config.Version)
	tag := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffbe00")).Render(tagline)
	diamond := lipgloss.NewStyle().Foreground(lipgloss.Color("#5a3010")).Render("  ◆  ")
	copyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#5a3010")).Render(copyright)

	body := lipgloss.JoinVertical(lipgloss.Center,
		artStyle.Render(Banner),
		diamond+version+diamond+tag+diamond,
		copyStyle,
	)

	panel := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		BorderForeground(lipgloss.Color("#ff7c00")).
		Padding(0, 2).
		Width(lipgloss.Width(body) + 4).
		Render(body)

	fmt.Println()
	fmt.Println(panel)

	// Feature badge strip.
	var rendered []string
	for _, b := range badges {
		rendered = append(rendered, lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(b.colour).
			Padding(0, 1).
			Bold(true).
			Foreground(b.colour).
			Render(b.label))
	}
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Center, rendered...))
	fmt.Println()
}

// PrintFarewell renders the final build status panel.
func PrintFarewell(success bool, elapsed float64, zipOutputDir string) {
	if success {
		ok := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#39d353")).Render("  ✦  BUILD COMPLETE  ✦  ")
		dir := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffbe00")).Render(zipOutputDir + "/")
		info := lipgloss.NewStyle().Foreground(lipgloss.Color("#a8b2c0")).
			Render(fmt.Sprintf("  Total time  %.1fs   ·   Flashable ZIP ready in ", elapsed))
		panel := lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#39d353")).
			Padding(1, 6).
			Render(strings.Join([]string{ok, info + dir}, "\n\n"))
		fmt.Println()
		fmt.Println(panel)
	} else {
		fail := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff4444")).Render("  ✗  BUILD FAILED  ✗  ")
		info := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9f2f")).
			Render(fmt.Sprintf("  Total time  %.1fs   ·   Check the log for errors", elapsed))
		panel := lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#ff4444")).
			Padding(1, 6).
			Render(strings.Join([]string{fail, info}, "\n\n"))
		fmt.Println()
		fmt.Println(panel)
	}
	fmt.Println()
}

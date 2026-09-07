// doctor.go implements the `forged doctor` diagnostic command: a one-shot
// health check for the host build environment.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vxyzview/forged/internal/toolchain"
)

// errSilent is returned by commands that already printed their own report
// and only need a non-zero exit status WITHOUT the red Fatal Error box.
var errSilent = errors.New("forged: command reported its own failure")

// check is a single diagnostic result.
type check struct {
	name string
	ok   bool
	// note explains the result — the found path, the missing package, etc.
	note string
	// fix is an optional command the user can run to resolve a failure.
	fix string
}

// doctorResult aggregates one doctor run.
type doctorResult struct {
	checks []check
	// fatal is set when the host cannot build at all (e.g. non-Linux).
	fatal string
}

// allOK reports whether every check passed.
func (r *doctorResult) allOK() bool {
	for _, c := range r.checks {
		if !c.ok {
			return false
		}
	}
	return r.fatal == ""
}

// runDoctorChecks gathers the environment diagnostics. Pure enough to unit
// test: no side effects, everything derived from PATH/filesystem probes.
func runDoctorChecks() *doctorResult {
	res := &doctorResult{}

	// ── Platform gate ────────────────────────────────────────────────────
	if runtime.GOOS != "linux" {
		res.fatal = fmt.Sprintf("kernel compilation needs Linux (this host: %s)", runtime.GOOS)
	}

	// ── Required tools ───────────────────────────────────────────────────
	res.checks = append(res.checks, checkBinaries([]string{
		"git", "make", "clang", "flex", "bison", "bc", "perl",
	})...)

	// ── LLVM binutils (required by the LLVM-only toolchain) ──────────────
	llvmBins := []string{"ld.lld", "llvm-ar", "llvm-objcopy"}
	for _, b := range llvmBins {
		if p := toolchain.Which(b); p != "" {
			res.checks = append(res.checks, check{name: b, ok: true, note: p})
		} else {
			res.checks = append(res.checks, check{
				name: b,
				ok:   false,
				note: "required for the LLVM-only toolchain",
				fix:  toolchain.InstallClangHint(),
			})
		}
	}

	// ── GNU cross-compilers (optional but recommended) ───────────────────
	gccMap := toolchain.CheckGnuCrossCompilers(nil)
	for _, arch := range toolchain.GnuPackageOrder {
		if gccMap[arch] != "" {
			res.checks = append(res.checks, check{
				name: "cross-compiler " + arch,
				ok:   true,
				note: gccMap[arch],
			})
		} else {
			pkg := "gcc-aarch64-linux-gnu"
			if arch == "arm" {
				pkg = "gcc-arm-linux-gnueabihf"
			}
			res.checks = append(res.checks, check{
				name: "cross-compiler " + arch,
				ok:   false,
				note: "optional — some kernel Makefile checks need it",
				fix:  "sudo apt install " + pkg,
			})
		}
	}

	// ── ccache (optional) ────────────────────────────────────────────────
	if p := toolchain.Which("ccache"); p != "" {
		res.checks = append(res.checks, check{name: "ccache", ok: true, note: p})
	} else {
		res.checks = append(res.checks, check{
			name: "ccache",
			ok:   false,
			note: "optional — rebuilds drop 60-90% with it",
			fix:  "sudo apt install ccache",
		})
	}

	// ── aria2 (optional, faster toolchain downloads) ─────────────────────
	if p := toolchain.Which("aria2c"); p != "" {
		res.checks = append(res.checks, check{name: "aria2c", ok: true, note: p})
	} else {
		res.checks = append(res.checks, check{
			name: "aria2c",
			ok:   false,
			note: "optional — 16-connection downloads fall back to net/http without it",
			fix:  "sudo apt install aria2",
		})
	}

	// ── AnyKernel3 staging sanity ────────────────────────────────────────
	ak3Dir := "AnyKernel3"
	if fi, err := os.Stat(ak3Dir); err == nil && fi.IsDir() {
		res.checks = append(res.checks, check{
			name: "AnyKernel3 staging",
			ok:   true,
			note: ak3Dir + "/",
		})
	} else {
		res.checks = append(res.checks, check{
			name: "AnyKernel3 staging",
			ok:   false,
			note: "no AnyKernel3/ directory in the working directory",
			fix:  "run the build from the forged repository, or set anykernel3.anykernel_dir",
		})
	}

	// ── Disk space (kernel trees need 20+ GiB) ───────────────────────────
	res.checks = append(res.checks, diskSpaceCheck(".")...)

	return res
}

// checkBinaries produces a check per binary name.
func checkBinaries(names []string) []check {
	checks := make([]check, 0, len(names))
	for _, name := range names {
		if p := toolchain.Which(name); p != "" {
			checks = append(checks, check{name: name, ok: true, note: p})
		} else {
			checks = append(checks, check{
				name: name,
				ok:   false,
				note: "required",
				fix:  "sudo apt install " + name,
			})
		}
	}
	return checks
}

// diskSpaceCheck reports free space for the filesystem holding dir. A no-op
// on platforms where the statfs syscall is unavailable.
func diskSpaceCheck(dir string) []check {
	free, err := freeDiskBytes(dir)
	if err != nil {
		return []check{{name: "disk space", ok: false, note: "could not stat " + dir}}
	}
	gb := float64(free) / (1 << 30)
	if gb < 5 {
		return []check{{
			name: "disk space",
			ok:   false,
			note: fmt.Sprintf("%.1f GiB free — kernel trees need 20+ GiB (source + out + ccache)", gb),
		}}
	}
	return []check{{
		name: "disk space",
		ok:   true,
		note: fmt.Sprintf("%.1f GiB free", gb),
	}}
}

const doctorOKBoxTmpl = "  ✦  All checks passed — ready to forge."

// printDoctor renders the check table and a summary box. Returns non-zero
// when any required check failed (used as the process exit status).
func printDoctor(res *doctorResult) int {
	fmt.Println(stylePrimary.Render("  ━━━  ✚  FORGED DOCTOR  ✚  ━━━  "))
	fmt.Println()

	if res.fatal != "" {
		fmt.Println(boxWarn.Render(
			styleWarn.Bold(true).Render("  ▲  Host limitation  ") + "\n\n  " + styleWarn.Render(res.fatal)))
		fmt.Println()
	}

	for _, c := range res.checks {
		mark := styleOK.Render("✓")
		if !c.ok {
			mark = styleErr.Render("✗")
		}
		fmt.Printf("  %s  %-22s  %s\n", mark, styleSteel.Render(c.name), styleDim.Render(c.note))
		if !c.ok && c.fix != "" {
			fmt.Printf("        %s  %s\n", styleWarn.Render("fix:"), styleSecondary.Render(c.fix))
		}
	}

	fmt.Println()
	if res.allOK() {
		fmt.Println(boxOK.Render(styleOK.Bold(true).Render(doctorOKBoxTmpl)))
		return 0
	}
	failed := 0
	for _, c := range res.checks {
		if !c.ok {
			failed++
		}
	}
	fmt.Println(boxWarn.Render(
		fmt.Sprintf("  %s\n\n  %d of %d checks failed.",
			styleWarn.Bold(true).Render("  ▲  Attention needed  "), failed, len(res.checks))))
	fmt.Println()
	return 1
}

// trimDoctorPath shortens a binary path for display.
func trimDoctorPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(p, home)
		}
	}
	return p
}

// ── doctor command ───────────────────────────────────────────────────────────

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the build environment (git, make, clang, ccache, disk)",
		RunE: func(cmd *cobra.Command, args []string) error {
			res := runDoctorChecks()
			if code := printDoctor(res); code != 0 {
				return errSilent
			}
			return nil
		},
	}
}

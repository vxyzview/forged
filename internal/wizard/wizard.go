// Package wizard runs the interactive configuration wizard as a huh form.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/vxyzview/forged/internal/config"
	"github.com/vxyzview/forged/internal/toolchain"
)

// theme is the huh theme used for every form.
func theme() *huh.Theme {
	return huh.ThemeBase16()
}

// RunForm runs a single huh form with the forge theme.
func RunForm(form *huh.Form) error {
	return form.WithTheme(theme()).Run()
}

// Run launches the interactive wizard and returns the filled config.
func Run() (*config.BuildConfig, error) {
	cfg := config.New()

	cwd, _ := os.Getwd()

	// ── Kernel source ────────────────────────────────────────────────────────
	var (
		sourceType   string
		sourceURL    string
		sourceBranch string
		sourceDepthS = "1"
		sourceDest   = filepath.Join(cwd, "kernel")
		kernelSource = cwd
		defconfig    = "defconfig"
		arch         = "arm64"
	)

	if err := RunForm(huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Kernel source type").
				Description("Where does the kernel tree live?").
				Options(
					huh.NewOption("Local directory", "local"),
					huh.NewOption("Git repository (clone on first build)", "git"),
				).
				Value(&sourceType),
		),
	)); err != nil {
		return nil, err
	}

	if sourceType == "git" {
		if err := RunForm(huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Git URL").
					Description("Repository to clone on first build").
					Placeholder("https://github.com/yourorg/kernel.git").
					Value(&sourceURL),
				huh.NewInput().
					Title("Branch / tag").
					Description("Blank = remote default branch").
					Value(&sourceBranch),
				huh.NewInput().
					Title("Clone depth").
					Description("1 = shallow (fast) · 0 = full history").
					Placeholder("1").
					Value(&sourceDepthS),
				huh.NewInput().
					Title("Clone destination").
					Description("Local path to clone into").
					Value(&sourceDest),
			),
		)); err != nil {
			return nil, err
		}
		if d, err := strconv.Atoi(strings.TrimSpace(sourceDepthS)); err == nil && d >= 0 {
			cfg.KernelSourceDepth = d
		}
		cfg.KernelSourceURL = strings.TrimSpace(sourceURL)
		cfg.KernelSourceBranch = strings.TrimSpace(sourceBranch)
		cfg.KernelSource = strings.TrimSpace(sourceDest)
		fmt.Printf("\n  ✦  Kernel will be cloned from %s into %s on first build.\n\n",
			cfg.KernelSourceURL, cfg.KernelSource)
	} else {
		if err := RunForm(huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Kernel source directory").
					Value(&kernelSource),
				huh.NewInput().
					Title("Defconfig name").
					Placeholder("defconfig").
					Value(&defconfig),
				huh.NewInput().
					Title("Architecture").
					Placeholder("arm64").
					Value(&arch),
			),
		)); err != nil {
			return nil, err
		}
		cfg.KernelSource = strings.TrimSpace(kernelSource)
		if s := strings.TrimSpace(defconfig); s != "" {
			cfg.KernelDefconfig = s
		}
		if s := strings.TrimSpace(arch); s != "" {
			cfg.Arch = s
			cfg.Subarch = s
		}
	}

	// ── Toolchain ────────────────────────────────────────────────────────────
	var (
		preset       = "aosp-clang"
		clangVersion = "r584948b"
		autoClone    = true
		toolchainDir = toolchain.DefaultToolchainBase()
		extraPath    string
	)

	if err := RunForm(huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Toolchain preset").
				Options(
					huh.NewOption("aosp-clang — download prebuilt Clang from AOSP (recommended)", "aosp-clang"),
					huh.NewOption("system-clang — use clang already on $PATH", "system-clang"),
				).
				Value(&preset),
			huh.NewInput().
				Title("AOSP Clang version").
				Description("e.g. r584948b, r522817").
				Placeholder("r584948b").
				Value(&clangVersion),
			huh.NewConfirm().
				Title("Auto-download toolchain if not present?").
				Value(&autoClone),
			huh.NewInput().
				Title("Toolchain storage directory").
				Description("Used when auto-downloading").
				Value(&toolchainDir),
			huh.NewInput().
				Title("Extra PATH for toolchain binaries").
				Description("e.g. ~/toolchains/clang/bin — only when NOT auto-downloading").
				Value(&extraPath),
		),
	)); err != nil {
		return nil, err
	}

	cfg.Toolchain = config.NewToolchainConfig()
	if err := cfg.Toolchain.ApplyPreset(preset); err != nil {
		return nil, err
	}
	if preset == "aosp-clang" {
		if s := strings.TrimSpace(clangVersion); s != "" {
			cfg.Toolchain.AOSPClangVersion = s
		}
		fmt.Printf("\n  ›  Will download clang-%s.tar.gz from AOSP googlesource.\n", cfg.Toolchain.AOSPClangVersion)
		// AOSP only publishes kernel-build Clang prebuilts as linux-x86
		// archives — they cannot run on macOS, Windows, or Linux/arm hosts.
		// Fall back to system-clang everywhere else so the wizard never
		// produces a config that fails at build time.
		if !toolchain.AOSPClangSupported() {
			fmt.Printf("  ▲  AOSP prebuilts are linux-x86_64 only; this host is %s/%s.\n", runtime.GOOS, runtime.GOARCH)
			fmt.Printf("     Switching to system-clang — install it with:  %s\n", toolchain.InstallClangHint())
			if err := cfg.Toolchain.ApplyPreset("system-clang"); err != nil {
				return nil, err
			}
		}
	}
	// Keep auto-clone in sync with what the preset can actually do: the
	// aosp-clang preset has nothing to auto-download off linux/amd64.
	cfg.Toolchain.AutoClone = autoClone && cfg.Toolchain.Preset == "aosp-clang" && toolchain.AOSPClangSupported()
	cfg.AutoSetupToolchain = cfg.Toolchain.AutoClone
	if autoClone {
		if s := strings.TrimSpace(toolchainDir); s != "" {
			cfg.ToolchainDir = s
		}
		cfg.Toolchain.ExtraPath = []string{}
		fmt.Printf("  ✦  Toolchain will be stored in %s on first build.\n\n", cfg.ToolchainDir)
	} else if s := strings.TrimSpace(extraPath); s != "" {
		cfg.Toolchain.ExtraPath = []string{s}
	}

	// ── Cross-compiler availability ──────────────────────────────────────────
	fmt.Println("  ── Cross-Compiler Availability ──")
	gccMap := toolchain.CheckGnuCrossCompilers(func(line string) {
		fmt.Printf("  %s\n", line)
	})
	missing := false
	for _, a := range toolchain.GnuPackageOrder {
		if gccMap[a] == "" {
			missing = true
			break
		}
	}
	if missing {
		var install bool
		if err := RunForm(huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Install missing GNU cross-compiler packages via apt?").
					Value(&install),
			),
		)); err != nil {
			return nil, err
		}
		if install {
			if err := toolchain.InstallGnuCrossCompilers(func(line string) {
				fmt.Printf("  %s\n", line)
			}); err != nil {
				fmt.Printf("  ▲  %s\n\n", err)
			} else {
				fmt.Println("  ✦  Cross-compilers installed.")
			}
		}
	}
	fmt.Println()

	// ── Build settings ───────────────────────────────────────────────────────
	var (
		jobsS        = "0"
		localversion string
		buildUser    = config.DefaultKBUILDBuildUser()
		lto          = "none"
		outputDir    = "out"
		zipOutputDir = "releases"
	)

	if err := RunForm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Parallel jobs").
				Description("0 = auto-detect CPU count").
				Placeholder("0").
				Value(&jobsS),
			huh.NewInput().
				Title("LOCALVERSION suffix").
				Value(&localversion),
			huh.NewInput().
				Title("Build user").
				Description("Stamped into the kernel version string").
				Value(&buildUser),
			huh.NewSelect[string]().
				Title("LTO mode").
				Options(
					huh.NewOption("none", "none"),
					huh.NewOption("thin", "thin"),
					huh.NewOption("full", "full"),
				).
				Value(&lto),
			huh.NewInput().
				Title("Build output directory").
				Placeholder("out").
				Value(&outputDir),
			huh.NewInput().
				Title("ZIP output directory").
				Placeholder("releases").
				Value(&zipOutputDir),
		),
	)); err != nil {
		return nil, err
	}

	if j, err := strconv.Atoi(strings.TrimSpace(jobsS)); err == nil && j >= 0 {
		cfg.Jobs = j
	}
	cfg.Localversion = localversion
	if s := strings.TrimSpace(buildUser); s != "" {
		cfg.KBUILDBuildUser = s
	}
	switch lto {
	case "thin", "full":
		cfg.LTO = lto
	default:
		cfg.LTO = ""
	}
	if s := strings.TrimSpace(outputDir); s != "" {
		cfg.OutputDir = s
	}
	if s := strings.TrimSpace(zipOutputDir); s != "" {
		cfg.ZipOutputDir = s
	}

	// ── ccache ───────────────────────────────────────────────────────────────
	var (
		ccacheEnabled  = toolchain.Which("ccache") != ""
		ccacheDir      string
		ccacheMax      = "5G"
		ccacheCompress = true
	)

	if err := RunForm(huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable ccache for faster rebuilds?").
				Description("Cuts rebuild times 60–90%").
				Value(&ccacheEnabled),
			huh.NewInput().
				Title("ccache directory").
				Description("Blank = ccache default ~/.cache/ccache").
				Value(&ccacheDir),
			huh.NewInput().
				Title("Max cache size").
				Description("e.g. 5G, 10G, 500M").
				Placeholder("5G").
				Value(&ccacheMax),
			huh.NewConfirm().
				Title("Enable ccache compression?").
				Description("Saves disk space").
				Value(&ccacheCompress),
		),
	)); err != nil {
		return nil, err
	}

	cfg.Ccache.Enabled = ccacheEnabled
	if ccacheEnabled {
		cfg.Ccache.Dir = strings.TrimSpace(ccacheDir)
		if s := strings.TrimSpace(ccacheMax); s != "" {
			cfg.Ccache.MaxSize = s
		}
		cfg.Ccache.Compress = ccacheCompress
		fmt.Printf("\n  ✦  ccache enabled  max=%s  compress=%s\n\n",
			cfg.Ccache.MaxSize, map[bool]string{true: "on", false: "off"}[ccacheCompress])
	}

	// ── Extra build flags ────────────────────────────────────────────────────
	var (
		extraFlags string
		extraEnv   string
	)

	if err := RunForm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Extra make flags").
				Description("Space-separated VAR=VALUE — e.g. LLVM=1 LLVM_IAS=1 KCFLAGS=-pipe").
				Value(&extraFlags),
			huh.NewInput().
				Title("Extra env vars").
				Description("Space-separated KEY=VALUE — e.g. KBUILD_VERBOSE=1").
				Value(&extraEnv),
		),
	)); err != nil {
		return nil, err
	}

	if s := strings.TrimSpace(extraFlags); s != "" {
		cfg.ExtraMakeFlags = strings.Fields(s)
	}
	if s := strings.TrimSpace(extraEnv); s != "" {
		for _, pair := range strings.Fields(s) {
			if k, v, ok := strings.Cut(pair, "="); ok {
				cfg.ExtraEnv[strings.TrimSpace(k)] = v
			} else {
				cfg.ExtraEnv[strings.TrimSpace(pair)] = ""
			}
		}
	}

	// ── AnyKernel3 ───────────────────────────────────────────────────────────
	var (
		kernelName = "Forged"
		block      = "/dev/block/by-name/boot"
		slotDevice bool
		devices    string
	)

	if err := RunForm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Kernel name").
				Placeholder("Forged").
				Value(&kernelName),
			huh.NewInput().
				Title("Flash block").
				Value(&block),
			huh.NewConfirm().
				Title("Slot device (A/B)?").
				Value(&slotDevice),
			huh.NewInput().
				Title("Supported device names").
				Description("comma-separated — blank = all").
				Value(&devices),
		),
	)); err != nil {
		return nil, err
	}

	ak3 := config.NewAnyKernel3Config()
	if s := strings.TrimSpace(kernelName); s != "" {
		ak3.KernelName = s
	}
	if s := strings.TrimSpace(block); s != "" {
		ak3.Block = s
	}
	if slotDevice {
		ak3.IsSlotDevice = 1
	}
	for _, d := range strings.Split(devices, ",") {
		if d = strings.TrimSpace(d); d != "" {
			ak3.DeviceNames = append(ak3.DeviceNames, d)
		}
	}
	if len(ak3.DeviceNames) > 0 {
		ak3.DoDevicecheck = 1
	} else {
		ak3.DoDevicecheck = 0
	}
	cfg.Anykernel3 = ak3

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// ── Save ─────────────────────────────────────────────────────────────────
	var (
		save     bool
		savePath = "build_config.json"
	)

	if err := RunForm(huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Save this configuration?").
				Value(&save),
			huh.NewInput().
				Title("Save path").
				Placeholder("build_config.json").
				Value(&savePath),
		),
	)); err != nil {
		return nil, err
	}

	if save {
		if strings.TrimSpace(savePath) == "" {
			savePath = "build_config.json"
		}
		if err := cfg.ToJSON(savePath); err != nil {
			return nil, err
		}
		fmt.Printf("\n  ✦  Configuration saved to %s\n", savePath)
		fmt.Printf("     Run: forged build -c %s\n\n", savePath)
	}

	return cfg, nil
}

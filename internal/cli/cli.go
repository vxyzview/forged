// Package cli implements the FORGED command line: cobra commands, the Bubble
// Tea build runner and supporting helpers.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/vxyzview/forged/internal/banner"
	"github.com/vxyzview/forged/internal/builder"
	"github.com/vxyzview/forged/internal/cienv"
	"github.com/vxyzview/forged/internal/config"
	"github.com/vxyzview/forged/internal/packager"
	"github.com/vxyzview/forged/internal/toolchain"
	"github.com/vxyzview/forged/internal/tui"
	"github.com/vxyzview/forged/internal/wizard"
)

// ── lipgloss styles (forge-fire) ─────────────────────────────────────────────

var (
	stylePrimary   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff7c00"))
	styleSecondary = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffbe00"))
	styleSteel     = lipgloss.NewStyle().Foreground(lipgloss.Color("#a8b2c0"))
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("#606060"))
	styleOK        = lipgloss.NewStyle().Foreground(lipgloss.Color("#39d353"))
	styleWarn      = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9f2f"))
	styleErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4444"))

	boxPrimary = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#ff7c00")).Padding(1, 2)
	boxOK      = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#39d353")).Padding(0, 2)
	boxWarn    = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#ff9f2f")).Padding(0, 2)
	boxErr     = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#ff4444")).Padding(0, 2)
)

func info(msg string)    { fmt.Printf("  %s  %s\n", styleSteel.Render("›"), msg) }
func okLine(msg string)  { fmt.Printf("  %s  %s\n", styleOK.Render("✦"), msg) }
func warn(msg string)    { fmt.Printf("  %s  %s\n", styleWarn.Render("▲"), styleWarn.Render(msg)) }
func errLine(msg string) { fmt.Printf("  %s  %s\n", styleErr.Render("✗"), styleErr.Render(msg)) }

// bannerOnce prints the startup banner (used by the root command).
func bannerOnce() {
	banner.Print()
}

// ── Root command ─────────────────────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "forged",
	Short: "FORGED — Android Kernel Builder with AnyKernel3 packaging",
	Long: `FORGED — Android Kernel Builder · AnyKernel3 Ready · LLVM / Clang

Orchestrates mrproper → defconfig → build with a live Bubble Tea TUI,
then packages a flashable AnyKernel3 ZIP under releases/.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		bannerOnce()
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// Execute runs the CLI.
func Execute() int {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, errSilent) {
			return 1
		}
		fmt.Fprintln(os.Stderr, boxErr.Render(styleErr.Bold(true).Render("  ✗  Fatal Error  ")+"\n\n  "+err.Error()))
		return 1
	}
	return 0
}

// ── Build command ────────────────────────────────────────────────────────────

type buildFlags struct {
	configPath     string
	wizard         bool
	source         string
	sourceURL      string
	sourceBranch   string
	sourceDepth    int
	sourceDepthSet bool
	defconfig      string
	arch           string
	jobs           int
	noClean        bool
	noPackage      bool
	versionTag     string
	anykernelDir   string
	anykernelSrc   string
	logFile        string
	makeFlags      []string
	extraEnv       []string
	ccache         *bool
	ci             bool
	toolchainDir   string
	// toolchainExtraPath appends entries to toolchain.extra_path so CI jobs
	// can point at a pre-provisioned clang without a config file.
	toolchainExtraPath []string
}

func newBuildCmd() *cobra.Command {
	f := &buildFlags{}
	ccacheTrue, ccacheFalse := true, false

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the kernel (mrproper → defconfig → compile → package)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("ccache") {
				f.ccache = &ccacheTrue
			} else if cmd.Flags().Changed("no-ccache") {
				f.ccache = &ccacheFalse
			}
			return runBuild(cmd.Context(), f)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&f.configPath, "config", "c", "", "Build config file (JSON/TOML)")
	flags.BoolVarP(&f.wizard, "wizard", "w", false, "Launch interactive wizard")
	flags.StringVarP(&f.source, "source", "s", "", "Kernel source directory (local path)")
	flags.StringVar(&f.sourceURL, "source-url", "", "Git URL to clone the kernel source from")
	flags.StringVar(&f.sourceBranch, "source-branch", "", "Branch or tag to checkout when cloning (default: remote HEAD)")
	flags.IntVar(&f.sourceDepth, "source-depth", 1, "Clone depth (1=shallow, 0=full history)")
	flags.Lookup("source-depth").NoOptDefVal = ""
	flags.StringVarP(&f.defconfig, "defconfig", "d", "", "Defconfig name")
	flags.StringVar(&f.arch, "arch", "", "Target architecture: arm64, arm, or x86_64 (default: arm64)")
	flags.IntVarP(&f.jobs, "jobs", "j", -1, "Parallel jobs (0=auto)")
	flags.BoolVar(&f.noClean, "no-clean", false, "Skip mrproper step")
	flags.BoolVar(&f.noPackage, "no-package", false, "Skip AnyKernel3 packaging")
	flags.StringVar(&f.versionTag, "version-tag", "", "Version tag appended to ZIP name")
	flags.StringVar(&f.anykernelDir, "anykernel-dir", "", "Path to AnyKernel3 directory")
	flags.StringVar(&f.anykernelSrc, "anykernel-source", "", "AnyKernel3 source: osm0sis, git, local, or stub")
	flags.StringVar(&f.logFile, "log-file", "", "Write errors+warnings here (default: auto-generated under <output_dir>/logs/)")
	flags.StringArrayVarP(&f.makeFlags, "make-flag", "F", nil, "Append a make variable, e.g. -F LLVM=1 (repeatable)")
	flags.StringArrayVarP(&f.extraEnv, "env", "E", nil, "Inject an env var, e.g. -E KBUILD_VERBOSE=1 (repeatable)")
	flags.Bool("ccache", false, "Enable ccache")
	flags.Bool("no-ccache", false, "Disable ccache")
	flags.BoolVar(&f.ci, "ci", false, "CI mode: no interactive prompts, write results to GITHUB_OUTPUT + GITHUB_STEP_SUMMARY (auto-enabled on GitHub Actions)")
	flags.StringVar(&f.toolchainDir, "toolchain-dir", "", "Toolchain storage directory (default: ~/.local/share/forged/toolchains; useful with the CI cache action)")
	flags.StringSliceVar(&f.toolchainExtraPath, "toolchain-extra-path", nil, "Directory with a working clang (repeatable); skips the auto-download, useful for system-clang CI")

	// Track explicit --source-depth so argparse-default semantics match the
	// Python original: omitted flag must not clobber a config-set depth=0.
	cobra.OnInitialize()
	_ = f
	return cmd
}

// sourceDepthWasSet detects --source-depth on the raw command line.
func sourceDepthWasSet() bool {
	for _, a := range os.Args {
		if a == "--source-depth" || strings.HasPrefix(a, "--source-depth=") {
			return true
		}
	}
	return false
}

func runBuild(ctx context.Context, f *buildFlags) error {
	var cfg *config.BuildConfig

	ciMode := f.ci || cienv.Detected() || cienv.Forced()

	switch {
	case f.configPath != "":
		loaded, err := config.Load(f.configPath)
		if err != nil {
			return fmt.Errorf("cannot load config %q: %w", f.configPath, err)
		}
		cfg = loaded
	case f.wizard:
		w, err := wizard.Run()
		if err != nil {
			return err
		}
		cfg = w
	case ciMode && (f.source != "" || f.sourceURL != ""):
		// Flag-driven build: forge a config from the CLI flags so CI
		// workflows don't need a checked-in config file.
		cfg = config.New()
	case ciMode:
		// Never launch the interactive wizard in CI — stdin may be
		// /dev/null, so fail loudly with a pointer at the docs.
		return fmt.Errorf("no config supplied in --ci mode: pass --config <file> or --source/--source-url (see .github/workflows/build-kernel.yml template)")
	default:
		fmt.Println(boxWarn.Render(styleWarn.Bold(true).Render("  ▲  No Config  ") + "\n\n  No config supplied — launching interactive wizard."))
		w, err := wizard.Run()
		if err != nil {
			return err
		}
		cfg = w
	}

	// ── CLI overrides ──
	if f.source != "" {
		cfg.KernelSource = f.source
	}
	if f.sourceURL != "" {
		cfg.KernelSourceURL = f.sourceURL
	}
	if f.sourceBranch != "" {
		cfg.KernelSourceBranch = f.sourceBranch
	}
	if sourceDepthWasSet() {
		if f.sourceDepth < 0 {
			return fmt.Errorf("invalid --source-depth %d: must be 0 (full history) or a positive integer", f.sourceDepth)
		}
		cfg.KernelSourceDepth = f.sourceDepth
	}
	if f.defconfig != "" {
		cfg.KernelDefconfig = f.defconfig
	}
	if f.arch != "" {
		cfg.Arch = f.arch
		switch f.arch {
		case "arm64":
			cfg.Subarch = "arm64"
		case "arm":
			cfg.Subarch = "arm"
		case "x86_64":
			cfg.Subarch = "x86_64"
		}
	}
	if f.anykernelSrc != "" {
		switch f.anykernelSrc {
		case config.AK3SourceOsm0sis, config.AK3SourceGit, config.AK3SourceLocal, config.AK3SourceStub:
			cfg.Anykernel3.Source = f.anykernelSrc
		default:
			return fmt.Errorf("invalid --anykernel-source %q: must be one of %v", f.anykernelSrc, config.AnyKernel3SourceModes)
		}
		if f.anykernelSrc == config.AK3SourceOsm0sis && cfg.Anykernel3.RepoURL == "" {
			cfg.Anykernel3.RepoURL = config.DefaultAnyKernel3Repo
		}
	}
	if f.jobs >= 0 {
		cfg.Jobs = f.jobs
	}
	if f.ccache != nil {
		cfg.Ccache.Enabled = *f.ccache
	}

	seen := map[string]bool{}
	for _, flag := range cfg.ExtraMakeFlags {
		seen[flag] = true
	}
	for _, flag := range f.makeFlags {
		if !seen[flag] {
			cfg.ExtraMakeFlags = append(cfg.ExtraMakeFlags, flag)
			seen[flag] = true
		}
	}
	for _, pair := range f.extraEnv {
		if k, v, ok := strings.Cut(pair, "="); ok {
			cfg.ExtraEnv[strings.TrimSpace(k)] = v
		} else {
			cfg.ExtraEnv[strings.TrimSpace(pair)] = ""
		}
	}
	// Point the auto-managed toolchain at a cacheable directory (used by
	// the GitHub Actions template to persist AOSP Clang across runs).
	if f.toolchainDir != "" {
		cfg.ToolchainDir = f.toolchainDir
	}
	for _, p := range f.toolchainExtraPath {
		if p != "" {
			cfg.Toolchain.ExtraPath = append(cfg.Toolchain.ExtraPath, p)
		}
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	// CI mode is auto-enabled on GitHub Actions; the flag forces it
	// anywhere (runner-less debugging, self-hosted scripted builds).
	if ciMode {
		ci := newCI(true)
		ci.configName = f.configPath
		return executeBuild(ctx, cfg, !f.noClean, !f.noPackage,
			f.versionTag, f.anykernelDir, f.logFile, ci)
	}

	return executeBuild(ctx, cfg,
		!f.noClean,
		!f.noPackage,
		f.versionTag,
		f.anykernelDir,
		f.logFile,
		newCI(false),
	)
}

// ciRun describes how much CI plumbing a build performs.
type ciRun struct {
	// enabled reports whether CI outputs are emitted.
	enabled bool
	// strict reports whether toolchain warnings fail the build instead of
	// prompting. CI can never prompt — strict is the only sane default.
	strict bool
	// configName is echoed into the summary for traceability.
	configName string
}

// newCI returns a ciRun for an interactive (false) or CI (true) build.
func newCI(ci bool) *ciRun {
	return &ciRun{enabled: ci, strict: ci}
}

// ── Build execution with live Bubble Tea TUI ─────────────────────────────────

func executeBuild(ctx context.Context, cfg *config.BuildConfig, clean, doPackage bool, versionTag, anykernelDir, logFile string, ci *ciRun) error {
	if ci == nil {
		ci = newCI(false)
	}
	buildStart := time.Now()

	printConfigTable(cfg)
	fmt.Println()

	// ── Pre-build checklist ──
	sourceOK := cfg.KernelSourceURL != "" ||
		(cfg.KernelSource != "" && dirExists(cfg.KernelSource))
	printChecklist(sourceOK, cfg.Toolchain.Preset != "", toolchain.Which("ccache") != "", cfg.Ccache.Enabled, clean, doPackage)
	fmt.Println()

	b, err := builder.New(ctx, cfg, func(line string) { info(line) })
	if err != nil {
		return err
	}

	// ── Toolchain warnings ──
	warnings := b.ValidateToolchain()
	for _, w := range warnings {
		fmt.Println(boxWarn.Render(styleWarn.Bold(true).Render("  ▲  Warning  ") + "\n\n  " + styleWarn.Render(w)))
	}
	switch {
	case len(warnings) == 0:
		// nothing to decide
	case ci.strict:
		// Never prompt in CI — stdin may be /dev/null. Missing tools are a
		// hard error so the run fails loudly instead of producing a broken
		// ZIP that someone downstream might flash.
		for _, w := range warnings {
			fmt.Fprintf(os.Stdout, "::warning title=forged::%s\n", strings.ReplaceAll(w, "\n", " "))
		}
		return fmt.Errorf("aborted: %d toolchain warning(s) in --ci mode; fix the environment (forged doctor) and retry", len(warnings))
	default:
		if !confirmDefault("Continue anyway?", false) {
			return fmt.Errorf("aborted: toolchain issues must be resolved first")
		}
	}

	// ── Live TUI build ──
	steps := b.Steps(clean)
	names := make([]string, 0, len(steps))
	for _, s := range steps {
		names = append(names, s.Name)
	}

	// Detect a usable TTY first: without one (piped output, CI, here-docs)
	// Bubble Tea cannot open /dev/tty. Fall back to plain streaming output
	// so builds still work and logs are still collected.
	hasTTY := false
	if ttyOut, ttyErr := os.OpenFile("/dev/tty", os.O_WRONLY, 0); ttyOut != nil {
		ttyOut.Close()
		hasTTY = true
	} else {
		_ = ttyErr
	}
	if !hasTTY && !term.IsTerminal(int(os.Stdout.Fd())) && !term.IsTerminal(int(os.Stdin.Fd())) {
		results, allLines := runNoTTY(ctx, b, steps)
		return finishBuild(cfg, b, results, allLines, buildStart, doPackage, versionTag, anykernelDir, logFile, ci)
	}

	msgCh := make(chan tea.Msg, 1024)
	model := tui.NewBuildModel(names).WithMessages(msgCh)
	program := tea.NewProgram(model)

	// Runner: executes every build step, feeding streamed output into msgCh.
	go func() {
		defer close(msgCh)
		for i, step := range steps {
			msgCh <- tui.StepStartMsg{Name: step.Name, Index: i, Total: len(steps)}
			result := b.RunStep(ctx, step, func(line string) {
				msgCh <- tui.LogLineMsg{Line: line}
			})
			msgCh <- tui.StepDoneMsg{Result: result}
		}
	}()

	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	bm, ok := finalModel.(tui.BuildModel)
	if !ok {
		return fmt.Errorf("unexpected TUI model type %T", finalModel)
	}

	return finishBuild(cfg, b, bm.Results(), bm.AllLines(), buildStart, doPackage, versionTag, anykernelDir, logFile, ci)
}

// runNoTTY streams build output as plain log lines (no Bubble Tea screen).
// It returns every captured line plus per-step results for the shared
// post-build handling. Each streamed line is colourised exactly like the TUI.
func runNoTTY(ctx context.Context, b *builder.KernelBuilder, steps []builder.BuildStep) ([]builder.BuildResult, []string) {
	var (
		results  []builder.BuildResult
		allLines []string
	)

	for i, step := range steps {
		group := startLogGroup(fmt.Sprintf("forged-step-%d-%s", i+1, step.Name), step.Name)
		result := b.RunStep(ctx, step, func(line string) {
			allLines = append(allLines, line)
			fmt.Println(tui.Colourise(line))
		})
		group.close()
		if result.Success {
			okLine(fmt.Sprintf("%s  PASSED  (%.2fs)", styleSecondary.Render(result.Step), result.Duration))
		} else {
			errLine(fmt.Sprintf("%s  FAILED  (%.2fs)  —  %s",
				styleSecondary.Render(result.Step), result.Duration, result.Error))
		}
		results = append(results, result)
	}
	return results, allLines
}

// finishBuild performs the shared post-build handling: issues log, results
// table, AnyKernel3 packaging and the farewell banner. It is used by both the
// TTY (Bubble Tea) and no-TTY (plain streaming) build paths.
func finishBuild(cfg *config.BuildConfig, b *builder.KernelBuilder, results []builder.BuildResult, allLines []string, buildStart time.Time, doPackage bool, versionTag, anykernelDir, logFile string, ci *ciRun) error {
	if ci == nil {
		ci = newCI(false)
	}
	// ── Issues log file (always written) ──
	if logFile == "" {
		sourceRoot := cfg.KernelSource
		if sourceRoot == "" {
			sourceRoot = "."
		}
		ts := time.Now().Format("20060102_150405")
		logFile = filepath.Join(sourceRoot, cfg.OutputDir, "logs", "forged_issues_"+ts+".log")
	}
	errCount, warnCount := saveIssuesLog(logFile, cfg, results, allLines)
	if errCount+warnCount == 0 {
		okLine(fmt.Sprintf("Log saved  %s  (no issues found)", styleDim.Render(logFile)))
	} else {
		warn(fmt.Sprintf("Log saved  %s  %d error(s)  %d warning(s)",
			styleDim.Render(logFile), errCount, warnCount))
	}

	// ── Failure path ──
	allOK := true
	for _, r := range results {
		if !r.Success {
			allOK = false
		}
	}
	if !allOK {
		fmt.Println()
		fmt.Println(tui.RenderResultsTable(results, ""))
		fmt.Println()
		for _, r := range results {
			if !r.Success {
				fmt.Printf("  %s  Step %s failed — check the log above for details\n",
					styleWarn.Render("▲"), styleSecondary.Render(r.Step))
			}
		}
		fmt.Printf("\n  %s  Re-run with %s to skip mrproper on retry\n\n",
			styleSteel.Render("›"), styleSecondary.Render("--no-clean"))
		if ci.enabled {
			c := newCIOutputs()
			c.setOutput("outcome", "failure")
			runCIAnnotations(c.provider, results, nil)
			c.appendSummary(ciSummaryMarkdown(ci.configName, results, "", time.Since(buildStart)))
		}
		return fmt.Errorf("build failed")
	}

	// ── Packaging ──
	var zipPath string
	if doPackage {
		ak3Dir := anykernelDir
		if ak3Dir == "" {
			ak3Dir = cfg.Anykernel3.AnykernelDir
		}
		p := packager.NewWithProgress(cfg, ak3Dir, func(line string) { info(line) })

		image := b.FindKernelImage()
		if image == "" {
			fmt.Println(boxErr.Render(styleErr.Bold(true).Render("  ✗  Packaging Skipped  ") +
				"\n\n  Could not locate kernel image — skipping packaging.\n" +
				"  " + styleDim.Render("Searched: Image.gz-dtb, Image-dtb, Image.gz, Image, zImage-dtb, zImage")))
		} else {
			okLine(fmt.Sprintf("Kernel image  %s", styleSecondary.Render(image)))
			dtbs := b.FindDTBFiles()
			mods := b.FindModules()
			info(fmt.Sprintf("DTB files: %d  ·  Modules: %d", len(dtbs), len(mods)))

			if err := p.Prepare(image, dtbs, mods); err != nil {
				return fmt.Errorf("packaging failed: %w", err)
			}
			zp, err := p.CreateZip(cfg.ZipOutputDir, versionTag)
			if err != nil {
				return fmt.Errorf("zip creation failed: %w", err)
			}
			zipPath = zp
			var sizeMB float64
			if fi, err := os.Stat(zp); err == nil {
				sizeMB = float64(fi.Size()) / 1048576
			}
			fmt.Println()
			fmt.Println(boxOK.Render(
				styleOK.Bold(true).Render("  ✦  Package Ready  ") + "\n\n" +
					"  " + styleOK.Render("◎  ZIP Created") + "\n\n" +
					"  " + styleSecondary.Render(zp) + "\n" +
					"  " + styleDim.Render(fmt.Sprintf("%.2f MB  ·  Flash with TWRP or ADB sideload", sizeMB))))
		}
	}

	// ── Summary + farewell ──
	fmt.Println()
	fmt.Println(tui.RenderResultsTable(results, zipPath))

	elapsed := time.Since(buildStart)
	if ci.enabled {
		c := newCIOutputs()
		c.setOutput("outcome", "success")
		if zipPath != "" {
			c.setOutput("zip_path", zipPath)
			c.setOutput("zip_name", filepath.Base(zipPath))
		}
		if logFile != "" {
			c.setOutput("log_path", logFile)
		}
		issues := tui.ExtractIssues(allLines)
		var digest []string
		digest = append(digest, issues.Errors...)
		digest = append(digest, issues.Warnings...)
		runCIAnnotations(c.provider, results, digest)
		c.appendSummary(ciSummaryMarkdown(ci.configName, results, zipPath, elapsed))
	}
	banner.PrintFarewell(true, elapsed.Seconds(), cfg.ZipOutputDir)
	return nil
}

// ── Issues log ───────────────────────────────────────────────────────────────

func saveIssuesLog(dest string, cfg *config.BuildConfig, results []builder.BuildResult, lines []string) (int, int) {
	issues := tui.ExtractIssues(lines)

	if dir := filepath.Dir(dest); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}

	var sb strings.Builder
	ts := time.Now().Format("2006-01-02 15:04:05")
	sb.WriteString(strings.Repeat("=", 72) + "\n")
	sb.WriteString("  FORGED — Android Kernel Builder\n")
	sb.WriteString("  Build Issues Log  (errors + warnings)\n")
	sb.WriteString(strings.Repeat("=", 72) + "\n\n")

	sb.WriteString(fmt.Sprintf("  Generated : %s\n", ts))
	sb.WriteString(fmt.Sprintf("  Defconfig : %s\n", cfg.KernelDefconfig))
	sb.WriteString(fmt.Sprintf("  Arch      : %s\n", cfg.Arch))
	src := cfg.KernelSource
	if src == "" {
		src = "(not set)"
	}
	sb.WriteString(fmt.Sprintf("  Source    : %s\n", src))
	sb.WriteString(fmt.Sprintf("  Output    : %s\n", cfg.OutputDir))
	sb.WriteString(fmt.Sprintf("  Log lines : %d total captured\n", len(lines)))
	sb.WriteString(fmt.Sprintf("  Errors    : %d\n", len(issues.Errors)))
	sb.WriteString(fmt.Sprintf("  Warnings  : %d\n", len(issues.Warnings)))
	sb.WriteString("\n" + strings.Repeat("=", 72) + "\n\n")

	all := append([]string{}, issues.Errors...)
	all = append(all, issues.Warnings...)
	if len(all) == 0 {
		sb.WriteString("  No errors or warnings found in build log.\n\n")
	} else {
		sb.WriteString("  #       TAG         LINE\n")
		sb.WriteString("  ------  ----------  --------------------------------------------------\n\n")
		seq := 1
		for _, l := range issues.Errors {
			sb.WriteString(fmt.Sprintf("  %-6d  [ERROR]     %s\n", seq, stripANSI(l)))
			seq++
		}
		for _, l := range issues.Warnings {
			sb.WriteString(fmt.Sprintf("  %-6d  [WARNING]   %s\n", seq, stripANSI(l)))
			seq++
		}
	}
	sb.WriteString("\n" + strings.Repeat("=", 72) + "\n\n")

	sb.WriteString("  BUILD STEP SUMMARY\n")
	sb.WriteString("  STEP             STATUS    DURATION\n")
	sb.WriteString("  ---------------  --------  ----------\n")
	var total float64
	for _, r := range results {
		status := "PASS"
		if !r.Success {
			status = "FAIL"
		}
		sb.WriteString(fmt.Sprintf("  %-15s  %-8s  %.2fs\n", r.Step, status, r.Duration))
		if !r.Success && r.Error != "" {
			sb.WriteString(fmt.Sprintf("                  Error: %s\n", r.Error))
		}
		total += r.Duration
	}
	sb.WriteString(fmt.Sprintf("\n  Total build time: %.2fs\n", total))
	sb.WriteString("\n" + strings.Repeat("=", 72) + "\n")

	_ = os.WriteFile(dest, []byte(sb.String()), 0o644)
	return len(issues.Errors), len(issues.Warnings)
}

// stripANSI removes ANSI escape sequences.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == 0x1b {
			inEscape = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// ── Config table + checklist (static lipgloss render) ────────────────────────

func printConfigTable(cfg *config.BuildConfig) {
	val := func(v string) string {
		if strings.TrimSpace(v) == "" {
			return styleDim.Render("‹not set›")
		}
		return v
	}

	jobs := styleDim.Render("auto (cpu count)")
	if cfg.Jobs > 0 {
		jobs = fmt.Sprintf("%d", cfg.Jobs)
	}
	lto := styleDim.Render("disabled")
	if cfg.LTO != "" {
		lto = styleSecondary.Render(cfg.LTO)
	}
	cc := cfg.Toolchain.CC
	ccacheStatus := styleDim.Render("disabled")
	ccacheDirRow := ""
	if cfg.Ccache.Enabled {
		cc = styleOK.Render("ccache") + "  " + cfg.Toolchain.CC
		comp := "off"
		if cfg.Ccache.Compress {
			comp = "on"
		}
		ccacheStatus = fmt.Sprintf("%s  max %s  ·  compress %s",
			styleOK.Render("⚡ enabled"), cfg.Ccache.MaxSize, comp)
		dir := cfg.Ccache.Dir
		if dir == "" {
			dir = styleDim.Render("default  (~/.cache/ccache)")
		}
		ccacheDirRow = fmt.Sprintf("\n  %-16s  %s", styleSteel.Render("↳ ccache dir"), dir)
	}
	localver := cfg.Localversion
	if localver == "" {
		localver = styleDim.Render("‹none›")
	}
	devices := strings.Join(cfg.Anykernel3.DeviceNames, ", ")
	if devices == "" {
		devices = styleDim.Render("‹all devices›")
	}
	slot := styleDim.Render("no")
	if cfg.Anykernel3.IsSlotDevice != 0 {
		slot = styleOK.Render("yes")
	}
	ak3Source := cfg.Anykernel3.Source
	if ak3Source == "" {
		ak3Source = config.AK3SourceOsm0sis
	}
	if repoURL := strings.TrimSpace(cfg.Anykernel3.RepoURL); repoURL != "" {
		ak3Source += styleDim.Render("  ←  " + repoURL)
	}

	rows := []struct{ k, v string }{
		{"Kernel source", val(cfg.KernelSource)},
		{"Defconfig", styleSecondary.Render(cfg.KernelDefconfig)},
		{"Arch / Sub-arch", styleSecondary.Render(cfg.Arch) + " / " + cfg.Subarch},
		{"Output dir", cfg.OutputDir},
		{"Jobs", jobs},
		{"LTO", lto},
		{"Build user", cfg.KBUILDBuildUser},
		{"Build host", cfg.KBUILDBuildHost},
		{"Localversion", localver},
		{"Toolchain preset", styleSecondary.Render(cfg.Toolchain.Preset)},
		{"CC", cc},
		{"Cross compile", cfg.Toolchain.CrossCompile},
		{"ccache", ccacheStatus},
		{"AK3 kernel name", styleSecondary.Render(cfg.Anykernel3.KernelName)},
		{"AK3 source", styleSecondary.Render(ak3Source)},
		{"AK3 block", cfg.Anykernel3.Block},
		{"AK3 devices", devices},
		{"AK3 slot device", slot},
	}

	var sb strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#ff7c00")).Padding(0, 1).
		Render("  ◆  BUILD CONFIGURATION  ◆  ")
	sb.WriteString(title + "\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n")
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("  %-16s  %s\n", styleSteel.Render(r.k), r.v))
	}
	if ccacheDirRow != "" {
		sb.WriteString(ccacheDirRow + "\n")
	}
	fmt.Println(boxPrimary.Render(sb.String()))
}

func printChecklist(hasSource, hasToolchain, hasCcache, ccacheEnabled, clean, doPackage bool) {
	item := func(label string, active bool, note string, isWarn bool) string {
		var mark, lbl string
		switch {
		case active:
			mark = styleOK.Bold(true).Render("  ✦")
			lbl = lipgloss.NewStyle().Bold(true).Render(label)
		case isWarn:
			mark = styleWarn.Bold(true).Render("  ▲")
			lbl = styleWarn.Render(label)
		default:
			mark = styleDim.Render("  ○")
			lbl = styleDim.Render(label)
		}
		suffix := ""
		if note != "" {
			suffix = "   " + styleDim.Render(note)
		}
		return mark + "  " + lbl + suffix
	}

	srcNote := "path not verified"
	if hasSource {
		srcNote = "path verified"
	}
	ccacheActive := ccacheEnabled && hasCcache
	ccacheWarn := ccacheEnabled && !hasCcache
	ccacheNote := "disabled"
	if ccacheActive {
		ccacheNote = "60–90% faster rebuilds"
	} else if ccacheWarn {
		ccacheNote = "not found in PATH"
	}

	lines := []string{
		item("Kernel source", hasSource, srcNote, !hasSource),
		item("Toolchain", hasToolchain, "configured", false),
		item("mrproper  (clean)", clean, map[bool]string{true: "full clean", false: "skipped  (--no-clean)"}[clean], false),
		item("defconfig", true, "always", false),
		item("compile", true, "always", false),
		item("ccache", ccacheActive, ccacheNote, ccacheWarn),
		item("AnyKernel3 package", doPackage, map[bool]string{true: "produces flashable ZIP", false: "skipped  (--no-package)"}[doPackage], false),
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#ff7c00")).Padding(0, 1).
		Render("  ◆  PRE-BUILD CHECKLIST  ◆  ")
	fmt.Println(boxPrimary.Render(title + "\n\n" + strings.Join(lines, "\n")))
}

// confirmDefault prompts y/N on stdin (used outside the TUI for warnings).
func confirmDefault(prompt string, def bool) bool {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Printf("  %s %s [%s]: ", styleSecondary.Render(prompt), "", hint)
	var line string
	_, _ = fmt.Scanln(&line)
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return line == "y" || line == "yes"
}

// ── config command ───────────────────────────────────────────────────────────

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Create a build config interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := wizard.Run()
			return err
		},
	}
}

// ── info command ─────────────────────────────────────────────────────────────

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info CONFIG_FILE",
		Short: "Show details from a build config file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(args[0])
			if err != nil {
				return err
			}
			fmt.Println()
			printConfigTable(cfg)
			fmt.Println()
			return nil
		},
	}
}

// ── setup-toolchain command ──────────────────────────────────────────────────

func newSetupToolchainCmd() *cobra.Command {
	var preset, version, toolchainDir string
	var installCross bool

	cmd := &cobra.Command{
		Use:   "setup-toolchain",
		Short: "Download AOSP Clang and verify cross-compilers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupToolchain(cmd.Context(), preset, version, toolchainDir, installCross)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&preset, "preset", "aosp-clang", "Toolchain preset: aosp-clang or system-clang")
	flags.StringVar(&version, "version", "r584948b", "AOSP Clang revision")
	flags.StringVar(&toolchainDir, "toolchain-dir", "", fmt.Sprintf("Storage directory (default: %s)", toolchain.DefaultToolchainBase()))
	flags.BoolVar(&installCross, "install-cross-compilers", false, "Auto-install missing cross-compiler packages")
	return cmd
}

func runSetupToolchain(ctx context.Context, preset, version, toolchainDir string, installCross bool) error {
	fmt.Println(stylePrimary.Render("  ━━━  ⚙  TOOLCHAIN SETUP  ⚙  ━━━  "))
	fmt.Println()

	cfg := config.New()
	cfg.Toolchain = config.NewToolchainConfig()
	if err := cfg.Toolchain.ApplyPreset(preset); err != nil {
		return err
	}
	cfg.Toolchain.AutoClone = true
	cfg.AutoSetupToolchain = true
	cfg.Toolchain.AOSPClangVersion = version

	var base string
	if toolchainDir != "" {
		base = toolchainDir
	}

	cb := func(line string) { info(line) }
	if _, err := toolchain.AutoSetupToolchain(ctx, cfg, base, installCross, cb); err != nil {
		fmt.Println(boxErr.Render(styleErr.Bold(true).Render("  ✗  Setup Failed  ") + "\n\n  " + styleErr.Render(err.Error())))
		return err
	}

	clang := toolchain.ToolchainClangPath(cfg)
	if clang == "" {
		errLine("Clang binary not found after setup.")
		return fmt.Errorf("clang binary not found after setup")
	}
	okLine(fmt.Sprintf("Clang ready  %s", styleSecondary.Render(clang)))

	if len(cfg.Toolchain.ExtraPath) > 0 {
		ep := cfg.Toolchain.ExtraPath[0]
		okLine(fmt.Sprintf("extra_path  %s", styleSecondary.Render(ep)))
		fmt.Println()
		fmt.Println(boxPrimary.Render(
			"  " + styleSteel.Render("Add to your build config:") + "\n" +
				"  " + styleSecondary.Render("toolchain.extra_path") + " = " + styleSecondary.Render(fmt.Sprintf("[\"%s\"]", ep))))
	}

	fmt.Println()
	fmt.Println(stylePrimary.Render("  ━━━  ⬡  CROSS-COMPILER STATUS  ⬡  ━━━  "))
	gccMap := toolchain.CheckGnuCrossCompilers(cb)
	for _, a := range toolchain.GnuPackageOrder {
		if gccMap[a] != "" {
			okLine(fmt.Sprintf("%s  %s", styleSecondary.Render(a), styleDim.Render(gccMap[a])))
		} else {
			pkg := "gcc-aarch64-linux-gnu"
			if a == "arm" {
				pkg = "gcc-arm-linux-gnueabihf"
			}
			warn(fmt.Sprintf("%s missing  —  sudo apt install %s", styleSecondary.Render(a), pkg))
		}
	}

	fmt.Println()
	fmt.Println(boxOK.Render(styleOK.Bold(true).Render("  ✦  Setup Complete  ") +
		"\n\n  Run " + lipgloss.NewStyle().Bold(true).Render("forged build --wizard") + " to start a build."))
	fmt.Println()
	return nil
}

// ── ccache-stats command ─────────────────────────────────────────────────────

func newCcacheStatsCmd() *cobra.Command {
	var dir string
	var zero, verbose bool

	cmd := &cobra.Command{
		Use:   "ccache-stats",
		Short: "Display ccache statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCcacheStats(dir, zero, verbose)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&dir, "dir", "", "ccache storage directory")
	flags.BoolVar(&zero, "zero", false, "Reset statistics after displaying")
	flags.BoolVar(&verbose, "verbose", false, "Show verbose statistics")
	return cmd
}

func runCcacheStats(dir string, zero, verbose bool) error {
	fmt.Println(stylePrimary.Render("  ━━━  ◉  CCACHE STATISTICS  ◉  ━━━  "))
	fmt.Println()

	if toolchain.Which("ccache") == "" {
		fmt.Println(boxErr.Render(styleErr.Bold(true).Render("  ✗  ccache Not Found  ") +
			"\n\n  ccache is not installed.\n  Install via:  " +
			lipgloss.NewStyle().Bold(true).Render("sudo apt install ccache")))
		return fmt.Errorf("ccache is not installed")
	}

	env := os.Environ()
	if dir != "" {
		env = append(env, "CCACHE_DIR="+dir)
		info("Using CCACHE_DIR: " + styleSecondary.Render(dir))
	}

	show := []string{"ccache", "--show-stats"}
	if verbose {
		show = []string{"ccache", "--verbose", "--show-stats"}
	}

	out, code := runCapture(show, env)
	fmt.Println(boxPrimary.Render(
		styleSecondary.Bold(true).Render("  ◉  ccache Statistics  ") + "\n\n" + strings.TrimRight(out, "\n")))
	if code != 0 {
		warn(fmt.Sprintf("ccache exited with code %d", code))
	}

	if zero {
		c := exec.Command("ccache", "--zero-stats")
		c.Env = env
		if err := c.Run(); err != nil {
			warn(fmt.Sprintf("Could not reset statistics: %v", err))
		} else {
			okLine("Statistics cleared (ccache --zero-stats).")
		}
	}
	return nil
}

func runCapture(cmd []string, env []string) (string, int) {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Env = env
	out, err := c.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	if len(out) == 0 {
		return "(no output)", code
	}
	return string(out), code
}

func init() {
	rootCmd.AddCommand(newBuildCmd())
	rootCmd.AddCommand(newSetupToolchainCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newInfoCmd())
	rootCmd.AddCommand(newCcacheStatsCmd())
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.Version = config.Version
	rootCmd.SetVersionTemplate("forged {{.Version}}\n")
}

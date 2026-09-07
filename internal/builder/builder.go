// Package builder implements the FORGED kernel build engine: make invocation
// preparation (environment + command prefix) and step orchestration.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package builder

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vxyzview/forged/internal/config"
	"github.com/vxyzview/forged/internal/toolchain"
)

// LLVM binutils: passed as explicit make variables (and env) when
// use_llvm_binutils is true, removing any dependency on GCC.
var LLVMBinutils = map[string]string{
	"LD":      "ld.lld",
	"AR":      "llvm-ar",
	"NM":      "llvm-nm",
	"OBJCOPY": "llvm-objcopy",
	"OBJDUMP": "llvm-objdump",
	"READELF": "llvm-readelf",
	"STRIP":   "llvm-strip",
}

// Ordered keys so make/env output is deterministic.
var llvmBinutilsOrder = []string{"LD", "AR", "NM", "OBJCOPY", "OBJDUMP", "READELF", "STRIP"}

// BuildStep is one make invocation (mrproper / defconfig / build).
type BuildStep struct {
	Name    string
	Command []string
	Env     map[string]string
}

// BuildResult is the outcome of executing a BuildStep.
type BuildResult struct {
	Success  bool
	Step     string
	Duration float64
	Output   string
	Error    string
}

// LineCallback receives streamed output lines.
type LineCallback func(line string)

// ProgressCallback receives progress/log lines.
type ProgressCallback func(line string)

// ResolveJobs returns requested, or the CPU count when requested <= 0.
func ResolveJobs(requested int) int {
	if requested > 0 {
		return requested
	}
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 1
}

// FindCcache returns the absolute ccache path, or "".
func FindCcache() string {
	return toolchain.Which("ccache")
}

// BuildEnv builds the subprocess environment for every make invocation.
//
// Always uses Clang as CC. When use_llvm_binutils is true, LLVM binutils are
// exported. CROSS_COMPILE_ARM32 is exported when non-empty. When ccache is
// enabled, CCACHE_* vars are exported (the "ccache" prefix on CC is injected
// separately in MakeBase). ExtraEnv is applied last so it can override any
// built-in variable.
func BuildEnv(cfg *config.BuildConfig) map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}

	tc := &cfg.Toolchain

	var expanded []string
	for _, p := range tc.ExtraPath {
		if p == "" {
			continue
		}
		expanded = append(expanded, config.ExpandPath(p))
	}
	if joined := strings.Join(expanded, ":"); joined != "" {
		if base := env["PATH"]; base != "" {
			env["PATH"] = joined + ":" + base
		} else {
			env["PATH"] = joined
		}
	}

	env["ARCH"] = cfg.Arch
	env["SUBARCH"] = cfg.Subarch
	env["CC"] = tc.CC
	env["CROSS_COMPILE"] = tc.CrossCompile
	env["CLANG_TRIPLE"] = tc.ClangTriple
	env["KBUILD_BUILD_USER"] = cfg.KBUILDBuildUser
	env["KBUILD_BUILD_HOST"] = cfg.KBUILDBuildHost

	if tc.CrossCompileArm32 != "" {
		env["CROSS_COMPILE_ARM32"] = tc.CrossCompileArm32
	}

	if tc.UseLLVMBinutils {
		for _, k := range llvmBinutilsOrder {
			env[k] = LLVMBinutils[k]
		}
	}

	if cfg.Localversion != "" {
		env["LOCALVERSION"] = cfg.Localversion
	}

	if cfg.Ccache.Enabled {
		cc := &cfg.Ccache
		if cc.Dir != "" {
			env["CCACHE_DIR"] = config.ExpandPath(cc.Dir)
		}
		env["CCACHE_MAXSIZE"] = cc.MaxSize
		env["CCACHE_SLOPPINESS"] = cc.Sloppiness
		if cc.Compress {
			env["CCACHE_COMPRESS"] = "true"
		} else {
			env["CCACHE_COMPRESS"] = "false"
		}
		rawBasedir := cc.Basedir
		if rawBasedir == "" {
			rawBasedir = cfg.KernelSource
		}
		if rawBasedir != "" {
			env["CCACHE_BASEDIR"] = config.ExpandPath(rawBasedir)
		}
	}

	for k, v := range cfg.ExtraEnv {
		env[k] = config.ExpandPath(v)
	}

	return env
}

// MakeBase constructs the common make command prefix for every build step.
//
// Always emits CC/CLANG_TRIPLE/CROSS_COMPILE/SUBARCH. When ccache is enabled,
// CC is set to "ccache <compiler>". ExtraMakeFlags are appended verbatim last.
func MakeBase(cfg *config.BuildConfig, jobs int) []string {
	tc := &cfg.Toolchain

	ccValue := tc.CC
	if cfg.Ccache.Enabled {
		ccValue = "ccache " + tc.CC
	}

	cmd := []string{
		"make",
		fmt.Sprintf("-j%d", jobs),
		"O=" + cfg.OutputDir,
		"ARCH=" + cfg.Arch,
		"SUBARCH=" + cfg.Subarch,
		"CC=" + ccValue,
		"CLANG_TRIPLE=" + tc.ClangTriple,
		"CROSS_COMPILE=" + tc.CrossCompile,
	}

	if tc.CrossCompileArm32 != "" {
		cmd = append(cmd, "CROSS_COMPILE_ARM32="+tc.CrossCompileArm32)
	}

	if tc.UseLLVMBinutils {
		for _, k := range llvmBinutilsOrder {
			cmd = append(cmd, k+"="+LLVMBinutils[k])
		}
	}

	switch cfg.LTO {
	case "thin":
		cmd = append(cmd, "LTO=thin")
	case "full":
		cmd = append(cmd, "LTO=full")
	}

	cmd = append(cmd, cfg.ExtraMakeFlags...)
	return cmd
}

// KernelBuilder orchestrates kernel build steps.
type KernelBuilder struct {
	Cfg       *config.BuildConfig
	Jobs      int
	SourceDir string
	OutputDir string
}

// New creates a KernelBuilder. When cfg.KernelSourceURL is set the kernel
// source is cloned first (mutating cfg.KernelSource in place), then the
// toolchain is auto-set-up when enabled. Pass nil progress to silence logs.
func New(ctx context.Context, cfg *config.BuildConfig, progress ProgressCallback) (*KernelBuilder, error) {
	jobs := ResolveJobs(cfg.Jobs)

	if cfg.KernelSourceURL != "" {
		dest := cfg.KernelSource
		if dest == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			dest = filepath.Join(cwd, "kernel")
		}
		var cb toolchain.Progress
		if progress != nil {
			cb = toolchain.Progress(progress)
		}
		cloned, err := toolchain.CloneKernelSource(ctx, cfg.KernelSourceURL, dest, cfg.KernelSourceBranch, cfg.KernelSourceDepth, cb)
		if err != nil {
			return nil, err
		}
		cfg.KernelSource = cloned
	}

	sourceDir, err := filepath.Abs(cfg.KernelSource)
	if err != nil {
		return nil, err
	}

	if cfg.AutoSetupToolchain && cfg.Toolchain.AutoClone {
		var cb toolchain.Progress
		if progress != nil {
			cb = toolchain.Progress(progress)
		}
		if _, err := toolchain.AutoSetupToolchain(ctx, cfg, cfg.ToolchainDir, false, cb); err != nil {
			return nil, err
		}
	}

	return &KernelBuilder{
		Cfg:       cfg,
		Jobs:      jobs,
		SourceDir: sourceDir,
		OutputDir: filepath.Join(sourceDir, cfg.OutputDir),
	}, nil
}

// ValidateToolchain returns warnings about missing toolchain components.
func (b *KernelBuilder) ValidateToolchain() []string {
	var issues []string

	if toolchain.ToolchainClangPath(b.Cfg) == "" {
		issues = append(issues,
			fmt.Sprintf("Clang binary '%s' not found in extra_path or $PATH.  Run 'forged setup-toolchain' or set toolchain.extra_path in your build config.", b.Cfg.Toolchain.CC))
	}
	if b.Cfg.Ccache.Enabled && FindCcache() == "" {
		issues = append(issues,
			"ccache is enabled but the 'ccache' binary was not found in $PATH.  Install it with:  sudo apt install ccache\nAlternatively, disable ccache in your build config.")
	}
	return issues
}

// Steps returns the ordered build steps (mrproper optionally, then defconfig
// and build).
func (b *KernelBuilder) Steps(clean bool) []BuildStep {
	env := BuildEnv(b.Cfg)
	var steps []BuildStep
	if clean {
		steps = append(steps, BuildStep{Name: "mrproper", Command: append(MakeBase(b.Cfg, b.Jobs), "mrproper"), Env: env})
	}
	steps = append(steps,
		BuildStep{Name: "defconfig", Command: append(MakeBase(b.Cfg, b.Jobs), b.Cfg.KernelDefconfig), Env: env},
		BuildStep{Name: "build", Command: MakeBase(b.Cfg, b.Jobs), Env: env},
	)
	return steps
}

// RunStep executes one build step, streaming stdout+stderr line by line.
func (b *KernelBuilder) RunStep(ctx context.Context, step BuildStep, cb LineCallback) BuildResult {
	start := time.Now()
	var combined []string

	envList := envMapToList(step.Env)

	c := exec.CommandContext(ctx, step.Command[0], step.Command[1:]...)
	c.Dir = b.SourceDir
	c.Env = envList

	stdout, err := c.StdoutPipe()
	if err != nil {
		return BuildResult{Success: false, Step: step.Name, Duration: time.Since(start).Seconds(), Error: err.Error()}
	}
	c.Stderr = c.Stdout

	if err := c.Start(); err != nil {
		msg := err.Error()
		// exec.Error for missing binary reports "executable file not found".
		return BuildResult{Success: false, Step: step.Name, Duration: time.Since(start).Seconds(), Error: msg}
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		combined = append(combined, line)
		if cb != nil {
			cb(line)
		}
	}
	waitErr := c.Wait()
	duration := time.Since(start).Seconds()
	output := strings.Join(combined, "\n")

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return BuildResult{Success: false, Step: step.Name, Duration: duration, Output: output, Error: fmt.Sprintf("Exit code %d", exitErr.ExitCode())}
		}
		return BuildResult{Success: false, Step: step.Name, Duration: duration, Output: output, Error: waitErr.Error()}
	}
	return BuildResult{Success: true, Step: step.Name, Duration: duration, Output: output}
}

// searchNames is the kernel image search priority.
var searchNames = []string{
	"Image.gz-dtb",
	"Image-dtb",
	"Image.gz",
	"Image",
	"zImage-dtb",
	"zImage",
	"dtb.img",
}

// FindKernelImage locates the compiled kernel image, or "" when absent.
func (b *KernelBuilder) FindKernelImage() string {
	for _, name := range searchNames {
		candidate := filepath.Join(b.OutputDir, "arch", b.Cfg.Arch, "boot", name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return ""
}

// FindDTBFiles collects unique .dtb files from the output tree.
func (b *KernelBuilder) FindDTBFiles() []string {
	bootDir := filepath.Join(b.OutputDir, "arch", b.Cfg.Arch, "boot")
	seen := map[string]bool{}
	var dtbs []string

	// Recursive dts/**/*.dtb first, then top-level *.dtb — deduplicated.
	var walk func(dir string, top bool)
	walk = func(dir string, top bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if dir == bootDir {
					walk(p, false)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".dtb") {
				continue
			}
			if top && dir != bootDir {
				continue
			}
			// Only include top-level hits on the first pass; recursive pass
			// covers nested dirs.
			if !top && dir == bootDir {
				continue
			}
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				resolved = p
			}
			if !seen[resolved] {
				seen[resolved] = true
				dtbs = append(dtbs, p)
			}
		}
	}
	// Recursive walk skipping top-level files:
	var recWalk func(dir string, isTop bool)
	recWalk = func(dir string, isTop bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				recWalk(p, false)
				continue
			}
			if isTop {
				continue
			}
			if !strings.HasSuffix(e.Name(), ".dtb") {
				continue
			}
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				resolved = p
			}
			if !seen[resolved] {
				seen[resolved] = true
				dtbs = append(dtbs, p)
			}
		}
	}
	recWalk(bootDir, true)
	// Top-level *.dtb:
	if entries, err := os.ReadDir(bootDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".dtb") {
				continue
			}
			p := filepath.Join(bootDir, e.Name())
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				resolved = p
			}
			if !seen[resolved] {
				seen[resolved] = true
				dtbs = append(dtbs, p)
			}
		}
	}
	_ = walk
	return dtbs
}

// FindModules collects compiled kernel modules (.ko files).
func (b *KernelBuilder) FindModules() []string {
	var mods []string
	_ = filepath.Walk(b.OutputDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ko") {
			mods = append(mods, p)
		}
		return nil
	})
	if mods == nil {
		return []string{}
	}
	return mods
}

func envMapToList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vxyzview/forged/internal/config"
)

func testCfg(t *testing.T) *config.BuildConfig {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return config.NewWithSource(cwd)
}

func TestResolveJobs(t *testing.T) {
	if ResolveJobs(0) < 1 {
		t.Error("auto jobs must be >= 1")
	}
	if ResolveJobs(4) != 4 {
		t.Error("explicit jobs must be respected")
	}
}

func TestBuildEnvSetsArch(t *testing.T) {
	cfg := testCfg(t)
	cfg.Arch = "arm64"
	env := BuildEnv(cfg)
	if env["ARCH"] != "arm64" {
		t.Errorf("ARCH = %q", env["ARCH"])
	}
}

func TestBuildEnvSetsCC(t *testing.T) {
	cfg := testCfg(t)
	env := BuildEnv(cfg)
	if env["CC"] != "clang" {
		t.Errorf("CC = %q", env["CC"])
	}
	if env["CLANG_TRIPLE"] != "aarch64-linux-gnu-" {
		t.Errorf("CLANG_TRIPLE = %q", env["CLANG_TRIPLE"])
	}
}

func TestBuildEnvLLVMBinutils(t *testing.T) {
	cfg := testCfg(t)
	cfg.Toolchain.UseLLVMBinutils = true
	env := BuildEnv(cfg)
	for _, k := range []string{"LD", "AR", "NM", "OBJCOPY", "OBJDUMP", "READELF", "STRIP"} {
		if env[k] == "" {
			t.Errorf("%s missing from env", k)
		}
	}
	if env["LD"] != "ld.lld" || env["AR"] != "llvm-ar" {
		t.Errorf("llvm binutils values wrong: LD=%q AR=%q", env["LD"], env["AR"])
	}

	cfg.Toolchain.UseLLVMBinutils = false
	env = BuildEnv(cfg)
	if env["LD"] != "" || env["AR"] != "" {
		t.Error("llvm binutils must be absent when disabled")
	}
}

func TestBuildEnvExtraPath(t *testing.T) {
	cfg := testCfg(t)
	cfg.Toolchain.ExtraPath = []string{"/opt/clang-r584948b/bin"}
	env := BuildEnv(cfg)
	if len(env["PATH"]) < len("/opt/clang-r584948b/bin") || env["PATH"][:len("/opt/clang-r584948b/bin")] != "/opt/clang-r584948b/bin" {
		t.Errorf("PATH should start with extra_path: %q", env["PATH"])
	}
}

func TestBuildEnvExtraPathTilde(t *testing.T) {
	cfg := testCfg(t)
	cfg.Toolchain.ExtraPath = []string{"~/toolchains/clang/bin"}
	env := BuildEnv(cfg)
	first := env["PATH"]
	if i := indexByteStr(first, ':'); i >= 0 {
		first = first[:i]
	}
	if containsStr(first, "~") {
		t.Errorf("tilde must be expanded: %q", first)
	}
}

func TestMakeBaseCC(t *testing.T) {
	cfg := testCfg(t)
	cmd := MakeBase(cfg, 4)
	if !containsStrSlice(cmd, "CC=clang") {
		t.Errorf("CC=clang missing: %v", cmd)
	}
	hasTriple := false
	for _, a := range cmd {
		if len(a) > len("CLANG_TRIPLE=") && a[:len("CLANG_TRIPLE=")] == "CLANG_TRIPLE=" {
			hasTriple = true
		}
	}
	if !hasTriple {
		t.Error("CLANG_TRIPLE missing")
	}
	if !containsStrSlice(cmd, "SUBARCH=arm64") {
		t.Error("SUBARCH missing")
	}
}

func TestMakeBaseLLVMBinutils(t *testing.T) {
	cfg := testCfg(t)
	cfg.Toolchain.UseLLVMBinutils = true
	cmd := MakeBase(cfg, 4)
	if !containsStrSlice(cmd, "LD=ld.lld", "AR=llvm-ar", "STRIP=llvm-strip") {
		t.Errorf("llvm binutils flags missing: %v", cmd)
	}
	cfg.Toolchain.UseLLVMBinutils = false
	cmd = MakeBase(cfg, 4)
	if containsStrSlice(cmd, "LD=ld.lld") {
		t.Error("LD=ld.lld must be absent when disabled")
	}
}

func TestMakeBaseLTO(t *testing.T) {
	cfg := testCfg(t)
	cfg.LTO = "thin"
	if !containsStrSlice(MakeBase(cfg, 4), "LTO=thin") {
		t.Error("LTO=thin missing")
	}
	cfg.LTO = "full"
	if !containsStrSlice(MakeBase(cfg, 4), "LTO=full") {
		t.Error("LTO=full missing")
	}
	cfg.LTO = ""
	cmd := MakeBase(cfg, 4)
	if containsStrSlice(cmd, "LTO=thin", "LTO=full") {
		t.Error("no LTO flag expected by default")
	}
}

func TestMakeBaseExtraFlagsLast(t *testing.T) {
	cfg := testCfg(t)
	cfg.ExtraMakeFlags = []string{"CC=my-special-clang"}
	cmd := MakeBase(cfg, 4)
	last := -1
	for i, a := range cmd {
		if len(a) > 3 && a[:3] == "CC=" {
			last = i
		}
	}
	if cmd[last] != "CC=my-special-clang" {
		t.Errorf("user CC flag must come last: %v", cmd)
	}
}

func TestSteps(t *testing.T) {
	cfg := testCfg(t)
	b := &KernelBuilder{Cfg: cfg, Jobs: 4, SourceDir: cfg.KernelSource}

	names := []string{}
	for _, s := range b.Steps(true) {
		names = append(names, s.Name)
	}
	want := []string{"mrproper", "defconfig", "build"}
	if len(names) != len(want) {
		t.Fatalf("steps = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("steps = %v, want %v", names, want)
		}
	}

	names = nil
	for _, s := range b.Steps(false) {
		names = append(names, s.Name)
	}
	if len(names) != 2 || names[0] != "defconfig" || names[1] != "build" {
		t.Errorf("steps without clean = %v", names)
	}

	for _, s := range b.Steps(true) {
		if !containsStrSlice(s.Command, "CC=clang") {
			t.Errorf("step %q missing CC=clang", s.Name)
		}
		if !containsStrSlice(s.Command, "LD=ld.lld") {
			t.Errorf("step %q missing LD=ld.lld", s.Name)
		}
	}
}

func TestFindKernelImage(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "out", "arch", "arm64", "boot")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.NewWithSource(tmp)
	b := &KernelBuilder{Cfg: cfg, SourceDir: tmp, OutputDir: filepath.Join(tmp, "out")}
	if b.FindKernelImage() != "" {
		t.Error("no image expected in empty tree")
	}

	if err := os.WriteFile(filepath.Join(out, "Image.gz-dtb"), make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	found := b.FindKernelImage()
	if found == "" || filepath.Base(found) != "Image.gz-dtb" {
		t.Errorf("found = %q, want Image.gz-dtb", found)
	}
}

func TestFindModules(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.NewWithSource(tmp)
	b := &KernelBuilder{Cfg: cfg, SourceDir: tmp, OutputDir: filepath.Join(tmp, "out")}
	if len(b.FindModules()) != 0 {
		t.Error("no modules expected in empty tree")
	}
}

func TestFindDTBFilesNoDuplicates(t *testing.T) {
	tmp := t.TempDir()
	boot := filepath.Join(tmp, "out", "arch", "arm64", "boot")
	if err := os.MkdirAll(boot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(boot, "device.dtb"), []byte{0xd0, 0x0d}, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.NewWithSource(tmp)
	b := &KernelBuilder{Cfg: cfg, SourceDir: tmp, OutputDir: filepath.Join(tmp, "out")}
	found := b.FindDTBFiles()
	if len(found) != 1 {
		t.Errorf("dtbs = %v, want exactly 1", found)
	}
}

func TestRunStepCommandNotFound(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.NewWithSource(tmp)
	b := &KernelBuilder{Cfg: cfg, SourceDir: tmp}
	result := b.RunStep(t.Context(), BuildStep{Name: "test", Command: []string{"nonexistent_binary_xyz_1234"}}, nil)
	if result.Success {
		t.Error("expected failure for missing binary")
	}
	if result.Error == "" {
		t.Error("expected an error message")
	}
}

func TestRunStepSuccess(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.NewWithSource(tmp)
	b := &KernelBuilder{Cfg: cfg, SourceDir: tmp}
	var lines []string
	result := b.RunStep(t.Context(), BuildStep{Name: "echo", Command: []string{"echo", "hello"}}, func(l string) { lines = append(lines, l) })
	if !result.Success {
		t.Fatalf("echo should succeed: %v", result.Error)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Errorf("lines = %v", lines)
	}
}

func TestCcacheEnvInjection(t *testing.T) {
	cfg := testCfg(t)
	cfg.Ccache.Enabled = true
	cfg.Ccache.MaxSize = "3G"
	cfg.Ccache.Compress = true
	env := BuildEnv(cfg)
	if env["CCACHE_MAXSIZE"] != "3G" {
		t.Errorf("CCACHE_MAXSIZE = %q", env["CCACHE_MAXSIZE"])
	}
	if env["CCACHE_COMPRESS"] != "true" {
		t.Errorf("CCACHE_COMPRESS = %q", env["CCACHE_COMPRESS"])
	}
	if !containsStr(env["CCACHE_SLOPPINESS"], "time_macros") {
		t.Errorf("CCACHE_SLOPPINESS = %q", env["CCACHE_SLOPPINESS"])
	}

	// Disabled → no CCACHE vars at all.
	cfg.Ccache.Enabled = false
	env = BuildEnv(cfg)
	if _, ok := env["CCACHE_MAXSIZE"]; ok {
		t.Error("CCACHE_MAXSIZE must be absent when disabled")
	}
}

func TestCcacheMakeBasePrefix(t *testing.T) {
	cfg := testCfg(t)
	cfg.Ccache.Enabled = true
	if !containsStrSlice(MakeBase(cfg, 4), "CC=ccache clang") {
		t.Error("CC should be prefixed with ccache")
	}
	cfg.Ccache.Enabled = false
	if containsStrSlice(MakeBase(cfg, 4), "CC=ccache clang") {
		t.Error("CC must not be prefixed when disabled")
	}
}

func TestCrossCompileArm32(t *testing.T) {
	cfg := testCfg(t)
	cfg.Toolchain.CrossCompileArm32 = "arm-linux-gnueabihf-"
	env := BuildEnv(cfg)
	if env["CROSS_COMPILE_ARM32"] != "arm-linux-gnueabihf-" {
		t.Errorf("CROSS_COMPILE_ARM32 = %q", env["CROSS_COMPILE_ARM32"])
	}
	if _, ok := env["CROSS_COMPILE_COMPAT"]; ok {
		t.Error("CROSS_COMPILE_COMPAT must never be emitted")
	}

	cfg.Toolchain.CrossCompileArm32 = ""
	env = BuildEnv(cfg)
	if _, ok := env["CROSS_COMPILE_ARM32"]; ok {
		t.Error("CROSS_COMPILE_ARM32 must be omitted when empty")
	}

	cmd := MakeBase(cfg, 4)
	for _, a := range cmd {
		if len(a) > len("CROSS_COMPILE_ARM32=") && a[:len("CROSS_COMPILE_ARM32=")] == "CROSS_COMPILE_ARM32=" {
			t.Errorf("CROSS_COMPILE_ARM32 must not be in make command when empty: %v", cmd)
			break
		}
	}
}

func TestExtraEnvOverride(t *testing.T) {
	cfg := testCfg(t)
	cfg.ExtraEnv = map[string]string{"ARCH": "x86_64"}
	env := BuildEnv(cfg)
	if env["ARCH"] != "x86_64" {
		t.Error("extra_env must override built-ins")
	}
}

func TestExtraMakeFlagsInCommand(t *testing.T) {
	cfg := testCfg(t)
	cfg.ExtraMakeFlags = []string{"LLVM=1", "LLVM_IAS=1"}
	cmd := MakeBase(cfg, 4)
	if !containsStrSlice(cmd, "LLVM=1", "LLVM_IAS=1") {
		t.Errorf("extra flags missing: %v", cmd)
	}
}

// small helpers
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func indexByteStr(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func containsStrSlice(list []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, item := range list {
			if item == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

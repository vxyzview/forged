package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTmp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultConstruction(t *testing.T) {
	cfg := New()
	if cfg.Arch != "arm64" {
		t.Errorf("arch = %q, want arm64", cfg.Arch)
	}
	if cfg.Jobs != 0 {
		t.Errorf("jobs = %d, want 0", cfg.Jobs)
	}
	if cfg.LTO != "" {
		t.Errorf("lto = %q, want empty", cfg.LTO)
	}
}

func TestDefaultToolchainIsClang(t *testing.T) {
	cfg := New()
	if cfg.Toolchain.CC != "clang" {
		t.Errorf("cc = %q, want clang", cfg.Toolchain.CC)
	}
	if !cfg.Toolchain.UseLLVMBinutils {
		t.Error("use_llvm_binutils = false, want true")
	}
}

func TestApplyPresetSystemClang(t *testing.T) {
	var tc ToolchainConfig
	if err := tc.ApplyPreset("system-clang"); err != nil {
		t.Fatal(err)
	}
	if tc.CC != "clang" {
		t.Errorf("cc = %q, want clang", tc.CC)
	}
	if !tc.UseLLVMBinutils {
		t.Error("use_llvm_binutils = false, want true")
	}
	if !contains(tc.CrossCompile, "aarch64") {
		t.Errorf("cross_compile = %q, want aarch64 prefix", tc.CrossCompile)
	}
}

func TestApplyPresetAOSPClang(t *testing.T) {
	var tc ToolchainConfig
	if err := tc.ApplyPreset("aosp-clang"); err != nil {
		t.Fatal(err)
	}
	if tc.ClangTriple != "aarch64-linux-gnu-" {
		t.Errorf("clang_triple = %q", tc.ClangTriple)
	}
}

func TestAllPresetsUseClang(t *testing.T) {
	for name, p := range ToolchainPresets {
		if p.CC != "clang" {
			t.Errorf("preset %q must use cc=clang", name)
		}
		if !p.UseLLVMBinutils {
			t.Errorf("preset %q must set use_llvm_binutils=true", name)
		}
	}
}

func TestDeprecatedPresetMigrates(t *testing.T) {
	var tc ToolchainConfig
	if err := tc.ApplyPreset("aosp-clang-r547379"); err != nil {
		t.Fatal(err)
	}
	if tc.Preset != "aosp-clang" {
		t.Errorf("preset = %q, want migrated aosp-clang", tc.Preset)
	}
}

func TestDeprecatedAliasesMapToValidPresets(t *testing.T) {
	for old, new := range DeprecatedPresetAliases {
		if _, ok := ToolchainPresets[new]; !ok {
			t.Errorf("deprecated alias %q → %q but %q is not a preset", old, new, new)
		}
	}
}

func TestUnknownPresetRaises(t *testing.T) {
	var tc ToolchainConfig
	if err := tc.ApplyPreset("gcc-aarch64"); err == nil {
		t.Error("expected error for unknown preset")
	}
}

func TestLTOValidation(t *testing.T) {
	cfg := New()
	cfg.LTO = "thin"
	if err := cfg.Validate(); err != nil {
		t.Errorf("thin should be valid: %v", err)
	}
	cfg.LTO = "full"
	if err := cfg.Validate(); err != nil {
		t.Errorf("full should be valid: %v", err)
	}
	cfg.LTO = "fast"
	if err := cfg.Validate(); err == nil {
		t.Error("fast should be invalid")
	}
}

func TestKernelSourceDepthValidation(t *testing.T) {
	cfg := New()
	cfg.KernelSourceDepth = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("depth 0 (full history) should be valid: %v", err)
	}
	cfg.KernelSourceDepth = 5
	if err := cfg.Validate(); err != nil {
		t.Errorf("depth 5 should be valid: %v", err)
	}
	cfg.KernelSourceDepth = -1
	if err := cfg.Validate(); err == nil {
		t.Error("negative depth should be invalid")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	cfg := New()
	cfg.KernelSource = "/src/kernel"
	cfg.KernelDefconfig = "pixel_defconfig"
	cfg.LTO = "thin"
	cfg.Toolchain.AOSPClangVersion = "r522817"
	// Apply the preset BEFORE mutating fields so values stick.
	cfg.Toolchain.ApplyPreset("aosp-clang")
	cfg.Toolchain.UseLLVMBinutils = false

	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := cfg.ToJSON(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := FromJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.KernelSource != "/src/kernel" {
		t.Errorf("kernel_source = %q", loaded.KernelSource)
	}
	if loaded.KernelDefconfig != "pixel_defconfig" {
		t.Errorf("kernel_defconfig = %q", loaded.KernelDefconfig)
	}
	if loaded.LTO != "thin" {
		t.Errorf("lto = %q", loaded.LTO)
	}
	if loaded.Toolchain.UseLLVMBinutils {
		t.Error("use_llvm_binutils should be false")
	}
	if loaded.Toolchain.Preset != "aosp-clang" {
		t.Errorf("preset = %q", loaded.Toolchain.Preset)
	}
	if loaded.Toolchain.AOSPClangVersion != "r522817" {
		t.Errorf("aosp_clang_version = %q", loaded.Toolchain.AOSPClangVersion)
	}
}

func TestLoadUnsupportedExtension(t *testing.T) {
	if _, err := Load("/tmp/config.yaml"); err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestFromJSONToleratesUnknownKeys(t *testing.T) {
	p := writeTmp(t, "future.json", `{
		"kernel_source": "/src",
		"future_field": "new",
		"toolchain": {"preset": "aosp-clang", "new_tc_option": true},
		"anykernel3": {"new_ak3_option": "v"},
		"ccache": {"enabled": true, "max_size": "8G", "future_cc": 42}
	}`)
	cfg, err := FromJSON(p)
	if err != nil {
		t.Fatalf("unknown keys must not crash loading: %v", err)
	}
	if cfg.KernelSource != "/src" {
		t.Errorf("kernel_source = %q", cfg.KernelSource)
	}
	if cfg.Toolchain.Preset != "aosp-clang" {
		t.Errorf("preset = %q", cfg.Toolchain.Preset)
	}
	if !cfg.Ccache.Enabled || cfg.Ccache.MaxSize != "8G" {
		t.Errorf("ccache = %+v", cfg.Ccache)
	}
}

func TestLegacyJSONWithoutCcacheSection(t *testing.T) {
	p := writeTmp(t, "legacy.json", `{
		"kernel_source": "/src",
		"toolchain": {"preset": "system-clang", "use_llvm_binutils": true},
		"anykernel3": {}
	}`)
	cfg, err := FromJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ccache.Enabled {
		t.Error("ccache should default to disabled")
	}
	if cfg.Ccache.MaxSize != "5G" {
		t.Errorf("ccache max_size = %q, want 5G", cfg.Ccache.MaxSize)
	}
}

func TestDeprecatedPresetInJSONMigratedOnLoad(t *testing.T) {
	p := writeTmp(t, "old.json", `{
		"kernel_source": "/src",
		"toolchain": {"preset": "aosp-clang-r584948b"}
	}`)
	cfg, err := FromJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Toolchain.Preset != "aosp-clang" {
		t.Errorf("preset = %q, want migrated aosp-clang", cfg.Toolchain.Preset)
	}
}

// TestToolchainUnknownFieldsPreservePreset verifies the important behaviour
// that a raw preset name in JSON (deprecated or not) is preserved by
// unmarshalling — migration happens lazily via ApplyPreset consumers.
func TestToolchainUnknownFieldsPreservePreset(t *testing.T) {
	p := writeTmp(t, "preset.json", `{"toolchain": {"preset": "system-clang"}}`)
	cfg, err := FromJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Toolchain.Preset != "system-clang" {
		t.Errorf("preset = %q, want system-clang", cfg.Toolchain.Preset)
	}
}

func TestTOMLLoad(t *testing.T) {
	p := writeTmp(t, "config.toml", `
kernel_source = "/src/kernel"
kernel_defconfig = "vendor/test_defconfig"
arch = "arm64"
jobs = 8
lto = "thin"

[toolchain]
preset = "aosp-clang"
aosp_clang_version = "r584948b"

[ccache]
enabled = true
max_size = "10G"

[anykernel3]
kernel_name = "TestKernel"
device_names = ["device_a", "device_b"]
`)
	cfg, err := FromTOML(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KernelSource != "/src/kernel" {
		t.Errorf("kernel_source = %q", cfg.KernelSource)
	}
	if cfg.Jobs != 8 {
		t.Errorf("jobs = %d", cfg.Jobs)
	}
	if cfg.LTO != "thin" {
		t.Errorf("lto = %q", cfg.LTO)
	}
	if !cfg.Ccache.Enabled || cfg.Ccache.MaxSize != "10G" {
		t.Errorf("ccache = %+v", cfg.Ccache)
	}
	if cfg.Anykernel3.KernelName != "TestKernel" {
		t.Errorf("kernel_name = %q", cfg.Anykernel3.KernelName)
	}
	if len(cfg.Anykernel3.DeviceNames) != 2 {
		t.Errorf("device_names = %v", cfg.Anykernel3.DeviceNames)
	}
}

func TestTOMLToleratesUnknownKeys(t *testing.T) {
	p := writeTmp(t, "future.toml", `
kernel_source = "/src"
future_toml_key = 1
[toolchain]
preset = "system-clang"
new_key = "x"
`)
	cfg, err := FromTOML(p)
	if err != nil {
		t.Fatalf("unknown TOML keys must not crash loading: %v", err)
	}
	if cfg.Toolchain.Preset != "system-clang" {
		t.Errorf("preset = %q", cfg.Toolchain.Preset)
	}
}

func TestExampleJSONLoads(t *testing.T) {
	p := "../../config/example_build_config.json"
	if _, err := os.Stat(p); err == nil {
		cfg, err := FromJSON(p)
		if err != nil {
			t.Fatalf("example JSON must load: %v", err)
		}
		if cfg.KernelSource == "" {
			t.Error("example JSON kernel_source must not be empty")
		}
	}
}

func TestExampleTOMLLoads(t *testing.T) {
	p := "../../config/example_build_config.toml"
	if _, err := os.Stat(p); err == nil {
		cfg, err := FromTOML(p)
		if err != nil {
			t.Fatalf("example TOML must load: %v", err)
		}
		if cfg.KernelDefconfig == "" {
			t.Error("example TOML kernel_defconfig must not be empty")
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// Package config implements FORGED build configuration: structs plus
// JSON/TOML round-trip with forward compatibility (unknown keys are ignored).
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Version is the FORGED release version.
const Version = "1.0.0"

// Valid LTO modes.
var validLTOModes = map[string]bool{"thin": true, "full": true}

// ToolchainPresets holds the Clang-only toolchain presets.
//
// Each preset supplies the compiler, cross-compile prefixes and whether the
// toolchain can be auto-downloaded (auto_clone).
var ToolchainPresets = map[string]ToolchainPreset{
	"system-clang": {
		CC:                "clang",
		CrossCompile:      "aarch64-linux-gnu-",
		CrossCompileArm32: "arm-linux-gnueabihf-",
		ClangTriple:       "aarch64-linux-gnu-",
		UseLLVMBinutils:   true,
		AutoClone:         false,
	},
	// Generic AOSP prebuilt Clang — version selected via AOSPClangVersion.
	"aosp-clang": {
		CC:                "clang",
		CrossCompile:      "aarch64-linux-gnu-",
		CrossCompileArm32: "arm-linux-gnueabihf-",
		ClangTriple:       "aarch64-linux-gnu-",
		UseLLVMBinutils:   true,
		AutoClone:         true,
	},
}

// PresetOrder lists preset names in display order.
var PresetOrder = []string{"aosp-clang", "system-clang"}

// DeprecatedPresetAliases maps old preset names to their replacements.
var DeprecatedPresetAliases = map[string]string{
	"aosp-clang-r547379":  "aosp-clang",
	"aosp-clang-r584948b": "aosp-clang",
}

// ToolchainPreset is the raw preset definition.
type ToolchainPreset struct {
	CC                string
	CrossCompile      string
	CrossCompileArm32 string
	ClangTriple       string
	UseLLVMBinutils   bool
	AutoClone         bool
}

// ToolchainConfig holds compiler / toolchain settings (Clang-only).
type ToolchainConfig struct {
	Preset            string   `json:"preset" toml:"preset"`
	CC                string   `json:"cc" toml:"cc"`
	CrossCompile      string   `json:"cross_compile" toml:"cross_compile"`
	CrossCompileArm32 string   `json:"cross_compile_arm32" toml:"cross_compile_arm32"`
	ClangTriple       string   `json:"clang_triple" toml:"clang_triple"`
	UseLLVMBinutils   bool     `json:"use_llvm_binutils" toml:"use_llvm_binutils"`
	ExtraPath         []string `json:"extra_path" toml:"extra_path"`
	AutoClone         bool     `json:"auto_clone" toml:"auto_clone"`
	AOSPClangVersion  string   `json:"aosp_clang_version" toml:"aosp_clang_version"`
}

// NewToolchainConfig returns a ToolchainConfig filled from the aosp-clang preset.
func NewToolchainConfig() ToolchainConfig {
	tc := ToolchainConfig{ExtraPath: []string{}}
	tc.ApplyPreset("aosp-clang")
	return tc
}

// ApplyPreset applies a named preset, transparently migrating deprecated names.
func (t *ToolchainConfig) ApplyPreset(name string) error {
	if replacement, ok := DeprecatedPresetAliases[name]; ok {
		name = replacement
	}
	preset, ok := ToolchainPresets[name]
	if !ok {
		names := make([]string, 0, len(ToolchainPresets))
		for n := range ToolchainPresets {
			names = append(names, n)
		}
		return fmt.Errorf("unknown toolchain preset %q. Available: %v", name, names)
	}
	t.Preset = name
	t.CC = preset.CC
	t.CrossCompile = preset.CrossCompile
	t.CrossCompileArm32 = preset.CrossCompileArm32
	t.ClangTriple = preset.ClangTriple
	t.UseLLVMBinutils = preset.UseLLVMBinutils
	t.AutoClone = preset.AutoClone
	return nil
}

// CcacheConfig holds ccache compiler-cache settings.
type CcacheConfig struct {
	Enabled    bool   `json:"enabled" toml:"enabled"`
	Dir        string `json:"dir" toml:"dir"`
	MaxSize    string `json:"max_size" toml:"max_size"`
	Sloppiness string `json:"sloppiness" toml:"sloppiness"`
	Compress   bool   `json:"compress" toml:"compress"`
	Basedir    string `json:"basedir" toml:"basedir"`
}

// NewCcacheConfig returns the default ccache settings (disabled).
func NewCcacheConfig() CcacheConfig {
	return CcacheConfig{
		Enabled:    false,
		Dir:        "",
		MaxSize:    "5G",
		Sloppiness: "time_macros,include_file_mtime,file_stat_matches,pch_defines",
		Compress:   true,
		Basedir:    "",
	}
}

// AnyKernel3Config holds AnyKernel3 packaging settings.
type AnyKernel3Config struct {
	AnykernelDir         string   `json:"anykernel_dir" toml:"anykernel_dir"`
	KernelName           string   `json:"kernel_name" toml:"kernel_name"`
	Block                string   `json:"block" toml:"block"`
	IsSlotDevice         int      `json:"is_slot_device" toml:"is_slot_device"`
	RamdiskCompression   string   `json:"ramdisk_compression" toml:"ramdisk_compression"`
	DoDevicecheck        int      `json:"do_devicecheck" toml:"do_devicecheck"`
	SupportedVersions    string   `json:"supported_versions" toml:"supported_versions"`
	SupportedPatchlevels string   `json:"supported_patchlevels" toml:"supported_patchlevels"`
	ExtraCmds            string   `json:"extra_cmds" toml:"extra_cmds"`
	DeviceNames          []string `json:"device_names" toml:"device_names"`
}

// NewAnyKernel3Config returns the default AnyKernel3 settings.
func NewAnyKernel3Config() AnyKernel3Config {
	return AnyKernel3Config{
		AnykernelDir:         "AnyKernel3",
		KernelName:           "kernel",
		Block:                "/dev/block/by-name/boot",
		IsSlotDevice:         0,
		RamdiskCompression:   "auto",
		DoDevicecheck:        1,
		SupportedVersions:    "",
		SupportedPatchlevels: "",
		ExtraCmds:            "",
		DeviceNames:          []string{},
	}
}

// BuildConfig is the top-level build configuration.
type BuildConfig struct {
	KernelSource    string `json:"kernel_source" toml:"kernel_source"`
	KernelDefconfig string `json:"kernel_defconfig" toml:"kernel_defconfig"`
	Arch            string `json:"arch" toml:"arch"`
	Subarch         string `json:"subarch" toml:"subarch"`

	KernelSourceURL    string `json:"kernel_source_url" toml:"kernel_source_url"`
	KernelSourceBranch string `json:"kernel_source_branch" toml:"kernel_source_branch"`
	KernelSourceDepth  int    `json:"kernel_source_depth" toml:"kernel_source_depth"`

	OutputDir    string `json:"output_dir" toml:"output_dir"`
	ZipOutputDir string `json:"zip_output_dir" toml:"zip_output_dir"`

	Jobs            int               `json:"jobs" toml:"jobs"`
	LTO             string            `json:"lto" toml:"lto"`
	ExtraMakeFlags  []string          `json:"extra_make_flags" toml:"extra_make_flags"`
	ExtraEnv        map[string]string `json:"extra_env" toml:"extra_env"`
	KBUILDBuildUser string            `json:"kbuild_build_user" toml:"kbuild_build_user"`
	KBUILDBuildHost string            `json:"kbuild_build_host" toml:"kbuild_build_host"`
	Localversion    string            `json:"localversion" toml:"localversion"`

	ToolchainDir       string `json:"toolchain_dir" toml:"toolchain_dir"`
	AutoSetupToolchain bool   `json:"auto_setup_toolchain" toml:"auto_setup_toolchain"`

	Toolchain  ToolchainConfig  `json:"toolchain" toml:"toolchain"`
	Anykernel3 AnyKernel3Config `json:"anykernel3" toml:"anykernel3"`
	Ccache     CcacheConfig     `json:"ccache" toml:"ccache"`
}

// DefaultKBUILDBuildHost is the fixed build-host branding string.
const DefaultKBUILDBuildHost = "forged (github.com/vxyzview/forged)"

// DefaultKBUILDBuildUser is the default build-user stamp.
func DefaultKBUILDBuildUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "builder"
}

// NewWithSource returns a default config bound to a kernel source directory.
func NewWithSource(source string) *BuildConfig {
	cfg := New()
	cfg.KernelSource = source
	return cfg
}

// New returns a BuildConfig with all defaults applied.
func New() *BuildConfig {
	return &BuildConfig{
		KernelSource:       "",
		KernelDefconfig:    "defconfig",
		Arch:               "arm64",
		Subarch:            "arm64",
		KernelSourceURL:    "",
		KernelSourceBranch: "",
		KernelSourceDepth:  1,
		OutputDir:          "out",
		ZipOutputDir:       "releases",
		Jobs:               0,
		LTO:                "",
		ExtraMakeFlags:     []string{},
		ExtraEnv:           map[string]string{},
		KBUILDBuildUser:    DefaultKBUILDBuildUser(),
		KBUILDBuildHost:    DefaultKBUILDBuildHost,
		Localversion:       "",
		ToolchainDir:       "",
		AutoSetupToolchain: true,
		Toolchain:          NewToolchainConfig(),
		Anykernel3:         NewAnyKernel3Config(),
		Ccache:             NewCcacheConfig(),
	}
}

// Validate checks the config for invalid values.
func (c *BuildConfig) Validate() error {
	if c.LTO != "" && !validLTOModes[c.LTO] {
		return fmt.Errorf("invalid lto value %q. Must be one of: [full thin] or empty", c.LTO)
	}
	if c.KernelSourceDepth < 0 {
		return fmt.Errorf("invalid kernel_source_depth %q. Must be 0 (full history) or a positive integer (shallow clone depth)", c.KernelSourceDepth)
	}
	return nil
}

// ExpandPath expands a leading ~ and $VAR references in a path.
func ExpandPath(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return os.ExpandEnv(p)
}

// Load auto-detects the format by extension and loads the config from disk.
func Load(path string) (*BuildConfig, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FromJSON(path)
	case ".toml", ".tml":
		return FromTOML(path)
	default:
		return nil, fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}
}

// migrateDeprecatedPreset upgrades a stale preset tag left by an older FORGED
// version (e.g. "aosp-clang-r584948b") to its canonical replacement, leaving
// every other loaded field untouched.
func (c *BuildConfig) migrateDeprecatedPreset() {
	if replacement, ok := DeprecatedPresetAliases[c.Toolchain.Preset]; ok {
		c.Toolchain.Preset = replacement
	}
}

// FromJSON loads a build config from a JSON file.
//
// Unknown keys — from configs written by newer FORGED versions — are
// silently ignored so older builds keep working.
func FromJSON(path string) (*BuildConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := New()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.migrateDeprecatedPreset()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// FromTOML loads a build config from a TOML file.
func FromTOML(path string) (*BuildConfig, error) {
	cfg := New()
	md, err := toml.DecodeFile(path, cfg)
	if err != nil {
		return nil, err
	}
	_ = md // undecoded keys are tolerated (forward compatibility)
	cfg.migrateDeprecatedPreset()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ToJSON writes the config to *path* as pretty-printed JSON.
func (c *BuildConfig) ToJSON(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

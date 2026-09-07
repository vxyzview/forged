// Package packager assembles flashable AnyKernel3 ZIPs from compiled kernel
// artefacts.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package packager

import (
	"archive/zip"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vxyzview/forged/internal/config"
	"github.com/vxyzview/forged/internal/toolchain"
)

// anykernelShTemplate is the AnyKernel3 ramdisk-mod script written into the
// staging directory. extraCmds is substituted via replace so braces in
// user-supplied shell commands do not break templating.
const anykernelShTemplate = `# AnyKernel3 Ramdisk Mod Script
# osm0sis @ xda-developers

## AnyKernel setup
# begin properties
properties() { cat << EOF
kernel.string={{KERNEL_NAME}} Kernel
do.devicecheck={{DO_DEVICECHECK}}
do.modules={{DO_MODULES}}
do.systemless=1
do.cleanup=1
do.cleanuponabort=0
device.name1={{DEVICE_NAME1}}
device.name2={{DEVICE_NAME2}}
device.name3={{DEVICE_NAME3}}
device.name4={{DEVICE_NAME4}}
device.name5={{DEVICE_NAME5}}
supported.versions={{SUPPORTED_VERSIONS}}
supported.patchlevels={{SUPPORTED_PATCHLEVELS}}
EOF
}

# shell variables
block={{BLOCK}}
is_slot_device={{IS_SLOT_DEVICE}}
ramdisk_compression={{RAMDISK_COMPRESSION}}

## AnyKernel methods (DO NOT CHANGE)
. tools/ak3-core.sh

## AnyKernel file attributes
# ...attributes are set in post-fs-data.sh or service.sh

## AnyKernel install
dump_boot

# begin ramdisk changes

# end ramdisk changes

write_boot

## end init
{{EXTRA_CMDS}}
`

const updateBinaryTemplate = `#!/sbin/sh
# AnyKernel3 Backend
# osm0sis @ xda-developers

OUTFD=/proc/self/fd/$2;
ZIPFILE="$3";

. /tmp/anykernel/tools/ak3-core.sh;

ui_print " ";
ui_print "###########################";
ui_print "#   AnyKernel3 Flasher   #";
ui_print "###########################";
ui_print " ";
ui_print "Patching kernel...";

chmod -R 755 /tmp/anykernel/tools;
cd /tmp/anykernel && bash anykernel.sh $ZIPFILE 2>&1;

if [ $? != "0" ]; then
  ui_print "Failed. Unmounting partitions...";
  umount /system 2>/dev/null;
  umount /vendor 2>/dev/null;
  exit 1;
fi;
`

const updaterScript = `abort("E3004: This package is for use with Magisk Manager / TWRP / recovery only!");`

const ak3CoreStub = `#!/usr/bin/env bash
# ak3-core.sh stub — replace with real AnyKernel3 core script.
# Download from: https://github.com/osm0sis/AnyKernel3
echo "AnyKernel3 core stub — install completed (dry run)."
`

// Packager builds a flashable AnyKernel3 ZIP from compiled kernel artefacts.
type Packager struct {
	cfg           *config.BuildConfig
	ak3           config.AnyKernel3Config
	AnykernelBase string
	progress      Progress
}

// Progress receives log lines during long operations (clone/download).
type Progress func(line string)

// nopProgress is used when no callback is supplied.
func nopProgress(string) {}

// New creates a Packager for the given AnyKernel3 staging directory.
func New(cfg *config.BuildConfig, anykernelBase string) *Packager {
	return &Packager{cfg: cfg, ak3: cfg.Anykernel3, AnykernelBase: anykernelBase}
}

// NewWithProgress creates a Packager that logs long operations via progress.
func NewWithProgress(cfg *config.BuildConfig, anykernelBase string, progress Progress) *Packager {
	p := New(cfg, anykernelBase)
	p.progress = progress
	return p
}

func (p *Packager) log(line string) {
	if p.progress != nil {
		p.progress(line)
	}
}

// renderAnykernelSh fills the anykernel.sh template from the config.
// hasModules reports whether kernel modules were staged for this build; it
// drives the effective do.modules value in auto mode.
func (p *Packager) renderAnykernelSh(hasModules bool) string {
	names := make([]string, 5)
	for i := 0; i < 5; i++ {
		if i < len(p.ak3.DeviceNames) {
			names[i] = p.ak3.DeviceNames[i]
		}
	}

	replacer := strings.NewReplacer(
		"{{KERNEL_NAME}}", p.ak3.KernelName,
		"{{DO_DEVICECHECK}}", fmt.Sprintf("%d", p.ak3.DoDevicecheck),
		"{{DO_MODULES}}", fmt.Sprintf("%d", p.effectiveDoModules(hasModules)),
		"{{DEVICE_NAME1}}", names[0],
		"{{DEVICE_NAME2}}", names[1],
		"{{DEVICE_NAME3}}", names[2],
		"{{DEVICE_NAME4}}", names[3],
		"{{DEVICE_NAME5}}", names[4],
		"{{SUPPORTED_VERSIONS}}", p.ak3.SupportedVersions,
		"{{SUPPORTED_PATCHLEVELS}}", p.ak3.SupportedPatchlevels,
		"{{BLOCK}}", p.ak3.Block,
		"{{IS_SLOT_DEVICE}}", fmt.Sprintf("%d", p.ak3.IsSlotDevice),
		"{{RAMDISK_COMPRESSION}}", p.ak3.RamdiskCompression,
		"{{EXTRA_CMDS}}", p.ak3.ExtraCmds,
	)
	return replacer.Replace(anykernelShTemplate)
}

// effectiveDoModules resolves the configured do_modules value into the
// AnyKernel3 do.modules property:
//
//	-1 (auto) — 1 when modules were staged, 0 otherwise
//	 0        — never install modules
//	 1        — always install modules
//
// Any other config value is treated as auto.
func (p *Packager) effectiveDoModules(hasModules bool) int {
	switch p.ak3.DoModules {
	case config.DoModulesOff:
		return 0
	case config.DoModulesOn:
		return 1
	default:
		if hasModules {
			return 1
		}
		return 0
	}
}

// ensureStructure creates required AnyKernel3 directories and forged's
// generated files (anykernel.sh comes from Prepare; update-binary and
// updater-script are written here when missing).
//
// When the staging directory holds a real AnyKernel3 checkout (cloned or
// local), its own updater artefacts win: nothing already present is
// overwritten. Only the stub ak3-core.sh is written into a bare skeleton.
func (p *Packager) ensureStructure() error {
	dirs := []string{
		filepath.Join(p.AnykernelBase, "tools"),
		filepath.Join(p.AnykernelBase, "modules"),
		filepath.Join(p.AnykernelBase, "patch"),
		filepath.Join(p.AnykernelBase, "ramdisk"),
		filepath.Join(p.AnykernelBase, "META-INF", "com", "google", "android"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	coreSh := filepath.Join(p.AnykernelBase, "tools", "ak3-core.sh")
	if !fileExists(coreSh) {
		p.log("[ak3] No ak3-core.sh found — writing stub (dry-run packaging only).\n" +
			"      Set anykernel3.source = \"osm0sis\" (or \"git\"/\"local\") for a real flashable ZIP.")
		if err := os.WriteFile(coreSh, []byte(ak3CoreStub), 0o755); err != nil {
			return err
		}
	}

	updateBinary := filepath.Join(p.AnykernelBase, "META-INF", "com", "google", "android", "update-binary")
	if !fileExists(updateBinary) {
		if err := os.WriteFile(updateBinary, []byte(updateBinaryTemplate), 0o755); err != nil {
			return err
		}
	}

	updaterScriptPath := filepath.Join(p.AnykernelBase, "META-INF", "com", "google", "android", "updater-script")
	if !fileExists(updaterScriptPath) {
		if err := os.WriteFile(updaterScriptPath, []byte(updaterScript), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src to dst preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// ak3LooksPopulated reports whether the staging directory already contains a
// real AnyKernel3 checkout (anykernel.sh or the real ak3-core.sh present).
func ak3LooksPopulated(base string) bool {
	if fileExists(filepath.Join(base, "tools", "ak3-core.sh")) {
		// Present, but the stub is not a real AnyKernel3 core.
		data, err := os.ReadFile(filepath.Join(base, "tools", "ak3-core.sh"))
		if err == nil && strings.Contains(string(data), "AnyKernel3 core stub") {
			return false
		}
		return true
	}
	return fileExists(filepath.Join(base, "anykernel.sh")) ||
		fileExists(filepath.Join(base, "META-INF", "com", "google", "android", "update-binary"))
}

// EnsureSource makes sure the staging directory holds a usable AnyKernel3
// tree according to the configured source mode:
//
//	osm0sis — clone/pull github.com/osm0sis/AnyKernel3 (upstream)
//	git     — clone/pull cfg.Anykernel3.RepoURL (your own fork)
//	local   — copy an existing AnyKernel3 checkout from RepoURL (path)
//	stub    — keep the committed skeleton with a stub ak3-core.sh
//
// Templates (anykernel.sh / update-binary) are (re)rendered later by
// Prepare; an already-populated real tree is left untouched.
func (p *Packager) EnsureSource() error {
	base := p.AnykernelBase
	mode := p.ak3.Source
	if mode == "" {
		mode = config.AK3SourceOsm0sis
	}

	switch mode {
	case config.AK3SourceStub:
		// Committed skeleton — nothing to fetch.
		return nil

	case config.AK3SourceOsm0sis, config.AK3SourceGit:
		repo := p.ak3.RepoURL
		if mode == config.AK3SourceOsm0sis && repo == "" {
			repo = config.DefaultAnyKernel3Repo
		}
		if repo == "" {
			return fmt.Errorf("anykernel3.source = %q but anykernel3.repo_url is empty", mode)
		}
		if ak3LooksPopulated(base) {
			// Already have a real tree — try a fast refresh, ignore failures.
			if _, err := os.Stat(filepath.Join(base, ".git")); err == nil {
				p.log("[ak3] AnyKernel3 already present — pulling latest …")
				_ = toolchain.Run([]string{"git", "-C", base, "pull", "--ff-only"}, "", nil)
			} else {
				p.log(fmt.Sprintf("[ak3] AnyKernel3 already present at %s — reusing.", base))
			}
			return nil
		}
		p.log(fmt.Sprintf("[ak3] Fetching AnyKernel3 from %s (mode: %s) …", repo, mode))
		parent := filepath.Dir(base)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		if _, err := toolchain.CloneKernelSource(context.Background(), repo, base, p.ak3.RepoBranch, p.ak3.RepoDepth, p.log); err != nil {
			return fmt.Errorf("cloning AnyKernel3 failed: %w", err)
		}
		return nil

	case config.AK3SourceLocal:
		src := p.ak3.RepoURL
		if src == "" {
			return fmt.Errorf("anykernel3.source = \"local\" but anykernel3.repo_url must point at your AnyKernel3 checkout")
		}
		src = config.ExpandPath(src)
		if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
			return fmt.Errorf("anykernel3 local source %q does not exist", src)
		}
		// Copy the local checkout into staging, preserving tools/ and
		// META-INF/, skipping VCS noise. Overwrites templates only.
		p.log(fmt.Sprintf("[ak3] Copying AnyKernel3 from %s …", src))
		return copyTree(src, base)

	default:
		return fmt.Errorf("unknown anykernel3.source %q (valid: %v)", mode, config.AnyKernel3SourceModes)
	}
}

// copyTree recursively copies src into dst, skipping .git directories and
// compiled kernel artefacts that never belong in a fresh staging area.
func copyTree(src, dst string) error {
	skip := map[string]bool{".git": true, "out": true, "releases": true, "AnyKernel3": true}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skip[info.Name()] && path != src {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

// Prepare copies artefacts into the AnyKernel3 staging directory.
//
// It first resolves the staging source (clone/copy per anykernel3.source),
// then (re)renders forged's own templates — anykernel.sh, update-binary,
// updater-script — leaving upstream's ak3-core.sh and tools/ untouched.
func (p *Packager) Prepare(kernelImage string, dtbFiles, moduleFiles []string) error {
	if err := p.EnsureSource(); err != nil {
		return err
	}
	if err := p.ensureStructure(); err != nil {
		return err
	}

	akSh := filepath.Join(p.AnykernelBase, "anykernel.sh")
	if err := os.WriteFile(akSh, []byte(p.renderAnykernelSh(len(moduleFiles) > 0)), 0o644); err != nil {
		return err
	}

	destImage := filepath.Join(p.AnykernelBase, p.ak3.KernelName)
	if err := copyFile(kernelImage, destImage); err != nil {
		return err
	}

	if len(dtbFiles) > 0 {
		dtbDir := filepath.Join(p.AnykernelBase, "dtb")
		if err := os.MkdirAll(dtbDir, 0o755); err != nil {
			return err
		}
		for _, dtb := range dtbFiles {
			if err := copyFile(dtb, filepath.Join(dtbDir, filepath.Base(dtb))); err != nil {
				return err
			}
		}
	}

	if len(moduleFiles) > 0 {
		modDir := filepath.Join(p.AnykernelBase, "modules", "system", "lib", "modules")
		if err := os.MkdirAll(modDir, 0o755); err != nil {
			return err
		}

		// Modules from different source subdirectories can share a filename;
		// preserve the relative path so each lands in its own sub-directory.
		common := commonPath(moduleFiles)
		for _, mod := range moduleFiles {
			rel, err := filepath.Rel(common, mod)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				rel = filepath.Base(mod)
			}
			dest := filepath.Join(modDir, rel)
			if err := copyFile(mod, dest); err != nil {
				return err
			}
		}
	}
	return nil
}

// commonPath finds the deepest common ancestor directory of the given files.
func commonPath(files []string) string {
	if len(files) == 0 {
		return "/"
	}
	dirs := make([]string, 0, len(files))
	for _, f := range files {
		dirs = append(dirs, filepath.Dir(f))
	}
	prefix := dirs[0]
	for _, d := range dirs[1:] {
		for !strings.HasPrefix(d+string(filepath.Separator), prefix+string(filepath.Separator)) && d != prefix {
			// Walk prefix back until it is a prefix of d.
			parent := filepath.Dir(prefix)
			if parent == prefix {
				prefix = "/"
				break
			}
			prefix = parent
			if strings.HasPrefix(d+string(filepath.Separator), prefix+string(filepath.Separator)) {
				break
			}
		}
	}
	// If prefix equals one of the file dirs and that dir is itself a file path
	// shared by all, keep it (mirrors os.path.commonpath behaviour closely
	// enough for staging purposes).
	return prefix
}

// CreateZip zips the staging directory into a flashable archive under
// outputDir and returns the archive path.
//
// The ZIP is written to a temp file first and only renamed into place on
// success, so a failed mid-write never leaves a corrupt partial archive.
func (p *Packager) CreateZip(outputDir, versionTag string) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	timestamp := time.Now().Format("20060102-150405")
	name := p.ak3.KernelName
	if versionTag != "" {
		name += "-" + versionTag
	}
	zipName := fmt.Sprintf("%s-%s.zip", name, timestamp)
	zipPath := filepath.Join(outputDir, zipName)

	tmp, err := os.CreateTemp(outputDir, "*.zip.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()

	if err := writeZip(tmp, p.AnykernelBase); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, zipPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return zipPath, nil
}

func writeZip(f *os.File, base string) error {
	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})
	defer zw.Close()

	var files []string
	err := filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, file := range files {
		rel, err := filepath.Rel(base, file)
		if err != nil {
			return err
		}
		info, err := os.Lstat(file)
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(file)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, src); err != nil {
			src.Close()
			return err
		}
		src.Close()
	}
	return nil
}

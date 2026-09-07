// Package packager assembles flashable AnyKernel3 ZIPs from compiled kernel
// artefacts.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package packager

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vxyzview/forged/internal/config"
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
do.modules=0
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
}

// New creates a Packager for the given AnyKernel3 staging directory.
func New(cfg *config.BuildConfig, anykernelBase string) *Packager {
	return &Packager{cfg: cfg, ak3: cfg.Anykernel3, AnykernelBase: anykernelBase}
}

// renderAnykernelSh fills the anykernel.sh template from the config.
func (p *Packager) renderAnykernelSh() string {
	names := make([]string, 5)
	for i := 0; i < 5; i++ {
		if i < len(p.ak3.DeviceNames) {
			names[i] = p.ak3.DeviceNames[i]
		}
	}

	replacer := strings.NewReplacer(
		"{{KERNEL_NAME}}", p.ak3.KernelName,
		"{{DO_DEVICECHECK}}", fmt.Sprintf("%d", p.ak3.DoDevicecheck),
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

// ensureStructure creates required AnyKernel3 directories and stub files.
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

// Prepare copies artefacts into the AnyKernel3 staging directory.
func (p *Packager) Prepare(kernelImage string, dtbFiles, moduleFiles []string) error {
	if err := p.ensureStructure(); err != nil {
		return err
	}

	akSh := filepath.Join(p.AnykernelBase, "anykernel.sh")
	if err := os.WriteFile(akSh, []byte(p.renderAnykernelSh()), 0o644); err != nil {
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

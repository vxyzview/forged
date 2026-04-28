"""AnyKernel3 packaging — assembles flashable ZIPs."""

from __future__ import annotations

import contextlib
import datetime
import os
import shutil
import stat
import tempfile
import zipfile
from pathlib import Path

from .config import BuildConfig

# ── Template files written into AnyKernel3/ ──────────────────────────────────

# extra_cmds is interpolated via a sentinel-and-replace strategy rather than
# directly into str.format(), so any brace characters in user-supplied shell
# commands do not crash str.format() with KeyError / IndexError.

_ANYKERNEL_SH_TEMPLATE = """\
# AnyKernel3 Ramdisk Mod Script
# osm0sis @ xda-developers

## AnyKernel setup
# begin properties
properties() {{ cat << EOF
kernel.string={kernel_name} Kernel
do.devicecheck={do_devicecheck}
do.modules=0
do.systemless=1
do.cleanup=1
do.cleanuponabort=0
device.name1={device_name1}
device.name2={device_name2}
device.name3={device_name3}
device.name4={device_name4}
device.name5={device_name5}
supported.versions={supported_versions}
supported.patchlevels={supported_patchlevels}
EOF
}}

# shell variables
block={block}
is_slot_device={is_slot_device}
ramdisk_compression={ramdisk_compression}

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
__EXTRA_CMDS_PLACEHOLDER__
"""

_UPDATE_BINARY_TEMPLATE = """\
#!/sbin/sh
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
"""

_UPDATER_SCRIPT = (
    'abort("E3004: This package is for use with Magisk Manager / TWRP / recovery only!");'
)

_AK3_CORE_STUB = """\
#!/usr/bin/env bash
# ak3-core.sh stub — replace with real AnyKernel3 core script.
# Download from: https://github.com/osm0sis/AnyKernel3
echo "AnyKernel3 core stub — install completed (dry run)."
"""


# ── Packager ─────────────────────────────────────────────────────────────────


class AnyKernel3Packager:
    """Builds a flashable AnyKernel3 ZIP from compiled kernel artefacts."""

    def __init__(self, cfg: BuildConfig, anykernel_base: Path) -> None:
        self.cfg = cfg
        self.ak3_cfg = cfg.anykernel3
        self.anykernel_base = anykernel_base

    # ── Internal helpers ──────────────────────────────────────────────────────

    def _device_names(self) -> dict:
        names = (self.ak3_cfg.device_names + [""] * 5)[:5]
        return {f"device_name{i + 1}": n for i, n in enumerate(names)}

    def _render_anykernel_sh(self) -> str:
        # extra_cmds is substituted via str.replace() after format() so that
        # any brace characters in user-supplied shell commands do not cause
        # str.format() to raise KeyError / IndexError.
        rendered = _ANYKERNEL_SH_TEMPLATE.format(
            kernel_name=self.ak3_cfg.kernel_name,
            do_devicecheck=self.ak3_cfg.do_devicecheck,
            block=self.ak3_cfg.block,
            is_slot_device=self.ak3_cfg.is_slot_device,
            ramdisk_compression=self.ak3_cfg.ramdisk_compression,
            supported_versions=self.ak3_cfg.supported_versions,
            supported_patchlevels=self.ak3_cfg.supported_patchlevels,
            **self._device_names(),
        )
        return rendered.replace("__EXTRA_CMDS_PLACEHOLDER__", self.ak3_cfg.extra_cmds)

    def _ensure_structure(self) -> None:
        """Create required AnyKernel3 directories & stub files."""
        dirs = [
            self.anykernel_base / "tools",
            self.anykernel_base / "modules",
            self.anykernel_base / "patch",
            self.anykernel_base / "ramdisk",
            self.anykernel_base / "META-INF" / "com" / "google" / "android",
        ]
        for d in dirs:
            d.mkdir(parents=True, exist_ok=True)

        core_sh = self.anykernel_base / "tools" / "ak3-core.sh"
        if not core_sh.exists():
            core_sh.write_text(_AK3_CORE_STUB, encoding="utf-8")
            core_sh.chmod(core_sh.stat().st_mode | stat.S_IEXEC)

        update_binary = (
            self.anykernel_base / "META-INF" / "com" / "google" / "android" / "update-binary"
        )
        if not update_binary.exists():
            update_binary.write_text(_UPDATE_BINARY_TEMPLATE, encoding="utf-8")
            update_binary.chmod(update_binary.stat().st_mode | stat.S_IEXEC)

        updater_script = (
            self.anykernel_base / "META-INF" / "com" / "google" / "android" / "updater-script"
        )
        if not updater_script.exists():
            updater_script.write_text(_UPDATER_SCRIPT, encoding="utf-8")

    # ── Public API ────────────────────────────────────────────────────────────

    def prepare(
        self,
        kernel_image: Path,
        dtb_files: list[Path] | None = None,
        module_files: list[Path] | None = None,
    ) -> None:
        """Copy artefacts into the AnyKernel3 staging directory."""
        self._ensure_structure()

        ak_sh = self.anykernel_base / "anykernel.sh"
        ak_sh.write_text(self._render_anykernel_sh(), encoding="utf-8")

        dest_image = self.anykernel_base / self.ak3_cfg.kernel_name
        shutil.copy2(kernel_image, dest_image)

        if dtb_files:
            dtb_dir = self.anykernel_base / "dtb"
            dtb_dir.mkdir(exist_ok=True)
            for dtb in dtb_files:
                shutil.copy2(dtb, dtb_dir / dtb.name)

        if module_files:
            mod_dir = self.anykernel_base / "modules" / "system" / "lib" / "modules"
            mod_dir.mkdir(parents=True, exist_ok=True)
            # Modules from different source subdirectories can share the same
            # filename (e.g. drivers/net/foo.ko and fs/foo.ko).  Preserve the
            # relative path inside the output tree so each lands in its own
            # sub-directory, preventing silent overwrites.
            # Find the common ancestor among all module paths for relative naming.
            try:
                common = Path(os.path.commonpath([str(m) for m in module_files]))
                if common.is_file():
                    common = common.parent
            except ValueError:
                common = Path("/")

            for mod in module_files:
                try:
                    rel = mod.relative_to(common)
                except ValueError:
                    rel = Path(mod.name)
                dest = mod_dir / rel
                dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(mod, dest)

    def create_zip(self, output_dir: Path, version_tag: str = "") -> Path:
        """Zip the staging directory into a flashable archive.

        The ZIP is written to a temporary file first and only renamed into
        place on success, so a failed mid-write never leaves a corrupt
        partial archive at the final path.
        """
        output_dir.mkdir(parents=True, exist_ok=True)
        timestamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
        tag = f"-{version_tag}" if version_tag else ""
        zip_name = f"{self.ak3_cfg.kernel_name}{tag}-{timestamp}.zip"
        zip_path = output_dir / zip_name

        tmp_fd, tmp_path = tempfile.mkstemp(dir=output_dir, suffix=".zip.tmp")
        try:
            os.close(tmp_fd)
            with zipfile.ZipFile(
                tmp_path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9
            ) as zf:
                for file in sorted(self.anykernel_base.rglob("*")):
                    if file.is_file():
                        arcname = file.relative_to(self.anykernel_base)
                        zf.write(file, arcname)
            os.replace(tmp_path, zip_path)
        except Exception:
            with contextlib.suppress(OSError):
                os.unlink(tmp_path)
            raise

        return zip_path

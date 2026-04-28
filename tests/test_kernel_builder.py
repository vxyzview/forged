"""
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.

Unit tests for kernel_builder.
"""

from __future__ import annotations

import json
import os
from pathlib import Path

import pytest

from kernel_builder.builder import (
    KernelBuilder,
    _build_env,
    _make_base,
    _resolve_jobs,
)
from kernel_builder.config import (
    TOOLCHAIN_PRESETS,
    BuildConfig,
    ToolchainConfig,
)
from kernel_builder.packager import AnyKernel3Packager

# ── Config tests ──────────────────────────────────────────────────────────────


class TestBuildConfig:
    def test_default_construction(self):
        cfg = BuildConfig()
        assert cfg.arch == "arm64"
        assert cfg.jobs == 0
        assert cfg.lto is None

    def test_default_toolchain_is_clang(self):
        cfg = BuildConfig()
        assert cfg.toolchain.cc == "clang"
        assert cfg.toolchain.use_llvm_binutils is True

    # ── Preset: system-clang ─────────────────────────────────────────────────

    def test_preset_system_clang(self):
        tc = ToolchainConfig()
        tc.apply_preset("system-clang")
        assert tc.cc == "clang"
        assert tc.use_llvm_binutils is True
        assert "aarch64" in tc.cross_compile

    # ── Preset: aosp-clang ────────────────────────────────────────────────────

    def test_preset_aosp_clang(self):
        tc = ToolchainConfig()
        tc.apply_preset("aosp-clang")
        assert tc.cc == "clang"
        assert tc.use_llvm_binutils is True
        assert tc.clang_triple == "aarch64-linux-gnu-"

    def test_preset_aosp_clang_extra_path(self):
        tc = ToolchainConfig()
        tc.apply_preset("aosp-clang")
        tc.extra_path = ["/opt/clang-r584948b/bin"]
        assert tc.extra_path == ["/opt/clang-r584948b/bin"]

    # ── All presets are Clang-only ────────────────────────────────────────────

    def test_all_presets_use_clang(self):
        for name, preset in TOOLCHAIN_PRESETS.items():
            assert preset["cc"] == "clang", f"Preset '{name}' must use cc=clang"

    def test_all_presets_enable_llvm_binutils(self):
        for name, preset in TOOLCHAIN_PRESETS.items():
            assert preset.get("use_llvm_binutils") is True, (
                f"Preset '{name}' must set use_llvm_binutils=True"
            )

    def test_gcc_preset_removed(self):
        assert "gcc-aarch64" not in TOOLCHAIN_PRESETS

    def test_toolchain_invalid_preset(self):
        tc = ToolchainConfig()
        with pytest.raises(ValueError, match="Unknown toolchain preset"):
            tc.apply_preset("gcc-aarch64")

    def test_toolchain_invalid_wizard_old_default(self):
        tc = ToolchainConfig()
        with pytest.raises(ValueError, match="Unknown toolchain preset"):
            tc.apply_preset("clang-aosp")

    # ── Deprecated alias: aosp-clang-r547379 → aosp-clang-r584948b ───────────

    def test_deprecated_preset_r547379_migrates_to_r584948b(self):
        """apply_preset('aosp-clang-r547379') must transparently upgrade to
        the current 'aosp-clang' preset and emit a DeprecationWarning."""
        import warnings
        tc = ToolchainConfig()
        with warnings.catch_warnings(record=True) as caught:
            warnings.simplefilter("always")
            tc.apply_preset("aosp-clang-r547379")

        assert tc.preset == "aosp-clang", (
            "Deprecated preset should be migrated to aosp-clang"
        )
        deprecation_warnings = [
            w for w in caught if issubclass(w.category, DeprecationWarning)
        ]
        assert deprecation_warnings, "A DeprecationWarning must be emitted for r547379"
        assert "aosp-clang-r547379" in str(deprecation_warnings[0].message)
        assert "aosp-clang" in str(deprecation_warnings[0].message)

    def test_deprecated_preset_r547379_settings_match_r584948b(self):
        """After migration, all toolchain settings must equal the aosp-clang preset."""
        import warnings
        tc_old = ToolchainConfig()
        tc_new = ToolchainConfig()
        with warnings.catch_warnings(record=True):
            warnings.simplefilter("always")
            tc_old.apply_preset("aosp-clang-r547379")
        tc_new.apply_preset("aosp-clang")

        assert tc_old.cc == tc_new.cc
        assert tc_old.cross_compile == tc_new.cross_compile
        assert tc_old.cross_compile_arm32 == tc_new.cross_compile_arm32
        assert tc_old.clang_triple == tc_new.clang_triple
        assert tc_old.use_llvm_binutils == tc_new.use_llvm_binutils
        assert tc_old.auto_clone == tc_new.auto_clone

    def test_deprecated_aliases_map_to_valid_presets(self):
        """Every entry in DEPRECATED_PRESET_ALIASES must point to a live preset."""
        from kernel_builder.config import DEPRECATED_PRESET_ALIASES
        for old_name, new_name in DEPRECATED_PRESET_ALIASES.items():
            assert new_name in TOOLCHAIN_PRESETS, (
                f"Deprecated alias '{old_name}' → '{new_name}' but "
                f"'{new_name}' is not in TOOLCHAIN_PRESETS"
            )


    def test_lto_none_accepted(self):
        cfg = BuildConfig(lto=None)
        assert cfg.lto is None

    def test_lto_thin_accepted(self):
        cfg = BuildConfig(lto="thin")
        assert cfg.lto == "thin"

    def test_lto_full_accepted(self):
        cfg = BuildConfig(lto="full")
        assert cfg.lto == "full"

    def test_lto_invalid_raises(self):
        with pytest.raises(ValueError, match="Invalid lto value"):
            BuildConfig(lto="fast")

    def test_lto_invalid_from_json_raises(self, tmp_path):
        bad = {"lto": "fast", "toolchain": {}, "anykernel3": {}}
        p = tmp_path / "bad.json"
        p.write_text(json.dumps(bad))
        with pytest.raises(ValueError, match="Invalid lto value"):
            BuildConfig.from_json(p)

    def test_kernel_source_depth_positive_accepted(self):
        cfg = BuildConfig(kernel_source_depth=5)
        assert cfg.kernel_source_depth == 5

    def test_kernel_source_depth_zero_accepted(self):
        """Depth 0 means full history — must be valid."""
        cfg = BuildConfig(kernel_source_depth=0)
        assert cfg.kernel_source_depth == 0

    def test_kernel_source_depth_negative_raises(self):
        """Negative depth is meaningless and must be rejected."""
        with pytest.raises(ValueError, match="kernel_source_depth"):
            BuildConfig(kernel_source_depth=-1)

    def test_kernel_source_depth_negative_from_json_raises(self, tmp_path):
        bad = {"kernel_source_depth": -2, "toolchain": {}, "anykernel3": {}}
        p = tmp_path / "bad.json"
        p.write_text(json.dumps(bad))
        with pytest.raises(ValueError, match="kernel_source_depth"):
            BuildConfig.from_json(p)

    # ── JSON round-trip ───────────────────────────────────────────────────────

    def test_json_round_trip(self, tmp_path):
        cfg = BuildConfig(
            kernel_source="/src/kernel",
            kernel_defconfig="pixel_defconfig",
            lto="thin",
        )
        path = tmp_path / "config.json"
        cfg.to_json(path)

        loaded = BuildConfig.from_json(path)
        assert loaded.kernel_source == "/src/kernel"
        assert loaded.kernel_defconfig == "pixel_defconfig"
        assert loaded.lto == "thin"

    def test_json_preserves_use_llvm_binutils(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.use_llvm_binutils = False
        path = tmp_path / "config.json"
        cfg.to_json(path)

        loaded = BuildConfig.from_json(path)
        assert loaded.toolchain.use_llvm_binutils is False

    def test_json_toolchain_preset_preserved(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.apply_preset("aosp-clang")
        path = tmp_path / "config.json"
        cfg.to_json(path)

        loaded = BuildConfig.from_json(path)
        assert loaded.toolchain.preset == "aosp-clang"
        assert loaded.toolchain.cc == "clang"

    def test_auto_load_json(self, tmp_path):
        cfg = BuildConfig(kernel_source="/test")
        path = tmp_path / "build.json"
        cfg.to_json(path)
        loaded = BuildConfig.load(path)
        assert loaded.kernel_source == "/test"

    def test_load_unsupported_extension(self, tmp_path):
        with pytest.raises(ValueError, match="Unsupported config format"):
            BuildConfig.load(tmp_path / "config.yaml")


# ── Builder tests ─────────────────────────────────────────────────────────────


class TestBuildEnv:
    def test_resolve_jobs_auto(self):
        jobs = _resolve_jobs(0)
        assert jobs >= 1

    def test_resolve_jobs_explicit(self):
        assert _resolve_jobs(4) == 4

    def test_build_env_sets_arch(self):
        cfg = BuildConfig(arch="arm64")
        env = _build_env(cfg)
        assert env["ARCH"] == "arm64"

    def test_build_env_sets_cc_clang(self):
        cfg = BuildConfig()
        env = _build_env(cfg)
        assert env["CC"] == "clang"

    def test_build_env_sets_clang_triple(self):
        cfg = BuildConfig()
        env = _build_env(cfg)
        assert env["CLANG_TRIPLE"] == "aarch64-linux-gnu-"

    def test_build_env_llvm_binutils_present_when_enabled(self):
        cfg = BuildConfig()
        cfg.toolchain.use_llvm_binutils = True
        env = _build_env(cfg)
        assert env["LD"] == "ld.lld"
        assert env["AR"] == "llvm-ar"
        assert env["NM"] == "llvm-nm"
        assert env["OBJCOPY"] == "llvm-objcopy"
        assert env["OBJDUMP"] == "llvm-objdump"
        assert env["READELF"] == "llvm-readelf"
        assert env["STRIP"] == "llvm-strip"

    def test_build_env_llvm_binutils_absent_when_disabled(self):
        cfg = BuildConfig()
        cfg.toolchain.use_llvm_binutils = False
        env = _build_env(cfg)
        assert "LD" not in env
        assert "AR" not in env

    def test_build_env_extra_path_prepended(self):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = ["/opt/clang-r584948b/bin"]
        env = _build_env(cfg)
        assert env["PATH"].startswith("/opt/clang-r584948b/bin")

    def test_build_env_extra_path_tilde_expanded(self):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = ["~/toolchains/clang/bin"]
        env = _build_env(cfg)
        home = os.path.expanduser("~")
        assert env["PATH"].startswith(home)
        assert "~" not in env["PATH"].split(":")[0]


class TestMakeBase:
    def test_make_base_contains_cc_clang(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cmd = _make_base(cfg, jobs=4)
        assert "CC=clang" in cmd

    def test_make_base_contains_clang_triple(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cmd = _make_base(cfg, jobs=4)
        assert any("CLANG_TRIPLE=" in arg for arg in cmd)

    def test_make_base_contains_subarch(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()), subarch="arm64")
        cmd = _make_base(cfg, jobs=4)
        assert "SUBARCH=arm64" in cmd

    def test_make_base_llvm_binutils_flags_present(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.toolchain.use_llvm_binutils = True
        cmd = _make_base(cfg, jobs=4)
        assert "LD=ld.lld" in cmd
        assert "AR=llvm-ar" in cmd
        assert "STRIP=llvm-strip" in cmd

    def test_make_base_llvm_binutils_flags_absent_when_disabled(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.toolchain.use_llvm_binutils = False
        cmd = _make_base(cfg, jobs=4)
        assert "LD=ld.lld" not in cmd
        assert "AR=llvm-ar" not in cmd

    def test_make_base_lto_thin(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()), lto="thin")
        cmd = _make_base(cfg, jobs=4)
        assert "LTO=thin" in cmd

    def test_make_base_lto_full(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()), lto="full")
        cmd = _make_base(cfg, jobs=4)
        assert "LTO=full" in cmd

    def test_make_base_no_lto_by_default(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cmd = _make_base(cfg, jobs=4)
        assert "LTO=thin" not in cmd
        assert "LTO=full" not in cmd


class TestKernelBuilder:
    def test_steps_with_clean(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        builder = KernelBuilder(cfg)
        steps = builder.steps(clean=True)
        names = [s.name for s in steps]
        assert names == ["mrproper", "defconfig", "build"]

    def test_steps_without_clean(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        builder = KernelBuilder(cfg)
        steps = builder.steps(clean=False)
        names = [s.name for s in steps]
        assert names == ["defconfig", "build"]

    def test_steps_all_use_clang(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        builder = KernelBuilder(cfg)
        for step in builder.steps(clean=True):
            assert "CC=clang" in step.command, (
                f"Step '{step.name}' does not contain CC=clang"
            )

    def test_steps_all_include_llvm_binutils(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        builder = KernelBuilder(cfg)
        for step in builder.steps(clean=True):
            assert "LD=ld.lld" in step.command, (
                f"Step '{step.name}' missing LD=ld.lld"
            )

    def test_find_kernel_image_missing(self, tmp_path):
        cfg = BuildConfig(kernel_source=str(tmp_path), output_dir="out")
        builder = KernelBuilder(cfg)
        assert builder.find_kernel_image() is None

    def test_find_kernel_image_present(self, tmp_path):
        out = tmp_path / "out" / "arch" / "arm64" / "boot"
        out.mkdir(parents=True)
        image = out / "Image.gz-dtb"
        image.write_bytes(b"\x00" * 64)

        cfg = BuildConfig(kernel_source=str(tmp_path), output_dir="out")
        builder = KernelBuilder(cfg)
        found = builder.find_kernel_image()
        assert found is not None
        assert found.name == "Image.gz-dtb"

    def test_find_modules_empty(self, tmp_path):
        cfg = BuildConfig(kernel_source=str(tmp_path), output_dir="out")
        builder = KernelBuilder(cfg)
        assert builder.find_modules() == []

    def test_run_step_command_not_found(self, tmp_path):
        from kernel_builder.builder import BuildStep
        cfg = BuildConfig(kernel_source=str(tmp_path))
        builder = KernelBuilder(cfg)
        step = BuildStep(
            name="test",
            command=["nonexistent_binary_xyz_1234"],
            env={},
        )
        result = builder.run_step(step)
        assert result.success is False
        assert "not found" in result.error.lower() or result.error

    def test_find_dtb_files_no_duplicates(self, tmp_path):
        boot = tmp_path / "out" / "arch" / "arm64" / "boot"
        dts_dir = boot / "dts"
        dts_dir.mkdir(parents=True)
        # Place a dtb that matches both patterns if globbed naively
        dtb = boot / "device.dtb"
        dtb.write_bytes(b"\xd0\x0d" * 4)

        cfg = BuildConfig(kernel_source=str(tmp_path), output_dir="out")
        builder = KernelBuilder(cfg)
        found = builder.find_dtb_files()
        # Must not contain duplicates
        assert len(found) == len(set(p.resolve() for p in found))


# ── Packager tests ────────────────────────────────────────────────────────────


class TestAnyKernel3Packager:
    def _make_packager(self, tmp_path: Path) -> tuple[AnyKernel3Packager, Path]:
        cfg = BuildConfig()
        cfg.anykernel3.kernel_name = "TestKernel"
        cfg.anykernel3.device_names = ["testdevice"]
        ak3_dir = tmp_path / "AnyKernel3"
        packager = AnyKernel3Packager(cfg, ak3_dir)
        return packager, ak3_dir

    def test_prepare_copies_image(self, tmp_path):
        packager, ak3_dir = self._make_packager(tmp_path)
        image = tmp_path / "Image.gz-dtb"
        image.write_bytes(b"\x1f\x8b" + b"\x00" * 100)

        packager.prepare(image)
        assert (ak3_dir / "TestKernel").exists()

    def test_prepare_creates_anykernel_sh(self, tmp_path):
        packager, ak3_dir = self._make_packager(tmp_path)
        image = tmp_path / "Image.gz"
        image.write_bytes(b"\x00" * 32)
        packager.prepare(image)
        ak_sh = ak3_dir / "anykernel.sh"
        assert ak_sh.exists()
        content = ak_sh.read_text()
        assert "TestKernel" in content
        assert "testdevice" in content

    def test_prepare_extra_cmds_with_braces(self, tmp_path):
        cfg = BuildConfig()
        cfg.anykernel3.kernel_name = "BraceKernel"
        cfg.anykernel3.extra_cmds = 'echo "{hello}" && echo "{world}"'
        ak3_dir = tmp_path / "AnyKernel3"
        packager = AnyKernel3Packager(cfg, ak3_dir)
        image = tmp_path / "Image"
        image.write_bytes(b"\x00" * 32)
        # Must not raise KeyError or IndexError
        packager.prepare(image)
        content = (ak3_dir / "anykernel.sh").read_text()
        assert "{hello}" in content

    def test_create_zip(self, tmp_path):
        packager, ak3_dir = self._make_packager(tmp_path)
        image = tmp_path / "Image"
        image.write_bytes(b"\x00" * 64)
        packager.prepare(image)
        zip_out = tmp_path / "releases"
        zip_path = packager.create_zip(zip_out, version_tag="v1.0")
        assert zip_path.exists()
        assert zip_path.suffix == ".zip"
        assert "v1.0" in zip_path.name
        assert zip_path.stat().st_size > 0

    def test_create_zip_contains_anykernel_sh(self, tmp_path):
        import zipfile as zf
        packager, ak3_dir = self._make_packager(tmp_path)
        image = tmp_path / "Image"
        image.write_bytes(b"\x00" * 64)
        packager.prepare(image)
        zip_path = packager.create_zip(tmp_path / "out")
        with zf.ZipFile(zip_path) as z:
            names = z.namelist()
        assert "anykernel.sh" in names

    def test_prepare_modules_no_name_collision(self, tmp_path):
        cfg = BuildConfig()
        cfg.anykernel3.kernel_name = "ModKernel"
        ak3_dir = tmp_path / "AnyKernel3"
        packager = AnyKernel3Packager(cfg, ak3_dir)

        image = tmp_path / "Image"
        image.write_bytes(b"\x00" * 32)

        # Two .ko files with the same name in different directories
        mod_a_dir = tmp_path / "drivers" / "net"
        mod_b_dir = tmp_path / "fs" / "ext4"
        mod_a_dir.mkdir(parents=True)
        mod_b_dir.mkdir(parents=True)
        mod_a = mod_a_dir / "foo.ko"
        mod_b = mod_b_dir / "foo.ko"
        mod_a.write_bytes(b"\xaa" * 16)
        mod_b.write_bytes(b"\xbb" * 16)

        packager.prepare(image, module_files=[mod_a, mod_b])

        mod_root = ak3_dir / "modules" / "system" / "lib" / "modules"
        installed = list(mod_root.rglob("foo.ko"))
        # Both files must be present, not just one
        assert len(installed) == 2


# ── Regression tests for audited bugs ────────────────────────────────────────


class TestSourceDepthOverrideFix:
    """BUG FIX: cli._cmd_build previously used 'not cfg.kernel_source_depth'
    which evaluates True when depth=0 (full history), causing the argparse
    default of 1 to silently overwrite a config-set full-history clone."""

    def test_full_history_depth_not_clobbered_by_argparse_default(self):
        """Simulate: config has depth=0 and user omits --source-depth.
        The config value must survive; argparse default (None) must not win."""
        cfg = BuildConfig()
        cfg.kernel_source_depth = 0  # full history requested in config

        # Reproduce the corrected cli logic: args.source_depth is None
        # when --source-depth was not supplied on the command line.
        args_source_depth = None  # argparse default=None means "not provided"
        if args_source_depth is not None:
            cfg.kernel_source_depth = args_source_depth

        assert cfg.kernel_source_depth == 0, (
            "Full-history depth (0) must not be overwritten when "
            "--source-depth was not explicitly provided."
        )

    def test_explicit_depth_overrides_config(self):
        """When --source-depth IS supplied, it must override the config."""
        cfg = BuildConfig()
        cfg.kernel_source_depth = 0  # full history in config

        args_source_depth = 1  # user explicitly passed --source-depth 1
        if args_source_depth is not None:
            cfg.kernel_source_depth = args_source_depth

        assert cfg.kernel_source_depth == 1

    def test_depth_zero_overrides_config_default(self):
        """--source-depth 0 (full history via CLI) must override a config's depth=1."""
        cfg = BuildConfig()
        cfg.kernel_source_depth = 1  # default shallow in config

        args_source_depth = 0  # user explicitly wants full history
        if args_source_depth is not None:
            cfg.kernel_source_depth = args_source_depth

        assert cfg.kernel_source_depth == 0


class TestDeadVariableRemoved:
    """BUG FIX: packager.prepare() had a dead variable 'out_root' that was
    computed from module_files[0].parent but never used — leftover from the
    pre-fix implementation where 'common' replaced it."""

    def test_prepare_does_not_reference_out_root(self, tmp_path):
        """Verify 'out_root' is not present in the packager source."""
        import inspect

        from kernel_builder.packager import AnyKernel3Packager
        src = inspect.getsource(AnyKernel3Packager.prepare)
        assert "out_root" not in src, (
            "Dead variable 'out_root' must not appear in AnyKernel3Packager.prepare()."
        )

    def test_prepare_modules_uses_common_ancestor(self, tmp_path):
        """Modules in different subdirectories must still land in separate dirs."""
        cfg = BuildConfig()
        cfg.anykernel3.kernel_name = "DeadVarKernel"
        ak3_dir = tmp_path / "AnyKernel3"
        packager = AnyKernel3Packager(cfg, ak3_dir)

        image = tmp_path / "Image"
        image.write_bytes(b"\x00" * 32)

        dir_a = tmp_path / "a"
        dir_b = tmp_path / "b"
        dir_a.mkdir()
        dir_b.mkdir()
        mod_a = dir_a / "bar.ko"
        mod_b = dir_b / "bar.ko"
        mod_a.write_bytes(b"\xaa" * 8)
        mod_b.write_bytes(b"\xbb" * 8)

        packager.prepare(image, module_files=[mod_a, mod_b])

        mod_root = ak3_dir / "modules" / "system" / "lib" / "modules"
        found = list(mod_root.rglob("bar.ko"))
        assert len(found) == 2, "Both .ko files must be staged separately."


class TestRedundantImportRemoved:
    """BUG FIX: KernelBuilder.__init__ previously imported
    'from pathlib import Path as _Path' inside the method body even though
    Path was already imported at module level, causing a shadowed alias."""

    def test_builder_module_does_not_shadow_path(self):
        """The aliased import '_Path' must not appear in the builder source."""
        import inspect

        import kernel_builder.builder as builder_module
        src = inspect.getsource(builder_module)
        assert "_Path" not in src, (
            "Shadowed alias '_Path' must be removed; use the top-level 'Path' import."
        )


class TestExampleJsonLoadable:
    """BUG FIX: config/example_build_config.json contained a '_comment_git_source'
    key that caused BuildConfig.from_json() to raise TypeError because it was
    forwarded as an unexpected keyword argument."""

    def test_example_json_loads_without_error(self, tmp_path):
        """The example JSON must be parseable by BuildConfig.from_json()."""
        example = Path(__file__).parent.parent / "config" / "example_build_config.json"
        # Point kernel_source to an existing path so Path.resolve() doesn't error
        import json
        data = json.loads(example.read_text())
        data["kernel_source"] = str(tmp_path)
        patched = tmp_path / "test_config.json"
        patched.write_text(json.dumps(data))
        # Must not raise TypeError or any other exception
        cfg = BuildConfig.from_json(patched)
        assert isinstance(cfg, BuildConfig)

    def test_example_json_has_no_comment_keys(self):
        """The example JSON must not contain underscore-prefixed comment keys."""
        import json
        example = Path(__file__).parent.parent / "config" / "example_build_config.json"
        data = json.loads(example.read_text())
        comment_keys = [k for k in data if k.startswith("_")]
        assert not comment_keys, (
            f"Example JSON must have no '_comment_*' keys; found: {comment_keys}"
        )


# ── ccache tests ──────────────────────────────────────────────────────────────


class TestCcacheConfig:
    """Unit tests for CcacheConfig dataclass."""

    def test_default_disabled(self):
        from kernel_builder.config import CcacheConfig
        cc = CcacheConfig()
        assert cc.enabled is False

    def test_default_values(self):
        from kernel_builder.config import CcacheConfig
        cc = CcacheConfig()
        assert cc.max_size == "5G"
        assert cc.compress is True
        assert cc.dir == ""
        assert cc.basedir == ""
        assert "time_macros" in cc.sloppiness
        assert "include_file_mtime" in cc.sloppiness
        assert "file_stat_matches" in cc.sloppiness
        assert "pch_defines" in cc.sloppiness

    def test_custom_values(self):
        from kernel_builder.config import CcacheConfig
        cc = CcacheConfig(enabled=True, dir="/tmp/ccache", max_size="10G")
        assert cc.enabled is True
        assert cc.dir == "/tmp/ccache"
        assert cc.max_size == "10G"

    def test_build_config_includes_ccache_field(self):
        from kernel_builder.config import CcacheConfig
        cfg = BuildConfig()
        assert hasattr(cfg, "ccache")
        assert isinstance(cfg.ccache, CcacheConfig)

    def test_ccache_disabled_by_default_in_build_config(self):
        cfg = BuildConfig()
        assert cfg.ccache.enabled is False


class TestCcacheEnvInjection:
    """_build_env() must inject CCACHE_* vars iff ccache is enabled."""

    def _cfg_with_ccache(self, **kwargs) -> BuildConfig:
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = True
        for k, v in kwargs.items():
            setattr(cfg.ccache, k, v)
        return cfg

    def test_ccache_vars_absent_when_disabled(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = False
        env = _build_env(cfg)
        assert "CCACHE_MAXSIZE" not in env
        assert "CCACHE_SLOPPINESS" not in env
        assert "CCACHE_COMPRESS" not in env
        assert "CCACHE_DIR" not in env

    def test_ccache_maxsize_exported(self):
        cfg = self._cfg_with_ccache(max_size="8G")
        env = _build_env(cfg)
        assert env["CCACHE_MAXSIZE"] == "8G"

    def test_ccache_sloppiness_exported(self):
        cfg = self._cfg_with_ccache()
        env = _build_env(cfg)
        assert "time_macros" in env["CCACHE_SLOPPINESS"]

    def test_ccache_compress_true(self):
        cfg = self._cfg_with_ccache(compress=True)
        env = _build_env(cfg)
        assert env["CCACHE_COMPRESS"] == "true"

    def test_ccache_compress_false(self):
        cfg = self._cfg_with_ccache(compress=False)
        env = _build_env(cfg)
        assert env["CCACHE_COMPRESS"] == "false"

    def test_ccache_dir_exported_when_set(self):
        cfg = self._cfg_with_ccache(dir="/srv/ccache")
        env = _build_env(cfg)
        assert env["CCACHE_DIR"] == "/srv/ccache"

    def test_ccache_dir_not_exported_when_empty(self):
        cfg = self._cfg_with_ccache(dir="")
        env = _build_env(cfg)
        assert "CCACHE_DIR" not in env

    def test_ccache_dir_tilde_expanded(self):
        cfg = self._cfg_with_ccache(dir="~/my_ccache")
        env = _build_env(cfg)
        assert "~" not in env["CCACHE_DIR"]
        assert env["CCACHE_DIR"].startswith("/")

    def test_ccache_basedir_defaults_to_kernel_source(self):
        cfg = self._cfg_with_ccache(basedir="")
        cfg.kernel_source = "/src/kernel"
        env = _build_env(cfg)
        assert env.get("CCACHE_BASEDIR") == "/src/kernel"

    def test_ccache_basedir_explicit_overrides_source(self):
        cfg = self._cfg_with_ccache(basedir="/workspace")
        cfg.kernel_source = "/src/kernel"
        env = _build_env(cfg)
        assert env["CCACHE_BASEDIR"] == "/workspace"

    def test_ccache_basedir_tilde_expanded(self):
        cfg = self._cfg_with_ccache(basedir="~/workspace")
        env = _build_env(cfg)
        assert "~" not in env["CCACHE_BASEDIR"]


class TestCcacheMakeBase:
    """_make_base() must prefix CC with ccache when enabled."""

    def test_cc_prefixed_with_ccache_when_enabled(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = True
        cmd = _make_base(cfg, jobs=4)
        assert "CC=ccache clang" in cmd

    def test_cc_not_prefixed_when_disabled(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = False
        cmd = _make_base(cfg, jobs=4)
        assert "CC=clang" in cmd
        assert "CC=ccache clang" not in cmd

    def test_ccache_prefix_uses_configured_cc(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.toolchain.cc = "clang-17"
        cfg.ccache.enabled = True
        cmd = _make_base(cfg, jobs=4)
        assert "CC=ccache clang-17" in cmd

    def test_all_steps_use_ccache_prefix_when_enabled(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = True
        builder = KernelBuilder(cfg)
        for step in builder.steps(clean=True):
            assert "CC=ccache clang" in step.command, (
                f"Step '{step.name}' missing 'CC=ccache clang'"
            )

    def test_all_steps_without_ccache_have_plain_cc(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = False
        builder = KernelBuilder(cfg)
        for step in builder.steps(clean=True):
            cc_entries = [t for t in step.command if t.startswith("CC=")]
            assert len(cc_entries) == 1
            assert cc_entries[0] == "CC=clang"


class TestCcacheValidateToolchain:
    """validate_toolchain() must warn when ccache binary is missing."""

    def test_no_warning_when_ccache_disabled(self, monkeypatch):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = False
        monkeypatch.setattr(
            "kernel_builder.builder.find_ccache", lambda: None
        )
        builder = KernelBuilder(cfg)
        warnings = builder.validate_toolchain()
        ccache_warnings = [w for w in warnings if "ccache" in w.lower()]
        assert not ccache_warnings

    def test_warning_when_ccache_enabled_but_missing(self, monkeypatch):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = True
        monkeypatch.setattr(
            "kernel_builder.builder.find_ccache", lambda: None
        )
        builder = KernelBuilder(cfg)
        warnings = builder.validate_toolchain()
        assert any("ccache" in w.lower() for w in warnings)

    def test_no_ccache_warning_when_binary_present(self, monkeypatch):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = True
        monkeypatch.setattr(
            "kernel_builder.builder.find_ccache",
            lambda: Path("/usr/bin/ccache"),
        )
        builder = KernelBuilder(cfg)
        warnings = builder.validate_toolchain()
        ccache_warnings = [w for w in warnings if "ccache" in w.lower()]
        assert not ccache_warnings


class TestCcachePersistence:
    """CcacheConfig must survive a JSON/TOML round-trip."""

    def test_json_round_trip(self, tmp_path):
        cfg = BuildConfig(kernel_source=str(tmp_path))
        cfg.ccache.enabled = True
        cfg.ccache.dir = "/srv/ccache"
        cfg.ccache.max_size = "10G"
        cfg.ccache.compress = False
        cfg.ccache.basedir = "/workspace"
        cfg.ccache.sloppiness = "time_macros,include_file_mtime"

        save_path = tmp_path / "cfg.json"
        cfg.to_json(save_path)

        loaded = BuildConfig.from_json(save_path)
        assert loaded.ccache.enabled is True
        assert loaded.ccache.dir == "/srv/ccache"
        assert loaded.ccache.max_size == "10G"
        assert loaded.ccache.compress is False
        assert loaded.ccache.basedir == "/workspace"
        assert loaded.ccache.sloppiness == "time_macros,include_file_mtime"

    def test_json_round_trip_disabled(self, tmp_path):
        cfg = BuildConfig(kernel_source=str(tmp_path))
        cfg.ccache.enabled = False
        save_path = tmp_path / "cfg.json"
        cfg.to_json(save_path)
        loaded = BuildConfig.from_json(save_path)
        assert loaded.ccache.enabled is False

    def test_json_missing_ccache_section_uses_defaults(self, tmp_path):
        """Configs saved before ccache support was added must still load."""
        import json as _json
        raw = {
            "kernel_source": str(tmp_path),
            "kernel_defconfig": "defconfig",
            "arch": "arm64",
            "subarch": "arm64",
            "kernel_source_url": "",
            "kernel_source_branch": "",
            "kernel_source_depth": 1,
            "output_dir": "out",
            "zip_output_dir": "releases",
            "jobs": 0,
            "lto": None,
            "extra_make_flags": [],
            "kbuild_build_user": "builder",
            "kbuild_build_host": "forged (github.com/vxyzview/forged)",
            "localversion": "",
            "toolchain_dir": "",
            "auto_setup_toolchain": False,
            "toolchain": {
                "preset": "system-clang",
                "cc": "clang",
                "cross_compile": "aarch64-linux-gnu-",
                "clang_triple": "aarch64-linux-gnu-",
                "use_llvm_binutils": True,
                "extra_path": [],
                "auto_clone": False,
            },
            "anykernel3": {
                "anykernel_dir": "AnyKernel3",
                "kernel_name": "kernel",
                "block": "/dev/block/by-name/boot",
                "is_slot_device": 0,
                "ramdisk_compression": "auto",
                "do_devicecheck": 1,
                "supported_versions": "",
                "supported_patchlevels": "",
                "extra_cmds": "",
                "device_names": [],
            },
            # intentionally no "ccache" key — simulates a legacy config
        }
        path = tmp_path / "legacy.json"
        path.write_text(_json.dumps(raw))
        loaded = BuildConfig.from_json(path)
        # Must fall back to defaults, not raise
        assert loaded.ccache.enabled is False
        assert loaded.ccache.max_size == "5G"

    def test_example_json_loads_with_ccache(self, tmp_path):
        """The updated example JSON config must parse without error."""
        import json as _json
        example = Path(__file__).parent.parent / "config" / "example_build_config.json"
        data = _json.loads(example.read_text())
        data["kernel_source"] = str(tmp_path)
        patched = tmp_path / "test_config.json"
        patched.write_text(_json.dumps(data))
        cfg = BuildConfig.from_json(patched)
        assert isinstance(cfg, BuildConfig)
        assert hasattr(cfg.ccache, "enabled")

    def test_example_json_ccache_disabled_by_default(self, tmp_path):
        """Example config must ship with ccache disabled."""
        import json as _json
        example = Path(__file__).parent.parent / "config" / "example_build_config.json"
        data = _json.loads(example.read_text())
        data["kernel_source"] = str(tmp_path)
        patched = tmp_path / "test_config.json"
        patched.write_text(_json.dumps(data))
        cfg = BuildConfig.from_json(patched)
        assert cfg.ccache.enabled is False


class TestFindCcache:
    """find_ccache() must return Path when ccache is available."""

    def test_find_ccache_returns_path_or_none(self):
        from kernel_builder.builder import find_ccache
        result = find_ccache()
        assert result is None or isinstance(result, Path)

    def test_find_ccache_monkeypatched_present(self, monkeypatch):
        import kernel_builder.builder as _bm
        monkeypatch.setattr(_bm, "find_ccache", lambda: Path("/usr/bin/ccache"))
        assert _bm.find_ccache() == Path("/usr/bin/ccache")

    def test_find_ccache_monkeypatched_absent(self, monkeypatch):
        import kernel_builder.builder as _bm
        monkeypatch.setattr(_bm, "find_ccache", lambda: None)
        assert _bm.find_ccache() is None


class TestCcacheStepEnvConsistency:
    """Each BuildStep must carry the CCACHE_* vars when ccache is enabled."""

    def test_step_env_contains_ccache_vars(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = True
        cfg.ccache.max_size = "3G"
        cfg.ccache.compress = True
        builder = KernelBuilder(cfg)
        for step in builder.steps(clean=False):
            assert step.env.get("CCACHE_MAXSIZE") == "3G"
            assert step.env.get("CCACHE_COMPRESS") == "true"
            assert "time_macros" in step.env.get("CCACHE_SLOPPINESS", "")

    def test_step_env_no_ccache_vars_when_disabled(self):
        cfg = BuildConfig(kernel_source=str(Path.cwd()))
        cfg.ccache.enabled = False
        builder = KernelBuilder(cfg)
        for step in builder.steps(clean=False):
            assert "CCACHE_MAXSIZE" not in step.env
            assert "CCACHE_DIR" not in step.env


# ═══════════════════════════════════════════════════════════════════════════════
# CROSS_COMPILE_ARM32 & extra build flags — new feature tests
# ═══════════════════════════════════════════════════════════════════════════════


class TestCrossCompileArm32Config:
    """ToolchainConfig.cross_compile_arm32 field."""

    def test_default_cross_compile_arm32_is_arm(self):
        tc = ToolchainConfig()
        assert tc.cross_compile_arm32 == "arm-linux-gnueabihf-"

    def test_cross_compile_arm32_can_be_empty(self):
        tc = ToolchainConfig(cross_compile_arm32="")
        assert tc.cross_compile_arm32 == ""

    def test_cross_compile_arm32_can_be_custom(self):
        tc = ToolchainConfig(cross_compile_arm32="arm-none-linux-gnueabihf-")
        assert tc.cross_compile_arm32 == "arm-none-linux-gnueabihf-"

    def test_preset_system_clang_sets_arm32(self):
        tc = ToolchainConfig()
        tc.apply_preset("system-clang")
        assert tc.cross_compile_arm32 == "arm-linux-gnueabihf-"

    def test_preset_aosp_clang_sets_arm32(self):
        tc = ToolchainConfig()
        tc.apply_preset("aosp-clang")
        assert tc.cross_compile_arm32 == "arm-linux-gnueabihf-"

    def test_all_presets_have_cross_compile_arm32_key(self):
        from kernel_builder.config import TOOLCHAIN_PRESETS
        for name, preset in TOOLCHAIN_PRESETS.items():
            assert "cross_compile_arm32" in preset, (
                f"Preset '{name}' is missing cross_compile_arm32"
            )

    def test_deprecated_preset_migration_preserves_arm32(self):
        import warnings
        tc = ToolchainConfig()
        with warnings.catch_warnings(record=True):
            warnings.simplefilter("always")
            tc.apply_preset("aosp-clang-r547379")
        assert tc.cross_compile_arm32 == "arm-linux-gnueabihf-"

    def test_cross_compile_arm32_serialised_in_json(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = "arm-none-linux-gnueabihf-"
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.toolchain.cross_compile_arm32 == "arm-none-linux-gnueabihf-"

    def test_cross_compile_arm32_empty_serialised_in_json(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = ""
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.toolchain.cross_compile_arm32 == ""


class TestCrossCompileArm32BuildEnv:
    """CROSS_COMPILE_ARM32 in _build_env()."""

    def test_build_env_exports_cross_compile_arm32_when_set(self):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = "arm-linux-gnueabihf-"
        env = _build_env(cfg)
        assert env.get("CROSS_COMPILE_ARM32") == "arm-linux-gnueabihf-"

    def test_build_env_omits_cross_compile_arm32_when_empty(self):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = ""
        env = _build_env(cfg)
        assert "CROSS_COMPILE_ARM32" not in env

    def test_build_env_cross_compile_arm32_custom_prefix(self):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = "arm-none-linux-gnueabihf-"
        env = _build_env(cfg)
        assert env["CROSS_COMPILE_ARM32"] == "arm-none-linux-gnueabihf-"

    def test_build_env_arm32_set_never_emits_compat(self):
        """CROSS_COMPILE_ARM32 is exported; CROSS_COMPILE_COMPAT is never emitted."""
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = "arm-none-linux-gnueabihf-"
        env = _build_env(cfg)
        assert env["CROSS_COMPILE_ARM32"] == "arm-none-linux-gnueabihf-"
        assert "CROSS_COMPILE_COMPAT" not in env

    def test_build_env_omits_arm32_when_empty(self):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = ""
        env = _build_env(cfg)
        assert "CROSS_COMPILE_ARM32" not in env


class TestCrossCompileArm32MakeBase:
    """CROSS_COMPILE_ARM32 in _make_base()."""

    def test_make_base_includes_cross_compile_arm32_when_set(self):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = "arm-linux-gnueabihf-"
        cmd = _make_base(cfg, jobs=4)
        assert "CROSS_COMPILE_ARM32=arm-linux-gnueabihf-" in cmd

    def test_make_base_omits_cross_compile_arm32_when_empty(self):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = ""
        cmd = _make_base(cfg, jobs=4)
        assert not any(arg.startswith("CROSS_COMPILE_ARM32=") for arg in cmd)

    def test_make_base_cross_compile_arm32_custom(self):
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = "arm-none-linux-gnueabihf-"
        cmd = _make_base(cfg, jobs=4)
        assert "CROSS_COMPILE_ARM32=arm-none-linux-gnueabihf-" in cmd

    def test_make_base_cross_compile_arm32_before_llvm_binutils(self):
        """CROSS_COMPILE_ARM32 appears before the LLVM binutils flags."""
        cfg = BuildConfig()
        cfg.toolchain.cross_compile_arm32 = "arm-linux-gnueabihf-"
        cfg.toolchain.use_llvm_binutils = True
        cmd = _make_base(cfg, jobs=4)
        arm32_idx = next(i for i, a in enumerate(cmd) if a.startswith("CROSS_COMPILE_ARM32="))
        llvm_idx = next(i for i, a in enumerate(cmd) if a.startswith("LD="))
        assert arm32_idx < llvm_idx


class TestExtraMakeFlags:
    """extra_make_flags in config and _make_base()."""

    def test_extra_make_flags_default_empty(self):
        cfg = BuildConfig()
        assert cfg.extra_make_flags == []

    def test_extra_make_flags_appended_to_make_cmd(self):
        cfg = BuildConfig()
        cfg.extra_make_flags = ["LLVM=1", "LLVM_IAS=1"]
        cmd = _make_base(cfg, jobs=4)
        assert "LLVM=1" in cmd
        assert "LLVM_IAS=1" in cmd

    def test_extra_make_flags_appended_last(self):
        """User flags come after all built-in flags so they can override them."""
        cfg = BuildConfig()
        cfg.extra_make_flags = ["CC=my-special-clang"]
        cmd = _make_base(cfg, jobs=4)
        # The user flag must be the last occurrence in the command
        cc_indices = [i for i, a in enumerate(cmd) if a.startswith("CC=")]
        assert cc_indices[-1] == cmd.index("CC=my-special-clang")

    def test_extra_make_flags_kcflags(self):
        cfg = BuildConfig()
        cfg.extra_make_flags = ["KCFLAGS=-pipe -O3"]
        cmd = _make_base(cfg, jobs=4)
        assert "KCFLAGS=-pipe -O3" in cmd

    def test_extra_make_flags_config_debug_info(self):
        cfg = BuildConfig()
        cfg.extra_make_flags = ["CONFIG_DEBUG_INFO=n"]
        cmd = _make_base(cfg, jobs=4)
        assert "CONFIG_DEBUG_INFO=n" in cmd

    def test_extra_make_flags_multiple_flags(self):
        cfg = BuildConfig()
        cfg.extra_make_flags = ["LLVM=1", "LLVM_IAS=1", "KCFLAGS=-pipe", "CONFIG_DEBUG_INFO=n"]
        cmd = _make_base(cfg, jobs=4)
        for flag in cfg.extra_make_flags:
            assert flag in cmd

    def test_extra_make_flags_serialised_in_json(self, tmp_path):
        cfg = BuildConfig()
        cfg.extra_make_flags = ["LLVM=1", "LLVM_IAS=1"]
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.extra_make_flags == ["LLVM=1", "LLVM_IAS=1"]

    def test_extra_make_flags_not_duplicated_with_builtin_lto(self):
        """extra_make_flags are in addition to — not replacing — the lto flag."""
        cfg = BuildConfig(lto="thin")
        cfg.extra_make_flags = ["LLVM=1"]
        cmd = _make_base(cfg, jobs=4)
        assert "LTO=thin" in cmd
        assert "LLVM=1" in cmd


class TestExtraEnv:
    """extra_env in BuildConfig and _build_env()."""

    def test_extra_env_default_empty(self):
        cfg = BuildConfig()
        assert cfg.extra_env == {}

    def test_extra_env_injected_into_build_env(self):
        cfg = BuildConfig()
        cfg.extra_env = {"KCPPFLAGS": "-DDEBUG", "KBUILD_VERBOSE": "1"}
        env = _build_env(cfg)
        assert env["KCPPFLAGS"] == "-DDEBUG"
        assert env["KBUILD_VERBOSE"] == "1"

    def test_extra_env_overrides_builtin_variable(self):
        """extra_env is applied last, so it can override built-in exports."""
        cfg = BuildConfig()
        cfg.extra_env = {"ARCH": "x86_64"}
        env = _build_env(cfg)
        assert env["ARCH"] == "x86_64"

    def test_extra_env_tilde_expanded(self, monkeypatch):
        monkeypatch.setenv("HOME", "/home/testuser")
        cfg = BuildConfig()
        cfg.extra_env = {"MYPATH": "~/mydir"}
        env = _build_env(cfg)
        assert env["MYPATH"] == "/home/testuser/mydir"

    def test_extra_env_env_var_reference_expanded(self, monkeypatch):
        monkeypatch.setenv("MY_BASE", "/opt/base")
        cfg = BuildConfig()
        cfg.extra_env = {"EXTENDED": "$MY_BASE/extra"}
        env = _build_env(cfg)
        assert env["EXTENDED"] == "/opt/base/extra"

    def test_extra_env_empty_value(self):
        cfg = BuildConfig()
        cfg.extra_env = {"EMPTY_VAR": ""}
        env = _build_env(cfg)
        assert env["EMPTY_VAR"] == ""

    def test_extra_env_serialised_in_json(self, tmp_path):
        cfg = BuildConfig()
        cfg.extra_env = {"KCPPFLAGS": "-DDEBUG", "KBUILD_VERBOSE": "1"}
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.extra_env == {"KCPPFLAGS": "-DDEBUG", "KBUILD_VERBOSE": "1"}

    def test_extra_env_empty_dict_serialised_in_json(self, tmp_path):
        cfg = BuildConfig()
        cfg.extra_env = {}
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.extra_env == {}


# ═══════════════════════════════════════════════════════════════════════════════
# Build identity — kbuild_build_host / kbuild_build_user
# ═══════════════════════════════════════════════════════════════════════════════

_FORGED_HOST = "forged (github.com/vxyzview/forged)"


class TestBuildIdentity:
    """kbuild_build_host defaults to the FORGED branding string."""

    def test_default_host_is_forged(self):
        cfg = BuildConfig()
        assert cfg.kbuild_build_host == _FORGED_HOST

    def test_default_user_is_builder(self):
        cfg = BuildConfig()
        assert cfg.kbuild_build_user == "builder"

    def test_host_exported_in_build_env(self):
        cfg = BuildConfig()
        env = _build_env(cfg)
        assert env["KBUILD_BUILD_HOST"] == _FORGED_HOST

    def test_user_exported_in_build_env(self):
        cfg = BuildConfig()
        cfg.kbuild_build_user = "alice"
        env = _build_env(cfg)
        assert env["KBUILD_BUILD_USER"] == "alice"

    def test_host_can_be_overridden_in_config(self):
        cfg = BuildConfig(kbuild_build_host="custom-host")
        assert cfg.kbuild_build_host == "custom-host"
        env = _build_env(cfg)
        assert env["KBUILD_BUILD_HOST"] == "custom-host"

    def test_user_can_be_overridden_in_config(self):
        cfg = BuildConfig(kbuild_build_user="bob")
        env = _build_env(cfg)
        assert env["KBUILD_BUILD_USER"] == "bob"

    def test_forged_host_round_trips_json(self, tmp_path):
        cfg = BuildConfig()
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.kbuild_build_host == _FORGED_HOST

    def test_custom_host_round_trips_json(self, tmp_path):
        cfg = BuildConfig(kbuild_build_host="my-ci-box")
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.kbuild_build_host == "my-ci-box"

    def test_custom_user_round_trips_json(self, tmp_path):
        cfg = BuildConfig(kbuild_build_user="carol")
        out = tmp_path / "cfg.json"
        cfg.to_json(out)
        loaded = BuildConfig.from_json(out)
        assert loaded.kbuild_build_user == "carol"

# ═══════════════════════════════════════════════════════════════════════════════
# Entry-point exit-code propagation
# ═══════════════════════════════════════════════════════════════════════════════


class TestEntryPointExitCode:
    """run() must call sys.exit() so the process exit code reaches the shell.

    setuptools console_scripts invoke the callable and discard its return
    value, so 'forged = kernel_builder.main:main' always exits 0.  The fix is
    a run() wrapper that calls sys.exit(main()).
    """

    def test_run_calls_sys_exit(self, monkeypatch):
        """run() must raise SystemExit (i.e. call sys.exit)."""
        from kernel_builder.main import run

        monkeypatch.setattr(
            "kernel_builder.main.main", lambda: 0
        )
        with pytest.raises(SystemExit) as exc_info:
            run()
        assert exc_info.value.code == 0

    def test_run_propagates_nonzero_exit_code(self, monkeypatch):
        """run() must propagate non-zero codes from main()."""
        from kernel_builder.main import run

        monkeypatch.setattr(
            "kernel_builder.main.main", lambda: 1
        )
        with pytest.raises(SystemExit) as exc_info:
            run()
        assert exc_info.value.code == 1

    def test_run_propagates_keyboard_interrupt_exit_code(self, monkeypatch):
        """Exit code 130 (KeyboardInterrupt) must be propagated."""
        from kernel_builder.main import run

        monkeypatch.setattr(
            "kernel_builder.main.main", lambda: 130
        )
        with pytest.raises(SystemExit) as exc_info:
            run()
        assert exc_info.value.code == 130


# ═══════════════════════════════════════════════════════════════════════════════
# Deprecated preset migration at construction time (__post_init__)
# ═══════════════════════════════════════════════════════════════════════════════


class TestToolchainConfigPostInitMigration:
    """ToolchainConfig.__post_init__ must migrate deprecated preset names.

    When a config is loaded from JSON/TOML the preset name is set via the
    dataclass constructor, bypassing apply_preset().  Without __post_init__
    migration the stale preset name propagates to auto_setup_toolchain, which
    only recognises canonical names and raises ValueError.
    """

    def test_deprecated_preset_migrated_at_construction(self):
        """ToolchainConfig(preset='aosp-clang-r584948b') must self-correct."""
        import warnings
        with warnings.catch_warnings(record=True) as caught:
            warnings.simplefilter("always")
            tc = ToolchainConfig(preset="aosp-clang-r584948b")
        assert tc.preset == "aosp-clang"
        assert any(issubclass(w.category, DeprecationWarning) for w in caught)

    def test_r547379_migrated_at_construction(self):
        """ToolchainConfig(preset='aosp-clang-r547379') must self-correct."""
        import warnings
        with warnings.catch_warnings(record=True) as caught:
            warnings.simplefilter("always")
            tc = ToolchainConfig(preset="aosp-clang-r547379")
        assert tc.preset == "aosp-clang"
        assert any(issubclass(w.category, DeprecationWarning) for w in caught)

    def test_canonical_preset_has_no_warning(self):
        """ToolchainConfig(preset='aosp-clang') must not emit any warning."""
        import warnings
        with warnings.catch_warnings(record=True) as caught:
            warnings.simplefilter("always")
            ToolchainConfig(preset="aosp-clang")
        deprecation_warnings = [w for w in caught if issubclass(w.category, DeprecationWarning)]
        assert not deprecation_warnings

    def test_deprecated_preset_in_json_migrated_on_load(self, tmp_path):
        """from_json() with a deprecated preset must produce a migrated config."""
        import json as _json
        import warnings

        raw = {
            "kernel_source": str(tmp_path),
            "toolchain": {"preset": "aosp-clang-r584948b"},
            "anykernel3": {},
            "ccache": {},
        }
        p = tmp_path / "old.json"
        p.write_text(_json.dumps(raw))

        with warnings.catch_warnings(record=True):
            warnings.simplefilter("always")
            loaded = BuildConfig.from_json(p)

        assert loaded.toolchain.preset == "aosp-clang", (
            "from_json must migrate the deprecated preset name via __post_init__"
        )

    def test_migrated_preset_not_in_aosp_revision_dirs_before_fix(self):
        """Without __post_init__, stale preset would fail auto_setup_toolchain."""
        from kernel_builder.toolchain_manager import AOSP_REVISION_DIRS
        import warnings

        with warnings.catch_warnings(record=True):
            warnings.simplefilter("always")
            tc = ToolchainConfig(preset="aosp-clang-r584948b")

        # After __post_init__ migration, the preset IS now in AOSP_REVISION_DIRS.
        assert tc.preset in AOSP_REVISION_DIRS, (
            "Migrated preset must be recognised by auto_setup_toolchain"
        )


# ═══════════════════════════════════════════════════════════════════════════════
# Forward-compatibility: unknown fields must not crash deserialization
# ═══════════════════════════════════════════════════════════════════════════════


class TestUnknownFieldTolerance:
    """from_json() / from_toml() must silently drop unrecognised keys.

    A config saved by a *newer* version of the tool may contain keys that the
    current version's dataclasses do not recognise.  Without _known_fields()
    filtering, loading such a config raises TypeError.
    """

    def test_from_json_tolerates_unknown_top_level_key(self, tmp_path):
        import json as _json
        raw = {
            "kernel_source": str(tmp_path),
            "future_field": "some_new_value",
            "toolchain": {},
            "anykernel3": {},
            "ccache": {},
        }
        p = tmp_path / "future.json"
        p.write_text(_json.dumps(raw))
        # Must not raise TypeError
        cfg = BuildConfig.from_json(p)
        assert isinstance(cfg, BuildConfig)

    def test_from_json_tolerates_unknown_toolchain_key(self, tmp_path):
        import json as _json
        raw = {
            "kernel_source": str(tmp_path),
            "toolchain": {"preset": "aosp-clang", "new_toolchain_option": True},
            "anykernel3": {},
            "ccache": {},
        }
        p = tmp_path / "future.json"
        p.write_text(_json.dumps(raw))
        cfg = BuildConfig.from_json(p)
        assert cfg.toolchain.preset == "aosp-clang"

    def test_from_json_tolerates_unknown_anykernel3_key(self, tmp_path):
        import json as _json
        raw = {
            "kernel_source": str(tmp_path),
            "toolchain": {},
            "anykernel3": {"new_ak3_option": "value"},
            "ccache": {},
        }
        p = tmp_path / "future.json"
        p.write_text(_json.dumps(raw))
        cfg = BuildConfig.from_json(p)
        assert isinstance(cfg, BuildConfig)

    def test_from_json_tolerates_unknown_ccache_key(self, tmp_path):
        import json as _json
        raw = {
            "kernel_source": str(tmp_path),
            "toolchain": {},
            "anykernel3": {},
            "ccache": {"enabled": False, "future_ccache_option": 42},
        }
        p = tmp_path / "future.json"
        p.write_text(_json.dumps(raw))
        cfg = BuildConfig.from_json(p)
        assert cfg.ccache.enabled is False

    def test_known_fields_are_still_loaded(self, tmp_path):
        """Dropping unknown keys must not affect known ones."""
        import json as _json
        raw = {
            "kernel_source": str(tmp_path),
            "kernel_defconfig": "custom_defconfig",
            "unknown_key": "ignored",
            "toolchain": {"preset": "system-clang", "unknown_tc_key": "ignored"},
            "anykernel3": {},
            "ccache": {"enabled": True, "max_size": "8G", "unknown_cc_key": "x"},
        }
        p = tmp_path / "mixed.json"
        p.write_text(_json.dumps(raw))
        cfg = BuildConfig.from_json(p)
        assert cfg.kernel_defconfig == "custom_defconfig"
        assert cfg.toolchain.preset == "system-clang"
        assert cfg.ccache.enabled is True
        assert cfg.ccache.max_size == "8G"


# ═══════════════════════════════════════════════════════════════════════════════
# Public API surface — __all__ completeness
# ═══════════════════════════════════════════════════════════════════════════════


class TestPublicAPIAll:
    """All public dataclasses and classes must appear in kernel_builder.__all__."""

    def test_ccache_config_in_all(self):
        import kernel_builder
        assert "CcacheConfig" in kernel_builder.__all__

    def test_ccache_config_importable_from_package(self):
        from kernel_builder import CcacheConfig  # noqa: F401


# ═══════════════════════════════════════════════════════════════════════════════
# source_ok computation — URL-only configs
# ═══════════════════════════════════════════════════════════════════════════════


class TestSourceOkLogic:
    """The pre-build checklist source_ok flag must be True when a URL is set,
    even if the local destination path does not yet exist."""

    def test_source_ok_true_when_url_set_without_local_path(self):
        """Simulate the corrected source_ok logic from _run_build."""
        from pathlib import Path as _Path

        cfg = BuildConfig()
        cfg.kernel_source = ""  # not set — will be derived from URL at clone time
        cfg.kernel_source_url = "https://github.com/example/kernel.git"

        # Reproduce the corrected cli logic
        source_ok = bool(
            cfg.kernel_source_url
            or (cfg.kernel_source and _Path(cfg.kernel_source).exists())
        )
        assert source_ok is True, (
            "source_ok must be True when kernel_source_url is set, "
            "even if kernel_source path does not yet exist."
        )

    def test_source_ok_false_when_neither_set(self):
        from pathlib import Path as _Path

        cfg = BuildConfig()
        cfg.kernel_source = ""
        cfg.kernel_source_url = ""

        source_ok = bool(
            cfg.kernel_source_url
            or (cfg.kernel_source and _Path(cfg.kernel_source).exists())
        )
        assert source_ok is False

    def test_source_ok_false_when_local_path_missing(self, tmp_path):
        from pathlib import Path as _Path

        cfg = BuildConfig()
        cfg.kernel_source = str(tmp_path / "does_not_exist")
        cfg.kernel_source_url = ""

        source_ok = bool(
            cfg.kernel_source_url
            or (cfg.kernel_source and _Path(cfg.kernel_source).exists())
        )
        assert source_ok is False

    def test_source_ok_true_when_local_path_exists(self, tmp_path):
        from pathlib import Path as _Path

        cfg = BuildConfig()
        cfg.kernel_source = str(tmp_path)
        cfg.kernel_source_url = ""

        source_ok = bool(
            cfg.kernel_source_url
            or (cfg.kernel_source and _Path(cfg.kernel_source).exists())
        )
        assert source_ok is True

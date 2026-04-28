"""
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.

Build configuration — dataclasses + TOML/JSON round-trip.
"""

from __future__ import annotations

import dataclasses
import json
import warnings
from dataclasses import asdict, dataclass, field
from pathlib import Path

try:
    import tomllib  # Python 3.11+
except ModuleNotFoundError:  # pragma: no cover
    try:
        import tomli as tomllib  # type: ignore[no-redef]
    except ModuleNotFoundError:
        tomllib = None  # type: ignore[assignment]

# ── Valid LTO modes ───────────────────────────────────────────────────────────

_VALID_LTO_MODES = frozenset({"thin", "full"})

# ── Toolchain presets (Clang-only) ────────────────────────────────────────────

TOOLCHAIN_PRESETS: dict[str, dict] = {
    "system-clang": {
        "cc": "clang",
        "cross_compile": "aarch64-linux-gnu-",
        "cross_compile_arm32": "arm-linux-gnueabihf-",
        "clang_triple": "aarch64-linux-gnu-",
        "use_llvm_binutils": True,
        "auto_clone": False,
    },
    # Generic AOSP prebuilt Clang — version selected via aosp_clang_version field.
    "aosp-clang": {
        "cc": "clang",
        "cross_compile": "aarch64-linux-gnu-",
        "cross_compile_arm32": "arm-linux-gnueabihf-",
        "clang_triple": "aarch64-linux-gnu-",
        "use_llvm_binutils": True,
        "auto_clone": True,
    },
}

# ── Deprecated preset aliases ─────────────────────────────────────────────────
# Maps an old preset name → the current replacement.
# apply_preset() will emit a DeprecationWarning and transparently migrate.
DEPRECATED_PRESET_ALIASES: dict[str, str] = {
    "aosp-clang-r547379": "aosp-clang",
    # Old version-pinned preset name — replaced by aosp-clang + aosp_clang_version
    "aosp-clang-r584948b": "aosp-clang",
}


# ── Forward-compatibility helper ──────────────────────────────────────────────


def _known_fields(cls: type, data: dict) -> dict:
    """Return only the entries in *data* whose keys match a field of *cls*.

    Silently drops any unknown keys so that configs saved by a *newer* version
    of the tool (which may have added new fields) can still be loaded by an
    *older* version without raising ``TypeError: __init__() got an unexpected
    keyword argument '…'``.
    """
    known = {f.name for f in dataclasses.fields(cls)}
    return {k: v for k, v in data.items() if k in known}


@dataclass
class ToolchainConfig:
    """Compiler / toolchain settings (Clang-only).

    cross_compile vs cross_compile_arm32
    ─────────────────────────────────────
    ``cross_compile``       — arm64 (aarch64) GNU prefix, passed as
                              CROSS_COMPILE to make.  Required for all arm64
                              builds.

    ``cross_compile_arm32`` — Explicit arm32 cross-compiler prefix, passed as
                              CROSS_COMPILE_ARM32.  Required by modern Android
                              kernel trees (GKI, mainline Qualcomm, etc.) when
                              building 32-bit user-space support into an arm64
                              kernel.  Automatically set from the preset; not
                              shown in the interactive wizard.

    aosp_clang_version
    ──────────────────
    When preset is ``aosp-clang``, this field selects which AOSP prebuilt
    Clang revision to download.  The tarball fetched is::

        clang-{aosp_clang_version}.tar.gz

    from the AOSP googlesource archive.  Example valid values: ``r584948b``,
    ``r522817``.  The default is ``r584948b``.
    """

    preset: str = "aosp-clang"
    cc: str = "clang"
    cross_compile: str = "aarch64-linux-gnu-"
    # CROSS_COMPILE_ARM32 — explicit arm32 prefix used by modern Android trees.
    # Empty string → variable is omitted from both the env and make invocation.
    # Automatically configured from the preset; not exposed in the interactive
    # wizard to keep the setup simple.
    cross_compile_arm32: str = "arm-linux-gnueabihf-"
    clang_triple: str = "aarch64-linux-gnu-"
    use_llvm_binutils: bool = True
    extra_path: list[str] = field(default_factory=list)
    auto_clone: bool = True
    # AOSP prebuilt Clang revision — only used when preset == "aosp-clang".
    # Selects which clang-<revision>.tar.gz is fetched from googlesource.
    aosp_clang_version: str = "r584948b"

    def __post_init__(self) -> None:
        """Migrate deprecated preset names when a config is loaded from disk.

        ``apply_preset()`` is only called interactively (wizard) or explicitly
        by callers.  When a config is deserialised from JSON/TOML the preset
        name is set directly via the constructor, bypassing ``apply_preset()``.
        This means that a config saved with an old deprecated preset name (e.g.
        ``"aosp-clang-r584948b"``) would silently carry that stale name into
        ``auto_setup_toolchain``, which only recognises the current canonical
        names and would raise ``ValueError``.

        ``__post_init__`` catches this case: it only updates ``self.preset`` (the
        stale field), leaving all other fields (cc, cross_compile, etc.) exactly
        as they were loaded from the config file — the user's values are correct,
        only the internal preset tag is outdated.
        """
        replacement = DEPRECATED_PRESET_ALIASES.get(self.preset)
        if replacement is not None:
            warnings.warn(
                f"Toolchain preset '{self.preset}' is deprecated. "
                f"Automatically upgrading to '{replacement}'. "
                f"Please update your build config to use '{replacement}'.",
                DeprecationWarning,
                stacklevel=2,
            )
            self.preset = replacement

    def apply_preset(self, name: str) -> None:
        # ── Transparent migration of deprecated preset names ──────────────────
        replacement = DEPRECATED_PRESET_ALIASES.get(name)
        if replacement is not None:
            warnings.warn(
                f"Toolchain preset '{name}' is deprecated. "
                f"Automatically upgrading to '{replacement}'. "
                f"Please update your build config to use '{replacement}'.",
                DeprecationWarning,
                stacklevel=2,
            )
            name = replacement

        preset = TOOLCHAIN_PRESETS.get(name)
        if preset is None:
            raise ValueError(
                f"Unknown toolchain preset '{name}'. "
                f"Available: {list(TOOLCHAIN_PRESETS)}"
            )
        self.preset = name
        for key, value in preset.items():
            setattr(self, key, value)


@dataclass
class CcacheConfig:
    """ccache compiler-cache settings."""

    enabled: bool = False
    dir: str = ""
    max_size: str = "5G"
    sloppiness: str = (
        "time_macros,include_file_mtime,file_stat_matches,pch_defines"
    )
    compress: bool = True
    basedir: str = ""


@dataclass
class AnyKernel3Config:
    """AnyKernel3 packaging settings."""

    anykernel_dir: str = "AnyKernel3"
    kernel_name: str = "kernel"
    block: str = "/dev/block/by-name/boot"
    is_slot_device: int = 0
    ramdisk_compression: str = "auto"
    do_devicecheck: int = 1
    supported_versions: str = ""
    supported_patchlevels: str = ""
    extra_cmds: str = ""
    device_names: list[str] = field(default_factory=list)


@dataclass
class BuildConfig:
    """Top-level build configuration."""

    kernel_source: str = ""
    kernel_defconfig: str = "defconfig"
    arch: str = "arm64"
    subarch: str = "arm64"

    kernel_source_url: str = ""
    kernel_source_branch: str = ""
    kernel_source_depth: int = 1

    output_dir: str = "out"
    zip_output_dir: str = "releases"

    jobs: int = 0
    lto: str | None = None
    extra_make_flags: list[str] = field(default_factory=list)
    extra_env: dict[str, str] = field(default_factory=dict)
    kbuild_build_user: str = "builder"
    kbuild_build_host: str = "forged (github.com/vxyzview/forged)"
    localversion: str = ""

    toolchain_dir: str = ""
    auto_setup_toolchain: bool = True

    toolchain: ToolchainConfig = field(default_factory=ToolchainConfig)
    anykernel3: AnyKernel3Config = field(default_factory=AnyKernel3Config)
    ccache: CcacheConfig = field(default_factory=CcacheConfig)

    def __post_init__(self) -> None:
        if self.lto is not None and self.lto not in _VALID_LTO_MODES:
            raise ValueError(
                f"Invalid lto value '{self.lto}'. "
                f"Must be one of: {sorted(_VALID_LTO_MODES)} or None/omitted."
            )
        if self.kernel_source_depth < 0:
            raise ValueError(
                f"Invalid kernel_source_depth '{self.kernel_source_depth}'. "
                "Must be 0 (full history) or a positive integer (shallow clone depth)."
            )

    def to_json(self, path: Path) -> None:
        path.write_text(json.dumps(asdict(self), indent=2), encoding="utf-8")

    @classmethod
    def from_json(cls, path: Path) -> BuildConfig:
        data = json.loads(path.read_text(encoding="utf-8"))
        tc = ToolchainConfig(**_known_fields(ToolchainConfig, data.pop("toolchain", {})))
        ak3 = AnyKernel3Config(**_known_fields(AnyKernel3Config, data.pop("anykernel3", {})))
        ccache = CcacheConfig(**_known_fields(CcacheConfig, data.pop("ccache", {})))
        return cls(**_known_fields(cls, data), toolchain=tc, anykernel3=ak3, ccache=ccache)

    @classmethod
    def from_toml(cls, path: Path) -> BuildConfig:
        if tomllib is None:
            raise RuntimeError(
                "TOML support requires Python 3.11+ or 'tomli' package. "
                "Install via: pip install tomli"
            )
        with path.open("rb") as fh:
            data = tomllib.load(fh)
        tc_data = data.pop("toolchain", {})
        ak3_data = data.pop("anykernel3", {})
        ccache_data = data.pop("ccache", {})
        tc = ToolchainConfig(**_known_fields(ToolchainConfig, tc_data))
        ak3 = AnyKernel3Config(**_known_fields(AnyKernel3Config, ak3_data))
        ccache = CcacheConfig(**_known_fields(CcacheConfig, ccache_data))
        return cls(**_known_fields(cls, data), toolchain=tc, anykernel3=ak3, ccache=ccache)

    @classmethod
    def load(cls, path: Path) -> BuildConfig:
        """Auto-detect format by extension."""
        suffix = path.suffix.lower()
        if suffix == ".json":
            return cls.from_json(path)
        if suffix in (".toml", ".tml"):
            return cls.from_toml(path)
        raise ValueError(f"Unsupported config format: {suffix}")

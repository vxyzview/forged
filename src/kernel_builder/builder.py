"""
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.

Kernel build engine — orchestrates make invocations.
"""

from __future__ import annotations

import multiprocessing
import os
import shutil
import subprocess
import time
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

from .config import BuildConfig
from .toolchain_manager import auto_setup_toolchain, clone_kernel_source, toolchain_clang_path

# ── LLVM binutils tool names ──────────────────────────────────────────────────
#
# These are passed as explicit make variables when use_llvm_binutils=True,
# replacing the GNU equivalents and removing any dependency on GCC.

LLVM_BINUTILS: dict[str, str] = {
    "LD": "ld.lld",
    "AR": "llvm-ar",
    "NM": "llvm-nm",
    "OBJCOPY": "llvm-objcopy",
    "OBJDUMP": "llvm-objdump",
    "READELF": "llvm-readelf",
    "STRIP": "llvm-strip",
}


# ── Result types ──────────────────────────────────────────────────────────────


@dataclass
class BuildStep:
    name: str
    command: list[str]
    env: dict


@dataclass
class BuildResult:
    success: bool
    step: str
    duration: float
    output: str
    error: str = ""


# ── Helpers ───────────────────────────────────────────────────────────────────


def _resolve_jobs(requested: int) -> int:
    if requested > 0:
        return requested
    return max(1, multiprocessing.cpu_count())


def find_ccache() -> Path | None:
    """Return the absolute path to the ccache binary, or None if not found.

    The binary is looked up via PATH so it works whether ccache is installed
    system-wide (``/usr/bin/ccache``) or in a custom prefix.
    """
    found = shutil.which("ccache")
    return Path(found) if found else None


def _build_env(cfg: BuildConfig) -> dict:
    """Build the subprocess environment for all make invocations.

    Always uses Clang as CC.  When use_llvm_binutils is True, the LLVM
    equivalents of all binutils tools are exported so no GCC installation
    is required on the host.

    Cross-compiler variables exported
    ───────────────────────────────────
    CROSS_COMPILE       — arm64 GNU prefix (always exported).
    CROSS_COMPILE_ARM32 — explicit arm32 prefix for modern Android kernel trees
                          (exported when toolchain.cross_compile_arm32 is non-empty).
                          Leave the field empty in your config to suppress it.

    When ccache is enabled the following CCACHE_* variables are exported:

      CCACHE_DIR        — storage directory (when cfg.ccache.dir is set)
      CCACHE_MAXSIZE    — maximum cache size (e.g. "5G")
      CCACHE_SLOPPINESS — comma-separated flags tuned for kernel builds
      CCACHE_COMPRESS   — "true" / "false"
      CCACHE_BASEDIR    — base dir for relocatable entries (defaults to source)

    The CC make variable is *not* modified here; the ``ccache`` prefix is
    injected in ``_make_base()`` so it appears on the make command line too,
    which is required for ``ccache`` to intercept sub-make invocations
    (e.g. modpost).

    extra_path entries are expanded with os.path.expanduser /
    os.path.expandvars so that values like ~/toolchains/clang/bin work
    correctly instead of being passed through literally as '~'.

    extra_env is applied last, after all built-in variables, so it can
    override any variable when needed (e.g. to set KCPPFLAGS or KBUILD_VERBOSE).
    """
    env = os.environ.copy()

    tc = cfg.toolchain
    expanded_paths = [
        os.path.expandvars(os.path.expanduser(str(p)))
        for p in tc.extra_path
        if p
    ]
    extra_paths = ":".join(expanded_paths)
    if extra_paths:
        env["PATH"] = extra_paths + ":" + env.get("PATH", "")

    env.update(
        {
            "ARCH": cfg.arch,
            "SUBARCH": cfg.subarch,
            "CC": tc.cc,
            "CROSS_COMPILE": tc.cross_compile,
            "CLANG_TRIPLE": tc.clang_triple,
            "KBUILD_BUILD_USER": cfg.kbuild_build_user,
            "KBUILD_BUILD_HOST": cfg.kbuild_build_host,
        }
    )

    # CROSS_COMPILE_ARM32 — explicit arm32 prefix (modern Android kernel trees).
    # Omitted entirely when the field is empty so kernels that don't recognise
    # the variable are not affected.
    if tc.cross_compile_arm32:
        env["CROSS_COMPILE_ARM32"] = tc.cross_compile_arm32

    if tc.use_llvm_binutils:
        env.update(LLVM_BINUTILS)

    if cfg.localversion:
        env["LOCALVERSION"] = cfg.localversion

    # ── ccache environment ────────────────────────────────────────────────────
    if cfg.ccache.enabled:
        cc = cfg.ccache

        if cc.dir:
            env["CCACHE_DIR"] = os.path.expandvars(os.path.expanduser(cc.dir))

        env["CCACHE_MAXSIZE"] = cc.max_size
        env["CCACHE_SLOPPINESS"] = cc.sloppiness
        env["CCACHE_COMPRESS"] = "true" if cc.compress else "false"

        # CCACHE_BASEDIR helps ccache share cache entries across builds that
        # use different absolute paths (e.g. CI checkouts in /tmp/buildNNN).
        # Default to the kernel source dir when the user did not set basedir.
        raw_basedir = cc.basedir or cfg.kernel_source
        if raw_basedir:
            env["CCACHE_BASEDIR"] = os.path.expandvars(
                os.path.expanduser(str(raw_basedir))
            )

    # ── User-supplied arbitrary environment variables (applied last) ──────────
    # extra_env overrides any of the built-in variables above when they share
    # the same key.  Tilde and $VAR references are expanded for convenience.
    for key, value in cfg.extra_env.items():
        env[key] = os.path.expandvars(os.path.expanduser(value))

    return env


def _make_base(cfg: BuildConfig, jobs: int) -> list[str]:
    """Construct the common make command prefix used by every build step.

    Always emits CC/CLANG_TRIPLE/CROSS_COMPILE/SUBARCH flags.  When
    use_llvm_binutils is True, all LLVM binutils overrides are appended so
    the kernel Makefile never falls back to the GNU tools.

    Cross-compiler make variables
    ──────────────────────────────
    CROSS_COMPILE       — always included (arm64 prefix).
    CROSS_COMPILE_ARM32 — included when non-empty (modern arm32 prefix for
                          Android / GKI trees).  Omitted when the field is
                          empty so the kernel Makefile is not affected.

    When ccache is enabled, the CC make variable is set to
    ``"ccache <compiler>"`` (e.g. ``CC=ccache clang``).  This is separate
    from the CCACHE_* environment variables set in ``_build_env()`` — both
    are required so that ccache intercepts the top-level compiler call *and*
    any recursive make invocations that pass CC explicitly.

    SUBARCH is included on the make command line in addition to the
    environment so that kernel Makefiles that expect it as a variable
    (not just an env export) behave correctly.

    extra_make_flags are appended verbatim at the end, after all built-in
    variables, so they can extend or override anything above (e.g. LLVM=1).
    """
    tc = cfg.toolchain

    # Prefix compiler with ccache when enabled.
    cc_value = f"ccache {tc.cc}" if cfg.ccache.enabled else tc.cc

    cmd: list[str] = [
        "make",
        f"-j{jobs}",
        f"O={cfg.output_dir}",
        f"ARCH={cfg.arch}",
        f"SUBARCH={cfg.subarch}",
        f"CC={cc_value}",
        f"CLANG_TRIPLE={tc.clang_triple}",
        f"CROSS_COMPILE={tc.cross_compile}",
    ]

    # CROSS_COMPILE_ARM32 — modern arm32 prefix (Android / GKI kernel trees).
    # Omitted entirely when empty to avoid confusing kernels that don't expect it.
    if tc.cross_compile_arm32:
        cmd.append(f"CROSS_COMPILE_ARM32={tc.cross_compile_arm32}")

    if tc.use_llvm_binutils:
        cmd.extend(f"{var}={tool}" for var, tool in LLVM_BINUTILS.items())

    if cfg.lto == "thin":
        cmd.append("LTO=thin")
    elif cfg.lto == "full":
        cmd.append("LTO=full")

    # Append user-supplied flags last so they can extend or override anything.
    cmd.extend(cfg.extra_make_flags)
    return cmd


# ── Build steps ───────────────────────────────────────────────────────────────


def _step_mrproper(cfg: BuildConfig, jobs: int, env: dict) -> BuildStep:
    return BuildStep(
        name="mrproper",
        command=_make_base(cfg, jobs) + ["mrproper"],
        env=env,
    )


def _step_defconfig(cfg: BuildConfig, jobs: int, env: dict) -> BuildStep:
    return BuildStep(
        name="defconfig",
        command=_make_base(cfg, jobs) + [cfg.kernel_defconfig],
        env=env,
    )


def _step_build(cfg: BuildConfig, jobs: int, env: dict) -> BuildStep:
    return BuildStep(
        name="build",
        command=_make_base(cfg, jobs),
        env=env,
    )


# ── Runner ────────────────────────────────────────────────────────────────────


class KernelBuilder:
    """Orchestrate kernel build steps, yielding BuildResult for each."""

    def __init__(
        self,
        cfg: BuildConfig,
        progress_callback: Callable[[str], None] | None = None,
    ) -> None:
        self.cfg = cfg
        self.jobs = _resolve_jobs(cfg.jobs)

        # ── Auto-clone kernel source from git URL ─────────────────────────────
        # NOTE: cfg.kernel_source is mutated in-place when kernel_source_url is
        # set, so the caller's BuildConfig object will reflect the cloned path.
        if cfg.kernel_source_url:
            dest = Path(cfg.kernel_source) if cfg.kernel_source else Path.cwd() / "kernel"
            clone_kernel_source(
                cfg.kernel_source_url,
                dest,
                branch=cfg.kernel_source_branch,
                depth=cfg.kernel_source_depth,
                progress=progress_callback,
            )
            cfg.kernel_source = str(dest)

        self.source_dir = Path(cfg.kernel_source).resolve()
        self.output_dir = self.source_dir / cfg.output_dir

        # ── Auto-clone AOSP Clang and set arm64/arm PATH ──────────────────────
        if cfg.auto_setup_toolchain and cfg.toolchain.auto_clone:
            toolchain_base = (
                Path(cfg.toolchain_dir) if cfg.toolchain_dir else None
            )
            auto_setup_toolchain(
                cfg,
                toolchain_base=toolchain_base,
                install_cross_compilers=False,
                progress=progress_callback,
            )

    def validate_toolchain(self) -> list[str]:
        """Return a list of warning strings about missing toolchain components.

        Checks for the configured Clang binary and, when ccache is enabled,
        verifies that the ``ccache`` binary is available on PATH.  An empty
        list means everything looks fine.
        """
        issues: list[str] = []

        clang = toolchain_clang_path(self.cfg)
        if clang is None:
            issues.append(
                f"Clang binary '{self.cfg.toolchain.cc}' not found in "
                "extra_path or $PATH.  Run 'kernel-builder setup-toolchain' "
                "or set toolchain.extra_path in your build config."
            )

        # ── ccache binary check ───────────────────────────────────────────────
        if self.cfg.ccache.enabled and find_ccache() is None:
            issues.append(
                "ccache is enabled but the 'ccache' binary was not found in "
                "$PATH.  Install it with:  sudo apt install ccache\n"
                "Alternatively, disable ccache in your build config."
            )

        return issues

    # ── Public API ────────────────────────────────────────────────────────────

    def steps(self, clean: bool = True) -> list[BuildStep]:
        env = _build_env(self.cfg)
        step_list: list[BuildStep] = []
        if clean:
            step_list.append(_step_mrproper(self.cfg, self.jobs, env))
        step_list.append(_step_defconfig(self.cfg, self.jobs, env))
        step_list.append(_step_build(self.cfg, self.jobs, env))
        return step_list

    def run_step(
        self,
        step: BuildStep,
        line_callback: Callable[[str], None] | None = None,
    ) -> BuildResult:
        """Execute one build step, streaming stdout/stderr line-by-line."""
        start = time.monotonic()
        combined_lines: list[str] = []

        try:
            process = subprocess.Popen(
                step.command,
                cwd=str(self.source_dir),
                env=step.env,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                bufsize=1,
            )
            if process.stdout is None:  # pragma: no cover — Popen with PIPE always sets stdout
                raise RuntimeError("subprocess stdout is None; this should never happen")
            for line in process.stdout:
                stripped = line.rstrip()
                combined_lines.append(stripped)
                if line_callback:
                    line_callback(stripped)
            process.wait()
            duration = time.monotonic() - start
            success = process.returncode == 0
            return BuildResult(
                success=success,
                step=step.name,
                duration=duration,
                output="\n".join(combined_lines),
                error="" if success else f"Exit code {process.returncode}",
            )
        except FileNotFoundError as exc:
            duration = time.monotonic() - start
            return BuildResult(
                success=False,
                step=step.name,
                duration=duration,
                output="",
                error=f"Command not found: {exc}",
            )
        except Exception as exc:  # noqa: BLE001
            duration = time.monotonic() - start
            return BuildResult(
                success=False,
                step=step.name,
                duration=duration,
                output="\n".join(combined_lines),
                error=str(exc),
            )

    def find_kernel_image(self) -> Path | None:
        """Locate the compiled kernel image (Image, Image.gz, zImage, etc.)."""
        search_names = [
            "Image.gz-dtb",
            "Image-dtb",
            "Image.gz",
            "Image",
            "zImage-dtb",
            "zImage",
            "dtb.img",
        ]
        for name in search_names:
            candidate = self.output_dir / "arch" / self.cfg.arch / "boot" / name
            if candidate.exists():
                return candidate
        return None

    def find_dtb_files(self) -> list[Path]:
        """Collect unique .dtb files from output.

        Results are deduplicated via a seen-set so that files matched
        by both the recursive and top-level glob patterns are not returned
        twice.
        """
        boot_dir = self.output_dir / "arch" / self.cfg.arch / "boot"
        seen: set[Path] = set()
        dtbs: list[Path] = []
        for pattern in ("dts/**/*.dtb", "*.dtb"):
            for dtb in boot_dir.glob(pattern):
                resolved = dtb.resolve()
                if resolved not in seen:
                    seen.add(resolved)
                    dtbs.append(dtb)
        return dtbs

    def find_modules(self) -> list[Path]:
        """Collect compiled kernel modules (.ko files)."""
        return list(self.output_dir.rglob("*.ko"))

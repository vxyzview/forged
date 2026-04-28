"""
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.

Toolchain manager — download AOSP Clang tarball via aria2 (with urllib
fallback), auto-configure arm64/arm paths, and optionally clone the kernel
source tree from a remote git URL.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
import tarfile
import urllib.error
import urllib.request
from collections.abc import Callable
from pathlib import Path

from .config import BuildConfig

# ── AOSP prebuilts tarball base URL ───────────────────────────────────────────

# Direct tarball archive base served by android.googlesource.com.
# Full URL pattern: {AOSP_CLANG_TARBALL_BASE}/{revision_dir}.tar.gz
AOSP_CLANG_TARBALL_BASE = (
    "https://android.googlesource.com/platform/prebuilts/clang/host/linux-x86"
    "/+archive/refs/heads/main-kernel"
)

# Known AOSP preset names that support auto-download.
# The revision directory is built dynamically from aosp_clang_version.
# Default entry maps the preset name to the default revision string so that
# tests importing this dict can still validate the structure.
AOSP_REVISION_DIRS: dict[str, str] = {
    "aosp-clang": "clang-r584948b",
}

# Default location for auto-managed toolchains
DEFAULT_TOOLCHAIN_BASE = Path.home() / ".local" / "share" / "kernel-builder" / "toolchains"

# ── Logging helpers ───────────────────────────────────────────────────────────

ProgressCallback = Callable[[str], None] | None


def _log(msg: str, callback: ProgressCallback) -> None:
    if callback:
        callback(msg)
    else:
        print(msg, file=sys.stderr)


# ── Internal helpers ──────────────────────────────────────────────────────────


def _is_valid_clang_dir(path: Path) -> bool:
    """Return True when *path* contains a usable clang binary."""
    return (path / "bin" / "clang").exists()


def _run(
    cmd: list[str],
    cwd: Path | None = None,
    progress: ProgressCallback = None,
) -> None:
    """Run a subprocess, streaming output to the progress callback."""
    _log(f"  $ {' '.join(cmd)}", progress)
    process = subprocess.Popen(
        cmd,
        cwd=str(cwd) if cwd else None,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=1,
    )
    if process.stdout is None:  # pragma: no cover
        raise RuntimeError("subprocess stdout is None; this should never happen")
    for line in process.stdout:
        _log(line.rstrip(), progress)
    process.wait()
    if process.returncode != 0:
        raise RuntimeError(
            f"Command failed (exit {process.returncode}): {' '.join(cmd)}"
        )


def _download_with_aria2(
    url: str,
    dest: Path,
    progress: ProgressCallback = None,
) -> None:
    """Download *url* to *dest* using aria2c for fast multi-connection transfer.

    Uses 16 parallel connections and 16-way file splitting to maximise
    throughput on high-bandwidth links.  ``--continue=true`` resumes
    interrupted downloads automatically.
    """
    _log(f"[toolchain] Downloading via aria2c: {url}", progress)
    cmd = [
        "aria2c",
        "--split=16",
        "--max-connection-per-server=16",
        "--min-split-size=1M",
        "--continue=true",
        "--file-allocation=none",
        "--console-log-level=notice",
        f"--dir={dest.parent}",
        f"--out={dest.name}",
        url,
    ]
    _run(cmd, progress=progress)


def _download_with_urllib(
    url: str,
    dest: Path,
    progress: ProgressCallback = None,
) -> None:
    """Fallback downloader using Python's built-in urllib, logging every 10 MiB.

    Uses ``urllib.request.urlopen`` (not the legacy ``urlretrieve``) so that
    HTTP errors raise ``urllib.error.HTTPError`` immediately and the download
    can be streamed in chunks rather than loaded into memory all at once.
    """
    _log(f"[toolchain] → {url}", progress)

    chunk_size = 1024 * 1024  # 1 MiB per read
    downloaded = 0
    last_logged_mb = 0
    log_interval_mb = 10  # log every 10 MiB

    try:
        with urllib.request.urlopen(url) as response:  # noqa: S310
            total_size = int(response.headers.get("Content-Length") or -1)
            if total_size > 0:
                total_mb = total_size // (1024 * 1024)
                _log(f"[toolchain]   Download size: {total_mb} MiB", progress)
            with dest.open("wb") as fh:
                while True:
                    chunk = response.read(chunk_size)
                    if not chunk:
                        break
                    fh.write(chunk)
                    downloaded += len(chunk)
                    downloaded_mb = downloaded // (1024 * 1024)
                    if downloaded_mb >= last_logged_mb + log_interval_mb:
                        if total_size > 0:
                            total_mb = total_size // (1024 * 1024)
                            _log(
                                f"[toolchain]   {downloaded_mb} / {total_mb} MiB downloaded …",
                                progress,
                            )
                        else:
                            _log(
                                f"[toolchain]   {downloaded_mb} MiB downloaded …",
                                progress,
                            )
                        last_logged_mb = downloaded_mb
        final_mb = downloaded // (1024 * 1024)
        _log(f"[toolchain]   Download complete ({final_mb} MiB).", progress)
    except urllib.error.HTTPError as exc:
        raise RuntimeError(
            f"HTTP {exc.code} when downloading {url}: {exc.reason}"
        ) from exc


def _download(url: str, dest: Path, progress: ProgressCallback = None) -> None:
    """Download *url* to *dest*.

    Prefers aria2c when available for faster multi-connection downloads.
    Falls back to urllib when aria2c is not installed.
    """
    if shutil.which("aria2c") is not None:
        _download_with_aria2(url, dest, progress=progress)
    else:
        _log(
            "[toolchain] aria2c not found — falling back to urllib "
            "(install aria2 for faster downloads: sudo apt install aria2)",
            progress,
        )
        _download_with_urllib(url, dest, progress=progress)


def _extract_tarball(tarball: Path, dest: Path, progress: ProgressCallback = None) -> None:
    """Extract *tarball* (.tar.gz) into *dest*.

    AOSP clang tarballs are flat archives — all files live at the root with
    no wrapping top-level directory — so the extracted tree lands directly
    inside *dest*, giving ``dest/bin/clang``, ``dest/lib/…``, etc.
    """
    _log(f"[toolchain] Extracting {tarball.name} into {dest} …", progress)
    with tarfile.open(tarball, "r:gz") as tf:
        if sys.version_info >= (3, 12):
            tf.extractall(path=dest, filter="data")
        else:
            tf.extractall(path=dest)  # noqa: S202
    _log("[toolchain] Extraction complete.", progress)


# ── AOSP Clang download ───────────────────────────────────────────────────────


def download_aosp_clang(
    dest_base: Path,
    preset: str = "aosp-clang",
    version: str = "r584948b",
    progress: ProgressCallback = None,
) -> Path:
    """Download and extract the AOSP prebuilt Clang tarball.

    The tarball is fetched from the AOSP googlesource archive::

        https://android.googlesource.com/platform/prebuilts/clang/host/linux-x86
        /+archive/refs/heads/main-kernel/clang-<version>.tar.gz

    Downloads use aria2c when available (16 parallel connections), falling back
    to urllib.  The archive is extracted into
    ``dest_base/<preset>/clang-<version>/`` and the ``.tar.gz`` file is
    deleted once extraction succeeds.

    Returns the ``bin/`` directory that should be prepended to ``PATH``.

    Args:
        dest_base: Parent directory where the toolchain will be stored.
        preset:    Preset name — must be a key in ``AOSP_REVISION_DIRS``.
        version:   AOSP Clang revision string, e.g. ``"r584948b"``.
        progress:  Optional callable that receives log lines as strings.

    Raises:
        ValueError:   If *preset* is not a recognised AOSP preset.
        RuntimeError: If the download or extraction fails, or if the expected
                      ``bin/clang`` is missing after extraction.
    """
    if preset not in AOSP_REVISION_DIRS:
        raise ValueError(
            f"No AOSP auto-download handler for preset '{preset}'. "
            f"Known presets: {list(AOSP_REVISION_DIRS)}"
        )

    revision_dir = f"clang-{version}"
    url = f"{AOSP_CLANG_TARBALL_BASE}/{revision_dir}.tar.gz"
    dest_root = dest_base / preset
    clang_dir = dest_root / revision_dir
    bin_dir = clang_dir / "bin"

    # ── Fast-path: already downloaded and extracted ───────────────────────────
    if _is_valid_clang_dir(clang_dir):
        _log(f"[toolchain] AOSP Clang already present at {clang_dir}", progress)
        return bin_dir

    clang_dir.mkdir(parents=True, exist_ok=True)

    tarball = dest_root / f"{revision_dir}.tar.gz"

    _log(f"[toolchain] Downloading AOSP Clang '{revision_dir}' …", progress)
    _log(f"[toolchain] Source URL: {url}", progress)

    # ── Download ──────────────────────────────────────────────────────────────
    try:
        _download(url, tarball, progress=progress)
    except Exception as exc:
        tarball.unlink(missing_ok=True)
        raise RuntimeError(
            f"Failed to download AOSP Clang tarball from:\n  {url}\n"
            "Check your network connection and try again.\n"
            f"Error: {exc}"
        ) from exc

    # ── Extract ───────────────────────────────────────────────────────────────
    try:
        _extract_tarball(tarball, clang_dir, progress=progress)
    except Exception as exc:
        tarball.unlink(missing_ok=True)
        raise RuntimeError(
            f"Failed to extract AOSP Clang tarball '{tarball.name}'.\n"
            f"Error: {exc}"
        ) from exc

    # ── Clean up tarball to recover disk space ────────────────────────────────
    tarball.unlink(missing_ok=True)

    # ── Validate ──────────────────────────────────────────────────────────────
    if not _is_valid_clang_dir(clang_dir):
        raise RuntimeError(
            f"Download finished but '{clang_dir / 'bin' / 'clang'}' was not found.\n"
            "The tarball contents may differ from the expected structure.\n"
            f"Source URL: {url}"
        )

    _log(f"[toolchain] AOSP Clang ready at {bin_dir}", progress)
    return bin_dir


# ── Kernel source cloning ─────────────────────────────────────────────────────


def clone_kernel_source(
    url: str,
    dest: Path,
    branch: str = "",
    depth: int = 1,
    progress: ProgressCallback = None,
) -> Path:
    """Clone a kernel source tree from a remote git URL into *dest*.

    Args:
        url:      Git-compatible URL.
        dest:     Local path to clone into (parent dirs created automatically).
        branch:   Branch name, tag, or empty string for remote default.
        depth:    Clone depth.  ``1`` = shallow.  ``0`` = full history.
        progress: Optional callable that receives log lines as strings.

    Returns:
        *dest* (the cloned repository root).

    Raises:
        RuntimeError: If the ``git clone`` command exits non-zero.
    """
    if dest.exists() and (dest / ".git").is_dir():
        _log(f"[kernel] Kernel source already present at {dest}", progress)
        return dest

    dest.parent.mkdir(parents=True, exist_ok=True)

    _log(f"[kernel] Cloning kernel source from {url} …", progress)
    if branch:
        _log(f"[kernel]   Branch/tag : {branch}", progress)
    _log(f"[kernel]   Depth      : {depth if depth > 0 else 'full history'}", progress)

    cmd: list[str] = ["git", "clone"]
    if depth > 0:
        # Shallow clone: fast and small.  --filter is redundant with --depth
        # and can cause unexpected behaviour on some git versions.
        cmd.append(f"--depth={depth}")
    else:
        # Full-history blobless clone: commits + trees fetched up-front,
        # large blobs fetched on-demand only.
        cmd.append("--filter=blob:none")
    if branch:
        cmd += ["--branch", branch, "--single-branch"]
    cmd += [url, str(dest)]

    _run(cmd, progress=progress)

    _log(f"[kernel] Kernel source ready at {dest}", progress)
    return dest


# ── GNU cross-compiler checks ─────────────────────────────────────────────────

_GNU_PACKAGES: dict[str, tuple[str, str]] = {
    "aarch64": ("aarch64-linux-gnu-gcc", "gcc-aarch64-linux-gnu"),
    "arm": ("arm-linux-gnueabihf-gcc", "gcc-arm-linux-gnueabihf"),
}


def check_gnu_cross_compilers(
    progress: ProgressCallback = None,
) -> dict[str, Path | None]:
    """Check whether the arm64 and arm GNU cross-compiler prefixes are on PATH.

    Returns a mapping ``{"aarch64": Path|None, "arm": Path|None}`` where the
    value is the *directory* containing the cross-compiler binaries, or
    ``None`` when the prefix is absent.
    """
    result: dict[str, Path | None] = {}

    for arch, (binary, pkg) in _GNU_PACKAGES.items():
        found = shutil.which(binary)
        if found:
            result[arch] = Path(found).parent
            _log(f"[toolchain] {arch} cross-compiler OK → {found}", progress)
        else:
            result[arch] = None
            _log(
                f"[toolchain] WARNING: {arch} cross-compiler not found "
                f"(binary: {binary}).\n"
                f"           Install with:  sudo apt install {pkg}\n"
                "           Without it, some kernel Makefile checks may fail.",
                progress,
            )

    return result


def install_gnu_cross_compilers(progress: ProgressCallback = None) -> None:
    """Attempt to install the missing GNU cross-compiler packages via apt.

    Raises ``RuntimeError`` when apt is unavailable (non-Debian host).
    """
    if shutil.which("apt-get") is None:
        raise RuntimeError(
            "apt-get not found — cannot auto-install cross-compilers.\n"
            "Please install the following packages manually:\n"
            "  gcc-aarch64-linux-gnu   (arm64 cross-compiler)\n"
            "  gcc-arm-linux-gnueabihf (arm 32-bit cross-compiler)"
        )

    pkgs_to_install = [
        pkg
        for _, (binary, pkg) in _GNU_PACKAGES.items()
        if shutil.which(binary) is None
    ]

    if not pkgs_to_install:
        _log("[toolchain] All GNU cross-compiler packages already installed.", progress)
        return

    _log(f"[toolchain] Installing {pkgs_to_install} via apt-get …", progress)
    _run(
        ["sudo", "apt-get", "install", "-y", "--no-install-recommends"] + pkgs_to_install,
        progress=progress,
    )


# ── Top-level auto-setup ──────────────────────────────────────────────────────


def auto_setup_toolchain(
    cfg: BuildConfig,
    toolchain_base: Path | None = None,
    install_cross_compilers: bool = False,
    progress: ProgressCallback = None,
) -> BuildConfig:
    """Detect or download the toolchain and patch *cfg* in place.

    Workflow
    --------
    1. If ``cfg.toolchain.extra_path`` already points to a valid clang binary,
       do nothing (idempotent fast-path).
    2. Otherwise download the toolchain matching the configured preset and
       **automatically set** ``cfg.toolchain.extra_path`` to the resulting
       ``bin/`` directory.  The AOSP Clang revision is read from
       ``cfg.toolchain.aosp_clang_version``.
    3. Check for arm64/arm GNU cross-compiler prefixes; warn when absent, and
       optionally install them via apt.

    Returns the same *cfg* object, with ``toolchain.extra_path`` updated.
    """
    tc = cfg.toolchain
    base = toolchain_base or DEFAULT_TOOLCHAIN_BASE

    # ── Step 1: check whether the toolchain is already usable ────────────────
    for p in tc.extra_path:
        expanded = Path(os.path.expandvars(os.path.expanduser(str(p))))
        if (expanded / tc.cc).exists():
            _log(f"[toolchain] Clang found at {expanded / tc.cc} — skipping setup.", progress)
            _check_cross_compilers(install_cross_compilers, progress)
            return cfg

    # Also accept a system clang when extra_path is empty
    if tc.preset == "system-clang":
        found = shutil.which(tc.cc)
        if found:
            _log(f"[toolchain] System {tc.cc} found at {found}", progress)
            _check_cross_compilers(install_cross_compilers, progress)
            return cfg
        raise RuntimeError(
            f"system-clang preset selected but '{tc.cc}' is not on PATH.\n"
            "Install with:  sudo apt install clang lld llvm"
        )

    # ── Step 2: download the toolchain ────────────────────────────────────────
    bin_dir: Path

    if tc.preset in AOSP_REVISION_DIRS:
        bin_dir = download_aosp_clang(
            base,
            preset=tc.preset,
            version=tc.aosp_clang_version,
            progress=progress,
        )
    else:
        raise ValueError(
            f"No auto-setup handler for toolchain preset '{tc.preset}'.\n"
            "Please set 'toolchain.extra_path' manually in your build config."
        )

    # ── Step 3: write the bin/ path back into the config ─────────────────────
    tc.extra_path = [str(bin_dir)]
    _log(f"[toolchain] extra_path automatically set to: {bin_dir}", progress)

    # ── Step 4: verify arm64 + arm cross-compiler availability ────────────────
    _check_cross_compilers(install_cross_compilers, progress)

    return cfg


def _check_cross_compilers(
    install: bool,
    progress: ProgressCallback,
) -> None:
    """Wrapper that either installs or just checks the GNU cross-compilers."""
    if install:
        install_gnu_cross_compilers(progress)
    else:
        check_gnu_cross_compilers(progress)


# ── Convenience: resolve the effective PATH for a config ──────────────────────


def resolved_extra_paths(cfg: BuildConfig) -> list[str]:
    """Return ``cfg.toolchain.extra_path`` entries with ``~`` and ``$VAR`` expanded."""
    return [
        os.path.expandvars(os.path.expanduser(str(p)))
        for p in cfg.toolchain.extra_path
        if p
    ]


def toolchain_clang_path(cfg: BuildConfig) -> Path | None:
    """Return the full path to the clang binary, or None if not found."""
    for p in resolved_extra_paths(cfg):
        candidate = Path(p) / cfg.toolchain.cc
        if candidate.exists():
            return candidate
    found = shutil.which(cfg.toolchain.cc)
    return Path(found) if found else None

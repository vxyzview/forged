<div align="center">

```
███████╗ ██████╗ ██████╗   ██████╗ ███████╗██████╗
██╔════╝██╔═══██╗██╔══██╗ ██╔════╝ ██╔════╝██╔══██╗
█████╗  ██║   ██║██████╔╝ ██║  ███╗█████╗  ██║  ██║
██╔══╝  ██║   ██║██╔══██╗ ██║   ██║██╔══╝  ██║  ██║
██║     ╚██████╔╝██║  ██║ ╚██████╔╝███████╗██████╔╝
╚═╝      ╚═════╝ ╚═╝  ╚═╝  ╚═════╝ ╚══════╝╚═════╝
```

**Android Kernel Builder · AnyKernel3 Ready · LLVM / Clang**

[![Python](https://img.shields.io/badge/Python-3.10%2B-3776ab?style=flat-square&logo=python&logoColor=white)](https://python.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-ff7c00?style=flat-square)](LICENSE)
[![Rich TUI](https://img.shields.io/badge/TUI-Rich-ffbe00?style=flat-square)](https://github.com/Textualize/rich)
[![LLVM](https://img.shields.io/badge/Toolchain-LLVM%20%2F%20Clang-39d353?style=flat-square)](https://llvm.org)
[![AnyKernel3](https://img.shields.io/badge/Packaging-AnyKernel3-a8b2c0?style=flat-square)](https://github.com/osm0sis/AnyKernel3)

*Fast · Clean · Interactive · No GCC Required*

</div>

---

## Overview

**FORGED** is a command-line Android kernel builder that turns a multi-step, error-prone process into a single command. It orchestrates the full build pipeline — `mrproper → defconfig → compilation → AnyKernel3 packaging` — and presents everything through a live Rich terminal UI with real-time scrolling log output, progress tracking, and forge-fire styling.

Designed for kernel developers who want a fast, repeatable, scriptable workflow without sacrificing visibility into what's happening under the hood.

---

## Table of Contents

- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [CLI Reference](#cli-reference)
  - [forged build](#forged-build)
  - [forged setup-toolchain](#forged-setup-toolchain)
  - [forged config](#forged-config)
  - [forged info](#forged-info)
  - [forged ccache-stats](#forged-ccache-stats)
- [Configuration](#configuration)
  - [TOML example](#minimal-toml-example)
  - [JSON example](#minimal-json-example)
  - [Full reference](#full-config-reference)
- [ccache](#ccache)
- [Toolchains](#toolchains)
- [Build Output](#build-output)
- [AnyKernel3](#anykernel3)
- [Project Structure](#project-structure)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [License](#license)

---

## Features

| Category | What FORGED does |
|---|---|
| 🔨 **Build orchestration** | `mrproper → defconfig → build` in one command with a live scrolling log and per-step timing |
| 📦 **AnyKernel3 packaging** | Automatic flashable ZIP with DTB staging, kernel module collection, and configurable device targeting |
| ⚡ **ccache integration** | Full `CCACHE_*` environment injection with kernel-tuned sloppiness flags; `--ccache` / `--no-ccache` CLI overrides; cuts rebuild times **60–90%** |
| 🛠 **LLVM-only toolchain** | AOSP prebuilt Clang or system Clang; all GNU binutils replaced by LLVM equivalents — **zero GCC dependency** |
| 🤖 **Auto toolchain setup** | `forged setup-toolchain` downloads AOSP Clang tarballs via **aria2** (falls back to urllib); validates arm64/arm cross-compiler presence |
| 🌐 **Git source cloning** | Point `kernel_source_url` at any repo; FORGED shallow-clones it before the first build with configurable depth and branch |
| 📝 **TOML + JSON config** | Round-trip safe read/write; `forged config` runs an interactive wizard that saves a portable config file |
| 🎨 **Forge-fire TUI** | Orange / gold / steel / green / red palette; live progress bars; phase headers; animated spinners; no gradients — pure terminal colour |
| 🔍 **Toolchain validation** | Pre-build warnings for missing binaries, misconfigured paths, or unsatisfied cross-compiler dependencies |
| 🧩 **Extra flags & env** | Inject arbitrary `VAR=VALUE` make flags (`-F`) and environment variables (`-E`) from the CLI without touching your config file |
| 🏷 **LTO support** | `none` / `thin` / `full` — works transparently alongside ccache ≥ 4.0 |

---

## Requirements

| Dependency | Version / Notes |
|---|---|
| **Python** | 3.10 or newer |
| **git** | Must be on `$PATH` |
| **make** | Must be on `$PATH` |
| **Clang + LLVM tools** | Installed manually *or* auto-downloaded by `forged setup-toolchain` |
| **ccache** | Optional — `sudo apt install ccache` |
| **aria2** | Optional — used for faster tarball downloads; falls back to urllib |
| **GNU cross-compilers** | Optional — auto-installable via `forged setup-toolchain --install-cross-compilers` |

> **Minimum:** Python 3.10, git, make, and either a system Clang or a network connection for the first run.

---

## Installation

### From source (recommended)

```bash
git clone https://github.com/vxyzview/forged.git
cd forged
pip install .
```

### Development install

Includes `ruff`, `mypy`, and `pytest`:

```bash
pip install -e ".[dev]"
```

The `forged` command is added to your `$PATH` immediately after installation.

### Verify installation

```bash
forged --help
```

---

## Quick Start

```bash
# Step 1 — Download and verify the AOSP Clang toolchain (first time only)
forged setup-toolchain

# Step 2 — Build interactively: the wizard creates a config file
forged build --wizard

# Step 3 — Or build straight from a saved config
forged build -c build_config.json

# Step 4 — Skip mrproper on incremental builds
forged build -c build_config.json --no-clean

# Step 5 — Toggle ccache from the CLI without editing your config
forged build -c build_config.json --ccache
forged build -c build_config.json --no-ccache

# Step 6 — Inject extra make flags or env vars on the fly
forged build -c build_config.json -F LLVM=1 -F KCFLAGS=-pipe
forged build -c build_config.json -E KBUILD_VERBOSE=1

# Step 7 — Tag the output ZIP with a version string
forged build -c build_config.json --version-tag v1.2.3-beta
```

> **Tip:** Run `forged build` with no arguments to launch the interactive wizard automatically.

---

## CLI Reference

### `forged build`

```
forged build [OPTIONS]
```

| Option | Description |
|---|---|
| `-c, --config PATH` | Load a JSON or TOML build config file |
| `-w, --wizard` | Run the interactive setup wizard |
| `-s, --source DIR` | Kernel source directory (overrides config) |
| `--source-url URL` | Git URL to clone the kernel source from |
| `--source-branch BRANCH` | Branch or tag for `--source-url` (default: remote HEAD) |
| `--source-depth N` | Clone depth: `1` = shallow (default), `0` = full history |
| `-d, --defconfig NAME` | Defconfig name (overrides config) |
| `-j, --jobs N` | Parallel jobs (`0` = auto-detect CPU count) |
| `--no-clean` | Skip the `mrproper` step |
| `--no-package` | Skip AnyKernel3 ZIP creation |
| `--version-tag TAG` | Tag appended to the output ZIP filename |
| `--anykernel-dir DIR` | Path to the AnyKernel3 staging directory |
| `-F, --make-flag VAR=VALUE` | Append a make variable (repeatable) |
| `-E, --env KEY=VALUE` | Inject an environment variable (repeatable) |
| `--ccache` | Enable ccache (overrides config file) |
| `--no-ccache` | Disable ccache (overrides config file) |

---

### `forged setup-toolchain`

Download the AOSP Clang tarball and validate arm64/arm cross-compiler availability.

```
forged setup-toolchain [OPTIONS]
```

| Option | Description |
|---|---|
| `--preset PRESET` | Toolchain preset: `aosp-clang` or `system-clang` (default: `aosp-clang`) |
| `--version VERSION` | AOSP Clang revision, e.g. `r584948b` (default: `r584948b`) |
| `--toolchain-dir DIR` | Storage path (default: `~/.local/share/kernel-builder/toolchains`) |
| `--install-cross-compilers` | Auto-install missing GNU cross-compiler packages via `apt` |

---

### `forged config`

Launch the interactive wizard to create a new build config file (JSON). Equivalent to `forged build --wizard` but stops after saving without running the build.

---

### `forged info CONFIG_FILE`

Print a formatted summary table of a JSON or TOML config file.

```bash
forged info build_config.json
forged info build_config.toml
```

---

### `forged ccache-stats`

Display ccache statistics and optionally reset them.

```
forged ccache-stats [OPTIONS]
```

| Option | Description |
|---|---|
| `--dir DIR` | ccache storage directory (sets `CCACHE_DIR`) |
| `--zero` | Reset statistics after displaying them |
| `--verbose` | Show verbose statistics |

---

## Configuration

Both **JSON** and **TOML** formats are supported. Use `forged config` to generate one interactively, or copy from `config/` and edit.

### Minimal TOML example

```toml
kernel_source    = "/path/to/kernel/source"
kernel_defconfig = "vendor/your_device_defconfig"
arch             = "arm64"
jobs             = 0        # 0 = auto-detect CPU count

[toolchain]
preset             = "aosp-clang"
aosp_clang_version = "r584948b"
auto_clone         = true

[anykernel3]
kernel_name  = "Forged"
block        = "/dev/block/by-name/boot"
device_names = ["your_device"]

[ccache]
enabled  = true
max_size = "5G"
```

### Minimal JSON example

```json
{
  "kernel_source": "/path/to/kernel/source",
  "kernel_defconfig": "vendor/your_device_defconfig",
  "arch": "arm64",
  "jobs": 0,
  "toolchain": {
    "preset": "aosp-clang",
    "aosp_clang_version": "r584948b",
    "auto_clone": true
  },
  "anykernel3": {
    "kernel_name": "Forged",
    "block": "/dev/block/by-name/boot",
    "device_names": ["your_device"]
  },
  "ccache": {
    "enabled": true,
    "max_size": "5G"
  }
}
```

### Full config reference

See [`config/example_build_config.toml`](config/example_build_config.toml) and [`config/example_build_config.json`](config/example_build_config.json) for every available field with inline documentation.

---

## ccache

ccache caches compiled objects and cuts rebuild times by **60–90%** on second and subsequent builds. Highly recommended for iterative kernel development.

### Setup

```bash
sudo apt install ccache
```

Enable in your config:

```toml
[ccache]
enabled  = true
dir      = ""       # blank = ccache default (~/.cache/ccache)
max_size = "5G"
compress = true
basedir  = ""       # blank = kernel source dir
```

### Kernel-tuned sloppiness defaults

FORGED automatically applies sloppiness flags that are safe for kernel builds:

| Flag | Purpose |
|---|---|
| `time_macros` | Ignore `__DATE__` / `__TIME__` / `__TIMESTAMP__` to avoid cache misses |
| `include_file_mtime` | Accept hits when header mtimes change but content is equal |
| `file_stat_matches` | Use `stat()` instead of rehashing unchanged files |
| `pch_defines` | Relax pre-compiled header define checks |

### LTO + ccache compatibility

| ccache version | LTO mode |
|---|---|
| ≥ 4.0 | Thin LTO — transparent passthrough |
| ≥ 4.8 | Full LTO — fully supported |

Setting `lto = "thin"` and `ccache.enabled = true` together is **safe and recommended**.

### Statistics

```bash
forged ccache-stats                # summary
forged ccache-stats --verbose      # detailed breakdown
forged ccache-stats --zero         # display then reset
forged ccache-stats --dir /my/dir  # custom CCACHE_DIR
```

---

## Toolchains

See [`docs/toolchains.md`](docs/toolchains.md) for the full guide.

### Available presets

| Preset | Source | Auto-download | Notes |
|---|---|---|---|
| `aosp-clang` | AOSP prebuilts (googlesource.com) | **Yes** | Recommended |
| `system-clang` | System `clang` on `$PATH` | No | `sudo apt install clang lld llvm` |

All presets set `use_llvm_binutils = true`, replacing GNU binutils with:
`ld.lld` · `llvm-ar` · `llvm-nm` · `llvm-objcopy` · `llvm-objdump` · `llvm-readelf` · `llvm-strip`

**No GCC installation is required.**

### Cross-compiler requirements

| Arch | Binary | Package |
|---|---|---|
| arm64 | `aarch64-linux-gnu-gcc` | `gcc-aarch64-linux-gnu` |
| arm32 | `arm-linux-gnueabihf-gcc` | `gcc-arm-linux-gnueabihf` |

```bash
sudo apt install gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf
# or let FORGED install them automatically:
forged setup-toolchain --install-cross-compilers
```

### Selecting an AOSP Clang version

```bash
# Via CLI
forged setup-toolchain --preset aosp-clang --version r584948b
forged setup-toolchain --preset aosp-clang --version r522817

# Via config
[toolchain]
preset             = "aosp-clang"
aosp_clang_version = "r584948b"
```

---

## Build Output

### Live build display

During a build, FORGED shows:

- **Phase header** — labelled divider for each build step (mrproper / defconfig / build)
- **Progress bar** — step progress with spinner, elapsed time, and step count
- **Live log panel** — last 40 lines of compiler output, scrolling in real time
- **Per-step result panel** — pass/fail with duration on completion

### Full log

After the build finishes, the **complete log** (all lines, untruncated) is printed as a scrollable panel so you can review the full compiler output.

### Capturing output

```bash
forged build -c build_config.json 2>&1 | tee build.log
```

### Kernel image search priority

FORGED searches for the built kernel image in this order:

```
Image.gz-dtb  →  Image-dtb  →  Image.gz  →  Image  →  zImage-dtb  →  zImage
```

---

## AnyKernel3

The `AnyKernel3/` directory is the packaging staging area. FORGED copies the compiled kernel image, DTB files, and kernel modules into this directory automatically, then creates a flashable ZIP archive under `releases/`.

### First-time setup

Replace the stub `ak3-core.sh` with the real one from upstream:

```bash
git clone https://github.com/osm0sis/AnyKernel3 /tmp/ak3
cp /tmp/ak3/tools/ak3-core.sh AnyKernel3/tools/
```

### Output naming

```
releases/<kernel_name>-<device>-<timestamp>[-<version_tag>].zip
```

### Skip packaging

```bash
forged build -c build_config.json --no-package
```

---

## Project Structure

```
forged/
├── src/
│   └── kernel_builder/
│       ├── __init__.py           # package exports
│       ├── banner.py             # FORGED ASCII art and startup banner
│       ├── builder.py            # make orchestration + ccache injection
│       ├── cli.py                # argument parsing + interactive wizard + command dispatch
│       ├── config.py             # BuildConfig dataclasses + TOML/JSON I/O
│       ├── display.py            # Rich TUI — panels, progress, tables, live log
│       ├── main.py               # entry point
│       ├── packager.py           # AnyKernel3 ZIP builder
│       └── toolchain_manager.py  # AOSP Clang download + cross-compiler checks
├── tests/
│   ├── conftest.py               # pytest fixtures (toolchain auto-setup stub)
│   ├── test_kernel_builder.py    # builder, config, packager, ccache tests
│   └── test_toolchain_manager.py # toolchain manager tests
├── config/
│   ├── example_build_config.json # annotated JSON reference config
│   └── example_build_config.toml # annotated TOML reference config
├── docs/
│   └── toolchains.md             # toolchain setup guide
├── AnyKernel3/                   # packaging staging area
├── pyproject.toml
├── LICENSE
└── .gitignore
```

---

## Development

```bash
# Install dev dependencies (ruff, mypy, pytest, pytest-cov)
pip install -e ".[dev]"

# Run tests
pytest

# Run tests with coverage report
pytest --cov=kernel_builder --cov-report=term-missing

# Lint
ruff check src/ tests/

# Auto-fix lint issues
ruff check src/ tests/ --fix

# Type-check
mypy src/
```

### Adding a new toolchain preset

1. Add the preset entry to `TOOLCHAIN_PRESETS` in `config.py`
2. Handle any preset-specific download logic in `toolchain_manager.py`
3. Add a test fixture in `tests/conftest.py`
4. Document the preset in `docs/toolchains.md`

---

## Troubleshooting

### `clang: command not found` after setup-toolchain

FORGED sets `extra_path` in the config to the downloaded toolchain's `bin/` directory. Verify with:

```bash
forged info build_config.json
```

### Cross-compiler missing warnings

```bash
forged setup-toolchain                          # check status
forged setup-toolchain --install-cross-compilers  # auto-install (requires sudo)
```

### Build fails on `mrproper`

The kernel source directory path may be wrong or the directory doesn't exist yet.

```bash
forged info build_config.json   # check kernel_source field
ls /your/kernel/source          # confirm it exists
```

For Git-sourced kernels, add `--source-url` to trigger an automatic clone on first build.

### ccache not speeding up builds

```bash
forged ccache-stats --verbose   # verify cache_hit is increasing on second build
```

Ensure `CCACHE_DIR` is consistent between runs — set `dir` in your config rather than relying on environment variables.

### Output ZIP not created

Ensure:
- The build step completed successfully (green `✦ PASS` in the summary)
- `--no-package` is not passed
- `AnyKernel3/tools/ak3-core.sh` is the real file, not the stub

---

## License

```
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.
MIT License — see LICENSE for full text.
```

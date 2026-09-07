<div align="center">

```
 ██████╗  ██████╗ ██████╗  ██████╗ ███████╗██████╗
 ██╔══██╗██╔═══██╗██╔══██╗██╔════╝ ██╔════╝██╔══██╗
 ██╔══██╗██║   ██║██████╔╝██║  ███╗█████╗  ██║  ██║
 ██╔══██╗██║   ██║██╔══██╗██║   ██║██╔══╝  ██║  ██║
 ██████╔╝╚██████╔╝██║  ██║╚██████╔╝███████╗██████╔╝
 ╚═════╝  ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═════╝
```

**forge Android kernels with one command**

clang-only · live TUI · single binary · no GCC

[![Go](https://img.shields.io/badge/Go-1.24%2B-00add8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-ff7c00?style=flat-square)](LICENSE)
[![Bubble Tea](https://img.shields.io/badge/TUI-Bubble%20Tea-ffbe00?style=flat-square)](https://github.com/charmbracelet/bubbletea)
[![LLVM](https://img.shields.io/badge/Toolchain-LLVM%20%2F%20Clang-39d353?style=flat-square)](https://llvm.org)
[![AnyKernel3](https://img.shields.io/badge/Packaging-AnyKernel3-a8b2c0?style=flat-square)](https://github.com/osm0sis/AnyKernel3)

</div>

---

## Why forged?

Kernel building is a multi-step, copy-paste-from-Telegram ritual that breaks the moment you blink. **forged** turns it into a single command:

```
mrproper → defconfig → clang → AnyKernel3 zip
```

…all wrapped in a live Bubble Tea TUI with a scrolling log, phase headers, and forge-fire colors. One static binary. No interpreter, no runtime, no GCC.

```
$ forged build

  ▌ FORGE
 ──────────────────────────────────────────
  ✓ mrproper      1.2s
  ✓ defconfig     0.4s
  ● build         ████████░░░░░░░░  12m31s
    CC      drivers/gpio/gpio-x.o
    CC      drivers/clk/qcom/clk-rpmh.o
    LD      vmlinux.o
```

---

## Features

- **One command, full pipeline** — clean, defconfig, compile, and package a flashable AnyKernel3 ZIP automatically
- **LLVM-only toolchain** — AOSP prebuilt Clang or system Clang, with all GNU binutils swapped for `lld` / `llvm-*`. Zero GCC.
- **Live TUI** — progress bars, phase headers, spinners, colourised compiler output. Pure terminal, no bloat.
- **Non-TTY fallback** — piped or CI output automatically switches to plain streaming logs. Builds never break headless.
- **ccache built in** — kernel-tuned sloppiness flags, CLI overrides, stats command. Rebuilds **60–90% faster**.
- **Auto toolchain setup** — downloads AOSP Clang via aria2 (with net/http fallback), validates cross-compilers, offers to install them.
- **Git-sourced kernels** — point it at any kernel repo; forged shallow-clones before the first build.
- **TOML + JSON config** — forward-compatible reads, interactive wizard, portable files.
- **LTO aware** — `none` / `thin` / `full`, safe alongside ccache ≥ 4.0.
- **Issues log** — every build writes an errors + warnings digest under `logs/`.

---

## Install

One-liner — grabs the right prebuilt binary for your machine from GitHub Releases, verifies the checksum, and installs it:

```bash
curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | bash
```

Supported: **linux** (amd64, arm64, 386, armv7), **macOS** (Intel & Apple Silicon), **Windows** (amd64 & arm64 — download the `.zip` manually).

> Kernel *builds* need Linux — that's where the kernel toolchains live. On macOS and Windows, forged is still handy for `forged config`, `forged info`, cloning sources, and driving remote builds over SSH.

### With `go install`

```bash
go install github.com/vxyzview/forged/cmd/forged@latest
```

### From source

```bash
git clone https://github.com/vxyzview/forged.git
cd forged && go build -o forged ./cmd/forged
```

You need **Go 1.24+** to build. To run: `git`, `make`, and a Clang toolchain (or let forged fetch one).

---

## Quick start

```bash
# fetch the AOSP Clang toolchain (first time only)
forged setup-toolchain

# build — the wizard writes your config, then the forge lights up
forged build --wizard

# or straight from a saved config
forged build -c build_config.toml --no-clean --ccache
```

That's it. Flash the ZIP that lands in `releases/`.

---

## CLI

### `forged build`

| Flag | What it does |
|---|---|
| `-c, --config PATH` | Load a JSON or TOML build config |
| `-w, --wizard` | Interactive setup wizard |
| `-s, --source DIR` | Kernel source directory (overrides config) |
| `--source-url URL` | Git URL to clone the kernel source from |
| `-d, --defconfig NAME` | Defconfig (overrides config) |
| `-j, --jobs N` | Parallel jobs (`0` = auto) |
| `--no-clean` | Skip `mrproper` — for incremental builds |
| `--no-package` | Skip AnyKernel3 ZIP |
| `--version-tag TAG` | Tag appended to the output ZIP name |
| `-F, --make-flag VAR=VALUE` | Append a make variable (repeatable) |
| `-E, --env KEY=VALUE` | Inject an env var (repeatable) |
| `--ccache` / `--no-ccache` | Toggle ccache without touching config |

### `forged setup-toolchain`

| Flag | What it does |
|---|---|
| `--preset PRESET` | `aosp-clang` (default) or `system-clang` |
| `--version VERSION` | AOSP Clang revision, e.g. `r584948b` |
| `--toolchain-dir DIR` | Storage path (default `~/.local/share/forged/toolchains`) |
| `--install-cross-compilers` | Auto-install GNU cross-compilers via apt |

### Other commands

```bash
forged config build_config.toml   # interactive wizard, saves config
forged info build_config.toml     # print a config summary table
forged ccache-stats --verbose     # cache stats (--zero to reset)
```

---

## Config

Minimal TOML — see [`config/example_build_config.toml`](config/example_build_config.toml) for every field, annotated:

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

JSON works identically.

---

## Toolchains

| Preset | Source | Auto-download |
|---|---|---|
| `aosp-clang` | AOSP prebuilts | **yes** — recommended |
| `system-clang` | system `clang` | no |

All presets set `use_llvm_binutils`, replacing GNU binutils with
`ld.lld · llvm-ar · llvm-nm · llvm-objcopy · llvm-objdump · llvm-readelf · llvm-strip`.
**No GCC installation required.**

Cross-compilers (for arm64/arm targets):

```bash
forged setup-toolchain --install-cross-compilers
# or manually: sudo apt install gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf
```

More detail in [`docs/toolchains.md`](docs/toolchains.md).

---

## License

MIT — see [LICENSE](LICENSE). Forged with love by vxyzview.

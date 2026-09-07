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

`mrproper → defconfig → clang → AnyKernel3 zip`

[Go 1.24+](https://go.dev) · [LLVM/Clang](https://llvm.org) · [Bubble Tea](https://github.com/charmbracelet/bubbletea) · [MIT](LICENSE)

</div>

```text
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

## Why

Kernel building is a multi-step, copy-paste-from-Telegram ritual that breaks the moment you blink. forged collapses it into one command — live TUI, scrolling log, forge-fire colors — and packages a flashable AnyKernel3 ZIP when the smoke clears.

One static binary. No interpreter, no runtime, no GCC.

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | bash
```

| | |
|---|---|
| **Linux** | x86_64 · arm64 · x86 · armv7 |
| **macOS** | Apple Silicon · Intel |
| **Windows** | x86_64 · arm64 — [manual `.zip`](https://github.com/vxyzview/forged/releases/latest) |

Or with Go:

```bash
go install github.com/vxyzview/forged/cmd/forged@latest
```

> Kernel builds need Linux — that's where `make` and the AOSP Clang prebuilts live. Elsewhere, forged is your config wizard, source manager, and SSH co-pilot for a remote Linux box. The wizard auto-switches to `system-clang` and prints the right install hint (`brew install llvm`, `winget install LLVM.LLVM`).

---

## Quick start

```bash
forged setup-toolchain          # fetch AOSP Clang — first time only
forged build --wizard           # wizard writes your config, then the forge lights up
```

Flash the ZIP that lands in `releases/`.

```bash
forged build -c build_config.toml --no-clean --ccache   # incremental rebuild
```

---

## CLI

**`forged build`**

| | |
|---|---|
| `-c, --config PATH` | JSON or TOML build config |
| `-w, --wizard` | Interactive setup wizard |
| `-s, --source DIR` | Kernel source directory |
| `--source-url URL` | Clone the kernel source first |
| `-d, --defconfig NAME` | Defconfig |
| `-j, --jobs N` | Parallel jobs (`0` = auto) |
| `--no-clean` | Skip `mrproper` |
| `--no-package` | Skip the ZIP |
| `--version-tag TAG` | Tag on the output ZIP name |
| `-F, --make-flag VAR=VALUE` | Append a make variable |
| `-E, --env KEY=VALUE` | Inject an env var |
| `--ccache` / `--no-ccache` | Toggle ccache |

**`forged setup-toolchain`**

| | |
|---|---|
| `--preset PRESET` | `aosp-clang` (default) or `system-clang` |
| `--version VERSION` | AOSP Clang revision, e.g. `r584948b` |
| `--toolchain-dir DIR` | Storage path |
| `--install-cross-compilers` | Auto-install GNU cross-compilers |

**`forged config`** — wizard, saves a config · **`forged info`** — print a config summary · **`forged ccache-stats`** — cache statistics, `--zero` to reset

`forged build` also takes `--anykernel-source MODE` (`osm0sis` / `git` / `local` / `stub`) to override the staging source per run.

---

## Config

```toml
kernel_source    = "/path/to/kernel/source"
kernel_defconfig = "vendor/your_device_defconfig"
arch             = "arm64"
jobs             = 0        # 0 = auto

[toolchain]
preset             = "aosp-clang"
aosp_clang_version = "r584948b"
auto_clone         = true

[anykernel3]
source         = "osm0sis"       # osm0sis | git | local | stub
# repo_url     = "https://github.com/you/AnyKernel3-fork"   # git / local modes
kernel_name    = "Forged"
block          = "/dev/block/by-name/boot"
device_names   = ["your_device"]

[ccache]
enabled  = true
max_size = "5G"
```

Every field, annotated: [`config/example_build_config.toml`](config/example_build_config.toml). JSON works identically.

---

## Toolchains

| Preset | Source | Auto-download |
|---|---|---|
| `aosp-clang` | AOSP prebuilts | yes — linux-x86_64 only |
| `system-clang` | system `clang` | — |

All presets replace GNU binutils with LLVM:
`ld.lld · llvm-ar · llvm-nm · llvm-objcopy · llvm-objdump · llvm-readelf · llvm-strip`

**No GCC required.**

Cross-compilers for arm64/arm targets:

```bash
forged setup-toolchain --install-cross-compilers
```

More in [`docs/toolchains.md`](docs/toolchains.md).

---

## License

[MIT](LICENSE) — forged with love by [vxyzview](https://github.com/vxyzview).

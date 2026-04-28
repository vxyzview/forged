# Toolchain Reference

FORGED supports two toolchain presets for building Android kernels.

| Preset        | Description                                         | Requires manual setup? |
| ------------- | --------------------------------------------------- | ---------------------- |
| `aosp-clang`  | AOSP prebuilt Clang — downloaded automatically      | No (auto-downloaded)   |
| `system-clang`| System Clang already installed on `$PATH`           | Yes                    |

---

> **Migrating from `aosp-clang-r584948b`**
>
> The old version-pinned preset `aosp-clang-r584948b` has been replaced by the
> generic `aosp-clang` preset with a configurable `aosp_clang_version` field.
> The toolchain manager will automatically migrate the old name and emit a
> `DeprecationWarning`.  To silence the warning, update your build config:
>
> ```toml
> [toolchain]
> preset             = "aosp-clang"
> aosp_clang_version = "r584948b"
> ```

---

## `aosp-clang`

The recommended preset for Android kernel builds.  Downloads the official AOSP
prebuilt Clang tarball from `android.googlesource.com` using **aria2** for fast
multi-connection transfers (falls back to urllib when aria2 is not installed).

Downloads are cached on disk — subsequent builds skip the download entirely.

### Selecting a Clang version

Set `aosp_clang_version` in your build config to any valid AOSP Clang revision:

```toml
[toolchain]
preset             = "aosp-clang"
aosp_clang_version = "r584948b"   # default
# aosp_clang_version = "r522817"
```

Via the CLI:

```sh
forged setup-toolchain --preset aosp-clang --version r584948b
forged setup-toolchain --preset aosp-clang --version r522817
```

Via the interactive wizard (`forged build --wizard` or `forged config`):
after selecting the `aosp-clang` preset you will be prompted for the version
(press Enter to accept the default `r584948b`).

### Tarball URL pattern

```
https://android.googlesource.com/platform/prebuilts/clang/host/linux-x86
/+archive/refs/heads/main-kernel/clang-<version>.tar.gz
```

Example for the default version:

```
https://android.googlesource.com/platform/prebuilts/clang/host/linux-x86/+archive/refs/heads/main-kernel/clang-r584948b.tar.gz
```

### Storage layout

```
~/.local/share/kernel-builder/toolchains/
└── aosp-clang/
    └── clang-r584948b/
        ├── bin/
        │   ├── clang
        │   ├── clang++
        │   └── …
        └── lib/
```

### CROSS_COMPILE_ARM32

`CROSS_COMPILE_ARM32` (`arm-linux-gnueabihf-`) is **automatically** set from
the preset.  You do not need to configure it manually — it is not shown in the
interactive wizard.

### Manual download with aria2

If you prefer to manage the toolchain yourself:

```sh
mkdir -p ~/.local/share/kernel-builder/toolchains/aosp-clang
cd ~/.local/share/kernel-builder/toolchains/aosp-clang
aria2c --split=16 --max-connection-per-server=16 \
  "https://android.googlesource.com/platform/prebuilts/clang/host/linux-x86/+archive/refs/heads/main-kernel/clang-r584948b.tar.gz" \
  -o clang-r584948b.tar.gz
mkdir clang-r584948b && tar xzf clang-r584948b.tar.gz -C clang-r584948b
```

Then in your build config:

```toml
[toolchain]
preset     = "aosp-clang"
auto_clone = false
extra_path = ["~/.local/share/kernel-builder/toolchains/aosp-clang/clang-r584948b/bin"]
```

Or in JSON:

```json
"toolchain": {
  "preset": "aosp-clang",
  "aosp_clang_version": "r584948b",
  "auto_clone": false,
  "extra_path": [
    "~/.local/share/kernel-builder/toolchains/aosp-clang/clang-r584948b/bin"
  ]
}
```

---

## `system-clang`

Uses the Clang compiler already installed on the host system.  FORGED will
locate it via `$PATH` — no download is performed.

**Install on Debian/Ubuntu:**

```sh
sudo apt install clang lld llvm
```

```toml
[toolchain]
preset     = "system-clang"
auto_clone = false
```

---

## Cross-compilers

Both presets require GNU cross-compiler prefixes to be available on `$PATH`:

| Arch    | Binary                   | Package                   |
| ------- | ------------------------ | ------------------------- |
| arm64   | `aarch64-linux-gnu-gcc`  | `gcc-aarch64-linux-gnu`   |
| arm32   | `arm-linux-gnueabihf-gcc`| `gcc-arm-linux-gnueabihf` |

Install via:

```sh
sudo apt install gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf
```

Run `forged setup-toolchain` to check availability and optionally install them
automatically.

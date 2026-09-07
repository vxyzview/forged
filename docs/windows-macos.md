# Building on Windows (WSL2) and macOS (Docker)

Kernel compilation needs a real Linux host — `make`, the kernel's host
utilities and the AOSP Clang prebuilts are Linux-first. You do **not** need a
separate machine: forged runs natively on macOS and Windows for configs and
remote workflows, and both platforms have a first-class path to run builds
locally.

| Your host | Recommended path | Build performance |
|---|---|---|
| Windows 10/11 | **WSL2 (Ubuntu)** | native — best |
| Apple Silicon Mac | **Docker (this repo's image)** | good — linux/arm64 container |
| Intel Mac | **Docker** | good — linux/amd64 container |
| Linux | native, nothing extra | best |

---

## Windows: WSL2

1. Install WSL2 with Ubuntu (PowerShell, as Administrator):

   ```powershell
   wsl --install
   ```

   Reboot when asked, then open the *Ubuntu* app.

2. Inside Ubuntu, install forged:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | bash
   forged doctor    # verify git, make, clang, ccache, disk
   ```

3. Build as on any Linux box:

   ```bash
   forged setup-toolchain
   forged build --wizard
   ```

**Tips**

- Keep kernel trees **inside the WSL filesystem** (e.g. `~/kernel`), not under
  `/mnt/c/...` — builds on the 9p-mounted Windows drives are 5-10x slower.
- VS Code + the WSL extension gives you a full IDE against the same tree.
- The Windows-native `forged.exe` is still handy for editing configs and
  driving remote Linux builds over SSH — pick whichever side suits the task.

---

## macOS: Docker

The repo ships a ready-made build image — Ubuntu 24.04 with Clang/LLVM, the
kbuild host tools, cross-compilers, ccache and aria2 already installed.

```bash
git clone https://github.com/vxyzview/forged
cd forged
docker build -t forged .

# Run it against your kernel tree (mount it at /work):
docker run --rm -it -v "$PWD/my-kernel":/work -w /work forged
```

Inside the container it is a normal forged session:

```bash
forged doctor
forged build --wizard
```

The flashable ZIP lands in your kernel tree's `releases/` — it appears on the
macOS side through the bind mount. Pull the image parts you need:

- The container runs **linux/arm64 on Apple Silicon**; the AOSP prebuilt
  Clang is linux-x86_64 only, so use the `system-clang` preset there
  (`brew`'s sibling inside Ubuntu is already in the image). The wizard picks
  this automatically.
- ccache persists per container — pass `-v forged-ccache:/home/builder/.cache/ccache`
  to reuse it across runs.
- On Intel Macs everything works identically with `linux/amd64` images.

---

## Both hosts: SSH co-pilot

Prefer keeping the heavy iron remote? The native macOS/Windows binaries are
built for exactly that: edit the config locally, then run the build on your
Linux box/server over SSH. The TUI streams fine through an SSH session.

---

## FAQ

**WSL2 or Docker on Windows?**
WSL2 — it is native Linux, faster I/O, and forged installs into it directly.
Docker on Windows runs through WSL2 anyway.

**Can I flash the ZIP straight from macOS/Windows?**
The ZIP is just a file — copy it wherever your recovery lives. Only the
*build* needed Linux.

**`aosp-clang` in the Docker image on Apple Silicon?**
Not available (linux-x86_64 prebuilts only). The image's `system-clang` builds
kernels fine — clang is clang.

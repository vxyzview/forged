# Contributing to forged

Thanks for your interest in improving forged! This document covers the
basics. Please also read the [Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

Requirements:

- **Go 1.27+** (see the `toolchain` directive in `go.mod`)
- **git**, **make** — the build tooling forged drives
- Optional: **clang/lld/llvm**, GNU cross-compilers, ccache, aria2 — useful
  for actually building kernels locally; `forged doctor` tells you what's
  missing with fix commands.

```bash
git clone https://github.com/vxyzview/forged.git
cd forged
go build -o forged ./cmd/forged
go test ./...
./forged doctor || true
```

### E2E without a real kernel

`test/fixtures/kernel-fixture` is a tiny fake kernel tree that exercises the
full build + AnyKernel3 packaging pipeline in seconds — CI uses it too:

```bash
./forged build --ci \
  --source test/fixtures/kernel-fixture \
  --defconfig ci_defconfig \
  --toolchain-extra-path "$(dirname "$(command -v clang)")" \
  --version-tag dev
```

## Project layout

| Path | What lives there |
|---|---|
| `cmd/forged/` | entry point |
| `internal/cli/` | cobra commands, build runner, CI plumbing (`--ci`) |
| `internal/cienv/` | CI/CD provider detection + log dialects |
| `internal/builder/` | make orchestration, env injection, step runner |
| `internal/config/` | `BuildConfig`, presets, JSON/TOML I/O |
| `internal/packager/` | AnyKernel3 staging + ZIP creation |
| `internal/toolchain/` | AOSP Clang download, git clone, cross-compilers |
| `internal/tui/` | Bubble Tea live build monitor |
| `internal/wizard/` | huh-based interactive config wizard |

## Ground rules

- **gofmt-clean, `go vet`-clean.** CI enforces both.
- **Tests for behavior changes.** `go test ./...` must stay green.
- **No emoji in the README** — minimalist docs style; arrows and plain
  checkmarks inside code blocks are fine.
- **Keep the no-GCC story.** forged is LLVM/Clang-first; GNU
  cross-compilers are only convenience host tools. Don't add paths that
  require GNU ld/ar for the target.
- **Config compatibility.** Renames need a migration path in
  `config.ApplyPreset`-style aliasing or a `Validate()` warning, plus a
  changelog note.
- **Vendor-neutral CI code.** Provider-specific quirks belong in
  `internal/cienv`, not in the build pipeline.

## Pull requests

1. Fork, create a feature branch (`git checkout -b feat/thing`).
2. Commit with imperative subjects (`feat: …`, `fix: …`, `docs: …`).
3. Ensure `gofmt`, `go vet`, and `go test ./...` pass.
4. Open the PR against `main` and describe *why*, not just *what*.

## Reporting bugs

Open an issue with: forged version (`forged --version`), OS/arch, the
command you ran, and the relevant `out/logs/forged_issues_*.log` excerpt.
Redact anything you don't want public.

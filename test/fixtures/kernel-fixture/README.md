# kernel-fixture — a minimal kernel-tree stand-in

This directory is a tiny fake "kernel source tree" used by the CI smoke job
(`.github/workflows/ci.yml`, job `e2e`) to exercise forged's full build +
packaging pipeline without cloning a multi-gigabyte real kernel.

It satisfies exactly the make contract forged invokes:

| forged step | what the fixture Makefile does |
|---|---|
| `mrproper` (`O=out … mrproper`) | removes `$(O)` |
| `defconfig` (`O=out … ci_defconfig`) | writes `$(O)/.config` |
| default build (`O=out …`) | writes `$(O)/arch/arm64/boot/Image` |

## Layout

```
kernel-fixture/
├── Makefile                      # implements the three make targets above
└── arch/arm64/configs/ci_defconfig
```

## Run it locally

```bash
go build -o forged ./cmd/forged
# a working clang is only needed because forged verifies the toolchain;
# any directory containing an executable `clang` works:
./forged build --ci \
  --source test/fixtures/kernel-fixture \
  --defconfig ci_defconfig \
  --toolchain-extra-path /usr/bin
ls test/fixtures/kernel-fixture/releases/*.zip
```

Build outputs (`out/`, `releases/`, `AnyKernel3/`) inside this fixture are
git-ignored; CI always starts from a pristine checkout.

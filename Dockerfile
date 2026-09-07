# FORGED build environment — Ubuntu base with the full LLVM/Clang kernel
# toolchain and every host tool a kernel build needs.
#
#   docker build -t forged .
#   docker run --rm -it -v "$PWD":/work -w /work forged
#   forged setup-toolchain && forged build --wizard
#
# Works on Linux natively, and on macOS / Windows via Docker Desktop
# (on Apple Silicon the container runs linux/arm64 — use the system-clang
# preset there; the AOSP prebuilts are linux-x86_64 only).
#
# FORGED — Android Kernel Builder
# Copyright (c) 2026 vxyzview. Made with love.

FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/vxyzview/forged/internal/config.Version=docker" \
      -o /forged ./cmd/forged

FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

# Kernel build host deps:
#   clang/lld/llvm   the only toolchain forged needs
#   flex bison bc perl libssl-dev libelf-dev   kbuild host tools
#   gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf   cross-compilers
#   ccache aria2     optional speed-ups forged detects automatically
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git make \
        clang lld llvm \
        flex bison bc perl openssl dpkg-dev \
        libssl-dev libelf-dev dwarves \
        gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf \
        ccache aria2 \
        xz-utils zip \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /forged /usr/local/bin/forged

# Non-root builder; kernel trees are mounted at /work.
RUN useradd -ms /bin/bash builder
USER builder
WORKDIR /work

# Smoke test at build time.
RUN forged --version

ENTRYPOINT ["forged"]
CMD ["--help"]

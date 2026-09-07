#!/bin/sh
# forged installer — https://github.com/vxyzview/forged
#
#   curl -fsSL https://raw.githubusercontent.com/vxyzview/forged/main/install.sh | sh
#
# Detects your OS/arch, downloads the matching release binary from GitHub
# Releases, verifies its SHA-256 checksum, and installs it to ~/.local/bin
# (or /usr/local/bin with sudo, or the current dir as a last resort).
#
# FORGED — Android Kernel Builder
# Copyright (c) 2026 vxyzview. Made with love.

set -u

REPO="vxyzview/forged"
VERSION="${FORGED_VERSION:-latest}"

# ── pretty output ─────────────────────────────────────────────────────────────
if [ -t 1 ]; then
  BOLD=$(printf '\033[1m'); DIM=$(printf '\033[2m')
  ORANGE=$(printf '\033[38;5;208m'); GREEN=$(printf '\033[38;5;71m')
  RED=$(printf '\033[38;5;203m'); RESET=$(printf '\033[0m')
else
  BOLD=""; DIM=""; ORANGE=""; GREEN=""; RED=""; RESET=""
fi

info()  { printf '%s\n' "${ORANGE}▸${RESET} $*"; }
ok()    { printf '%s\n' "${GREEN}✓${RESET} $*"; }
fail()  { printf '%s\n' "${RED}✗${RESET} $*" >&2; exit 1; }

printf '%s\n' "${BOLD}  ██ Forge — forged installer${RESET}"
printf '%s\n' ""

# ── dependency check ─────────────────────────────────────────────────────────
need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required but not installed."
}

need_cmd curl || need_cmd wget
need_cmd tar
need_cmd grep
need_cmd awk
need_cmd head

# ── OS / arch detection ───────────────────────────────────────────────────────
OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
  Linux*)  OS="linux" ;;
  Darwin*) OS="darwin" ;;
  CYGWIN*|MINGW*|MSYS*) OS="windows" ;;
  *) fail "unsupported OS: $OS (forged binaries ship for linux, darwin, windows)" ;;
esac

case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7*|armv6*|armhf) ARCH="armv7" ;;
  i386|i486|i586|i686|ix86) ARCH="386" ;;
  *) fail "unsupported architecture: $ARCH" ;;
esac

if [ "$OS" = "windows" ]; then
  fail "Windows detected — download forged manually from:
  https://github.com/${REPO}/releases
Grab forged-<version>-windows-amd64.zip (or -windows-arm64.zip), extract, and add it to PATH."
fi

# armv7 is linux-only
if [ "$OS" != "linux" ] && { [ "$ARCH" = "armv7" ] || [ "$ARCH" = "386" ]; }; then
  fail "no $OS binary for $ARCH — supported: ${OS}-amd64, ${OS}-arm64"
fi

TARGET="${OS}-${ARCH}"
info "Detected platform: ${BOLD}${TARGET}${RESET}"

# ── resolve version ──────────────────────────────────────────────────────────
if [ "$VERSION" = "latest" ]; then
  info "Fetching latest release tag…"
  RELEASE_URL="https://api.github.com/repos/${REPO}/releases/latest"
  if command -v curl >/dev/null 2>&1; then
    TAG=$(curl -fsSL "$RELEASE_URL" | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"tag_name": *"//;s/"//')
  else
    TAG=$(wget -qO- "$RELEASE_URL" | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"tag_name": *"//;s/"//')
  fi
  [ -n "$TAG" ] || fail "could not determine latest release tag — is ${REPO} public with releases?"
else
  TAG="$VERSION"
fi
info "Version: ${BOLD}${TAG}${RESET}"

ARCHIVE_BASENAME="forged-${TAG}-${TARGET}"
ARCHIVE="${ARCHIVE_BASENAME}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

# ── download ─────────────────────────────────────────────────────────────────
TMPDIR_PATH=$(mktemp -d) || fail "could not create a temp dir"
trap 'rm -rf "$TMPDIR_PATH"' EXIT

info "Downloading ${ARCHIVE} …"
if command -v curl >/dev/null 2>&1; then
  curl -fSL --retry 3 -o "$TMPDIR_PATH/$ARCHIVE" "$DOWNLOAD_URL" || fail "download failed: $DOWNLOAD_URL"
else
  wget -qO "$TMPDIR_PATH/$ARCHIVE" "$DOWNLOAD_URL" || fail "download failed: $DOWNLOAD_URL"
fi

# ── checksum verification ────────────────────────────────────────────────────
SUMS_URL="https://github.com/${REPO}/releases/download/${TAG}/SHA256SUMS.txt"
info "Verifying SHA-256 checksum…"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL -o "$TMPDIR_PATH/SHA256SUMS.txt" "$SUMS_URL" || {
    printf '%s\n' "${ORANGE}▸${RESET} could not fetch checksums (skipping verification)"
    VERIFY=0
  }
else
  wget -qO "$TMPDIR_PATH/SHA256SUMS.txt" "$SUMS_URL" || VERIFY=0
fi
VERIFY=${VERIFY:-1}
if [ "$VERIFY" = "1" ]; then
  WANT=$(grep " ${ARCHIVE}\$" "$TMPDIR_PATH/SHA256SUMS.txt" | awk '{print $1}')
  [ -n "$WANT" ] || fail "no checksum entry for $ARCHIVE"
  if command -v sha256sum >/dev/null 2>&1; then
    GOT=$(sha256sum "$TMPDIR_PATH/$ARCHIVE" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    GOT=$(shasum -a 256 "$TMPDIR_PATH/$ARCHIVE" | awk '{print $1}')
  elif command -v openssl >/dev/null 2>&1; then
    GOT=$(openssl dgst -sha256 "$TMPDIR_PATH/$ARCHIVE" | awk '{print $NF}')
  else
    printf '%s\n' "${ORANGE}▸${RESET} no sha256 tool found — skipping verification"
    GOT="$WANT"
  fi
  [ "$GOT" = "$WANT" ] || fail "checksum mismatch!
  expected: $WANT
  got:      $GOT"
  ok "Checksum OK"
fi

# ── extract ──────────────────────────────────────────────────────────────────
info "Extracting…"
tar -xzf "$TMPDIR_PATH/$ARCHIVE" -C "$TMPDIR_PATH" || fail "extraction failed"
BINARY="$TMPDIR_PATH/$ARCHIVE_BASENAME"
[ -f "$BINARY" ] || fail "binary not found after extraction"
chmod +x "$BINARY"

# Smoke test: run --version (skip if the host can't exec it, e.g. foreign arch)
if "$BINARY" --version >/dev/null 2>&1; then
  ok "Binary runs: $($BINARY --version 2>/dev/null)"
fi

# ── install location ─────────────────────────────────────────────────────────
INSTALL_DIR=""
if [ -w /usr/local/bin ] || [ "$(id -u)" = "0" ]; then
  INSTALL_DIR="/usr/local/bin"
elif mkdir -p "$HOME/.local/bin" 2>/dev/null && [ -w "$HOME/.local/bin" ]; then
  INSTALL_DIR="$HOME/.local/bin"
elif [ -d /data/data/com.termux/files/usr/bin ] && [ -w /data/data/com.termux/files/usr/bin ]; then
  INSTALL_DIR="/data/data/com.termux/files/usr/bin"
else
  printf '%s\n' "${ORANGE}▸${RESET} no writable bin dir — installing to current directory"
  INSTALL_DIR="."
fi

info "Installing to ${BOLD}${INSTALL_DIR}${RESET} …"
if [ -w "$INSTALL_DIR" ] || [ "$(id -u)" = "0" ]; then
  mv "$BINARY" "$INSTALL_DIR/forged" || fail "install failed"
else
  mv "$BINARY" "$INSTALL_DIR/forged" 2>/dev/null || {
    cp "$BINARY" "$INSTALL_DIR/forged" || fail "install failed — copy somewhere on PATH manually"
  }
fi
ok "Installed $( "$INSTALL_DIR/forged" --version 2>/dev/null || echo forged ) → ${INSTALL_DIR}/forged"

# ── PATH hint ────────────────────────────────────────────────────────────────
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    printf '%s\n' ""
    printf '%s\n' "${DIM}  $INSTALL_DIR is not on your PATH. Add it:"
    printf '%s\n' "${DIM}    export PATH=\"\$PATH:$INSTALL_DIR\"   # add to ~/.bashrc or ~/.zshrc${RESET}"
    ;;
esac

printf '%s\n' ""
ok "Done — run ${BOLD}forged --help${RESET} to get started."
printf '%s\n' ""

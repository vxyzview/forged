"""
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.
"""

from __future__ import annotations

from importlib.metadata import PackageNotFoundError
from importlib.metadata import version as _pkg_version

from rich.align import Align
from rich.box import HEAVY, HEAVY_HEAD
from rich.columns import Columns
from rich.console import Console
from rich.panel import Panel
from rich.table import Table
from rich.text import Text

# ── FORGED banner art ─────────────────────────────────────────────────────────

BANNER = r"""
 ▄████  ▒█████   ██▀███    ▄████ ▓█████ ▓█████▄
 ██▒ ▀█▒▒██▒  ██▒▓██ ▒ ██▒ ██▒ ▀█▒▓█   ▀ ▒██▀ ██▌
▒██░▄▄▄░▒██░  ██▒▓██ ░▄█ ▒▒██░▄▄▄░▒███   ░██   █▌
░▓█  ██▓▒██   ██░▒██▀▀█▄  ░▓█  ██▓▒▓█  ▄ ░▓█▄   ▌
░▒▓███▀▒░ ████▓▒░░██▓ ▒██▒░▒▓███▀▒░▒████▒░▒████▓
 ░▒   ▒ ░ ▒░▒░▒░ ░ ▒▓ ░▒▓░ ░▒   ▒ ░░ ░  ░ ░ ▒  ▒
  ░   ░   ░ ▒ ▒░   ░▒ ░ ▒░  ░   ░    ░    ░ ░  ░
░ ░   ░ ░ ░ ░ ▒    ░░   ░ ░ ░   ░    ░      ░
      ░     ░ ░     ░           ░    ░  ░"""

_TAGLINE   = "Android Kernel Builder  ·  AnyKernel3 Ready  ·  LLVM / Clang"
_COPYRIGHT = "© 2026 vxyzview  —  Made with love"

try:
    _VERSION = f"v{_pkg_version('forged')}"
except PackageNotFoundError:
    _VERSION = "v1.0.0"

# ── Feature badges ────────────────────────────────────────────────────────────

_BADGES = [
    ("  AOSP Clang  ", "#ff7c00"),
    ("  AnyKernel3  ", "#ffbe00"),
    ("  arm64 · arm  ", "#a8b2c0"),
    ("   LTO Ready   ", "#39d353"),
    ("   ccache ⚡   ", "#ff9f2f"),
]


def _badge(label: str, colour: str) -> Panel:
    """Single compact badge rendered as a heavy-bordered panel."""
    return Panel(
        Align.center(Text(label, style=f"bold {colour}")),
        border_style=colour,
        box=HEAVY,
        padding=(0, 1),
        expand=False,
    )


def _make_info_table() -> Table:
    """Render a compact four-column info strip."""
    table = Table.grid(padding=(0, 4))
    table.add_column(style="bold #ff7c00",  justify="right")
    table.add_column(style="#a8b2c0",       justify="left")
    table.add_column(style="bold #ff7c00",  justify="right")
    table.add_column(style="#a8b2c0",       justify="left")

    table.add_row("TOOLCHAIN", "LLVM / Clang  ·  no GCC",  "CACHE",    "ccache  ·  60–90% faster rebuilds")
    table.add_row("PACKAGING", "AnyKernel3  ·  flashable ZIP", "CONFIG", "TOML / JSON  ·  wizard included")
    return table


def print_banner(console: Console) -> None:
    """Render the FORGED startup banner with forge-fire styling."""

    art  = Text(BANNER, style="bold #ff7c00", justify="center")

    ver_line = Text(justify="center")
    ver_line.append("  ◆  ", style="#5a3010")
    ver_line.append(_VERSION, style="bold #ff9f2f")
    ver_line.append("  ◆  ", style="#5a3010")
    ver_line.append(_TAGLINE, style="#ffbe00")
    ver_line.append("  ◆  ", style="#5a3010")

    copy_ = Text(_COPYRIGHT, style="#5a3010", justify="center")

    body = Text(justify="center")
    body.append_text(art)
    body.append("\n")
    body.append_text(ver_line)
    body.append("\n")
    body.append_text(copy_)

    console.print()
    console.print(
        Panel(
            Align.center(body),
            border_style="#ff7c00",
            box=HEAVY,
            padding=(0, 2),
            expand=True,
        )
    )

    # ── Feature badge strip ───────────────────────────────────────────────
    console.print(
        Columns(
            [_badge(label, col) for label, col in _BADGES],
            equal=True,
            expand=True,
        )
    )

    # ── Info strip ────────────────────────────────────────────────────────
    console.print(
        Panel(
            Align.center(_make_info_table()),
            border_style="#3a1a00",
            box=HEAVY_HEAD,
            padding=(0, 4),
            expand=True,
        )
    )
    console.print()


def print_farewell(
    console: Console,
    success: bool,
    elapsed: float,
    zip_output_dir: str = "releases",
) -> None:
    """Print a styled farewell panel after the build completes."""
    if success:
        colour = "#39d353"
        msg = Text(justify="center")
        msg.append("  ✦  BUILD COMPLETE  ✦  \n\n", style=f"bold {colour}")
        msg.append("  Total time  ", style="#a8b2c0")
        msg.append(f"{elapsed:.1f}s", style="bold white")
        msg.append("   ·   ", style="#606060")
        msg.append("Flashable ZIP ready in ", style="#a8b2c0")
        msg.append(f"{zip_output_dir}/", style="bold #ffbe00")
    else:
        colour = "#ff4444"
        msg = Text(justify="center")
        msg.append("  ✗  BUILD FAILED  ✗  \n\n", style=f"bold {colour}")
        msg.append("  Total time  ", style="#a8b2c0")
        msg.append(f"{elapsed:.1f}s", style="bold white")
        msg.append("   ·   ", style="#606060")
        msg.append("Check the full log above for errors", style="#ff9f2f")

    console.print()
    console.print(
        Panel(
            Align.center(msg),
            border_style=colour,
            box=HEAVY,
            padding=(1, 6),
            expand=True,
        )
    )
    console.print()

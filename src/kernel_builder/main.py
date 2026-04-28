#!/usr/bin/env python3
"""
FORGED — Android Kernel Builder entry point.
Copyright (c) 2026 vxyzview. Made with love.
"""

from __future__ import annotations

import logging
import sys

from rich.box import HEAVY_HEAD
from rich.console import Console
from rich.logging import RichHandler
from rich.panel import Panel
from rich.text import Text

from .banner import print_banner
from .cli import build_cli
from .display import PALETTE

# ── Logging setup ─────────────────────────────────────────────────────────────

if not logging.root.handlers:
    logging.basicConfig(
        level=logging.INFO,
        format="%(message)s",
        handlers=[
            RichHandler(
                rich_tracebacks=True,
                markup=True,
                show_path=False,
                show_time=False,
                show_level=True,
            )
        ],
    )

console = Console()


def main() -> int:
    """Application entry point."""
    print_banner(console)
    app = build_cli()
    try:
        return app()
    except KeyboardInterrupt:
        console.print()
        msg = Text(justify="center")
        msg.append("  ⚡  Build interrupted by user  ⚡  ", style=f"bold {PALETTE['warning']}")
        console.print(
            Panel(
                msg,
                border_style=PALETTE["warning"],
                box=HEAVY_HEAD,
                padding=(0, 2),
            )
        )
        console.print()
        return 130
    except Exception as exc:
        console.print()
        console.print(
            Panel(
                Text.from_markup(f"  [{PALETTE['error']}]{exc}[/{PALETTE['error']}]"),
                title=f"[bold {PALETTE['error']}]  ✗  Fatal Error  [/bold {PALETTE['error']}]",
                border_style=PALETTE["error"],
                box=HEAVY_HEAD,
                padding=(0, 2),
            )
        )
        console.print()
        return 1


def run() -> None:
    """Console entry point — calls main() and propagates its exit code."""
    sys.exit(main())


if __name__ == "__main__":
    sys.exit(main())

"""
FORGED — Rich TUI components: panels, progress, tables, live log.
Copyright (c) 2026 vxyzview. Made with love.
"""

from __future__ import annotations

import datetime
import re
from pathlib import Path

from rich.box import HEAVY_HEAD
from rich.panel import Panel
from rich.progress import (
    BarColumn,
    MofNCompleteColumn,
    Progress,
    SpinnerColumn,
    TaskProgressColumn,
    TextColumn,
    TimeElapsedColumn,
)
from rich.rule import Rule
from rich.table import Column, Table
from rich.text import Text

from .builder import BuildResult
from .config import BuildConfig

# ── Forge-fire colour palette ─────────────────────────────────────────────────
#
#  No gradients, no transparency — pure solid terminal colours.
#  Every colour is drawn from a "hot forge" theme:
#    · #ff7c00  forge orange  — primary brand / borders / accents
#    · #ffbe00  molten gold   — secondary highlights / taglines
#    · #a8b2c0  cool steel    — neutral UI chrome
#    · #39d353  forge green   — success states
#    · #ff4444  quench red    — error states
#    · #ff9f2f  ember amber   — warnings / progress accent
#    · #606060  dark steel    — dim / muted text
#    · #3a1a00  deep forge    — dark borders / alternating rows
#    · white / bold white     — primary content text

PALETTE = {
    "primary":   "#ff7c00",   # forge orange
    "secondary": "#ffbe00",   # molten gold
    "success":   "#39d353",   # bright green
    "error":     "#ff4444",   # red
    "warning":   "#ff9f2f",   # ember amber
    "dim":       "#606060",   # dark steel
    "steel":     "#a8b2c0",   # cool steel
    "header":    "bold white",
    "accent":    "#ff9f2f",   # ember amber
    "deep":      "#3a1a00",   # deep forge dark
    "brand":     "#c84800",   # deep orange brand
}

# ── Step phase icons ──────────────────────────────────────────────────────────

_PHASE_ICONS: dict[str, str] = {
    "mrproper":  "◈",
    "defconfig": "◉",
    "compile":   "⬡",
    "clone":     "↓",
    "package":   "◎",
    "setup":     "⚙",
}


def _phase_icon(name: str) -> str:
    lower = name.lower()
    for key, icon in _PHASE_ICONS.items():
        if key in lower:
            return icon
    return "◆"


# ── Log line colouriser ────────────────────────────────────────────────────────

_ERROR_RE   = re.compile(r"\berror\b",   re.IGNORECASE)
_WARN_RE    = re.compile(r"\bwarning\b", re.IGNORECASE)
_NOTE_RE    = re.compile(r"\bnote\b",    re.IGNORECASE)
_OK_RE      = re.compile(r"\b(OK|PASS|done|success)\b", re.IGNORECASE)
_LINK_RE    = re.compile(r"\bLD\b|\bLINK\b")
_CC_RE      = re.compile(r"\b(CC|AS|AR|OBJCOPY|STRIP|NM)\b")
_GEN_RE     = re.compile(r"\b(GEN|INSTALL|HOSTCC|HOSTLD)\b")
_MAKE_RE    = re.compile(r"^\s*make\[")


def _colourise_log_line(line: str) -> Text:
    """Apply colour hints to a raw compiler/make output line."""
    t = Text(overflow="fold")
    stripped = line.rstrip()
    if not stripped:
        t.append(line, style="white")
        return t

    if _ERROR_RE.search(line):
        t.append("  ✗  ", style="bold #ff4444")
        t.append(stripped, style="#ff6b6b")
    elif _WARN_RE.search(line):
        t.append("  ▲  ", style="#ffbe00")
        t.append(stripped, style="#ffbe00")
    elif _NOTE_RE.search(line):
        t.append("  ›  ", style="#a8b2c0")
        t.append(stripped, style="#a8b2c0")
    elif _OK_RE.search(line):
        t.append("  ✦  ", style="#39d353")
        t.append(stripped, style="#39d353")
    elif _LINK_RE.search(line):
        t.append("  ⬡  ", style="#ff9f2f")
        t.append(stripped, style="#ff9f2f")
    elif _GEN_RE.search(line):
        t.append("  ◉  ", style="#c8d0d8")
        t.append(stripped, style="#c8d0d8")
    elif _CC_RE.search(line):
        t.append("  ◆  ", style="#d8e0e8")
        t.append(stripped, style="#d8e0e8")
    elif _MAKE_RE.search(line):
        t.append("     ", style="white")
        t.append(stripped, style="#606060")
    else:
        t.append("     ", style="white")
        t.append(stripped, style="white")
    return t


# ── Phase header ──────────────────────────────────────────────────────────────


def render_phase_header(title: str, step_num: int, total_steps: int) -> Rule:
    """Render a styled section divider for a build phase."""
    icon    = _phase_icon(title)
    counter = f"[{PALETTE['dim']}]{step_num}/{total_steps}[/{PALETTE['dim']}]"
    label   = (
        f"[bold white on {PALETTE['primary']}]  {icon}  {title.upper()}  {icon}  "
        f"[/bold white on {PALETTE['primary']}]"
    )
    return Rule(
        title=f"{label}  {counter}",
        style=PALETTE["primary"],
        characters="━",
    )


# ── Config summary ─────────────────────────────────────────────────────────────


def render_config_table(cfg: BuildConfig) -> Table:
    """Render a Rich table summarising the active build config."""
    table = Table(
        title=Text("  ◆  BUILD CONFIGURATION  ◆  ", style="bold white on #ff7c00"),
        show_header=True,
        header_style=f"bold {PALETTE['secondary']}",
        border_style=PALETTE["primary"],
        box=HEAVY_HEAD,
        expand=True,
        show_lines=True,
        padding=(0, 2),
        title_justify="center",
    )
    table.add_column("Parameter", style=f"bold {PALETTE['steel']}", no_wrap=True, ratio=1)
    table.add_column("Value",     style=PALETTE["secondary"],        ratio=2)

    def _val(v: str) -> str:
        return v if v.strip() else f"[{PALETTE['dim']}]‹not set›[/{PALETTE['dim']}]"

    # ── ccache display ────────────────────────────────────────────────────────
    if cfg.ccache.enabled:
        cc_display = f"[bold white]ccache[/bold white]  {cfg.toolchain.cc}"
        ccache_dir_display = (
            cfg.ccache.dir
            or f"[{PALETTE['dim']}]default  (~/.cache/ccache)[/{PALETTE['dim']}]"
        )
        ccache_status = (
            f"[{PALETTE['success']}]⚡ enabled[/{PALETTE['success']}]  "
            f"max {cfg.ccache.max_size}  ·  "
            f"compress {'on' if cfg.ccache.compress else 'off'}"
        )
    else:
        cc_display = cfg.toolchain.cc
        ccache_status = f"[{PALETTE['dim']}]disabled[/{PALETTE['dim']}]"
        ccache_dir_display = ""

    rows: list[tuple[str, str]] = [
        ("Kernel source",    _val(cfg.kernel_source or "")),
        ("Defconfig",        f"[bold white]{cfg.kernel_defconfig}[/bold white]"),
        ("Arch / Sub-arch",  f"[bold white]{cfg.arch}[/bold white] / {cfg.subarch}"),
        ("Output dir",       cfg.output_dir),
        ("Jobs",             str(cfg.jobs) if cfg.jobs > 0 else f"[{PALETTE['dim']}]auto (cpu count)[/{PALETTE['dim']}]"),
        ("LTO",              f"[bold white]{cfg.lto}[/bold white]" if cfg.lto else f"[{PALETTE['dim']}]disabled[/{PALETTE['dim']}]"),
        ("Build user",       cfg.kbuild_build_user),
        ("Build host",       cfg.kbuild_build_host),
        ("Localversion",     cfg.localversion or f"[{PALETTE['dim']}]‹none›[/{PALETTE['dim']}]"),
        ("Toolchain preset", f"[bold white]{cfg.toolchain.preset}[/bold white]"),
        ("CC",               cc_display),
        ("Cross compile",    cfg.toolchain.cross_compile),
        ("ccache",           ccache_status),
        *([(  "  ↳ ccache dir", ccache_dir_display)] if cfg.ccache.enabled else []),
        ("AK3 kernel name",  f"[bold white]{cfg.anykernel3.kernel_name}[/bold white]"),
        ("AK3 block",        cfg.anykernel3.block),
        (
            "AK3 devices",
            ", ".join(cfg.anykernel3.device_names)
            or f"[{PALETTE['dim']}]‹all devices›[/{PALETTE['dim']}]",
        ),
        ("AK3 slot device",  f"[{PALETTE['success']}]yes[/{PALETTE['success']}]" if cfg.anykernel3.is_slot_device else f"[{PALETTE['dim']}]no[/{PALETTE['dim']}]"),
    ]

    for i, (param, value) in enumerate(rows):
        row_style = "" if i % 2 == 0 else "on #160a00"
        table.add_row(param, value, style=row_style)

    return table


# ── Step progress ─────────────────────────────────────────────────────────────


def make_build_progress() -> Progress:
    """Create a styled Rich progress bar for build steps."""
    return Progress(
        SpinnerColumn(style=PALETTE["primary"], spinner_name="aesthetic"),
        TextColumn(
            "[bold white]{task.description}",
            justify="left",
            table_column=Column(ratio=3),
        ),
        BarColumn(
            bar_width=None,
            style="#2a1000",
            complete_style="#ff7c00",
            finished_style="#39d353",
            pulse_style="#ff9f2f",
            table_column=Column(ratio=4),
        ),
        TaskProgressColumn(style="#ffbe00"),
        MofNCompleteColumn(table_column=Column(style="#606060", justify="right")),
        TimeElapsedColumn(),
        expand=True,
        transient=False,
    )


# ── Checklist renderer ────────────────────────────────────────────────────────


def render_pre_build_checklist(
    has_source: bool,
    has_toolchain: bool,
    has_ccache: bool,
    ccache_enabled: bool,
    clean: bool,
    package: bool,
) -> Panel:
    """Render a pre-build checklist panel showing what will happen."""

    def _item(label: str, active: bool, note: str = "", warn: bool = False) -> str:
        if active:
            mark  = f"[bold {PALETTE['success']}]  ✦[/bold {PALETTE['success']}]"
            lbl   = f"[bold white]{label}[/bold white]"
        elif warn:
            mark  = f"[bold {PALETTE['warning']}]  ▲[/bold {PALETTE['warning']}]"
            lbl   = f"[{PALETTE['warning']}]{label}[/{PALETTE['warning']}]"
        else:
            mark  = f"[{PALETTE['dim']}]  ○[/{PALETTE['dim']}]"
            lbl   = f"[{PALETTE['dim']}]{label}[/{PALETTE['dim']}]"
        suffix = f"   [{PALETTE['dim']}]{note}[/{PALETTE['dim']}]" if note else ""
        return f"{mark}  {lbl}{suffix}"

    ccache_active = ccache_enabled and has_ccache
    ccache_warn   = ccache_enabled and not has_ccache

    lines = [
        _item("Kernel source",      has_source,
              "path verified" if has_source else "path not verified",
              warn=not has_source),
        _item("Toolchain",          has_toolchain,
              "configured" if has_toolchain else "will auto-download"),
        _item("mrproper  (clean)",  clean,
              "full clean" if clean else "skipped  (--no-clean)"),
        _item("defconfig",          True,    "always"),
        _item("compile",            True,    "always"),
        _item("ccache",             ccache_active,
              "60–90% faster rebuilds" if ccache_active else
              ("disabled" if not ccache_enabled else "not found in PATH"),
              warn=ccache_warn),
        _item("AnyKernel3 package", package,
              "produces flashable ZIP" if package else "skipped  (--no-package)"),
    ]

    body = Text.from_markup("\n".join(lines))
    return Panel(
        body,
        title=f"[bold white on {PALETTE['primary']}]  ◆  PRE-BUILD CHECKLIST  ◆  [/bold white on {PALETTE['primary']}]",
        border_style=PALETTE["primary"],
        box=HEAVY_HEAD,
        padding=(1, 3),
    )


# ── Result rendering ──────────────────────────────────────────────────────────


def render_result_panel(result: BuildResult) -> Panel:
    """Render a pass/fail panel for a single build step."""
    icon  = _phase_icon(result.step)
    if result.success:
        colour  = PALETTE["success"]
        heading = f"[bold {colour}]{icon}  {result.step.upper()}  —  PASSED[/bold {colour}]"
        content = Text()
        content.append("  STATUS    ", style=f"bold {PALETTE['steel']}")
        content.append("PASSED\n",    style=f"bold {colour}")
        content.append("  DURATION  ", style=f"bold {PALETTE['steel']}")
        content.append(f"{result.duration:.2f}s", style="bold white")
    else:
        colour  = PALETTE["error"]
        heading = f"[bold {colour}]{icon}  {result.step.upper()}  —  FAILED[/bold {colour}]"
        content = Text()
        content.append("  STATUS    ", style=f"bold {PALETTE['steel']}")
        content.append("FAILED\n",    style=f"bold {colour}")
        content.append("  ERROR     ", style=f"bold {PALETTE['steel']}")
        content.append(f"{result.error}\n", style="white")
        content.append("  DURATION  ", style=f"bold {PALETTE['steel']}")
        content.append(f"{result.duration:.2f}s", style=f"{PALETTE['dim']}")

    return Panel(
        content,
        title=heading,
        border_style=colour,
        box=HEAVY_HEAD,
        padding=(0, 2),
    )


def render_build_summary(
    results: list[BuildResult],
    zip_path: Path | None = None,
) -> Table:
    """Render a summary table of all build steps with timing."""
    all_ok  = all(r.success for r in results)
    total   = sum(r.duration for r in results)

    overall_colour = PALETTE["success"] if all_ok else PALETTE["error"]
    overall_label  = "  ✦  ALL STEPS PASSED  " if all_ok else "  ✗  BUILD FAILED  "

    table = Table(
        title=Text(overall_label, style=f"bold white on {overall_colour}"),
        show_header=True,
        header_style=f"bold {PALETTE['secondary']}",
        border_style=overall_colour,
        box=HEAVY_HEAD,
        expand=True,
        show_lines=True,
        padding=(0, 2),
        title_justify="center",
    )
    table.add_column("Step",     style=f"bold {PALETTE['steel']}")
    table.add_column("Status",   justify="center", style="bold white", no_wrap=True)
    table.add_column("Duration", justify="right",  style=PALETTE["dim"],    no_wrap=True)

    for i, result in enumerate(results):
        icon = _phase_icon(result.step)
        if result.success:
            status = f"[{PALETTE['success']}]{icon}  PASS[/{PALETTE['success']}]"
        else:
            status = f"[{PALETTE['error']}]✗  FAIL[/{PALETTE['error']}]"
        row_style = "" if i % 2 == 0 else "on #160a00"
        table.add_row(result.step, status, f"{result.duration:.2f}s", style=row_style)

    table.add_section()
    table.add_row(
        "[bold white]Total[/bold white]",
        f"[bold {overall_colour}]{overall_label.strip()}[/bold {overall_colour}]",
        f"[bold white]{total:.2f}s[/bold white]",
    )

    if zip_path:
        table.add_section()
        size_mb = zip_path.stat().st_size / 1_048_576
        table.add_row(
            f"[bold {PALETTE['primary']}]◎  Output ZIP[/bold {PALETTE['primary']}]",
            f"[bold {PALETTE['secondary']}]{zip_path.name}[/bold {PALETTE['secondary']}]",
            f"[{PALETTE['steel']}]{size_mb:.2f} MB[/{PALETTE['steel']}]",
        )

    return table


# ── Live log panel ─────────────────────────────────────────────────────────────


class LiveLogPanel:
    """
    Scrolling log panel rendered inside a Rich Live context.

    All lines are retained in ``_all_lines`` so the full build output is
    available after the build finishes via ``get_all_lines()``.  The live
    display shows only the most recent ``LIVE_WINDOW`` lines to fit the
    terminal, but nothing is ever discarded.
    """

    LIVE_WINDOW: int = 35

    def __init__(self, title: str = "Build Output") -> None:
        self.title     = title
        self._all_lines:  list[str] = []
        self._live_lines: list[str] = []

    # ── Mutation ──────────────────────────────────────────────────────────────

    def add_line(self, line: str) -> None:
        """Append *line* to the full history and update the live window."""
        self._all_lines.append(line)
        self._live_lines.append(line)
        if len(self._live_lines) > self.LIVE_WINDOW:
            self._live_lines = self._live_lines[-self.LIVE_WINDOW :]

    # ── Read-only accessors ───────────────────────────────────────────────────

    def get_all_lines(self) -> list[str]:
        """Return every line captured since the panel was created."""
        return list(self._all_lines)

    def line_count(self) -> int:
        """Total lines collected so far."""
        return len(self._all_lines)

    def get_errors_and_warnings(self) -> list[tuple[str, str]]:
        """Return a list of (tag, line) tuples for every error and warning line.

        Each entry is tagged as ``"ERROR"`` or ``"WARNING"`` according to which
        pattern matched first.  Lines that match both patterns (rare in
        practice) are tagged ``"ERROR"`` — the higher severity wins.

        Returns an empty list when no issues were found.
        """
        results: list[tuple[str, str]] = []
        for line in self._all_lines:
            if _ERROR_RE.search(line):
                results.append(("ERROR", line))
            elif _WARN_RE.search(line):
                results.append(("WARNING", line))
        return results

    # ── Rendering ─────────────────────────────────────────────────────────────

    def render(self) -> Panel:
        """Render the sliding-window panel for use inside a Live context."""
        hidden = self.line_count() - self.LIVE_WINDOW
        overflow_note = (
            f"[{PALETTE['dim']}]  ┄  {hidden} earlier lines hidden "
            f"(full log shown after build)  ┄[/{PALETTE['dim']}]\n"
            if hidden > 0
            else ""
        )

        text_body = Text(overflow="fold")
        if overflow_note:
            text_body.append(overflow_note)
        for raw_line in self._live_lines:
            text_body.append_text(_colourise_log_line(raw_line))
            text_body.append("\n")

        subtitle = (
            f"[{PALETTE['dim']}]  {self.line_count()} lines  ·  "
            f"showing last {min(self.line_count(), self.LIVE_WINDOW)}  [/{PALETTE['dim']}]"
        )

        return Panel(
            text_body,
            title=f"[bold {PALETTE['secondary']}]  ◉  {self.title}  [/bold {PALETTE['secondary']}]",
            subtitle=subtitle,
            border_style=PALETTE["primary"],
            box=HEAVY_HEAD,
            padding=(0, 1),
        )


def save_build_log_file(
    log_panel: LiveLogPanel,
    dest: Path,
    cfg: "BuildConfig | None" = None,
    results: "list[BuildResult] | None" = None,
) -> tuple[int, int]:
    """Write errors and warnings from *log_panel* to *dest* as plain text.

    The output file has three sections:

    1. **Header** — build metadata (defconfig, arch, timestamp, total log lines).
    2. **Issues** — every line tagged ``[ERROR]`` or ``[WARNING]``, in the order
       they appeared in the build log.  Each entry also carries a 1-based
       sequence number so it is easy to search or count in any text editor.
    3. **Summary** — per-step pass/fail table and total error/warning counts.

    Parameters
    ----------
    log_panel:
        The ``LiveLogPanel`` instance that collected the build output.
    dest:
        File path to write.  Parent directories are created automatically.
    cfg:
        Optional ``BuildConfig`` — used to add kernel/arch metadata to the
        header.  Pass ``None`` when not available.
    results:
        Optional list of ``BuildResult`` — used to add the per-step summary.
        Pass ``None`` to omit.

    Returns
    -------
    tuple[int, int]
        ``(error_count, warning_count)`` — number of issues written.
    """
    issues = log_panel.get_errors_and_warnings()
    error_count   = sum(1 for tag, _ in issues if tag == "ERROR")
    warning_count = sum(1 for tag, _ in issues if tag == "WARNING")

    dest.parent.mkdir(parents=True, exist_ok=True)

    timestamp = datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    with dest.open("w", encoding="utf-8") as fh:
        # ── Header ────────────────────────────────────────────────────────────
        fh.write("=" * 72 + "\n")
        fh.write("  FORGED — Android Kernel Builder\n")
        fh.write("  Build Issues Log  (errors + warnings)\n")
        fh.write("=" * 72 + "\n\n")

        fh.write(f"  Generated : {timestamp}\n")
        if cfg is not None:
            fh.write(f"  Defconfig : {cfg.kernel_defconfig}\n")
            fh.write(f"  Arch      : {cfg.arch}\n")
            fh.write(f"  Source    : {cfg.kernel_source or '(not set)'}\n")
            fh.write(f"  Output    : {cfg.output_dir}\n")
        fh.write(f"  Log lines : {log_panel.line_count()} total captured\n")
        fh.write(f"  Errors    : {error_count}\n")
        fh.write(f"  Warnings  : {warning_count}\n")
        fh.write("\n" + "=" * 72 + "\n\n")

        # ── Issues ────────────────────────────────────────────────────────────
        if not issues:
            fh.write("  No errors or warnings found in build log.\n\n")
        else:
            fh.write(f"  {'#':<6}  {'TAG':<10}  LINE\n")
            fh.write(f"  {'-'*6}  {'-'*10}  {'-'*50}\n\n")
            for seq, (tag, line) in enumerate(issues, start=1):
                # Strip any residual ANSI escape sequences that may have
                # slipped through before writing to a plain-text file.
                clean_line = re.sub(r"\x1b\[[0-9;]*m", "", line)
                fh.write(f"  {seq:<6}  [{tag}]{'':>{10 - len(tag) - 2}}  {clean_line}\n")

        fh.write("\n" + "=" * 72 + "\n\n")

        # ── Per-step summary ──────────────────────────────────────────────────
        if results:
            fh.write("  BUILD STEP SUMMARY\n")
            fh.write(f"  {'STEP':<15}  {'STATUS':<8}  DURATION\n")
            fh.write(f"  {'-'*15}  {'-'*8}  {'-'*10}\n")
            for r in results:
                status = "PASS" if r.success else "FAIL"
                fh.write(f"  {r.step:<15}  {status:<8}  {r.duration:.2f}s\n")
                if not r.success and r.error:
                    fh.write(f"  {'':15}  Error: {r.error}\n")
            total_duration = sum(r.duration for r in results)
            fh.write(f"\n  Total build time: {total_duration:.2f}s\n")

        fh.write("\n" + "=" * 72 + "\n")

    return error_count, warning_count


def render_full_log_panel(log_panel: LiveLogPanel) -> Panel:
    """
    Render a non-scrolling panel containing the *complete* build log.
    Shown after the Live context exits so the user can scroll freely.
    """
    all_lines = log_panel.get_all_lines()
    if all_lines:
        text_body = Text(overflow="fold")
        for raw_line in all_lines:
            text_body.append_text(_colourise_log_line(raw_line))
            text_body.append("\n")
    else:
        text_body = Text(
            "\n  ‹ no output captured ›\n",
            style=PALETTE["dim"],
            justify="center",
        )

    return Panel(
        text_body,
        title=(
            f"[bold white]  ◈  Full Build Log  "
            f"[{PALETTE['secondary']}]({len(all_lines)} lines)"
            f"[/{PALETTE['secondary']}]  [/bold white]"
        ),
        border_style=PALETTE["primary"],
        box=HEAVY_HEAD,
        padding=(0, 1),
    )

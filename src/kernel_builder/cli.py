"""
FORGED — CLI: argument parsing, interactive wizard, command dispatch.
Copyright (c) 2026 vxyzview. Made with love.
"""

from __future__ import annotations

import argparse
import datetime
import getpass
import os
import subprocess
import time
from collections.abc import Callable
from pathlib import Path

from rich.box import HEAVY_HEAD
from rich.console import Console, Group
from rich.live import Live
from rich.panel import Panel
from rich.prompt import Confirm, IntPrompt, Prompt
from rich.rule import Rule
from rich.text import Text

from .banner import print_banner, print_farewell
from .builder import BuildResult, KernelBuilder, find_ccache
from .config import TOOLCHAIN_PRESETS, AnyKernel3Config, BuildConfig, ToolchainConfig
from .display import (
    PALETTE,
    LiveLogPanel,
    make_build_progress,
    render_build_summary,
    render_config_table,
    render_full_log_panel,
    render_phase_header,
    render_pre_build_checklist,
    render_result_panel,
    save_build_log_file,
)
from .packager import AnyKernel3Packager
from .toolchain_manager import (
    DEFAULT_TOOLCHAIN_BASE,
    auto_setup_toolchain,
    check_gnu_cross_compilers,
    install_gnu_cross_compilers,
    toolchain_clang_path,
)

console = Console()

# ── Styled helpers ────────────────────────────────────────────────────────────

_SECTION_ICONS: dict[str, str] = {
    "source":      "↓",
    "toolchain":   "⚙",
    "cross":       "⬡",
    "build":       "◈",
    "ccache":      "⚡",
    "flags":       "◉",
    "anykernel":   "◎",
    "review":      "✦",
    "wizard":      "◆",
    "setup":       "⚙",
    "statistics":  "◉",
}


def _section_icon(title: str) -> str:
    lower = title.lower()
    for key, icon in _SECTION_ICONS.items():
        if key in lower:
            return icon
    return "◆"


def _section(title: str) -> None:
    """Print a forge-styled section divider."""
    icon = _section_icon(title)
    console.print()
    console.print(
        Rule(
            title=(
                f"[bold white on {PALETTE['primary']}]"
                f"  {icon}  {title.upper()}  {icon}  "
                f"[/bold white on {PALETTE['primary']}]"
            ),
            style=PALETTE["primary"],
            characters="━",
            align="center",
        )
    )
    console.print()


def _info(msg: str) -> None:
    console.print(f"  [{PALETTE['steel']}]›[/{PALETTE['steel']}]  {msg}")


def _ok(msg: str) -> None:
    console.print(f"  [{PALETTE['success']}]✦[/{PALETTE['success']}]  {msg}")


def _warn(msg: str) -> None:
    console.print(
        f"  [{PALETTE['warning']}]▲[/{PALETTE['warning']}]  "
        f"[{PALETTE['warning']}]{msg}[/{PALETTE['warning']}]"
    )


def _err(msg: str) -> None:
    console.print(
        f"  [{PALETTE['error']}]✗[/{PALETTE['error']}]  "
        f"[{PALETTE['error']}]{msg}[/{PALETTE['error']}]"
    )


def _prompt(label: str) -> str:
    """Return a styled prompt prefix."""
    return f"[bold {PALETTE['secondary']}]{label}[/bold {PALETTE['secondary']}]"


def _hint(text: str) -> str:
    """Return a dimmed hint string."""
    return f"[{PALETTE['dim']}]{text}[/{PALETTE['dim']}]"


def _kv(key: str, value: str) -> None:
    """Print a single key=value info line."""
    console.print(
        f"  [{PALETTE['steel']}]{key}[/{PALETTE['steel']}]"
        f"  [bold white]{value}[/bold white]"
    )


def _tip_panel(msg: str, title: str = "Tip") -> None:
    """Print a compact tip/hint panel."""
    console.print(
        Panel(
            f"  [{PALETTE['steel']}]{msg}[/{PALETTE['steel']}]",
            title=f"[{PALETTE['dim']}]  ›  {title}  [/{PALETTE['dim']}]",
            border_style=PALETTE["deep"],
            box=HEAVY_HEAD,
            padding=(0, 2),
            expand=True,
        )
    )


def _success_panel(msg: str, title: str = "Done") -> None:
    """Print a success panel."""
    console.print(
        Panel(
            f"  [{PALETTE['success']}]{msg}[/{PALETTE['success']}]",
            title=f"[bold {PALETTE['success']}]  ✦  {title}  [/bold {PALETTE['success']}]",
            border_style=PALETTE["success"],
            box=HEAVY_HEAD,
            padding=(0, 2),
        )
    )


def _warning_panel(msg: str, title: str = "Warning") -> None:
    """Print a warning panel."""
    console.print(
        Panel(
            f"  [{PALETTE['warning']}]{msg}[/{PALETTE['warning']}]",
            title=f"[bold {PALETTE['warning']}]  ▲  {title}  [/bold {PALETTE['warning']}]",
            border_style=PALETTE["warning"],
            box=HEAVY_HEAD,
            padding=(0, 2),
        )
    )


def _error_panel(msg: str, title: str = "Error") -> None:
    """Print an error panel."""
    console.print(
        Panel(
            f"  [{PALETTE['error']}]{msg}[/{PALETTE['error']}]",
            title=f"[bold {PALETTE['error']}]  ✗  {title}  [/bold {PALETTE['error']}]",
            border_style=PALETTE["error"],
            box=HEAVY_HEAD,
            padding=(0, 2),
        )
    )


# ── Interactive wizard ────────────────────────────────────────────────────────


def _wizard() -> BuildConfig:
    """Interactive prompt wizard — build a config from scratch."""
    _section("Interactive Build Wizard")

    _tip_panel(
        "Answer each prompt to configure your build.  "
        "Defaults are shown in brackets — press Enter to accept.",
        title="How this works",
    )
    console.print()

    cfg = BuildConfig()

    # ── Kernel source ─────────────────────────────────────────────────────────
    _section("Kernel Source")

    source_type = Prompt.ask(
        f"{_prompt('Kernel source type')} {_hint('(local / git)')}",
        choices=["local", "git"],
        default="local",
    )

    if source_type == "git":
        cfg.kernel_source_url = Prompt.ask(_prompt("Git URL"))
        cfg.kernel_source_branch = Prompt.ask(
            f"{_prompt('Branch / tag')} {_hint('(blank = remote default)')}",
            default="",
        )
        cfg.kernel_source_depth = IntPrompt.ask(
            f"{_prompt('Clone depth')} {_hint('(1 = shallow  ·  0 = full history)')}",
            default=1,
        )
        default_dest = str(Path.cwd() / "kernel")
        cfg.kernel_source = Prompt.ask(
            f"{_prompt('Clone destination')} {_hint('(local path)')}",
            default=default_dest,
        )
        _ok(
            f"Kernel will be cloned from [bold]{cfg.kernel_source_url}[/bold] "
            f"into [bold]{cfg.kernel_source}[/bold] on first build."
        )
    else:
        cfg.kernel_source = Prompt.ask(
            _prompt("Kernel source directory"),
            default=str(Path.cwd()),
        )

    console.print()
    cfg.kernel_defconfig = Prompt.ask(
        _prompt("Defconfig name"),
        default="defconfig",
    )
    cfg.arch    = Prompt.ask(_prompt("Architecture"), default="arm64")
    cfg.subarch = cfg.arch

    # ── Toolchain ─────────────────────────────────────────────────────────────
    _section("Toolchain")

    preset_choices = " / ".join(TOOLCHAIN_PRESETS.keys())
    preset = Prompt.ask(
        f"{_prompt('Toolchain preset')} {_hint(f'({preset_choices})')}",
        default="aosp-clang",
    )
    cfg.toolchain = ToolchainConfig()
    cfg.toolchain.apply_preset(preset)

    if preset == "aosp-clang":
        clang_version = Prompt.ask(
            f"{_prompt('AOSP Clang version')} {_hint('(e.g. r584948b, r522817)')}",
            default="r584948b",
        )
        cfg.toolchain.aosp_clang_version = clang_version
        _info(
            f"Will download [bold]clang-{clang_version}.tar.gz[/bold] "
            "from AOSP googlesource."
        )

    auto_clone = Confirm.ask(
        _prompt("Auto-download toolchain if not present?"),
        default=cfg.toolchain.auto_clone,
    )
    cfg.toolchain.auto_clone   = auto_clone
    cfg.auto_setup_toolchain   = auto_clone

    if auto_clone:
        tc_base = Prompt.ask(
            _prompt("Toolchain storage directory"),
            default=str(DEFAULT_TOOLCHAIN_BASE),
        )
        cfg.toolchain_dir        = tc_base
        cfg.toolchain.extra_path = []
        _ok(f"Toolchain will be stored in [bold]{tc_base}[/bold] on first build.")
    else:
        toolchain_path = Prompt.ask(
            f"{_prompt('Extra PATH for toolchain binaries')} "
            f"{_hint('(e.g. ~/toolchains/clang/bin — blank to skip)')}",
            default="",
        )
        if toolchain_path:
            cfg.toolchain.extra_path = [toolchain_path]

    # ── Cross-compiler check ──────────────────────────────────────────────────
    _section("Cross-Compiler Availability")

    # Pass a no-op callback so internal log lines are suppressed — the wizard
    # renders its own _ok / _warn output from the returned dict below.
    gcc_map = check_gnu_cross_compilers(progress=lambda _line: None)
    for arch, path in gcc_map.items():
        if path:
            _ok(f"[bold]{arch}[/bold]  [{PALETTE['dim']}]{path}[/{PALETTE['dim']}]")
        else:
            pkg = (
                "gcc-aarch64-linux-gnu" if arch == "aarch64" else "gcc-arm-linux-gnueabihf"
            )
            _warn(
                f"[bold]{arch}[/bold] not found  —  "
                f"[{PALETTE['dim']}]sudo apt install {pkg}[/{PALETTE['dim']}]"
            )

    if not all(gcc_map.values()) and Confirm.ask(
        _prompt("Install missing GNU cross-compiler packages via apt?"),
        default=False,
    ):
        try:
            install_gnu_cross_compilers()
            _ok("Cross-compilers installed.")
        except RuntimeError as exc:
            _warn(str(exc))

    # ── Build settings ────────────────────────────────────────────────────────
    _section("Build Settings")

    cfg.jobs = IntPrompt.ask(
        f"{_prompt('Parallel jobs')} {_hint('(0 = auto)')}",
        default=0,
    )
    cfg.localversion = Prompt.ask(_prompt("LOCALVERSION suffix"), default="")

    try:
        _default_user = getpass.getuser()
    except Exception:
        _default_user = "builder"
    cfg.kbuild_build_user = Prompt.ask(
        f"{_prompt('Build user')} {_hint('(stamped into kernel version string)')}",
        default=_default_user,
    )
    _host_val = cfg.kbuild_build_host
    _info(
        f"Build host  [{PALETTE['secondary']}]{_host_val}[/{PALETTE['secondary']}]  "
        f"{_hint('(fixed — edit kbuild_build_host in the saved config to override)')}"
    )
    lto_raw = Prompt.ask(
        f"{_prompt('LTO mode')} {_hint('(none / thin / full)')}",
        default="none",
        choices=["none", "thin", "full"],
    )
    cfg.lto = None if lto_raw == "none" else lto_raw

    cfg.output_dir     = Prompt.ask(_prompt("Build output directory"), default="out")
    cfg.zip_output_dir = Prompt.ask(_prompt("ZIP output directory"),   default="releases")

    # ── ccache ────────────────────────────────────────────────────────────────
    _section("ccache — Compiler Cache")

    _tip_panel(
        "ccache caches compiled objects and cuts rebuild times by 60–90%.  "
        "Highly recommended for iterative kernel development.",
        title="About ccache",
    )
    console.print()

    _ccache_binary = find_ccache()
    if _ccache_binary:
        _ok(f"ccache found  [{PALETTE['dim']}]{_ccache_binary}[/{PALETTE['dim']}]")
    else:
        _warn("ccache not found in $PATH — install via:  sudo apt install ccache")

    use_ccache = Confirm.ask(
        _prompt("Enable ccache for faster rebuilds?"),
        default=bool(_ccache_binary),
    )
    cfg.ccache.enabled = use_ccache

    if use_ccache:
        ccache_dir_input = Prompt.ask(
            f"{_prompt('ccache directory')} "
            f"{_hint('(blank = ccache default ~/.cache/ccache)')}",
            default="",
        )
        cfg.ccache.dir = ccache_dir_input
        cfg.ccache.max_size = Prompt.ask(
            f"{_prompt('Max cache size')} {_hint('(e.g. 5G, 10G, 500M)')}",
            default="5G",
        )
        compress_cache = Confirm.ask(
            _prompt("Enable ccache compression? (saves disk space)"),
            default=True,
        )
        cfg.ccache.compress = compress_cache
        _ok(
            f"ccache enabled  max={cfg.ccache.max_size}  "
            f"compress={'on' if compress_cache else 'off'}"
        )

    # ── Extra build flags ─────────────────────────────────────────────────────
    _section("Extra Build Flags")

    _tip_panel(
        "Pass arbitrary make variables or env vars to every build invocation.  "
        "Examples:  LLVM=1  LLVM_IAS=1  KCFLAGS=-pipe  CONFIG_DEBUG_INFO=n",
        title="Examples",
    )
    console.print()

    extra_flags_raw = Prompt.ask(
        f"{_prompt('Extra make flags')} "
        f"{_hint('(space-separated VAR=VALUE pairs — blank to skip)')}",
        default="",
    )
    if extra_flags_raw.strip():
        cfg.extra_make_flags = extra_flags_raw.split()

    extra_env_raw = Prompt.ask(
        f"{_prompt('Extra env vars')} "
        f"{_hint('(space-separated KEY=VALUE pairs — blank to skip)')}",
        default="",
    )
    if extra_env_raw.strip():
        for pair in extra_env_raw.split():
            if "=" in pair:
                k, _, v = pair.partition("=")
                cfg.extra_env[k.strip()] = v
            else:
                cfg.extra_env[pair.strip()] = ""

    # ── AnyKernel3 ────────────────────────────────────────────────────────────
    _section("AnyKernel3 Settings")

    ak3             = AnyKernel3Config()
    ak3.kernel_name = Prompt.ask(_prompt("Kernel name"), default="Forged")
    ak3.block       = Prompt.ask(
        _prompt("Flash block"),
        default="/dev/block/by-name/boot",
    )
    ak3.is_slot_device = 1 if Confirm.ask(
        _prompt("Slot device (A/B)?"), default=False
    ) else 0

    device_input = Prompt.ask(
        f"{_prompt('Supported device names')} "
        f"{_hint('(comma-separated — blank = all)')}",
        default="",
    )
    ak3.device_names   = [d.strip() for d in device_input.split(",") if d.strip()]
    ak3.do_devicecheck = 1 if ak3.device_names else 0
    cfg.anykernel3     = ak3

    # ── Review and save ───────────────────────────────────────────────────────
    _section("Configuration Review")
    console.print(render_config_table(cfg))
    console.print()

    if Confirm.ask(_prompt("Save this configuration?"), default=True):
        save_path = Prompt.ask(_prompt("Save path"), default="build_config.json")
        cfg.to_json(Path(save_path))
        _success_panel(
            f"Configuration saved to  [{PALETTE['secondary']}]{save_path}"
            f"[/{PALETTE['secondary']}]\n"
            f"  Run [bold white]forged build -c {save_path}[/bold white] to build.",
            title="Config Saved",
        )

    return cfg


# ── Log-file helper ───────────────────────────────────────────────────────────


def _save_issues_log(
    log_panel: "LiveLogPanel",
    cfg: "BuildConfig",
    results: "list[BuildResult]",
    log_file: Path | None,
) -> None:
    """Save errors + warnings to *log_file*, auto-generating a path if needed.

    A file is **always** written after a build so you always have a record even
    when there are no issues (the file will say "No errors or warnings found").
    If *log_file* is ``None`` the path is auto-generated as::

        <output_dir>/logs/forged_issues_<YYYYMMDD_HHMMSS>.log

    where ``<output_dir>`` is resolved from the build config.
    """
    if log_file is None:
        timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
        source_root = Path(cfg.kernel_source) if cfg.kernel_source else Path(".")
        out_base = source_root / cfg.output_dir
        log_file  = out_base / "logs" / f"forged_issues_{timestamp}.log"

    try:
        error_count, warning_count = save_build_log_file(
            log_panel,
            dest=log_file,
            cfg=cfg,
            results=results,
        )
        total = error_count + warning_count
        if total == 0:
            _ok(
                f"Log saved  [{PALETTE['dim']}]{log_file}[/{PALETTE['dim']}]  "
                f"[{PALETTE['success']}](no issues found)[/{PALETTE['success']}]"
            )
        else:
            _warn(
                f"Log saved  [{PALETTE['dim']}]{log_file}[/{PALETTE['dim']}]  "
                f"[{PALETTE['error']}]{error_count} error(s)[/{PALETTE['error']}]"
                f"  [{PALETTE['warning']}]{warning_count} warning(s)[/{PALETTE['warning']}]"
            )
    except Exception as exc:  # noqa: BLE001
        _warn(f"Could not write issues log: {exc}")


# ── Build runner ──────────────────────────────────────────────────────────────


def _run_build(
    cfg: BuildConfig,
    clean: bool,
    package: bool,
    version_tag: str,
    anykernel_dir: Path | None,
    log_file: Path | None = None,
) -> int:
    build_start = time.monotonic()

    # ── Config table ──────────────────────────────────────────────────────────
    console.print(render_config_table(cfg))
    console.print()

    # ── Pre-build checklist ───────────────────────────────────────────────────
    _ccache_bin = find_ccache()
    source_ok   = bool(
        cfg.kernel_source_url
        or (cfg.kernel_source and Path(cfg.kernel_source).exists())
    )
    console.print(render_pre_build_checklist(
        has_source=source_ok,
        has_toolchain=bool(cfg.toolchain.preset),
        has_ccache=bool(_ccache_bin),
        ccache_enabled=cfg.ccache.enabled,
        clean=clean,
        package=package,
    ))
    console.print()

    builder = KernelBuilder(
        cfg,
        progress_callback=_info,
    )

    # ── Toolchain warnings ────────────────────────────────────────────────────
    toolchain_warnings = builder.validate_toolchain()
    for warning in toolchain_warnings:
        _warning_panel(warning)

    if toolchain_warnings and not Confirm.ask(
        _prompt("Continue anyway?"), default=False
    ):
        return 1

    steps = builder.steps(clean=clean)

    progress  = make_build_progress()
    log_panel = LiveLogPanel("Build Output")
    results: list[BuildResult] = []

    overall_task = progress.add_task(
        f"[bold {PALETTE['secondary']}]◆  Overall Progress",
        total=len(steps),
    )

    def _line_cb(line: str) -> None:
        log_panel.add_line(line)
        live.update(_renderable())

    def _renderable() -> Group:
        return Group(progress, log_panel.render())

    # ── Live build display ────────────────────────────────────────────────────
    with Live(_renderable(), console=console, refresh_per_second=20) as live:
        for idx, step in enumerate(steps, 1):
            live.console.print(render_phase_header(step.name, idx, len(steps)))

            step_task = progress.add_task(
                f"[{PALETTE['steel']}]{step.name}",
                total=1,
            )

            result = builder.run_step(step, line_callback=_line_cb)
            results.append(result)

            progress.update(step_task,    completed=1)
            progress.update(overall_task, advance=1)
            live.update(_renderable())

    # ── Full build log ────────────────────────────────────────────────────────
    console.print()
    console.print(render_full_log_panel(log_panel))

    # ── Save errors + warnings log file ──────────────────────────────────────
    _save_issues_log(log_panel, cfg, results, log_file)

    # ── Per-step result panels ────────────────────────────────────────────────
    console.print()
    for result in results:
        console.print(render_result_panel(result))

    if not all(r.success for r in results):
        console.print()
        console.print(render_build_summary(results))

        failed = [r for r in results if not r.success]
        tip_lines = [
            f"  [{PALETTE['warning']}]▲[/{PALETTE['warning']}]  "
            f"Step [bold white]{r.step}[/bold white] failed — "
            f"[{PALETTE['dim']}]check the full log above for details[/{PALETTE['dim']}]"
            for r in failed
        ]
        tip_lines.append(
            f"\n  [{PALETTE['steel']}]›[/{PALETTE['steel']}]  "
            "Re-run with [bold white]--no-clean[/bold white] to skip mrproper on retry"
        )
        console.print()
        console.print(
            Panel(
                "\n".join(tip_lines),
                title=f"[bold {PALETTE['warning']}]  ▲  Debug Tips  [/bold {PALETTE['warning']}]",
                border_style=PALETTE["warning"],
                box=HEAVY_HEAD,
                padding=(1, 2),
            )
        )
        return 1

    # ── Packaging ─────────────────────────────────────────────────────────────
    zip_path: Path | None = None
    if package:
        ak3_dir  = anykernel_dir or Path(cfg.anykernel3.anykernel_dir)
        packager = AnyKernel3Packager(cfg, ak3_dir)

        kernel_image = builder.find_kernel_image()
        if kernel_image is None:
            _error_panel(
                "Could not locate kernel image — skipping packaging.\n"
                f"  [{PALETTE['dim']}]Searched: Image.gz-dtb, Image-dtb, Image.gz, "
                f"Image, zImage-dtb, zImage[/{PALETTE['dim']}]",
                title="Packaging Skipped",
            )
        else:
            _ok(f"Kernel image  [{PALETTE['secondary']}]{kernel_image}[/{PALETTE['secondary']}]")
            dtb_files    = builder.find_dtb_files()
            module_files = builder.find_modules()
            _info(
                f"DTB files: [bold]{len(dtb_files)}[/bold]  "
                f"·  Modules: [bold]{len(module_files)}[/bold]"
            )
            packager.prepare(kernel_image, dtb_files, module_files)
            zip_path = packager.create_zip(Path(cfg.zip_output_dir), version_tag)
            size_mb  = zip_path.stat().st_size / 1_048_576

            console.print()
            console.print(
                Panel(
                    Text.from_markup(
                        f"  [{PALETTE['success']}]◎  ZIP Created[/{PALETTE['success']}]\n\n"
                        f"  [bold {PALETTE['secondary']}]{zip_path}[/bold {PALETTE['secondary']}]\n"
                        f"  [{PALETTE['dim']}]{size_mb:.2f} MB  ·  "
                        f"Flash with TWRP or ADB sideload[/{PALETTE['dim']}]"
                    ),
                    title=f"[bold {PALETTE['success']}]  ✦  Package Ready  [/bold {PALETTE['success']}]",
                    border_style=PALETTE["success"],
                    box=HEAVY_HEAD,
                    padding=(1, 2),
                )
            )

    # ── Summary + farewell ────────────────────────────────────────────────────
    console.print()
    console.print(render_build_summary(results, zip_path))

    elapsed = time.monotonic() - build_start
    print_farewell(console, success=True, elapsed=elapsed, zip_output_dir=cfg.zip_output_dir)

    return 0


# ── Sub-commands ──────────────────────────────────────────────────────────────


def _cmd_build(args: argparse.Namespace) -> int:
    if args.config:
        cfg = BuildConfig.load(Path(args.config))
    elif args.wizard:
        cfg = _wizard()
    else:
        _warning_panel(
            "No config supplied — launching interactive wizard.",
            title="No Config",
        )
        cfg = _wizard()

    if args.source:
        cfg.kernel_source = args.source
    if args.source_url:
        cfg.kernel_source_url = args.source_url
    if args.source_branch:
        cfg.kernel_source_branch = args.source_branch
    if args.source_depth is not None:
        if args.source_depth < 0:
            _error_panel(
                f"Invalid --source-depth {args.source_depth}: "
                "must be 0 (full history) or a positive integer.",
                title="Invalid Argument",
            )
            return 1
        cfg.kernel_source_depth = args.source_depth
    if args.defconfig:
        cfg.kernel_defconfig = args.defconfig
    if args.jobs is not None:
        if args.jobs < 0:
            _error_panel(
                f"Invalid --jobs {args.jobs}: must be 0 (auto) or a positive integer.",
                title="Invalid Argument",
            )
            return 1
        cfg.jobs = args.jobs
    if args.ccache_enabled is not None:
        cfg.ccache.enabled = args.ccache_enabled

    if args.make_flags:
        seen = set(cfg.extra_make_flags)
        for flag in args.make_flags:
            if flag not in seen:
                cfg.extra_make_flags.append(flag)
                seen.add(flag)

    for pair in args.extra_env:
        if "=" in pair:
            key, _, value = pair.partition("=")
            cfg.extra_env[key.strip()] = value
        else:
            cfg.extra_env[pair.strip()] = ""

    return _run_build(
        cfg=cfg,
        clean=not args.no_clean,
        package=not args.no_package,
        version_tag=args.version_tag or "",
        anykernel_dir=Path(args.anykernel_dir) if args.anykernel_dir else None,
        log_file=Path(args.log_file) if args.log_file else None,
    )


def _cmd_config(_args: argparse.Namespace) -> int:
    _wizard()
    return 0


def _cmd_info(args: argparse.Namespace) -> int:
    cfg = BuildConfig.load(Path(args.config))
    console.print()
    console.print(render_config_table(cfg))
    console.print()
    return 0


def _cmd_setup_toolchain(args: argparse.Namespace) -> int:
    """Clone / verify the toolchain and check arm64 + arm cross-compilers."""
    _section("Toolchain Setup")

    cfg = BuildConfig()
    cfg.toolchain = ToolchainConfig()
    cfg.toolchain.apply_preset(args.preset)
    cfg.toolchain.auto_clone         = True
    cfg.auto_setup_toolchain         = True
    cfg.toolchain.aosp_clang_version = args.version

    toolchain_base = Path(args.toolchain_dir) if args.toolchain_dir else None

    def _cb(line: str) -> None:
        _info(line)

    try:
        auto_setup_toolchain(
            cfg,
            toolchain_base=toolchain_base,
            install_cross_compilers=args.install_cross_compilers,
            progress=_cb,
        )
    except Exception as exc:
        _error_panel(str(exc), title="Setup Failed")
        return 1

    clang = toolchain_clang_path(cfg)
    if clang:
        _ok(f"Clang ready  [{PALETTE['secondary']}]{clang}[/{PALETTE['secondary']}]")
    else:
        _err("Clang binary not found after setup.")
        return 1

    if cfg.toolchain.extra_path:
        ep = cfg.toolchain.extra_path[0]
        _ok(f"extra_path  [{PALETTE['secondary']}]{ep}[/{PALETTE['secondary']}]")
        console.print()
        console.print(
            Panel(
                Text.from_markup(
                    f'  [{PALETTE["steel"]}]Add to your build config:[/{PALETTE["steel"]}]\n'
                    f'  [bold {PALETTE["secondary"]}]toolchain.extra_path[/bold {PALETTE["secondary"]}]'
                    f' = [white]["{ep}"][/white]'
                ),
                title=f"[{PALETTE['dim']}]  ›  Config Snippet  [/{PALETTE['dim']}]",
                border_style=PALETTE["primary"],
                box=HEAVY_HEAD,
                padding=(0, 2),
            )
        )

    _section("Cross-Compiler Status")
    gcc_map = check_gnu_cross_compilers(_cb)
    for arch, path in gcc_map.items():
        if path:
            _ok(f"[bold]{arch}[/bold]  [{PALETTE['dim']}]{path}[/{PALETTE['dim']}]")
        else:
            pkg = "gcc-aarch64-linux-gnu" if arch == "aarch64" else "gcc-arm-linux-gnueabihf"
            _warn(f"[bold]{arch}[/bold] missing  —  sudo apt install {pkg}")

    _success_panel(
        "Toolchain setup complete.\n"
        "  Run [bold white]forged build --wizard[/bold white] to start a build.",
        title="Setup Complete",
    )
    console.print()
    return 0


# ── ccache-stats command ──────────────────────────────────────────────────────


def _cmd_ccache_stats(args: argparse.Namespace) -> int:
    """Display ccache statistics and optionally reset them."""
    _section("ccache Statistics")

    if find_ccache() is None:
        _error_panel(
            "ccache is not installed.\n"
            "  Install via:  [bold white]sudo apt install ccache[/bold white]",
            title="ccache Not Found",
        )
        return 1

    env = os.environ.copy()
    if args.dir:
        env["CCACHE_DIR"] = args.dir
        _info(f"Using CCACHE_DIR: [bold]{args.dir}[/bold]")

    show_cmd = ["ccache", "--show-stats"]
    if args.verbose:
        show_cmd.insert(1, "--verbose")

    try:
        result = subprocess.run(show_cmd, capture_output=True, text=True, env=env)
        stats_text = result.stdout or result.stderr or "(no output)"
        console.print(
            Panel(
                stats_text.rstrip(),
                title=f"[bold {PALETTE['secondary']}]  ◉  ccache Statistics  [/bold {PALETTE['secondary']}]",
                border_style=PALETTE["primary"],
                box=HEAVY_HEAD,
                padding=(1, 2),
            )
        )
        if result.returncode != 0:
            _warn(f"ccache exited with code {result.returncode}")
    except Exception as exc:
        _error_panel(f"Failed to run ccache: {exc}")
        return 1

    if args.zero:
        try:
            subprocess.run(["ccache", "--zero-stats"], check=True, env=env)
            _ok("Statistics cleared (ccache --zero-stats).")
        except Exception as exc:
            _warn(f"Could not reset statistics: {exc}")

    return 0


# ── CLI factory ───────────────────────────────────────────────────────────────


def build_cli() -> Callable[[], int]:
    parser = argparse.ArgumentParser(
        prog="forged",
        description="FORGED — Android Kernel Builder with AnyKernel3 packaging",
    )
    sub = parser.add_subparsers(dest="command")

    # build
    build_p = sub.add_parser("build", help="Build the kernel")
    build_p.add_argument("-c", "--config",  metavar="PATH", help="Build config file (JSON/TOML)")
    build_p.add_argument("-w", "--wizard",  action="store_true", help="Launch interactive wizard")
    build_p.add_argument("-s", "--source",  metavar="DIR",  help="Kernel source directory (local path)")
    build_p.add_argument(
        "--source-url", metavar="URL",
        help="Git URL to clone the kernel source from.",
    )
    build_p.add_argument(
        "--source-branch", metavar="BRANCH", default="",
        help="Branch or tag to checkout when cloning (default: remote HEAD).",
    )
    build_p.add_argument(
        "--source-depth", metavar="N", type=int, default=None,
        help="Clone depth (1=shallow [default], 0=full history).",
    )
    build_p.add_argument("-d", "--defconfig", metavar="NAME", help="Defconfig name")
    build_p.add_argument("-j", "--jobs",      type=int, default=None, help="Parallel jobs (0=auto)")
    build_p.add_argument("--no-clean",   action="store_true", help="Skip mrproper step")
    build_p.add_argument("--no-package", action="store_true", help="Skip AnyKernel3 packaging")
    build_p.add_argument("--version-tag",   metavar="TAG", help="Version tag appended to ZIP name")
    build_p.add_argument("--anykernel-dir", metavar="DIR", help="Path to AnyKernel3 directory")
    build_p.add_argument(
        "--log-file", metavar="PATH",
        default=None,
        help=(
            "Write errors and warnings to this file after the build.  "
            "If omitted, a timestamped file is auto-created inside "
            "<output_dir>/logs/ (e.g. out/logs/forged_issues_20260428_153000.log)."
        ),
    )
    build_p.add_argument(
        "-F", "--make-flag", dest="make_flags", metavar="VAR=VALUE",
        action="append", default=[],
        help="Append an arbitrary make variable (repeatable).  Example: -F LLVM=1",
    )
    build_p.add_argument(
        "-E", "--env", dest="extra_env", metavar="KEY=VALUE",
        action="append", default=[],
        help="Inject an environment variable (repeatable).  Example: -E KBUILD_VERBOSE=1",
    )
    _ccache_grp = build_p.add_mutually_exclusive_group()
    _ccache_grp.add_argument("--ccache",    dest="ccache_enabled", action="store_true",  default=None, help="Enable ccache")
    _ccache_grp.add_argument("--no-ccache", dest="ccache_enabled", action="store_false", help="Disable ccache")
    build_p.set_defaults(ccache_enabled=None)

    # setup-toolchain
    setup_p = sub.add_parser("setup-toolchain", help="Download AOSP Clang and verify cross-compilers")
    setup_p.add_argument("--preset",  metavar="PRESET",  default="aosp-clang", choices=list(TOOLCHAIN_PRESETS.keys()))
    setup_p.add_argument("--version", metavar="VERSION", default="r584948b", help="AOSP Clang revision")
    setup_p.add_argument("--toolchain-dir",          metavar="DIR", help=f"Storage directory (default: {DEFAULT_TOOLCHAIN_BASE})")
    setup_p.add_argument("--install-cross-compilers", action="store_true", help="Auto-install missing cross-compiler packages")

    # config
    sub.add_parser("config", help="Create a build config interactively")

    # info
    info_p = sub.add_parser("info", help="Show details from a build config file")
    info_p.add_argument("config", metavar="CONFIG_FILE")

    # ccache-stats
    ccache_stats_p = sub.add_parser("ccache-stats", help="Display ccache statistics")
    ccache_stats_p.add_argument("--dir",     metavar="DIR", default="", help="ccache storage directory")
    ccache_stats_p.add_argument("--zero",    action="store_true", help="Reset statistics after displaying")
    ccache_stats_p.add_argument("--verbose", action="store_true", help="Show verbose statistics")

    def dispatch() -> int:
        args = parser.parse_args()
        if args.command == "build":
            return _cmd_build(args)
        if args.command == "setup-toolchain":
            return _cmd_setup_toolchain(args)
        if args.command == "config":
            return _cmd_config(args)
        if args.command == "info":
            return _cmd_info(args)
        if args.command == "ccache-stats":
            return _cmd_ccache_stats(args)
        parser.print_help()
        return 0

    return dispatch

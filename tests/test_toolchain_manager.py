"""
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.

Tests for toolchain_manager — AOSP Clang tarball download + arm64/arm path
auto-setup + kernel source cloning.
"""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

import pytest

from kernel_builder.config import BuildConfig
from kernel_builder.toolchain_manager import (
    AOSP_REVISION_DIRS,
    DEFAULT_TOOLCHAIN_BASE,
    _is_valid_clang_dir,
    auto_setup_toolchain,
    check_gnu_cross_compilers,
    download_aosp_clang,
    clone_kernel_source,
    resolved_extra_paths,
    toolchain_clang_path,
)

# ── Helpers ───────────────────────────────────────────────────────────────────

PRESET = "aosp-clang"
VERSION = "r584948b"
REVISION = "clang-r584948b"


def _make_fake_clang_dir(base: Path, revision_dir: str) -> Path:
    """Create a fake clang directory tree under *base*."""
    bin_dir = base / revision_dir / "bin"
    bin_dir.mkdir(parents=True, exist_ok=True)
    (bin_dir / "clang").write_text("#!/bin/sh\necho clang mock\n")
    (bin_dir / "clang").chmod(0o755)
    return bin_dir


# ── _is_valid_clang_dir ───────────────────────────────────────────────────────


class TestIsValidClangDir:
    def test_returns_false_for_missing_dir(self, tmp_path):
        assert _is_valid_clang_dir(tmp_path / "nonexistent") is False

    def test_returns_false_when_clang_binary_absent(self, tmp_path):
        (tmp_path / "bin").mkdir()
        assert _is_valid_clang_dir(tmp_path) is False

    def test_returns_true_when_clang_binary_present(self, tmp_path):
        _make_fake_clang_dir(tmp_path.parent, tmp_path.name)
        assert _is_valid_clang_dir(tmp_path) is True


# ── resolved_extra_paths ──────────────────────────────────────────────────────


class TestResolvedExtraPaths:
    def test_empty_extra_path_returns_empty(self):
        cfg = BuildConfig()
        assert resolved_extra_paths(cfg) == []

    def test_tilde_is_expanded(self):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = ["~/toolchains/clang/bin"]
        result = resolved_extra_paths(cfg)
        assert len(result) == 1
        assert result[0].startswith("/")
        assert "~" not in result[0]

    def test_env_var_is_expanded(self, monkeypatch):
        monkeypatch.setenv("CLANG_DIR", "/opt/clang")
        cfg = BuildConfig()
        cfg.toolchain.extra_path = ["$CLANG_DIR/bin"]
        result = resolved_extra_paths(cfg)
        assert result == ["/opt/clang/bin"]

    def test_empty_entries_are_filtered(self):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = ["", "/real/path"]
        result = resolved_extra_paths(cfg)
        assert "" not in result
        assert "/real/path" in result


# ── toolchain_clang_path ──────────────────────────────────────────────────────


class TestToolchainClangPath:
    def test_returns_none_when_not_found(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = [str(tmp_path)]
        with patch("shutil.which", return_value=None):
            assert toolchain_clang_path(cfg) is None

    def test_finds_clang_in_extra_path(self, tmp_path):
        bin_dir = _make_fake_clang_dir(tmp_path, REVISION)
        cfg = BuildConfig()
        cfg.toolchain.extra_path = [str(bin_dir)]
        result = toolchain_clang_path(cfg)
        assert result is not None
        assert result.name == "clang"

    def test_falls_back_to_which(self):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = []
        with patch("shutil.which", return_value="/usr/bin/clang"):
            result = toolchain_clang_path(cfg)
        assert result == Path("/usr/bin/clang")


# ── download_aosp_clang ───────────────────────────────────────────────────────


class TestDownloadAospClang:
    def test_raises_for_unknown_preset(self, tmp_path):
        with pytest.raises(ValueError, match="No AOSP auto-download handler"):
            download_aosp_clang(tmp_path, preset="unknown-preset")

    def test_skips_download_when_already_present(self, tmp_path):
        """When clang binary already exists, no download should occur."""
        # The actual dest_root inside download_aosp_clang is dest_base / preset,
        # i.e. tmp_path / PRESET — NOT tmp_path / f"aosp-{PRESET}".
        clone_root = tmp_path / PRESET
        _make_fake_clang_dir(clone_root, REVISION)

        logs: list[str] = []
        with patch("urllib.request.urlretrieve") as mock_dl:
            bin_dir = download_aosp_clang(tmp_path, preset=PRESET, progress=logs.append)

        mock_dl.assert_not_called()
        assert "already present" in " ".join(logs)
        assert bin_dir.name == "bin"

    def test_raises_on_download_failure(self, tmp_path):
        """RuntimeError is raised when the HTTP download fails."""
        def _fail_download(url, dest, progress=None):
            raise OSError("Connection refused")

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_fail_download),
            pytest.raises(RuntimeError, match="Failed to download"),
        ):
            download_aosp_clang(tmp_path, preset=PRESET)

    def test_raises_on_extraction_failure(self, tmp_path):
        """RuntimeError is raised when tarball extraction fails."""
        import tarfile as tarfile_mod

        def _noop_download(url, dest, progress=None):
            Path(dest).touch()

        def _fail_extract(tarball, dest, progress=None):
            raise tarfile_mod.TarError("corrupt archive")

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_noop_download),
            patch("kernel_builder.toolchain_manager._extract_tarball", side_effect=_fail_extract),
            pytest.raises(RuntimeError, match="Failed to extract"),
        ):
            download_aosp_clang(tmp_path, preset=PRESET)

    def test_raises_when_bin_missing_after_extraction(self, tmp_path):
        """If extraction completes but bin/clang is absent, raise RuntimeError."""
        def _noop_download(url, dest, progress=None):
            Path(dest).touch()

        def _noop_extract(tarball, dest, progress=None):
            pass  # do not create anything

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_noop_download),
            patch("kernel_builder.toolchain_manager._extract_tarball", side_effect=_noop_extract),
            pytest.raises(RuntimeError, match="bin/clang.*was not found"),
        ):
            download_aosp_clang(tmp_path, preset=PRESET)

    def test_tarball_removed_after_successful_extraction(self, tmp_path):
        """The downloaded tarball must be deleted after extraction succeeds."""
        # dest_root = dest_base / preset → tmp_path / PRESET
        clone_root = tmp_path / PRESET
        tarball_path = clone_root / f"{REVISION}.tar.gz"

        def _fake_download(url, dest, progress=None):
            Path(dest).touch()

        def _fake_extract(tarball, dest, progress=None):
            _make_fake_clang_dir(dest.parent, dest.name)

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_fake_download),
            patch("kernel_builder.toolchain_manager._extract_tarball", side_effect=_fake_extract),
        ):
            download_aosp_clang(tmp_path, preset=PRESET)

        assert not tarball_path.exists(), "Tarball must be removed after extraction"

    def test_tarball_removed_on_download_error(self, tmp_path):
        """Partially-downloaded tarball must be cleaned up on error."""
        # dest_root = dest_base / preset → tmp_path / PRESET
        clone_root = tmp_path / PRESET
        tarball_path = clone_root / f"{REVISION}.tar.gz"

        def _fail_download(url, dest, progress=None):
            Path(dest).touch()          # partial file
            raise OSError("network cut")

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_fail_download),
            pytest.raises(RuntimeError),
        ):
            download_aosp_clang(tmp_path, preset=PRESET)

        assert not tarball_path.exists(), "Partial tarball must be cleaned up on error"

    def test_returned_bin_dir_is_correct(self, tmp_path):
        """The returned path must end with bin/ inside the revision dir."""
        def _fake_download(url, dest, progress=None):
            Path(dest).touch()

        def _fake_extract(tarball, dest, progress=None):
            _make_fake_clang_dir(dest.parent, dest.name)

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_fake_download),
            patch("kernel_builder.toolchain_manager._extract_tarball", side_effect=_fake_extract),
        ):
            bin_dir = download_aosp_clang(tmp_path, preset=PRESET)

        assert bin_dir.name == "bin"
        assert bin_dir.parent.name == REVISION

    def test_url_contains_correct_revision_and_base(self, tmp_path):
        """The constructed URL must reference the correct revision and AOSP base."""
        captured_urls: list[str] = []

        def _capture_download(url, dest, progress=None):
            captured_urls.append(url)
            Path(dest).touch()

        def _fake_extract(tarball, dest, progress=None):
            _make_fake_clang_dir(dest.parent, dest.name)

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_capture_download),
            patch("kernel_builder.toolchain_manager._extract_tarball", side_effect=_fake_extract),
        ):
            download_aosp_clang(tmp_path, preset=PRESET)

        assert len(captured_urls) == 1
        url = captured_urls[0]
        assert "android.googlesource.com" in url
        assert REVISION in url
        assert "main-kernel" in url
        assert url.endswith(".tar.gz")

    def test_destination_dir_created_before_download(self, tmp_path):
        """The clang_dir must exist before the download begins."""
        # dest_root = dest_base / preset → tmp_path / PRESET
        # clang_dir  = dest_root / revision_dir → tmp_path / PRESET / REVISION
        clang_dir = tmp_path / PRESET / REVISION

        def _check_dir_exists(url, dest, progress=None):
            assert clang_dir.exists(), "clang_dir must exist before download starts"
            Path(dest).touch()

        def _fake_extract(tarball, dest, progress=None):
            _make_fake_clang_dir(dest.parent, dest.name)

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_check_dir_exists),
            patch("kernel_builder.toolchain_manager._extract_tarball", side_effect=_fake_extract),
        ):
            download_aosp_clang(tmp_path, preset=PRESET)

    def test_all_presets_have_known_revision_dir(self):
        for preset_name in AOSP_REVISION_DIRS:
            assert AOSP_REVISION_DIRS[preset_name].startswith("clang-r")

    def test_progress_callback_receives_messages(self, tmp_path):
        """progress callback must receive at least one log message."""
        logs: list[str] = []

        def _fake_download(url, dest, progress=None):
            Path(dest).touch()

        def _fake_extract(tarball, dest, progress=None):
            _make_fake_clang_dir(dest.parent, dest.name)

        with (
            patch("kernel_builder.toolchain_manager._download", side_effect=_fake_download),
            patch("kernel_builder.toolchain_manager._extract_tarball", side_effect=_fake_extract),
        ):
            download_aosp_clang(tmp_path, preset=PRESET, progress=logs.append)

        assert logs, "progress callback must receive at least one log message"


# ── clone_kernel_source ───────────────────────────────────────────────────────


class TestCloneKernelSource:
    def test_skips_clone_when_already_present(self, tmp_path):
        """If .git already exists, do not call git clone again."""
        dest = tmp_path / "kernel"
        dest.mkdir()
        (dest / ".git").mkdir()

        logs: list[str] = []
        with patch("subprocess.Popen") as mock_popen:
            result = clone_kernel_source(
                "https://example.com/kernel.git",
                dest,
                progress=logs.append,
            )

        mock_popen.assert_not_called()
        assert "already present" in " ".join(logs)
        assert result == dest

    def test_shallow_clone_uses_depth_1(self, tmp_path):
        dest = tmp_path / "kernel"
        captured: list[list[str]] = []

        def _fake_run(cmd, cwd=None, progress=None):
            captured.append(cmd)

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source("https://example.com/k.git", dest, depth=1)

        assert captured, "No command was run"
        assert "--depth=1" in captured[0]

    def test_full_history_omits_depth_flag(self, tmp_path):
        dest = tmp_path / "kernel"
        captured: list[list[str]] = []

        def _fake_run(cmd, cwd=None, progress=None):
            captured.append(cmd)

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source("https://example.com/k.git", dest, depth=0)

        assert not any(a.startswith("--depth") for a in captured[0]), \
            "--depth flag should be omitted when depth=0"

    def test_branch_is_passed_when_supplied(self, tmp_path):
        dest = tmp_path / "kernel"
        captured: list[list[str]] = []

        def _fake_run(cmd, cwd=None, progress=None):
            captured.append(cmd)

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source(
                "https://example.com/k.git", dest, branch="android-13-release"
            )

        cmd = captured[0]
        assert "--branch" in cmd
        assert "android-13-release" in cmd
        assert "--single-branch" in cmd

    def test_no_branch_flag_when_branch_is_empty(self, tmp_path):
        dest = tmp_path / "kernel"
        captured: list[list[str]] = []

        def _fake_run(cmd, cwd=None, progress=None):
            captured.append(cmd)

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source("https://example.com/k.git", dest, branch="")

        assert "--branch" not in captured[0]

    def test_url_is_present_in_clone_command(self, tmp_path):
        dest = tmp_path / "kernel"
        url = "https://github.com/torvalds/linux.git"
        captured: list[list[str]] = []

        def _fake_run(cmd, cwd=None, progress=None):
            captured.append(cmd)

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source(url, dest)

        assert url in captured[0]

    def test_dest_path_is_present_in_clone_command(self, tmp_path):
        dest = tmp_path / "kernel"
        captured: list[list[str]] = []

        def _fake_run(cmd, cwd=None, progress=None):
            captured.append(cmd)

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source("https://example.com/k.git", dest)

        assert str(dest) in captured[0]

    def test_parent_dirs_created_automatically(self, tmp_path):
        dest = tmp_path / "deep" / "nested" / "kernel"

        def _fake_run(cmd, cwd=None, progress=None):
            pass

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source("https://example.com/k.git", dest)

        assert dest.parent.exists()

    def test_returns_dest_path(self, tmp_path):
        dest = tmp_path / "kernel"

        def _fake_run(cmd, cwd=None, progress=None):
            pass

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            result = clone_kernel_source("https://example.com/k.git", dest)

        assert result == dest

    def test_filter_blob_none_is_set(self, tmp_path):
        """Partial clone filter should always be enabled for efficiency."""
        dest = tmp_path / "kernel"
        captured: list[list[str]] = []

        def _fake_run(cmd, cwd=None, progress=None):
            captured.append(cmd)

        with patch("kernel_builder.toolchain_manager._run", side_effect=_fake_run):
            clone_kernel_source("https://example.com/k.git", dest)

        assert "--filter=blob:none" in captured[0]


# ── check_gnu_cross_compilers ─────────────────────────────────────────────────


class TestCheckGnuCrossCompilers:
    def test_returns_paths_when_found(self):
        with patch("shutil.which", return_value="/usr/bin/aarch64-linux-gnu-gcc"):
            result = check_gnu_cross_compilers()
        assert "aarch64" in result
        assert "arm" in result

    def test_returns_none_when_missing(self):
        with patch("shutil.which", return_value=None):
            result = check_gnu_cross_compilers()
        assert result["aarch64"] is None
        assert result["arm"] is None

    def test_warnings_logged_for_missing(self):
        logs: list[str] = []
        with patch("shutil.which", return_value=None):
            check_gnu_cross_compilers(progress=logs.append)
        combined = " ".join(logs)
        assert "aarch64" in combined
        assert "arm" in combined
        assert "sudo apt install" in combined

    def test_aarch64_key_is_present(self):
        with patch("shutil.which", return_value=None):
            result = check_gnu_cross_compilers()
        assert "aarch64" in result

    def test_arm_key_is_present(self):
        with patch("shutil.which", return_value=None):
            result = check_gnu_cross_compilers()
        assert "arm" in result


# ── auto_setup_toolchain ──────────────────────────────────────────────────────


class TestAutoSetupToolchain:
    def test_skips_when_clang_already_in_extra_path(self, tmp_path):
        """If clang exists in extra_path, do not call download."""
        bin_dir = _make_fake_clang_dir(tmp_path, "clang")
        cfg = BuildConfig()
        cfg.toolchain.extra_path = [str(bin_dir)]

        with patch("kernel_builder.toolchain_manager.download_aosp_clang") as mock_dl, \
             patch("shutil.which", return_value=None):
            auto_setup_toolchain(cfg)

        mock_dl.assert_not_called()

    def test_downloads_aosp_when_extra_path_empty(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.preset = PRESET
        cfg.toolchain.auto_clone = True
        cfg.toolchain.extra_path = []
        fake_bin = tmp_path / "bin"
        fake_bin.mkdir()

        with patch(
            "kernel_builder.toolchain_manager.download_aosp_clang",
            return_value=fake_bin,
        ) as mock_dl, patch("shutil.which", return_value=None):
            auto_setup_toolchain(cfg, toolchain_base=tmp_path)

        mock_dl.assert_called_once_with(
            tmp_path, preset=PRESET, version=VERSION, progress=None
        )
        assert cfg.toolchain.extra_path == [str(fake_bin)]

    def test_extra_path_is_set_after_download(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.preset = PRESET
        cfg.toolchain.auto_clone = True
        cfg.toolchain.extra_path = []
        fake_bin = tmp_path / "bin"
        fake_bin.mkdir()

        with patch(
            "kernel_builder.toolchain_manager.download_aosp_clang",
            return_value=fake_bin,
        ), patch("shutil.which", return_value=None):
            result = auto_setup_toolchain(cfg, toolchain_base=tmp_path)

        assert result.toolchain.extra_path == [str(fake_bin)]

    def test_returns_same_cfg_object(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = []
        cfg.toolchain.preset = PRESET
        fake_bin = tmp_path / "bin"
        fake_bin.mkdir()

        with patch(
            "kernel_builder.toolchain_manager.download_aosp_clang",
            return_value=fake_bin,
        ), patch("shutil.which", return_value=None):
            result = auto_setup_toolchain(cfg, toolchain_base=tmp_path)

        assert result is cfg

    def test_system_clang_skips_download_when_on_path(self):
        cfg = BuildConfig()
        cfg.toolchain.preset = "system-clang"
        cfg.toolchain.auto_clone = False
        cfg.toolchain.extra_path = []

        with patch("shutil.which", return_value="/usr/bin/clang"), \
             patch("kernel_builder.toolchain_manager.download_aosp_clang") as mock_dl:
            auto_setup_toolchain(cfg)

        mock_dl.assert_not_called()

    def test_system_clang_raises_when_not_on_path(self):
        cfg = BuildConfig()
        cfg.toolchain.preset = "system-clang"
        cfg.toolchain.auto_clone = False
        cfg.toolchain.extra_path = []

        with (
            patch("shutil.which", return_value=None),
            pytest.raises(RuntimeError, match="not on PATH"),
        ):
            auto_setup_toolchain(cfg)

    def test_unknown_preset_raises_valueerror(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.preset = "my-custom-clang"
        cfg.toolchain.auto_clone = True
        cfg.toolchain.extra_path = []

        with (
            patch("shutil.which", return_value=None),
            pytest.raises(ValueError, match="No auto-setup handler"),
        ):
            auto_setup_toolchain(cfg, toolchain_base=tmp_path)

    def test_install_cross_compilers_flag_passed(self, tmp_path):
        cfg = BuildConfig()
        cfg.toolchain.extra_path = []
        cfg.toolchain.preset = PRESET
        fake_bin = tmp_path / "bin"
        fake_bin.mkdir()

        with patch(
            "kernel_builder.toolchain_manager.download_aosp_clang",
            return_value=fake_bin,
        ), patch(
            "kernel_builder.toolchain_manager.install_gnu_cross_compilers"
        ) as mock_install:
            auto_setup_toolchain(cfg, toolchain_base=tmp_path, install_cross_compilers=True)

        mock_install.assert_called_once()

    def test_uses_default_toolchain_base_when_none_given(self):
        """When toolchain_base is None, DEFAULT_TOOLCHAIN_BASE is used."""
        cfg = BuildConfig()
        cfg.toolchain.extra_path = []
        cfg.toolchain.preset = PRESET

        with patch(
            "kernel_builder.toolchain_manager.download_aosp_clang",
            return_value=Path("/fake/bin"),
        ) as mock_dl, patch("shutil.which", return_value=None):
            auto_setup_toolchain(cfg, toolchain_base=None)

        mock_dl.assert_called_once_with(
            DEFAULT_TOOLCHAIN_BASE, preset=PRESET, version=VERSION, progress=None
        )

"""
FORGED — Android Kernel Builder
Copyright (c) 2026 vxyzview. Made with love.

Pytest configuration.
"""
import sys
from pathlib import Path
from unittest.mock import patch

import pytest

# Ensure src/ is on the path when running tests directly
sys.path.insert(0, str(Path(__file__).parent.parent / "src"))


@pytest.fixture(autouse=True)
def disable_toolchain_auto_setup(request):
    """Prevent KernelBuilder.__init__ from attempting to clone AOSP Clang or
    the kernel source tree during unit tests.  Tests that exercise
    toolchain_manager directly should mock at a lower level (as they already do).

    Any test that genuinely wants to exercise the full auto-setup path can
    mark itself with ``@pytest.mark.allow_toolchain_setup`` to opt out.
    """
    if request.node.get_closest_marker("allow_toolchain_setup"):
        yield
        return

    with (
        patch(
            "kernel_builder.builder.auto_setup_toolchain",
            side_effect=lambda cfg, **_kw: cfg,
        ),
        patch(
            "kernel_builder.builder.clone_kernel_source",
            side_effect=lambda url, dest, **_kw: dest,
        ),
    ):
        yield

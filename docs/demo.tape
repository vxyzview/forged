# forged demo — rendered to docs/demo.gif by the vhs workflow.
#
# Local render (after `go install github.com/charmbracelet/vhs@latest`):
#   vhs docs/demo.tape
#
# The recording requires a real kernel fixture; the workflow assembles one
# from test/fixtures/kernel-fixture before running this tape.

Output docs/demo.gif
Require vhs
Set FontSize 20
Set Width 1100
Set Height 620
Set Padding 24
Set Theme "GruvboxDark"
Set Shell bash
Set Margin 30
Set MarginFill "#1d2021"
Set BorderRadius 10

# ── 1 · Banner + version ─────────────────────────────────────────────
Type "forged --version" Enter
Sleep 1.2s

# ── 2 · Environment doctor ───────────────────────────────────────────
Type "forged doctor" Enter
Sleep 3s

# ── 3 · Full build of the committed fixture kernel tree ─────────────
Type@250ms "forged build --ci --source test/fixtures/kernel-fixture --defconfig ci_defconfig --toolchain-extra-path /usr/bin --version-tag demo"
Sleep 500ms
Enter
Sleep 7s

# ── 4 · The flashable ZIP ────────────────────────────────────────────
Type "ls -lh releases/" Enter
Sleep 1.2s

# ── 5 · Built-in config explorer ─────────────────────────────────────
Type "forged info config/example_build_config.toml" Enter
Sleep 3s

# ── 6 · Where to go next ─────────────────────────────────────────────
Type@80ms "echo 'next: forged config  ·  forged setup-toolchain  ·  https://github.com/vxyzview/forged'"
Enter
Sleep 1.5s

Sleep 600ms

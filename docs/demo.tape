# forged demo — rendered to docs/demo.gif by the vhs workflow.
#
# Local render (after `go install github.com/charmbracelet/vhs@latest`):
#   vhs docs/demo.tape
#
# The recording is 100% real output: doctor, a full fixture kernel build,
# the flashable ZIP and the config explorer. No staged screens.

Output docs/demo.gif
Require vhs

Set FontSize 21
Set Width 1200
Set Height 660
Set Padding 0
Set Theme { "name": "ForgedDark", "black": "#3b3b3b", "red": "#ff6b6b", "green": "#7ee787", "yellow": "#ffbe00", "blue": "#79c0ff", "magenta": "#d2a8ff", "cyan": "#76e3ea", "white": "#e6e6e6", "brightBlack": "#7c7c7c", "brightGreen": "#39d353", "brightYellow": "#ffd21e", "background": "#121212", "foreground": "#e8e8e8" }
Set Shell bash
Set Margin 42
Set MarginFill "#1a1a1a"
Set BorderRadius 14
Set WindowBar Rings
Set WindowBarSize 44
Set TypingSpeed 80ms
Set CursorBlink false

# ── 1 · First command: version (shows the forged banner) ────────────
Type "forged --version" Enter
Sleep 2s

# ── 2 · Environment doctor ───────────────────────────────────────────
Type "forged doctor" Enter
Sleep 3.2s

# ── 3 · Full build of the committed fixture kernel tree ─────────────
Type "forged build --ci --source test/fixtures/kernel-fixture --defconfig ci_defconfig --toolchain-extra-path /usr/bin --version-tag demo"
Sleep 600ms
Enter
Sleep 7.5s

# ── 4 · The flashable ZIP ────────────────────────────────────────────
Ctrl+L
Sleep 400ms
Type "ls -lh test/fixtures/kernel-fixture/releases/" Enter
Sleep 1.4s

# ── 5 · Config explorer on the example config ────────────────────────
Ctrl+L
Sleep 400ms
Type "forged info config/example_build_config.toml" Enter
Sleep 3.2s

# ── 6 · Where to go next ─────────────────────────────────────────────
Ctrl+L
Sleep 400ms
Type "echo ' forge faster → github.com/vxyzview/forged'" Enter
Sleep 1.6s

Sleep 800ms

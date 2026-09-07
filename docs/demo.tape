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

Type "forged --version" Enter
Sleep 700ms

Type "forged doctor" Enter
Sleep 2.5s

# Build the committed fixture kernel tree end-to-end.
Type@250ms "forged build --ci --source test/fixtures/kernel-fixture --defconfig ci_defconfig --toolchain-extra-path /usr/bin --version-tag demo"
Sleep 500ms
Enter
Sleep 6s

Type "ls releases/" Enter
Sleep 900ms

Sleep 600ms

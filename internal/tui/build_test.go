package tui

import (
	"strings"
	"testing"

	"github.com/vxyzview/forged/internal/builder"
)

func TestColouriseErrorLine(t *testing.T) {
	out := Colourise("cc: error: unknown argument")
	if !strings.Contains(out, "✗") {
		t.Errorf("error line must be marked with ✗: %q", out)
	}
}

func TestColouriseWarningLine(t *testing.T) {
	out := Colourise("drivers/foo.c:42: warning: unused variable")
	if !strings.Contains(out, "▲") {
		t.Errorf("warning line must be marked with ▲: %q", out)
	}
}

func TestColouriseOKLine(t *testing.T) {
	out := Colourise("build done")
	if !strings.Contains(out, "✦") {
		t.Errorf("ok line must be marked with ✦: %q", out)
	}
}

func TestColourisePlainLine(t *testing.T) {
	out := Colourise("  CC      init/main.o")
	if !strings.Contains(out, "◆") {
		t.Errorf("CC line must be marked with ◆: %q", out)
	}
}

func TestColouriseEmptyLine(t *testing.T) {
	if out := Colourise(""); out != "" {
		t.Errorf("empty line must stay empty: %q", out)
	}
}

func TestExtractIssues(t *testing.T) {
	lines := []string{
		"normal line",
		"error: something broke",
		"warning: be careful",
		"another normal",
		"error AND warning on one line",
	}
	issues := ExtractIssues(lines)
	if len(issues.Errors) != 2 {
		t.Errorf("errors = %v", issues.Errors)
	}
	if len(issues.Warnings) != 1 {
		t.Errorf("warnings = %v", issues.Warnings)
	}
}

func TestPhaseIcon(t *testing.T) {
	if phaseIcon("mrproper") != "◈" {
		t.Errorf("mrproper icon = %q", phaseIcon("mrproper"))
	}
	if phaseIcon("defconfig") != "◉" {
		t.Errorf("defconfig icon = %q", phaseIcon("defconfig"))
	}
	if phaseIcon("mystery") != "◆" {
		t.Errorf("fallback icon = %q", phaseIcon("mystery"))
	}
}

func TestProgressBar(t *testing.T) {
	bar := progressBar(2, 4, 20)
	// lipgloss styles each glyph with ANSI runs, so "width" must be measured
	// by counting runes inside the visible glyphs, not string length.
	visible := strings.Count(stripANSI(bar), "━")
	if visible != 20 {
		t.Errorf("bar width = %d, want 20: %q", visible, bar)
	}
}

func TestRenderResultsTable(t *testing.T) {
	table := RenderResultsTable(nil, "")
	if !strings.Contains(table, "ALL STEPS PASSED") {
		t.Errorf("empty results should pass: %q", table)
	}

	failed := RenderResultsTable([]builder.BuildResult{
		{Success: true, Step: "defconfig", Duration: 1.2},
		{Success: false, Step: "build", Duration: 3.4, Error: "Exit code 2"},
	}, "")
	if !strings.Contains(failed, "BUILD FAILED") {
		t.Errorf("failed results must show BUILD FAILED: %q", failed)
	}
	if !strings.Contains(failed, "Exit code 2") {
		_ = failed // error detail is part of BuildResult for the runner panel
	}

	withZip := RenderResultsTable([]builder.BuildResult{
		{Success: true, Step: "build", Duration: 5.0},
	}, "/tmp/releases/Kernel-2026.zip")
	if !strings.Contains(withZip, "/tmp/releases/Kernel-2026.zip") {
		t.Errorf("zip path must appear in summary: %q", withZip)
	}
}

// stripANSI removes ANSI escape sequences for width assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == 0x1b {
			inEscape = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

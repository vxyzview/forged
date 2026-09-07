package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vxyzview/forged/internal/builder"
	"github.com/vxyzview/forged/internal/config"
)

func TestStripANSI(t *testing.T) {
	in := "\x1b[38;2;255;124;0mhello\x1b[0m world"
	if got := stripANSI(in); got != "hello world" {
		t.Errorf("stripANSI = %q", got)
	}
}

func TestSaveIssuesLog(t *testing.T) {
	cfg := config.New()
	cfg.KernelDefconfig = "test_defconfig"
	cfg.Arch = "arm64"

	results := []builder.BuildResult{
		{Success: true, Step: "mrproper", Duration: 0.5},
		{Success: false, Step: "build", Duration: 12.3, Error: "Exit code 2"},
	}
	lines := []string{
		"normal output",
		"error: bad thing",
		"warning: careful",
	}

	dest := filepath.Join(t.TempDir(), "logs", "issues.log")
	errs, warns := saveIssuesLog(dest, cfg, results, lines)

	if errs != 1 || warns != 1 {
		t.Errorf("errors=%d warnings=%d, want 1/1", errs, warns)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"FORGED — Android Kernel Builder",
		"test_defconfig",
		"[ERROR]",
		"[WARNING]",
		"BUILD STEP SUMMARY",
		"Exit code 2",
		"Total build time: 12.80s",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("log missing %q\n%s", want, text)
		}
	}
}

func TestSaveIssuesLogNoIssues(t *testing.T) {
	cfg := config.New()
	dest := filepath.Join(t.TempDir(), "issues.log")
	errs, warns := saveIssuesLog(dest, cfg, nil, []string{"clean line"})
	if errs != 0 || warns != 0 {
		t.Errorf("expected 0/0, got %d/%d", errs, warns)
	}
	data, _ := os.ReadFile(dest)
	if !strings.Contains(string(data), "No errors or warnings found") {
		t.Error("expected 'no issues' message")
	}
}

func TestPrintConfigTable(t *testing.T) {
	cfg := config.New()
	// Must not panic and must produce output without error.
	printConfigTable(cfg)
}

func TestPrintChecklist(t *testing.T) {
	printChecklist(true, true, false, true, true, false)
}

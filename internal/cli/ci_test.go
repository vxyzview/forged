package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vxyzview/forged/internal/builder"
)

// inGitHubActions follows the documented GITHUB_ACTIONS=true convention.
func TestInGitHubActions(t *testing.T) {
	old, had := os.LookupEnv("GITHUB_ACTIONS")
	defer func() {
		if had {
			os.Setenv("GITHUB_ACTIONS", old)
		} else {
			os.Unsetenv("GITHUB_ACTIONS")
		}
	}()

	os.Setenv("GITHUB_ACTIONS", "true")
	if !inGitHubActions() {
		t.Error("expected true when GITHUB_ACTIONS=true")
	}
	os.Unsetenv("GITHUB_ACTIONS")
	if inGitHubActions() {
		t.Error("expected false when GITHUB_ACTIONS is unset")
	}
	os.Setenv("GITHUB_ACTIONS", "false")
	if inGitHubActions() {
		t.Error("expected false when GITHUB_ACTIONS=false")
	}
}

// setOutput appends KEY=value lines to $GITHUB_OUTPUT.
func TestSetOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "github_output")
	os.Setenv("GITHUB_OUTPUT", out)
	defer os.Unsetenv("GITHUB_OUTPUT")
	old, had := os.LookupEnv("GITHUB_STEP_SUMMARY")
	defer func() {
		if had {
			os.Setenv("GITHUB_STEP_SUMMARY", old)
		} else {
			os.Unsetenv("GITHUB_STEP_SUMMARY")
		}
	}()

	c := newCIOutputs()
	c.setOutput("zip_path", "/artifacts/Image-kernel.zip")
	c.setOutput("outcome", "success")

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("expected GITHUB_OUTPUT to be written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "zip_path=/artifacts/Image-kernel.zip\n") {
		t.Errorf("missing zip_path output, got:\n%s", content)
	}
	if !strings.Contains(content, "outcome=success\n") {
		t.Errorf("missing outcome output, got:\n%s", content)
	}
}

// Multi-line values must use the heredoc form so the runner parses them.
func TestSetOutputMultiline(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "github_output")
	os.Setenv("GITHUB_OUTPUT", out)
	defer os.Unsetenv("GITHUB_OUTPUT")

	c := newCIOutputs()
	c.setOutput("digest", "line1\nline2")

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "digest<<"+ciEOF) {
		t.Errorf("expected heredoc form for multi-line value, got:\n%s", content)
	}
}

// Outside Actions the methods degrade to no-ops (never error, never write).
func TestCIOutputsNoopOutsideActions(t *testing.T) {
	os.Unsetenv("GITHUB_OUTPUT")
	os.Unsetenv("GITHUB_STEP_SUMMARY")
	c := newCIOutputs()
	c.setOutput("zip_path", "/x.zip") // must not panic or create files
	c.appendSummary("## forged")
}

// The step summary must name the ZIP and every failed step.
func TestCISummaryMarkdown(t *testing.T) {
	results := []builder.BuildResult{
		{Step: "mrproper", Success: true, Duration: 1.2},
		{Step: "defconfig", Success: false, Duration: 0.4, Error: "no rule"},
	}
	md := ciSummaryMarkdown("build.toml", results, "/artifacts/Image-kernel.zip", 90*time.Second)

	for _, want := range []string{
		"## forged build report",
		"`build.toml`",
		"mrproper",
		"defconfig",
		"✗ failed",
		"✓ passed",
		"Image-kernel.zip",
		"upload-artifact",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("summary missing %q:\n%s", want, md)
		}
	}
}

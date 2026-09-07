// ci.go implements forged's GitHub Actions integration: GITHUB_OUTPUT /
// GITHUB_STEP_SUMMARY plumbing and a machine-friendly --ci flag.
//
// In CI, forged must never block on stdin (toolchain prompts), must keep
// streaming plain logs instead of the TUI (already the case when no TTY is
// present), and should surface the ZIP path + step results as outputs so a
// workflow can attach the ZIP as a build artifact or publish a release.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vxyzview/forged/internal/builder"
)

// inGitHubActions reports whether the process runs inside a GitHub Actions
// runner (GITHUB_ACTIONS=true is documented by the runner spec).
func inGitHubActions() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// ciOutputs wraps GitHub Actions output files. When the env vars are unset
// (not on a runner) every method degrades to a no-op so the flag can be
// safely used anywhere.
type ciOutputs struct {
	outputPath  string
	summaryPath string
}

// newCIOutputs reads GITHUB_OUTPUT / GITHUB_STEP_SUMMARY from the env.
func newCIOutputs() *ciOutputs {
	return &ciOutputs{
		outputPath:  os.Getenv("GITHUB_OUTPUT"),
		summaryPath: os.Getenv("GITHUB_STEP_SUMMARY"),
	}
}

// setOutput writes a single workflow output (key=value), appending to
// GITHUB_OUTPUT with the runner's heredoc-compatible format.
func (c *ciOutputs) setOutput(key, value string) {
	if c.outputPath == "" || key == "" {
		return
	}
	f, err := os.OpenFile(c.outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	// Multi-line values use the documented %EOF% heredoc form.
	if strings.Contains(value, "\n") {
		fmt.Fprintf(f, "%s<<%s\n%s\n%s\n", key, ciEOF, value, ciEOF)
	} else {
		fmt.Fprintf(f, "%s=%s\n", key, value)
	}
}

// appendSummary appends a Markdown section to GITHUB_STEP_SUMMARY.
func (c *ciOutputs) appendSummary(markdown string) {
	if c.summaryPath == "" || markdown == "" {
		return
	}
	f, err := os.OpenFile(c.summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(markdown)
}

// ciEOF is the heredoc delimiter for multi-line outputs.
const ciEOF = "FORGED_EOF"

// runCIAnnotations writes build results to GitHub's annotation stream when
// running inside Actions: each failed step becomes a ::error:: annotation at
// the top of the run log, and each warning digest gets ::warning:: lines.
func runCIAnnotations(results []builder.BuildResult, issuesDigest []string) {
	if !inGitHubActions() {
		return
	}
	for _, r := range results {
		if r.Success {
			continue
		}
		msg := fmt.Sprintf("step %q failed (%.1fs): %s", r.Step, r.Duration, r.Error)
		fmt.Fprintf(os.Stdout, "::error title=forged:%s::%s\n", r.Step, strings.ReplaceAll(msg, "\n", " "))
	}
	for i, line := range issuesDigest {
		if i >= 5 {
			fmt.Fprintln(os.Stdout, "::warning title=forged::more warnings suppressed — see the issues log")
			break
		}
		fmt.Fprintf(os.Stdout, "::warning title=forged::%s\n", strings.ReplaceAll(line, "\n", " "))
	}
}

// ciSummaryMarkdown renders the step-results table + ZIP info as Markdown for
// the GitHub run summary page.
func ciSummaryMarkdown(cfgName string, results []builder.BuildResult, zipPath string, elapsed time.Duration) string {
	var sb strings.Builder
	sb.WriteString("## forged build report\n\n")
	if cfgName != "" {
		sb.WriteString(fmt.Sprintf("**config**: `%s`\n\n", cfgName))
	}
	sb.WriteString("| Step | Result | Duration |\n|---|---|---|\n")
	for _, r := range results {
		status := "✓ passed"
		if !r.Success {
			status = "✗ failed"
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %.1fs |\n", r.Step, status, r.Duration))
	}
	if zipPath != "" {
		size := ""
		if fi, err := os.Stat(zipPath); err == nil {
			size = fmt.Sprintf(" (%.2f MB)", float64(fi.Size())/1048576)
		}
		sb.WriteString(fmt.Sprintf("\n**flashable ZIP**: `%s`%s\n", filepath.Base(zipPath), size))
		sb.WriteString(fmt.Sprintf("\nUpload it with:\n\n```yaml\n- uses: actions/upload-artifact@v4\n  with:\n    name: kernel-zip\n    path: %s\n```\n", zipPath))
	}
	sb.WriteString(fmt.Sprintf("\n**total time**: %s\n", elapsed.Round(time.Second)))
	return sb.String()
}

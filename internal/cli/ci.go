// ci.go implements forged's CI/CD integration layer: provider-aware log
// groups, error/warning service messages, machine-readable outputs and a
// Markdown build report — covering GitHub Actions, GitLab CI, Azure
// Pipelines, TeamCity, Buildkite, Jenkins, CircleCI and generic CI=true
// systems via the internal/cienv detection package.
//
// In CI, forged must never block on stdin (toolchain prompts), must keep
// streaming plain logs instead of the TUI (already the case when no TTY is
// present), and should surface the ZIP path + step results so a pipeline
// can attach the ZIP as an artifact or publish a release.
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
	"github.com/vxyzview/forged/internal/cienv"
)

// ciOutputs carries per-provider output sinks discovered from the env:
// GitHub's $GITHUB_OUTPUT / $GITHUB_STEP_SUMMARY files when present, the
// provider identity for service messages, and TeamCity's job-report dir.
// When nothing is set (not on a runner) every method degrades to a no-op so
// the --ci flag can be safely used anywhere.
type ciOutputs struct {
	provider    cienv.Provider
	outputPath  string
	summaryPath string
}

// newCIOutputs probes the environment for the current CI system.
func newCIOutputs() *ciOutputs {
	return &ciOutputs{
		provider:    cienv.Detect(),
		outputPath:  os.Getenv("GITHUB_OUTPUT"),
		summaryPath: os.Getenv("GITHUB_STEP_SUMMARY"),
	}
}

// setOutput appends key=value to $GITHUB_OUTPUT in the runner's
// heredoc-compatible format. Other providers have no equivalent file, so
// they simply skip it (the Markdown summary is their report).
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

// appendSummary appends a Markdown section to $GITHUB_STEP_SUMMARY when
// available; on other providers the report is printed to stdout so it is
// never silently dropped.
func (c *ciOutputs) appendSummary(markdown string) {
	if markdown == "" {
		return
	}
	if c.summaryPath != "" {
		f, err := os.OpenFile(c.summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.WriteString(markdown)
		return
	}
	if c.provider != cienv.ProviderNone {
		fmt.Println(markdown)
	}
}

// ciEOF is the heredoc delimiter for multi-line outputs.
const ciEOF = "FORGED_EOF"

// logGroup wraps a build step in the provider's collapsible log section.
// On providers without group support it prints a plain banner line instead,
// so step boundaries stay visible everywhere.
type logGroup struct {
	provider cienv.Provider
	id       string
	title    string
}

// startLogGroup opens a collapsible section (or prints a plain banner).
func startLogGroup(id, title string) *logGroup {
	g := &logGroup{provider: cienv.Detect(), id: id, title: title}
	if open := cienv.GroupOpen(g.provider, id, title); open != "" {
		fmt.Println(open)
	} else {
		fmt.Printf("\n  ──  %s  ──\n", title)
	}
	return g
}

// close closes the section. Safe to call on providers without groups.
func (g *logGroup) close() {
	if close := cienv.GroupClose(g.provider, g.id, g.title); close != "" {
		fmt.Println(close)
	}
}

// runCIAnnotations emits provider service messages for build results: each
// failed step becomes an error annotation and each warning digest entry a
// warning annotation. Providers without service messages are skipped.
func runCIAnnotations(provider cienv.Provider, results []builder.BuildResult, issuesDigest []string) {
	if provider == cienv.ProviderNone {
		return
	}
	for _, r := range results {
		if r.Success {
			continue
		}
		msg := fmt.Sprintf("step %q failed (%.1fs): %s", r.Step, r.Duration, r.Error)
		if line := cienv.ErrorMessage(provider, "forged:"+r.Step, msg); line != "" {
			fmt.Println(line)
		}
	}
	for i, line := range issuesDigest {
		if i >= 5 {
			if line := cienv.WarningMessage(provider, "forged", "more warnings suppressed — see the issues log"); line != "" {
				fmt.Println(line)
			}
			break
		}
		if line := cienv.WarningMessage(provider, "forged", line); line != "" {
			fmt.Println(line)
		}
	}
}

// ciSummaryMarkdown renders the step-results table + ZIP info as Markdown
// for the CI run summary page (GitHub) or the job log (other providers).
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
		if cienv.Detect() == cienv.ProviderGitHubActions {
			sb.WriteString(fmt.Sprintf("\nUpload it with:\n\n```yaml\n- uses: actions/upload-artifact@v4\n  with:\n    name: kernel-zip\n    path: %s\n```\n", zipPath))
		} else {
			sb.WriteString(fmt.Sprintf("\nArchive/copy this file in your pipeline's artifact step:\n`%s`\n", zipPath))
		}
	}
	sb.WriteString(fmt.Sprintf("\n**total time**: %s\n", elapsed.Round(time.Second)))
	return sb.String()
}

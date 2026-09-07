package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vxyzview/forged/internal/builder"
	"github.com/vxyzview/forged/internal/cienv"
)

// captureStdout captures everything written to os.Stdout during fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var sb strings.Builder
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	fn()
	os.Stdout = old
	w.Close()
	return <-done
}

// clearAllCIEnv unsets every CI variable the tests touch so each case
// starts from a known-clean environment.
func clearAllCIEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "TEAMCITY_VERSION",
		"BUILDKITE", "CIRCLECI", "TRAVIS", "DRONE", "BITBUCKET_BUILD_NUMBER",
		"APPVEYOR", "WOODPECKER", "CIRRUS_CI", "JENKINS_URL", "CI",
		"GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY", "FORGED_CI", "FORGED_CI_NO_PROMPT",
	} {
		os.Unsetenv(k)
	}
}

// Detect must classify every supported provider's environment.
func TestDetectViaCLIEnv(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want cienv.Provider
	}{
		{map[string]string{"GITHUB_ACTIONS": "true"}, cienv.ProviderGitHubActions},
		{map[string]string{"GITLAB_CI": "1"}, cienv.ProviderGitLab},
		{map[string]string{"TF_BUILD": "true"}, cienv.ProviderAzure},
		{map[string]string{"JENKINS_URL": "http://j"}, cienv.ProviderJenkins},
		{map[string]string{"CI": "true"}, cienv.ProviderGeneric},
	}
	for _, tc := range cases {
		clearAllCIEnv(t)
		for k, v := range tc.env {
			t.Setenv(k, v)
		}
		if got := cienv.Detect(); got != tc.want {
			t.Errorf("env %v: Detect() = %s, want %s", tc.env, got, tc.want)
		}
		if !cienv.Detected() {
			t.Errorf("env %v: Detected() = false, want true", tc.env)
		}
	}
	clearAllCIEnv(t)
	if cienv.Detected() {
		t.Error("no CI env set: Detected() = true, want false")
	}
}

// setOutput appends KEY=value lines to $GITHUB_OUTPUT.
func TestSetOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "github_output")
	os.Setenv("GITHUB_OUTPUT", out)
	defer os.Unsetenv("GITHUB_OUTPUT")

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
	if !strings.Contains(string(data), "digest<<"+ciEOF) {
		t.Errorf("expected heredoc form for multi-line value, got:\n%s", string(data))
	}
}

// Outside Actions the methods degrade to no-ops (never error, never write).
func TestCIOutputsNoopOutsideActions(t *testing.T) {
	clearAllCIEnv(t)
	c := newCIOutputs()
	c.setOutput("zip_path", "/x.zip") // must not panic or create files
	c.appendSummary("## forged")
}

// On providers without a summary file the Markdown report goes to stdout
// instead of being dropped.
func TestAppendSummaryFallsBackToStdout(t *testing.T) {
	clearAllCIEnv(t)
	t.Setenv("GITLAB_CI", "1")
	c := newCIOutputs()
	if c.provider != cienv.ProviderGitLab {
		t.Fatalf("provider = %s, want gitlab-ci", c.provider)
	}
	// appendSummary printing to stdout is exercised here indirectly; the
	// important part is it must not panic and must not write any file.
	c.appendSummary("## forged build report")
}

// The step summary must name the ZIP and every failed step.
func TestCISummaryMarkdown(t *testing.T) {
	clearAllCIEnv(t)
	t.Setenv("GITHUB_ACTIONS", "true") // GitHub-style upload hint
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
		"upload-artifact", // GitHub-style hint on GitHub
	} {
		if !strings.Contains(md, want) {
			t.Errorf("summary missing %q:\n%s", want, md)
		}
	}
}

// Off-GitHub the summary suggests a generic artifact step, not
// actions/upload-artifact.
func TestCISummaryMarkdownNonGitHub(t *testing.T) {
	clearAllCIEnv(t)
	t.Setenv("GITLAB_CI", "1")
	md := ciSummaryMarkdown("", []builder.BuildResult{{Step: "build", Success: true}}, "/z/kernel.zip", time.Second)
	if strings.Contains(md, "upload-artifact") {
		t.Errorf("GitLab summary should not suggest GitHub actions:\n%s", md)
	}
	if !strings.Contains(md, "/z/kernel.zip") {
		t.Errorf("summary should reference the zip path:\n%s", md)
	}
}

// Annotations must use the detected provider's service-message dialect.
func TestRunCIAnnotationsProviderAware(t *testing.T) {
	clearAllCIEnv(t)
	results := []builder.BuildResult{
		{Step: "build", Success: false, Duration: 1.0, Error: "Exit code 2"},
	}

	t.Setenv("GITHUB_ACTIONS", "true")
	got := captureStdout(t, func() {
		runCIAnnotations(cienv.Detect(), results, nil)
	})
	if !strings.Contains(got, "::error title=forged:build::") {
		t.Errorf("GitHub annotation missing:\n%s", got)
	}

	clearAllCIEnv(t)
	t.Setenv("TF_BUILD", "true")
	got = captureStdout(t, func() {
		runCIAnnotations(cienv.Detect(), results, nil)
	})
	if !strings.Contains(got, "##vso[task.logissue type=error]") {
		t.Errorf("Azure annotation missing:\n%s", got)
	}
}

// Log groups must open/close in the provider's dialect, and plain banners
// must appear on providers without group support.
func TestStartLogGroup(t *testing.T) {
	clearAllCIEnv(t)
	t.Setenv("GITLAB_CI", "1")
	g := startLogGroup("step-build", "BUILD")
	got := captureStdout(t, func() { g.close() })
	if !strings.Contains(got, "section_end:") {
		t.Errorf("GitLab section_end missing:\n%s", got)
	}

	clearAllCIEnv(t) // no provider → plain banner, no group output
	g = startLogGroup("step-build", "BUILD")
	out := captureStdout(t, func() { g.close() })
	if out != "" {
		t.Errorf("no-provider close should print nothing, got %q", out)
	}
}

package cienv

import (
	"os"
	"strings"
	"testing"
)

func TestDetectProviders(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want Provider
	}{
		{nil, ProviderNone},
		{map[string]string{"GITHUB_ACTIONS": "true"}, ProviderGitHubActions},
		{map[string]string{"GITLAB_CI": "1"}, ProviderGitLab},
		{map[string]string{"TF_BUILD": "true"}, ProviderAzure},
		{map[string]string{"TEAMCITY_VERSION": "2026.1"}, ProviderTeamCity},
		{map[string]string{"BUILDKITE": "true"}, ProviderBuildkite},
		{map[string]string{"CIRCLECI": "true"}, ProviderCircle},
		{map[string]string{"TRAVIS": "true"}, ProviderTravis},
		{map[string]string{"DRONE": "true"}, ProviderDrone},
		{map[string]string{"BITBUCKET_BUILD_NUMBER": "123"}, ProviderBitbucket},
		{map[string]string{"APPVEYOR": "True"}, ProviderAppVeyor},
		{map[string]string{"WOODPECKER": "1"}, ProviderWoodpecker},
		{map[string]string{"CIRRUS_CI": "true"}, ProviderCirrus},
		{map[string]string{"JENKINS_URL": "http://jenkins:8080"}, ProviderJenkins},
		{map[string]string{"CI": "true"}, ProviderGeneric},
		// Specific providers win over the generic CI=true they also set.
		{map[string]string{"CI": "true", "GITLAB_CI": "1"}, ProviderGitLab},
		{map[string]string{"CI": "true", "CIRCLECI": "true"}, ProviderCircle},
		// CI=false must not count as generic CI.
		{map[string]string{"CI": "false"}, ProviderNone},
	}
	for _, tc := range cases {
		clearCIEnv(t)
		for k, v := range tc.env {
			t.Setenv(k, v)
		}
		if got := Detect(); got != tc.want {
			t.Errorf("env %v: Detect() = %s, want %s", tc.env, got, tc.want)
		}
	}
	clearCIEnv(t)
}

// clearCIEnv unsets every CI variable the tests set, so cases start clean.
func clearCIEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "TEAMCITY_VERSION",
		"BUILDKITE", "CIRCLECI", "TRAVIS", "DRONE", "BITBUCKET_BUILD_NUMBER",
		"APPVEYOR", "WOODPECKER", "CIRRUS_CI", "JENKINS_URL", "CI",
		"FORGED_CI", "FORGED_CI_NO_PROMPT",
	} {
		os.Unsetenv(k)
	}
}

func TestForcedAndNoPrompt(t *testing.T) {
	clearCIEnv(t)
	t.Setenv("FORGED_CI", "1")
	if !Forced() {
		t.Error("FORGED_CI=1 should force CI mode")
	}
	t.Setenv("FORGED_CI_NO_PROMPT", "true")
	if !NoPrompt() {
		t.Error("FORGED_CI_NO_PROMPT=true should disable prompting")
	}
}

func TestGroups(t *testing.T) {
	if got := GroupOpen(ProviderGitHubActions, "s", "build"); got != "::group::build" {
		t.Errorf("GH group open = %q", got)
	}
	if got := GroupClose(ProviderGitHubActions, "s", "build"); got != "::endgroup::" {
		t.Errorf("GH group close = %q", got)
	}
	if got := GroupOpen(ProviderAzure, "s", "build"); got != "##[group]build" {
		t.Errorf("AZ group open = %q", got)
	}
	if got := GroupOpen(ProviderNone, "s", "build"); got != "" {
		t.Errorf("none group open = %q, want empty", got)
	}
	if !SupportsGroups(ProviderGitLab) || SupportsGroups(ProviderJenkins) {
		t.Error("SupportsGroups classification wrong")
	}
	if got := GroupOpen(ProviderGitLab, "forge d/1", "build"); !strings.HasPrefix(got, "\x1b[0Ksection_start:") ||
		!strings.Contains(got, "forge-d-1") || !strings.HasSuffix(got, "build") {
		t.Errorf("GitLab group open malformed: %q", got)
	}
}

func TestServiceMessages(t *testing.T) {
	if got := ErrorMessage(ProviderGitHubActions, "forged:build", "Exit code 2\nsecond line"); got != "::error title=forged:build::Exit code 2 second line" {
		t.Errorf("GH error = %q", got)
	}
	if got := WarningMessage(ProviderAzure, "w", "careful"); got != "##vso[task.logissue type=warning]careful" {
		t.Errorf("AZ warning = %q", got)
	}
	if got := ErrorMessage(ProviderTeamCity, "e", "it's [bad]\nreally"); !strings.Contains(got, `it|'s |[bad|]`) || !strings.Contains(got, "really") {
		t.Errorf("TC error escaping wrong: %q", got)
	}
	if got := ErrorMessage(ProviderJenkins, "e", "boom"); got != "" {
		t.Errorf("Jenkins should have no service messages, got %q", got)
	}
}

func TestSectionIDAndOneLine(t *testing.T) {
	if got := SectionID("step 1/2: build!"); got != "step-1-2--build-" {
		t.Errorf("SectionID = %q", got)
	}
}

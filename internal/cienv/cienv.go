// Package cienv detects CI/CD providers from the environment and provides
// provider-aware log formatting: collapsible step groups and error/warning
// service messages. forged uses it to integrate with every CI system
// (GitHub Actions, GitLab CI, Jenkins, Azure Pipelines, CircleCI, …)
// without per-provider plumbing inside the build code.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package cienv

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Provider identifies a detected CI/CD system.
type Provider int

const (
	ProviderNone Provider = iota
	ProviderGitHubActions
	ProviderGitLab
	ProviderAzure
	ProviderTeamCity
	ProviderBuildkite
	ProviderCircle
	ProviderTravis
	ProviderDrone
	ProviderBitbucket
	ProviderAppVeyor
	ProviderWoodpecker
	ProviderCirrus
	ProviderJenkins
	ProviderGeneric
)

var providerNames = map[Provider]string{
	ProviderNone:          "none",
	ProviderGitHubActions: "github-actions",
	ProviderGitLab:        "gitlab-ci",
	ProviderAzure:         "azure-pipelines",
	ProviderTeamCity:      "teamcity",
	ProviderBuildkite:     "buildkite",
	ProviderCircle:        "circleci",
	ProviderTravis:        "travis-ci",
	ProviderDrone:         "drone",
	ProviderBitbucket:     "bitbucket-pipelines",
	ProviderAppVeyor:      "appveyor",
	ProviderWoodpecker:    "woodpecker",
	ProviderCirrus:        "cirrus-ci",
	ProviderJenkins:       "jenkins",
	ProviderGeneric:       "generic-ci",
}

func (p Provider) String() string {
	if n, ok := providerNames[p]; ok {
		return n
	}
	return "unknown"
}

// Detect inspects well-known CI env vars and returns the provider.
// Specific providers are checked before the generic CI=true convention so
// e.g. GitLab (which also sets CI=true) is never misclassified.
func Detect() Provider {
	switch {
	case envTrue("GITHUB_ACTIONS"):
		return ProviderGitHubActions
	case envSet("GITLAB_CI"):
		return ProviderGitLab
	case envTrue("TF_BUILD"):
		return ProviderAzure
	case envSet("TEAMCITY_VERSION"):
		return ProviderTeamCity
	case envTrue("BUILDKITE"):
		return ProviderBuildkite
	case envTrue("CIRCLECI"):
		return ProviderCircle
	case envTrue("TRAVIS"):
		return ProviderTravis
	case envTrue("DRONE"):
		return ProviderDrone
	case envSet("BITBUCKET_BUILD_NUMBER"):
		return ProviderBitbucket
	case envTrue("APPVEYOR"):
		return ProviderAppVeyor
	case envTrue("WOODPECKER"):
		return ProviderWoodpecker
	case envTrue("CIRRUS_CI"):
		return ProviderCirrus
	case envSet("JENKINS_URL"):
		return ProviderJenkins
	case envTrue("CI"):
		return ProviderGeneric
	default:
		return ProviderNone
	}
}

// Forced reports whether the user opted into CI mode without a provider
// environment (FORGED_CI=1) — self-hosted scripts, cron jobs, systemd.
func Forced() bool { return envTrue("FORGED_CI") }

// NoPrompt reports whether subprocesses must never prompt for credentials
// (FORGED_CI_NO_PROMPT=1) — git clones fail fast instead of hanging.
func NoPrompt() bool { return envTrue("FORGED_CI_NO_PROMPT") }

// Detected reports whether any CI provider environment was found.
func Detected() bool { return Detect() != ProviderNone }

func envTrue(k string) bool {
	v := os.Getenv(k)
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func envSet(k string) bool { return os.Getenv(k) != "" }

// SupportsGroups reports whether collapsible log sections are available.
func SupportsGroups(p Provider) bool {
	switch p {
	case ProviderGitHubActions, ProviderGitLab, ProviderAzure,
		ProviderTeamCity, ProviderBuildkite:
		return true
	default:
		return false
	}
}

// GroupOpen returns the escape sequence / service message that starts a
// collapsible log section, or "" when the provider has none.
func GroupOpen(p Provider, id, title string) string {
	switch p {
	case ProviderGitHubActions:
		return "::group::" + title
	case ProviderGitLab:
		return fmt.Sprintf("\x1b[0Ksection_start:%d:%s[collapsed=true]\r\x1b[0K%s",
			time.Now().Unix(), SectionID(id), title)
	case ProviderAzure:
		return "##[group]" + title
	case ProviderTeamCity:
		return "##teamcity[blockOpened name='" + EscapeTeamCity(title) + "']"
	case ProviderBuildkite:
		return "--- " + title
	default:
		return ""
	}
}

// GroupClose closes a section opened by GroupOpen (same id + title).
func GroupClose(p Provider, id, title string) string {
	switch p {
	case ProviderGitHubActions:
		return "::endgroup::"
	case ProviderGitLab:
		return fmt.Sprintf("\x1b[0Ksection_end:%d:%s\r\x1b[0K",
			time.Now().Unix(), SectionID(id))
	case ProviderAzure:
		return "##[endgroup]"
	case ProviderTeamCity:
		return "##teamcity[blockClosed name='" + EscapeTeamCity(title) + "']"
	case ProviderBuildkite:
		return "^^^ +++"
	default:
		return ""
	}
}

// ErrorMessage returns the provider's error service message (shown as an
// annotation / issue in the CI UI), or "" when unsupported.
func ErrorMessage(p Provider, title, msg string) string {
	msg = oneLine(msg)
	switch p {
	case ProviderGitHubActions:
		return fmt.Sprintf("::error title=%s::%s", title, msg)
	case ProviderAzure:
		return "##vso[task.logissue type=error]" + msg
	case ProviderTeamCity:
		return "##teamcity[message text='" + EscapeTeamCity(msg) + "' status='ERROR']"
	default:
		return ""
	}
}

// WarningMessage returns the provider's warning service message, or "".
func WarningMessage(p Provider, title, msg string) string {
	msg = oneLine(msg)
	switch p {
	case ProviderGitHubActions:
		return fmt.Sprintf("::warning title=%s::%s", title, msg)
	case ProviderAzure:
		return "##vso[task.logissue type=warning]" + msg
	case ProviderTeamCity:
		return "##teamcity[message text='" + EscapeTeamCity(msg) + "' status='WARNING']"
	default:
		return ""
	}
}

// SectionID normalizes a GitLab section identifier ([A-Za-z0-9_-] only).
func SectionID(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, id)
}

// EscapeTeamCity escapes a value for use inside a TeamCity service message.
func EscapeTeamCity(s string) string {
	r := strings.NewReplacer(
		"\\", "|\\",
		"'", "|'",
		"[", "|[",
		"]", "|]",
		"\n", "|n",
		"\r", "|r",
	)
	return r.Replace(s)
}

// oneLine flattens a multi-line message for service-message embedding.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

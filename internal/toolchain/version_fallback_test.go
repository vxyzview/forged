package toolchain

import (
	"context"
	"strings"
	"testing"
)

// An empty version (zero-value aosp_clang_version) must fall back to
// DefaultAOSPClangVersion instead of building a bogus "clang-.tar.gz" URL.
func TestDownloadAOSPClangEmptyVersion(t *testing.T) {
	var capturedURL string
	origDownload := downloadFn
	origSupported := aospClangSupportedFn
	origExtract := extractFn
	defer func() {
		downloadFn = origDownload
		aospClangSupportedFn = origSupported
		extractFn = origExtract
	}()

	aospClangSupportedFn = func() bool { return true }
	downloadFn = func(_ context.Context, url, dest string, _ Progress) error {
		capturedURL = url
		return nil
	}
	extractFn = func(_, _ string, _ Progress) error { return nil }

	_, _ = downloadAOSPClangImpl(t.Context(), t.TempDir(), "aosp-clang", "", nil)

	if !strings.Contains(capturedURL, "clang-"+DefaultAOSPClangVersion) {
		t.Errorf("empty version did not fall back to %q, URL: %q", DefaultAOSPClangVersion, capturedURL)
	}
	if strings.Contains(capturedURL, "clang-.tar.gz") {
		t.Errorf("bogus clang-.tar.gz URL produced: %q", capturedURL)
	}
}

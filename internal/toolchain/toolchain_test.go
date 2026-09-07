package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vxyzview/forged/internal/config"
)

const (
	preset   = "aosp-clang"
	version  = "r584948b"
	revision = "clang-r584948b"
)

func makeFakeClangDir(t *testing.T, base, revDir string) string {
	t.Helper()
	binDir := filepath.Join(base, revDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "clang"), []byte("#!/bin/sh\necho clang mock\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return binDir
}

func TestIsValidClangDir(t *testing.T) {
	if IsValidClangDir(filepath.Join(t.TempDir(), "missing")) {
		t.Error("missing dir must be invalid")
	}
	empty := t.TempDir()
	_ = os.MkdirAll(filepath.Join(empty, "bin"), 0o755)
	if IsValidClangDir(empty) {
		t.Error("dir without clang binary must be invalid")
	}
	base := t.TempDir()
	makeFakeClangDir(t, base, "clang")
	if !IsValidClangDir(filepath.Join(base, "clang")) {
		t.Error("dir with clang binary must be valid")
	}
}

func TestResolvedExtraPaths(t *testing.T) {
	cfg := config.New()
	if got := ResolvedExtraPaths(cfg); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
	cfg.Toolchain.ExtraPath = []string{"~/toolchains/clang/bin"}
	got := ResolvedExtraPaths(cfg)
	if len(got) != 1 || strings.HasPrefix(got[0], "~") {
		t.Errorf("tilde must be expanded: %v", got)
	}
}

func TestToolchainClangPath(t *testing.T) {
	tmp := t.TempDir()
	binDir := makeFakeClangDir(t, tmp, revision)
	cfg := config.New()
	cfg.Toolchain.ExtraPath = []string{binDir}
	got := ToolchainClangPath(cfg)
	if got == "" || filepath.Base(got) != "clang" {
		t.Errorf("ToolchainClangPath = %q", got)
	}
}

func TestDownloadAOSPClangUnknownPreset(t *testing.T) {
	_, err := DownloadAOSPClang(t.Context(), t.TempDir(), "unknown-preset", version, nil)
	if err == nil || !strings.Contains(err.Error(), "handler") {
		t.Errorf("expected handler error, got %v", err)
	}
}

func TestDownloadAOSPClangAlreadyPresent(t *testing.T) {
	base := t.TempDir()
	makeFakeClangDir(t, filepath.Join(base, preset), revision)

	var logs []string
	binDir, err := DownloadAOSPClang(context.Background(), base, preset, version, func(l string) { logs = append(logs, l) })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(binDir) != "bin" {
		t.Errorf("binDir = %q", binDir)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "already present") {
		t.Errorf("expected 'already present' log, got %v", logs)
	}
}

func TestDownloadAOSPClangSuccess(t *testing.T) {
	base := t.TempDir()

	fakeDownload := func(_ context.Context, url, dest string, _ Progress) error {
		if !strings.Contains(url, "android.googlesource.com") ||
			!strings.Contains(url, revision) ||
			!strings.Contains(url, "main-kernel") ||
			!strings.HasSuffix(url, ".tar.gz") {
			t.Errorf("unexpected URL: %s", url)
		}
		if !dirExists(filepath.Dir(dest)) {
			t.Errorf("clang_dir must exist before download starts")
		}
		return os.WriteFile(dest, []byte("tarball"), 0o644)
	}
	// The fake extract must produce a valid clang layout so validation passes.
	fakeExtract := func(tarball, dest string, _ Progress) error {
		bin := filepath.Join(dest, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(bin, "clang"), []byte("#!/bin/sh\n"), 0o755)
	}

	origDownload, origExtract := downloadFn, extractFn
	downloadFn, extractFn = fakeDownload, fakeExtract
	defer func() { downloadFn, extractFn = origDownload, origExtract }()

	var logs []string
	binDir, err := DownloadAOSPClang(context.Background(), base, preset, version, func(l string) { logs = append(logs, l) })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(binDir) != "bin" || filepath.Base(filepath.Dir(binDir)) != revision {
		t.Errorf("binDir = %q", binDir)
	}
	if len(logs) == 0 {
		t.Error("progress callback must receive messages")
	}
	// Tarball must be removed after successful extraction.
	if _, err := os.Stat(filepath.Join(base, preset, revision+".tar.gz")); !os.IsNotExist(err) {
		t.Error("tarball must be removed after extraction")
	}
}

func TestDownloadAOSPClangDownloadFailureCleansUp(t *testing.T) {
	base := t.TempDir()
	origDownload := downloadFn
	downloadFn = func(_ context.Context, _, dest string, _ Progress) error {
		_ = os.WriteFile(dest, []byte("partial"), 0o644)
		return context.DeadlineExceeded
	}
	defer func() { downloadFn = origDownload }()

	if _, err := DownloadAOSPClang(context.Background(), base, preset, version, nil); err == nil {
		t.Fatal("expected download failure error")
	}
	if _, err := os.Stat(filepath.Join(base, preset, revision+".tar.gz")); !os.IsNotExist(err) {
		t.Error("partial tarball must be cleaned up on error")
	}
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func TestCloneKernelSourceSkipsWhenPresent(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "kernel")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	var logs []string
	got, err := CloneKernelSource(context.Background(), "https://example.com/kernel.git", dest, "", 1, func(l string) { logs = append(logs, l) })
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Errorf("got = %q", got)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "already present") {
		t.Errorf("expected 'already present' log: %v", logs)
	}
}

func TestCloneKernelSourceCommandShape(t *testing.T) {
	var captured [][]string
	origRun := runFn
	runFn = func(cmd []string, dir string, progress Progress) error {
		captured = append(captured, cmd)
		return nil
	}
	defer func() { runFn = origRun }()

	dest := filepath.Join(t.TempDir(), "kernel")

	// Shallow clone.
	if _, err := CloneKernelSource(context.Background(), "https://example.com/k.git", dest, "", 1, nil); err != nil {
		t.Fatal(err)
	}
	if !sliceContains(captured[0], "--depth=1") {
		t.Errorf("shallow clone must use --depth=1: %v", captured[0])
	}
	for _, a := range captured[0] {
		if a == "--filter=blob:none" {
			// blob:none only for full history; shallow must not carry it.
			t.Errorf("shallow clone must not use --filter=blob:none: %v", captured[0])
		}
	}

	// Full history.
	captured = nil
	if _, err := CloneKernelSource(context.Background(), "https://example.com/k.git", dest, "", 0, nil); err != nil {
		t.Fatal(err)
	}
	for _, a := range captured[0] {
		if strings.HasPrefix(a, "--depth") {
			t.Errorf("full history must omit --depth: %v", captured[0])
		}
	}
	if !sliceContains(captured[0], "--filter=blob:none") {
		t.Errorf("full history must use --filter=blob:none: %v", captured[0])
	}

	// Branch.
	captured = nil
	if _, err := CloneKernelSource(context.Background(), "https://example.com/k.git", dest, "android-13-release", 1, nil); err != nil {
		t.Fatal(err)
	}
	if !sliceContains(captured[0], "--branch", "android-13-release", "--single-branch") {
		t.Errorf("branch flags missing: %v", captured[0])
	}

	// URL and dest present.
	if !sliceContains(captured[0], "https://example.com/k.git", dest) {
		t.Errorf("url/dest missing: %v", captured[0])
	}

	// Parent dirs created.
	nested := filepath.Join(t.TempDir(), "deep", "nested", "kernel")
	if _, err := CloneKernelSource(context.Background(), "https://example.com/k.git", nested, "", 1, nil); err != nil {
		t.Fatal(err)
	}
	if !dirExists(filepath.Dir(nested)) {
		t.Error("parent dirs must be created")
	}
}

func TestCheckGnuCrossCompilers(t *testing.T) {
	var logs []string
	result := CheckGnuCrossCompilers(func(l string) { logs = append(logs, l) })
	if _, ok := result["aarch64"]; !ok {
		t.Error("aarch64 key missing")
	}
	if _, ok := result["arm"]; !ok {
		t.Error("arm key missing")
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "sudo apt install") {
		t.Error("missing compilers should log install hints")
	}
}

func TestAutoSetupToolchainSkipsWhenClangInExtraPath(t *testing.T) {
	tmp := t.TempDir()
	binDir := makeFakeClangDir(t, tmp, "clang")
	cfg := config.New()
	cfg.Toolchain.ExtraPath = []string{binDir}
	if _, err := AutoSetupToolchain(context.Background(), cfg, "", false, nil); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Toolchain.ExtraPath) != 1 || cfg.Toolchain.ExtraPath[0] != binDir {
		t.Errorf("extra_path must be untouched: %v", cfg.Toolchain.ExtraPath)
	}
}

func TestAutoSetupToolchainDownloadsAOSP(t *testing.T) {
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}

	orig := downloadAOSPClangFn
	downloadAOSPClangFn = func(_ context.Context, base, p, v string, _ Progress) (string, error) {
		if base != tmp || p != preset || v != version {
			t.Errorf("unexpected args: %s %s %s", base, p, v)
		}
		return fakeBin, nil
	}
	defer func() { downloadAOSPClangFn = orig }()

	cfg := config.New()
	cfg.Toolchain.Preset = preset
	cfg.Toolchain.AOSPClangVersion = version
	cfg.Toolchain.ExtraPath = []string{}
	result, err := AutoSetupToolchain(context.Background(), cfg, tmp, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != cfg {
		t.Error("must return the same cfg object")
	}
	if len(cfg.Toolchain.ExtraPath) != 1 || cfg.Toolchain.ExtraPath[0] != fakeBin {
		t.Errorf("extra_path = %v, want [%s]", cfg.Toolchain.ExtraPath, fakeBin)
	}
}

func TestAutoSetupToolchainSystemClang(t *testing.T) {
	cfg := config.New()
	cfg.Toolchain.Preset = "system-clang"
	cfg.Toolchain.ExtraPath = []string{}

	origWhich := whichFn
	whichFn = func(string) string { return "" }
	defer func() { whichFn = origWhich }()

	if _, err := AutoSetupToolchain(context.Background(), cfg, "", false, nil); err == nil {
		t.Error("expected error when system clang is missing")
	}
}

func TestAutoSetupToolchainUnknownPreset(t *testing.T) {
	cfg := config.New()
	cfg.Toolchain.Preset = "my-custom-clang"
	cfg.Toolchain.ExtraPath = []string{}

	origWhich := whichFn
	whichFn = func(string) string { return "" }
	defer func() { whichFn = origWhich }()

	if _, err := AutoSetupToolchain(context.Background(), cfg, t.TempDir(), false, nil); err == nil {
		t.Error("expected error for unknown preset")
	}
}

func sliceContains(list []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, item := range list {
			if item == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

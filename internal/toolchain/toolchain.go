// Package toolchain manages toolchain setup: downloading the AOSP prebuilt
// Clang tarball (aria2c with net/http fallback), cloning kernel sources and
// checking GNU cross-compiler availability.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package toolchain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vxyzview/forged/internal/config"
)

// AOSPClangTarballBase is the direct tarball archive base served by
// android.googlesource.com. Full URL pattern:
// {AOSPClangTarballBase}/{revision}.tar.gz
//
// AOSP publishes kernel-build Clang prebuilts as linux-x86 archives only.
// Auto-download is therefore only offered on linux/amd64 hosts; every other
// platform should use the system-clang preset instead.
const AOSPClangTarballBase = "https://android.googlesource.com/platform/prebuilts/clang/host/linux-x86/+archive/refs/heads/main-kernel"

// aospClangSupportedFn indirection lets tests force AOSP availability
// regardless of the host platform.
var aospClangSupportedFn = AOSPClangSupported

// AOSPClangSupported reports whether the AOSP prebuilt Clang archive can run
// on the current host (linux-x86_64 only).
func AOSPClangSupported() bool {
	return runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
}

// DefaultToolchainBase is the default location for auto-managed toolchains.
func DefaultToolchainBase() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".local/share/forged/toolchains"
	}
	return filepath.Join(home, ".local", "share", "forged", "toolchains")
}

// Progress receives log lines during long operations.
type Progress func(line string)

// nopProgress is used when no callback is supplied.
func nopProgress(string) {}

// Run executes a command, streaming stdout+stderr lines to progress.
// runFn allows tests to intercept subprocess execution.
var runFn = runImpl

// inCIEnv reports whether the process appears to run inside a CI system
// (GitHub Actions sets GITHUB_ACTIONS=true; other CIs set CI=true).
func inCIEnv() bool {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return true
	}
	if v := os.Getenv("CI"); v == "true" || v == "1" {
		return true
	}
	return false
}

// ciNoPrompt reports whether FORGED_CI_NO_PROMPT is set — an escape hatch
// for scripted (non-CI) environments where a git credential prompt would
// block forever. Set it to "0" to re-enable prompts.
func ciNoPrompt() bool {
	return os.Getenv("FORGED_CI_NO_PROMPT") == "1"
}

// Run executes a command, streaming stdout+stderr lines to progress.
func Run(cmd []string, dir string, progress Progress) error {
	return runFn(cmd, dir, progress)
}

func runImpl(cmd []string, dir string, progress Progress) error {
	if progress == nil {
		progress = nopProgress
	}
	progress(fmt.Sprintf("  $ %s", strings.Join(cmd, " ")))
	c := exec.Command(cmd[0], cmd[1:]...)
	if dir != "" {
		c.Dir = dir
	}
	// Never let git block on credential prompts in CI (stdin is /dev/null):
	// fail fast with a visible error instead of hanging a build for hours.
	if cmd[0] == "git" && (inCIEnv() || ciNoPrompt()) {
		c.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_ASKPASS=/bin/echo",
		)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		return err
	}
	reader := io.Reader(stdout)
	scanner := newLineReader(reader)
	for scanner.scan() {
		progress(strings.TrimRight(scanner.line(), "\r\n"))
	}
	return c.Wait()
}

func newLineReader(r io.Reader) lineScanner {
	return lineScanner{r: r, buf: make([]byte, 0, 64*1024)}
}

type lineScanner struct {
	r       io.Reader
	buf     []byte
	lineBuf []byte
}

func (l *lineScanner) scan() bool {
	for {
		if i := indexByte(l.buf, '\n'); i >= 0 {
			l.lineBuf = l.buf[:i]
			l.buf = l.buf[i+1:]
			return true
		}
		tmp := make([]byte, 4096)
		n, err := l.r.Read(tmp)
		if n > 0 {
			l.buf = append(l.buf, tmp[:n]...)
		}
		if err != nil {
			if len(l.buf) > 0 {
				l.lineBuf = l.buf
				l.buf = nil
				return true
			}
			return false
		}
	}
}

func (l *lineScanner) line() string { return string(l.lineBuf) }

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// IsValidClangDir reports whether path contains a usable clang binary.
func IsValidClangDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, "bin", "clang"))
	return err == nil && !info.IsDir()
}

// DownloadWithAria2 downloads url to dest using aria2c with 16 connections.
func DownloadWithAria2(url, dest string, progress Progress) error {
	if progress == nil {
		progress = nopProgress
	}
	progress(fmt.Sprintf("[toolchain] Downloading via aria2c: %s", url))
	cmd := []string{
		"aria2c",
		"--split=16",
		"--max-connection-per-server=16",
		"--min-split-size=1M",
		"--continue=true",
		"--file-allocation=none",
		"--console-log-level=notice",
		"--dir=" + filepath.Dir(dest),
		"--out=" + filepath.Base(dest),
		url,
	}
	return Run(cmd, "", progress)
}

// DownloadWithHTTP streams url to dest using net/http, logging every 64 MiB.
func DownloadWithHTTP(ctx context.Context, url, dest string, progress Progress) error {
	if progress == nil {
		progress = nopProgress
	}
	progress(fmt.Sprintf("[toolchain] → %s", url))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d when downloading %s: %s", resp.StatusCode, url, resp.Status)
	}

	if total := resp.ContentLength; total > 0 {
		progress(fmt.Sprintf("[toolchain]   Download size: %d MiB", total>>20))
	}

	const chunkLog = 64 << 20 // log every 64 MiB
	var downloaded int64
	var lastLogged int64

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 1024*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			downloaded += int64(n)
			if downloaded-lastLogged >= chunkLog {
				if resp.ContentLength > 0 {
					progress(fmt.Sprintf("[toolchain]   %d / %d MiB downloaded …",
						downloaded>>20, resp.ContentLength>>20))
				} else {
					progress(fmt.Sprintf("[toolchain]   %d MiB downloaded …", downloaded>>20))
				}
				lastLogged = downloaded
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}
	progress(fmt.Sprintf("[toolchain]   Download complete (%d MiB).", downloaded>>20))
	return nil
}

// Which reports the absolute path of an executable on PATH, or "".
// whichFn allows tests to stub PATH lookups.
var whichFn = func(binary string) string {
	p, err := exec.LookPath(binary)
	if err != nil {
		return ""
	}
	return p
}

// Which reports the absolute path of an executable on PATH, or "".
func Which(binary string) string {
	return whichFn(binary)
}

// Download fetches url to dest, preferring aria2c when available.
// downloadFn / extractFn indirection allows tests to avoid the network.
var downloadFn = func(ctx context.Context, url, dest string, progress Progress) error {
	return Download(ctx, url, dest, progress)
}

var extractFn = func(tarball, dest string, progress Progress) error {
	return ExtractTarball(tarball, dest, progress)
}

var downloadAOSPClangFn = func(ctx context.Context, destBase, preset, version string, progress Progress) (string, error) {
	return downloadAOSPClangImpl(ctx, destBase, preset, version, progress)
}

// Download fetches url to dest, preferring aria2c when available.
func Download(ctx context.Context, url, dest string, progress Progress) error {
	if Which("aria2c") != "" {
		return DownloadWithAria2(url, dest, progress)
	}
	if progress != nil {
		progress("[toolchain] aria2c not found — falling back to net/http " +
			"(install aria2 for faster downloads: sudo apt install aria2)")
	}
	return DownloadWithHTTP(ctx, url, dest, progress)
}

// ExtractTarball extracts a .tar.gz into dest. AOSP clang tarballs are flat
// archives (no wrapping top-level directory), so files land directly in dest.
func ExtractTarball(tarball, dest string, progress Progress) error {
	if progress == nil {
		progress = nopProgress
	}
	progress(fmt.Sprintf("[toolchain] Extracting %s into %s …", filepath.Base(tarball), dest))

	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("tar entry escapes destination: %s", hdr.Name)
		}
		target := filepath.Join(dest, clean)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	progress("[toolchain] Extraction complete.")
	return nil
}

// DownloadAOSPClang downloads and extracts the AOSP prebuilt Clang tarball.
//
// The tarball is fetched from the AOSP googlesource archive:
//
//	https://android.googlesource.com/platform/prebuilts/clang/host/linux-x86
//	/+archive/refs/heads/main-kernel/clang-<version>.tar.gz
//
// and extracted into destBase/<preset>/clang-<version>/. The .tar.gz file is
// deleted once extraction succeeds. Returns the bin/ directory that should be
// prepended to PATH.
func DownloadAOSPClang(ctx context.Context, destBase, preset, version string, progress Progress) (string, error) {
	if preset != "aosp-clang" {
		return "", fmt.Errorf("no AOSP auto-download handler for preset '%s'", preset)
	}
	if !aospClangSupportedFn() {
		return "", fmt.Errorf("aosp-clang is only distributed for linux/amd64 hosts (this host: %s/%s).\nUse the system-clang preset instead — install clang with:  %s",
			runtime.GOOS, runtime.GOARCH, InstallClangHint())
	}
	return downloadAOSPClangFn(ctx, destBase, preset, version, progress)
}

func downloadAOSPClangImpl(ctx context.Context, destBase, preset, version string, progress Progress) (string, error) {
	if progress == nil {
		progress = nopProgress
	}
	revisionDir := "clang-" + version
	url := fmt.Sprintf("%s/%s.tar.gz", AOSPClangTarballBase, revisionDir)
	destRoot := filepath.Join(destBase, preset)
	clangDir := filepath.Join(destRoot, revisionDir)
	binDir := filepath.Join(clangDir, "bin")

	// Fast path: already downloaded and extracted.
	if IsValidClangDir(clangDir) {
		progress(fmt.Sprintf("[toolchain] AOSP Clang already present at %s", clangDir))
		return binDir, nil
	}

	if err := os.MkdirAll(clangDir, 0o755); err != nil {
		return "", err
	}

	tarball := filepath.Join(destRoot, revisionDir+".tar.gz")

	progress(fmt.Sprintf("[toolchain] Downloading AOSP Clang '%s' …", revisionDir))
	progress(fmt.Sprintf("[toolchain] Source URL: %s", url))

	if err := downloadFn(ctx, url, tarball, progress); err != nil {
		_ = os.Remove(tarball)
		return "", fmt.Errorf("failed to download AOSP Clang tarball from:\n  %s\nCheck your network connection and try again.\nError: %w", url, err)
	}

	if err := extractFn(tarball, clangDir, progress); err != nil {
		_ = os.Remove(tarball)
		return "", fmt.Errorf("failed to extract AOSP Clang tarball '%s'.\nError: %w", filepath.Base(tarball), err)
	}

	_ = os.Remove(tarball)

	if !IsValidClangDir(clangDir) {
		return "", fmt.Errorf("download finished but '%s' was not found.\nThe tarball contents may differ from the expected structure.\nSource URL: %s", filepath.Join(binDir, "clang"), url)
	}

	progress(fmt.Sprintf("[toolchain] AOSP Clang ready at %s", binDir))
	return binDir, nil
}

// CloneKernelSource clones a kernel source tree from url into dest.
//
// depth of 1 = shallow clone; 0 = full history (blobless partial clone).
// Returns dest.
func CloneKernelSource(ctx context.Context, url, dest, branch string, depth int, progress Progress) (string, error) {
	if progress == nil {
		progress = nopProgress
	}
	if fi, err := os.Stat(filepath.Join(dest, ".git")); err == nil && fi.IsDir() {
		progress(fmt.Sprintf("[kernel] Kernel source already present at %s", dest))
		return dest, nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}

	progress(fmt.Sprintf("[kernel] Cloning kernel source from %s …", url))
	if branch != "" {
		progress(fmt.Sprintf("[kernel]   Branch/tag : %s", branch))
	}
	if depth > 0 {
		progress(fmt.Sprintf("[kernel]   Depth      : %d", depth))
	} else {
		progress("[kernel]   Depth      : full history")
	}

	cmd := []string{"git", "clone"}
	if depth > 0 {
		// Shallow clone: fast and small.
		cmd = append(cmd, fmt.Sprintf("--depth=%d", depth))
	} else {
		// Full-history blobless clone: commits + trees fetched up-front,
		// large blobs fetched on-demand only.
		cmd = append(cmd, "--filter=blob:none")
	}
	if branch != "" {
		cmd = append(cmd, "--branch", branch, "--single-branch")
	}
	cmd = append(cmd, url, dest)

	if err := Run(cmd, "", progress); err != nil {
		return "", err
	}
	progress(fmt.Sprintf("[kernel] Kernel source ready at %s", dest))
	return dest, nil
}

// gnuPackages maps arch → (binary, apt package).
var gnuPackages = map[string][2]string{
	"aarch64": {"aarch64-linux-gnu-gcc", "gcc-aarch64-linux-gnu"},
	"arm":     {"arm-linux-gnueabihf-gcc", "gcc-arm-linux-gnueabihf"},
}

// GnuPackageOrder is the iteration order for display.
var GnuPackageOrder = []string{"aarch64", "arm"}

// CheckGnuCrossCompilers checks whether arm64/arm cross-compiler prefixes are
// on PATH. Returns a map arch → directory (or "" when absent).
func CheckGnuCrossCompilers(progress Progress) map[string]string {
	if progress == nil {
		progress = nopProgress
	}
	result := make(map[string]string, len(GnuPackageOrder))
	for _, arch := range GnuPackageOrder {
		pkg := gnuPackages[arch]
		found := Which(pkg[0])
		result[arch] = ""
		if found != "" {
			result[arch] = filepath.Dir(found)
			progress(fmt.Sprintf("[toolchain] %s cross-compiler OK → %s", arch, found))
		} else {
			progress(fmt.Sprintf(
				"[toolchain] WARNING: %s cross-compiler not found (binary: %s).\n"+
					"           Install with:  sudo apt install %s\n"+
					"           Without it, some kernel Makefile checks may fail.",
				arch, pkg[0], pkg[1]))
		}
	}
	return result
}

// InstallGnuCrossCompilers attempts to install missing cross-compiler packages
// via apt. Returns an error when apt is unavailable (non-Debian host).
func InstallGnuCrossCompilers(progress Progress) error {
	if progress == nil {
		progress = nopProgress
	}
	if Which("apt-get") == "" {
		return fmt.Errorf(
			"apt-get not found — cannot auto-install cross-compilers.\n" +
				"Please install the following packages manually:\n" +
				"  gcc-aarch64-linux-gnu   (arm64 cross-compiler)\n" +
				"  gcc-arm-linux-gnueabihf (arm 32-bit cross-compiler)")
	}

	var toInstall []string
	for _, arch := range GnuPackageOrder {
		binary, pkg := gnuPackages[arch][0], gnuPackages[arch][1]
		if Which(binary) == "" {
			toInstall = append(toInstall, pkg)
		}
	}
	if len(toInstall) == 0 {
		progress("[toolchain] All GNU cross-compiler packages already installed.")
		return nil
	}

	progress(fmt.Sprintf("[toolchain] Installing %v via apt-get …", toInstall))
	return Run(append([]string{"sudo", "apt-get", "install", "-y", "--no-install-recommends"}, toInstall...), "", progress)
}

// checkCrossCompilers either installs or just checks the cross-compilers.
func checkCrossCompilers(install bool, progress Progress) {
	if install {
		_ = InstallGnuCrossCompilers(progress)
	} else {
		CheckGnuCrossCompilers(progress)
	}
}

// AutoSetupToolchain detects or downloads the toolchain and patches cfg.
//
//  1. If cfg.Toolchain.ExtraPath already points at a valid clang, do nothing.
//  2. Otherwise download the toolchain for the configured preset and set
//     cfg.Toolchain.ExtraPath to the resulting bin/ directory.
//  3. Check (or install) GNU cross-compilers.
func AutoSetupToolchain(ctx context.Context, cfg *config.BuildConfig, toolchainBase string, installCrossCompilers bool, progress Progress) (*config.BuildConfig, error) {
	if progress == nil {
		progress = nopProgress
	}
	tc := &cfg.Toolchain
	base := toolchainBase
	if base == "" {
		base = DefaultToolchainBase()
	}

	// Step 1: already usable?
	for _, p := range tc.ExtraPath {
		expanded := config.ExpandPath(p)
		if expanded != "" && fileExists(filepath.Join(expanded, tc.CC)) {
			progress(fmt.Sprintf("[toolchain] Clang found at %s — skipping setup.", filepath.Join(expanded, tc.CC)))
			checkCrossCompilers(installCrossCompilers, progress)
			return cfg, nil
		}
	}

	// system-clang preset uses the PATH clang.
	if tc.Preset == "system-clang" {
		found := Which(tc.CC)
		if found != "" {
			progress(fmt.Sprintf("[toolchain] System %s found at %s", tc.CC, found))
			checkCrossCompilers(installCrossCompilers, progress)
			return cfg, nil
		}
		return nil, fmt.Errorf("system-clang preset selected but '%s' is not on PATH.\nInstall with:  %s", tc.CC, InstallClangHint())
	}

	// Step 2: download (aosp-clang). The prebuilt archive is linux-x86_64
	// only — fail early with a useful hint instead of downloading an
	// unrunnable toolchain.
	if tc.Preset != "aosp-clang" {
		return nil, fmt.Errorf("no auto-setup handler for toolchain preset '%s'.\nPlease set 'toolchain.extra_path' manually in your build config.", tc.Preset)
	}
	if !aospClangSupportedFn() {
		return nil, fmt.Errorf("aosp-clang auto-download is only supported on linux/amd64 (this host: %s/%s).\nAOSP publishes no Clang prebuilts for other platforms.\nSwitch to the system-clang preset and install clang with:  %s",
			runtime.GOOS, runtime.GOARCH, InstallClangHint())
	}
	binDir, err := downloadAOSPClangFn(ctx, base, tc.Preset, tc.AOSPClangVersion, progress)
	if err != nil {
		return nil, err
	}

	// Step 3: write bin/ back into the config.
	tc.ExtraPath = []string{binDir}
	progress(fmt.Sprintf("[toolchain] extra_path automatically set to: %s", binDir))

	// Step 4: verify cross-compilers.
	checkCrossCompilers(installCrossCompilers, progress)
	return cfg, nil
}

// ResolvedExtraPaths returns cfg.Toolchain.ExtraPath with ~ and $VAR expanded.
func ResolvedExtraPaths(cfg *config.BuildConfig) []string {
	out := make([]string, 0, len(cfg.Toolchain.ExtraPath))
	for _, p := range cfg.Toolchain.ExtraPath {
		if p == "" {
			continue
		}
		out = append(out, config.ExpandPath(p))
	}
	return out
}

// ToolchainClangPath returns the full path to the clang binary, or "".
func ToolchainClangPath(cfg *config.BuildConfig) string {
	for _, p := range ResolvedExtraPaths(cfg) {
		candidate := filepath.Join(p, cfg.Toolchain.CC)
		if fileExists(candidate) {
			return candidate
		}
	}
	return Which(cfg.Toolchain.CC)
}

// fileExists reports whether the path exists and is a regular file.
func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// HostArch returns the runtime host architecture (e.g. "arm64").
func HostArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "amd64":
		return "x86_64"
	default:
		return runtime.GOARCH
	}
}

// InstallClangHint returns a platform-appropriate hint for installing Clang.
//
// Exported so the wizard can print the same guidance as toolchain errors.
func InstallClangHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install llvm"
	case "windows":
		return "winget install LLVM.LLVM (or download from https://releases.llvm.org)"
	default:
		return "sudo apt install clang lld llvm"
	}
}

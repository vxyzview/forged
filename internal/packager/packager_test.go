package packager

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vxyzview/forged/internal/config"
)

func makePackager(t *testing.T) (*Packager, string) {
	t.Helper()
	cfg := config.New()
	cfg.Anykernel3.KernelName = "TestKernel"
	cfg.Anykernel3.DeviceNames = []string{"testdevice"}
	ak3Dir := filepath.Join(t.TempDir(), "AnyKernel3")
	return New(cfg, ak3Dir), ak3Dir
}

func TestPrepareCopiesImage(t *testing.T) {
	p, ak3Dir := makePackager(t)
	tmp := t.TempDir()
	image := filepath.Join(tmp, "Image.gz-dtb")
	if err := os.WriteFile(image, append([]byte{0x1f, 0x8b}, make([]byte, 100)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(image, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(ak3Dir, "TestKernel")) {
		t.Error("kernel image must be staged under its configured name")
	}
}

func TestPrepareCreatesAnykernelSh(t *testing.T) {
	p, ak3Dir := makePackager(t)
	image := filepath.Join(t.TempDir(), "Image.gz")
	if err := os.WriteFile(image, make([]byte, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(image, nil, nil); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(ak3Dir, "anykernel.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "TestKernel") {
		t.Error("anykernel.sh must contain the kernel name")
	}
	if !strings.Contains(string(content), "testdevice") {
		t.Error("anykernel.sh must contain the device name")
	}
}

func TestPrepareExtraCmdsWithBraces(t *testing.T) {
	cfg := config.New()
	cfg.Anykernel3.KernelName = "BraceKernel"
	cfg.Anykernel3.ExtraCmds = `echo "{hello}" && echo "{world}"`
	ak3Dir := filepath.Join(t.TempDir(), "AnyKernel3")
	p := New(cfg, ak3Dir)
	image := filepath.Join(t.TempDir(), "Image")
	if err := os.WriteFile(image, make([]byte, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(image, nil, nil); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(ak3Dir, "anykernel.sh"))
	if !strings.Contains(string(content), "{hello}") {
		t.Error("braces in extra_cmds must survive templating")
	}
}

func TestCreateZip(t *testing.T) {
	p, _ := makePackager(t)
	image := filepath.Join(t.TempDir(), "Image")
	if err := os.WriteFile(image, make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(image, nil, nil); err != nil {
		t.Fatal(err)
	}
	zipPath, err := p.CreateZip(filepath.Join(t.TempDir(), "releases"), "v1.0")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(zipPath) != ".zip" {
		t.Errorf("zip name = %q", zipPath)
	}
	if !strings.Contains(filepath.Base(zipPath), "v1.0") {
		t.Errorf("version tag missing from %q", zipPath)
	}
	info, err := os.Stat(zipPath)
	if err != nil || info.Size() == 0 {
		t.Error("zip must exist and be non-empty")
	}
}

func TestCreateZipContainsAnykernelSh(t *testing.T) {
	p, _ := makePackager(t)
	image := filepath.Join(t.TempDir(), "Image")
	if err := os.WriteFile(image, make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(image, nil, nil); err != nil {
		t.Fatal(err)
	}
	zipPath, err := p.CreateZip(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	found := false
	for _, f := range zr.File {
		if f.Name == "anykernel.sh" {
			found = true
		}
	}
	if !found {
		t.Error("zip must contain anykernel.sh at its root")
	}
}

func TestPrepareModulesNoNameCollision(t *testing.T) {
	cfg := config.New()
	cfg.Anykernel3.KernelName = "ModKernel"
	ak3Dir := filepath.Join(t.TempDir(), "AnyKernel3")
	p := New(cfg, ak3Dir)

	tmp := t.TempDir()
	image := filepath.Join(tmp, "Image")
	if err := os.WriteFile(image, make([]byte, 32), 0o644); err != nil {
		t.Fatal(err)
	}

	dirA := filepath.Join(tmp, "drivers", "net")
	dirB := filepath.Join(tmp, "fs", "ext4")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatal(err)
	}
	modA := filepath.Join(dirA, "foo.ko")
	modB := filepath.Join(dirB, "foo.ko")
	if err := os.WriteFile(modA, []byte{0xaa}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modB, []byte{0xbb}, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := p.Prepare(image, nil, []string{modA, modB}); err != nil {
		t.Fatal(err)
	}

	modRoot := filepath.Join(ak3Dir, "modules", "system", "lib", "modules")
	var installed []string
	_ = filepath.Walk(modRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "foo.ko" {
			installed = append(installed, path)
		}
		return nil
	})
	if len(installed) != 2 {
		t.Errorf("both foo.ko files must be staged separately, got %v", installed)
	}
}

func TestZipIsStableOnFailure(t *testing.T) {
	// Creating a zip from a missing base dir must not leave a partial zip.
	p, _ := makePackager(t)
	p.AnykernelBase = filepath.Join(t.TempDir(), "does-not-exist")
	out := t.TempDir()
	_, err := p.CreateZip(out, "")
	if err == nil {
		t.Fatal("expected error for missing staging dir")
	}
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zip") {
			t.Errorf("no partial zip should remain, found %s", e.Name())
		}
	}
}

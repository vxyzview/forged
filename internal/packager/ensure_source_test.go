package packager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vxyzview/forged/internal/config"
	"github.com/vxyzview/forged/internal/toolchain"
)

// An empty (or junk) staging directory must be cleared and re-cloned in
// osm0sis/git mode instead of failing with "destination path already exists".
func TestEnsureSourceClearsUnpopulatedDir(t *testing.T) {
	cloneCalled := false
	origClone := cloneKernelSourceFn
	defer func() { cloneKernelSourceFn = origClone }()
	cloneKernelSourceFn = func(_ context.Context, _ string, dest string, _ string, _ int, _ toolchain.Progress) (string, error) {
		cloneCalled = true
		if err := os.MkdirAll(filepath.Join(dest, "tools"), 0o755); err != nil {
			return "", err
		}
		return dest, nil
	}

	cfg := config.New()
	cfg.Anykernel3.Source = config.AK3SourceOsm0sis
	base := filepath.Join(t.TempDir(), "AnyKernel3")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(base, "keep")
	if err := os.WriteFile(junk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(cfg, base)
	if err := p.EnsureSource(); err != nil {
		t.Fatalf("EnsureSource failed: %v", err)
	}
	if !cloneCalled {
		t.Error("expected a fresh clone to be attempted")
	}
	if _, err := os.Stat(junk); !os.IsNotExist(err) {
		t.Error("expected junk staging dir to be removed before cloning")
	}
}

// A populated staging tree (real ak3-core.sh) must be reused, never removed.
func TestEnsureSourceReusesPopulatedDir(t *testing.T) {
	cloneCalled := false
	origClone := cloneKernelSourceFn
	defer func() { cloneKernelSourceFn = origClone }()
	cloneKernelSourceFn = func(_ context.Context, _ string, dest string, _ string, _ int, _ toolchain.Progress) (string, error) {
		cloneCalled = true
		return dest, nil
	}

	cfg := config.New()
	cfg.Anykernel3.Source = config.AK3SourceOsm0sis
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "tools", "ak3-core.sh"), []byte("#!/bin/sh\n# real ak3 core"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := New(cfg, base)
	if err := p.EnsureSource(); err != nil {
		t.Fatalf("EnsureSource failed: %v", err)
	}
	if cloneCalled {
		t.Error("populated tree must be reused, not re-cloned")
	}
}

package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsOurTscAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not used on windows")
	}
	dir := t.TempDir()

	ours := filepath.Join(dir, "tsc-ours")
	if err := os.Symlink("ts-cli", ours); err != nil {
		t.Fatal(err)
	}
	if !isOurTscAlias(ours) {
		t.Error("symlink to ts-cli should be recognised as ours")
	}

	// A regular file (e.g. the TypeScript compiler) must never be ours.
	theirs := filepath.Join(dir, "tsc-theirs")
	if err := os.WriteFile(theirs, []byte("#!/bin/sh\necho typescript\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if isOurTscAlias(theirs) {
		t.Error("regular file wrongly recognised as our alias")
	}

	// A symlink to something else is not ours either.
	other := filepath.Join(dir, "tsc-other")
	if err := os.Symlink("/usr/lib/node_modules/typescript/bin/tsc", other); err != nil {
		t.Fatal(err)
	}
	if isOurTscAlias(other) {
		t.Error("symlink to typescript wrongly recognised as our alias")
	}

	if isOurTscAlias(filepath.Join(dir, "missing")) {
		t.Error("missing path should not be ours")
	}
}

func TestInstallBinaryDoesNotClobberForeignTsc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not used on windows")
	}
	dir := t.TempDir()

	src := filepath.Join(dir, "src-binary")
	if err := os.WriteFile(src, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "bin")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dest, "tsc")
	if err := os.WriteFile(foreign, []byte("typescript"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := installBinary(src, dest); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatalf("foreign tsc was removed: %v", err)
	}
	if string(data) != "typescript" {
		t.Fatalf("foreign tsc was overwritten: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dest, "ts-cli")); err != nil {
		t.Fatalf("ts-cli not installed: %v", err)
	}
}

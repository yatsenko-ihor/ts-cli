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

func TestCopyFileReplacesWithANewInodeAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "ts-cli")
	if err := os.WriteFile(src, []byte("new-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(dst)

	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	after, _ := os.Stat(dst)
	if string(got) != "new-binary" || after.Mode().Perm() != 0o755 {
		t.Fatalf("content %q mode %v", got, after.Mode())
	}
	if os.SameFile(before, after) {
		t.Fatal("the destination must be a new file, not rewritten in place (macOS kills a binary whose cached signature is stale)")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".ts-cli-install-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestCopyFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst")); err == nil {
		t.Fatal("missing source must fail")
	}
	src := filepath.Join(dir, "src")
	_ = os.WriteFile(src, []byte("x"), 0o644)
	if err := copyFile(src, filepath.Join(dir, "no-such-dir", "dst")); err == nil {
		t.Fatal("missing destination directory must fail")
	}
	// rename onto a non-empty directory fails and must not leave the temp file
	target := filepath.Join(dir, "target")
	_ = os.MkdirAll(filepath.Join(target, "sub"), 0o755)
	if err := copyFile(src, target); err == nil {
		t.Fatal("renaming over a directory must fail")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".ts-cli-install-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

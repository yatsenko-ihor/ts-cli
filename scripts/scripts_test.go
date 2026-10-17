package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/util"
)

func TestParseMetadata(t *testing.T) {
	content := []byte(`#!/usr/bin/env bash
# ts-cli: name=Disk usage; os=linux, darwin
# ts-cli: desc=Largest directories
# ts-cli: args=path depth
echo hi
`)
	s := Parse("du.sh", content)
	if s.Name != "Disk usage" || s.Description != "Largest directories" {
		t.Fatalf("name/desc: %+v", s)
	}
	if len(s.OS) != 2 || s.OS[0] != "linux" || s.OS[1] != "darwin" {
		t.Fatalf("os: %v", s.OS)
	}
	if len(s.Args) != 2 || s.Args[0] != "path" || s.Args[1] != "depth" {
		t.Fatalf("args: %v", s.Args)
	}
	if !s.HasShebang() {
		t.Fatal("expected shebang")
	}
	if !s.SupportsOS("macOS") || !s.SupportsOS("linux") || s.SupportsOS("windows") {
		t.Fatal("SupportsOS mismatch")
	}
}

func TestParseDefaultsAndSlashComments(t *testing.T) {
	s := Parse("x.js", []byte("// ts-cli: name=Node thing\nconsole.log(1)\n"))
	if s.Name != "Node thing" || len(s.OS) != 0 || !s.SupportsOS("anything") {
		t.Fatalf("got %+v", s)
	}
	plain := Parse("plain.sh", []byte("echo no header\n"))
	if plain.Name != "plain.sh" || plain.HasShebang() {
		t.Fatalf("got %+v", plain)
	}
}

func TestRemoteCommand(t *testing.T) {
	s := Parse("a.sh", []byte("#!/bin/sh\necho $1\n"))
	cmd := s.RemoteCommand([]string{"/tmp/it's here", "2"})
	for _, want := range []string{`mktemp`, `cat >"$f"`, `chmod 700 "$f"`, `"$f" '/tmp/it'"'"'s here' '2'`, `rm -f "$f"`, `exit $rc`} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("missing %q in %q", want, cmd)
		}
	}
	noShebang := Parse("b.sh", []byte("echo x\n")).RemoteCommand(nil)
	if !strings.Contains(noShebang, `sh "$f";`) {
		t.Fatalf("script without shebang should run via sh: %q", noShebang)
	}
}

func TestLoadReadsDirectoryAndSorts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(util.ConfigDirEnv, dir)

	// Missing directory is fine.
	if got, err := Load(); err != nil || len(got) != 0 {
		t.Fatalf("missing dir: %v %v", got, err)
	}

	sdir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(filepath.Join(sdir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sdir, "zeta.sh"), []byte("# ts-cli: name=Zeta\n"), 0700)
	_ = os.WriteFile(filepath.Join(sdir, "alpha.sh"), []byte("# ts-cli: name=alpha\n"), 0700)
	_ = os.WriteFile(filepath.Join(sdir, ".hidden"), []byte("x"), 0600)

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "Zeta" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Path != filepath.Join(sdir, "alpha.sh") {
		t.Fatalf("path: %s", got[0].Path)
	}
}

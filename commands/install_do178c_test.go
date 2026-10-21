package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Requirements-based tests for install.go (verification/requirements/install.md).
// The tests never touch /usr/local/bin or the real PATH: the installation
// directory, the executable path, the operating system and the external
// programs are all redirected.

// ─── shared helpers (used by the other *_do178c_test.go files of this package) ──

// d178Capture runs fn with os.Stdout and os.Stderr redirected and returns what was written.
func d178Capture(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wo, we
	done := make(chan [2]string)
	go func() {
		var o, e bytes.Buffer
		c := make(chan struct{})
		go func() { _, _ = io.Copy(&e, re); close(c) }()
		_, _ = io.Copy(&o, ro)
		<-c
		done <- [2]string{o.String(), e.String()}
	}()
	func() {
		defer func() {
			os.Stdout, os.Stderr = oldOut, oldErr
			wo.Close()
			we.Close()
		}()
		fn()
	}()
	r := <-done
	return r[0], r[1]
}

// d178Run executes cmd with args and stdin; stdout/stderr include both the
// cobra writers and the process-level os.Stdout/os.Stderr.
func d178Run(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var ob, eb bytes.Buffer
	cmd.SetOut(&ob)
	cmd.SetErr(&eb)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if args == nil {
		args = []string{}
	}
	cmd.SetArgs(args)
	o, e := d178Capture(t, func() { err = cmd.Execute() })
	return ob.String() + o, eb.String() + e, err
}

// d178Fake writes an executable shell script called name into dir.
func d178Fake(t *testing.T, dir, name, script string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func d178SetGoos(t *testing.T, g string) {
	t.Helper()
	old := goos
	goos = g
	t.Cleanup(func() { goos = old })
}

func d178Read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func d178Exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func d178NoTemp(t *testing.T, dir string) {
	t.Helper()
	if m, _ := filepath.Glob(filepath.Join(dir, ".ts-cli-install-*")); len(m) != 0 {
		t.Fatalf("temp files left behind: %v", m)
	}
}

func d178Source(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "src-binary")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// d178Faulty wraps a real temp file and fails selected operations.
type d178Faulty struct {
	tempFile
	failWrite, failChmod, failClose bool
}

var errD178Injected = errors.New("injected failure")

func (f *d178Faulty) Write(b []byte) (int, error) {
	if f.failWrite {
		return 0, errD178Injected
	}
	return f.tempFile.Write(b)
}
func (f *d178Faulty) Chmod(m os.FileMode) error {
	if f.failChmod {
		return errD178Injected
	}
	return f.tempFile.Chmod(m)
}
func (f *d178Faulty) Close() error {
	err := f.tempFile.Close()
	if f.failClose {
		return errD178Injected
	}
	return err
}

// d178FaultyTemp replaces createTemp; fail is called with the 1-based call number
// and returns the faults for that call (nil = a normal temp file).
func d178FaultyTemp(t *testing.T, fail func(n int) *d178Faulty) {
	t.Helper()
	old := createTemp
	n := 0
	createTemp = func(dir, pattern string) (tempFile, error) {
		n++
		f, err := old(dir, pattern)
		if err != nil {
			return nil, err
		}
		if ft := fail(n); ft != nil {
			ft.tempFile = f
			return ft, nil
		}
		return f, nil
	}
	t.Cleanup(func() { createTemp = old })
}

// ─── installBinary ───────────────────────────────────────────────────────────

// Verifies: REQ-INS-001
func TestDO178CInstallBinaryInstallsNewExecutableFile(t *testing.T) {
	src := d178Source(t, "binary-v2")
	dest := filepath.Join(t.TempDir(), "a", "b", "bin") // parents do not exist yet

	if err := installBinary(src, dest); err != nil {
		t.Fatal(err)
	}
	got := filepath.Join(dest, "ts-cli")
	fi, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if d178Read(t, got) != "binary-v2" || fi.Mode().Perm() != 0o755 {
		t.Fatalf("content %q mode %v", d178Read(t, got), fi.Mode())
	}
	d178NoTemp(t, dest)

	// A second install replaces the file by a different file (new inode), never in place.
	if err := os.WriteFile(src, []byte("binary-v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(src, dest); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(got)
	if os.SameFile(fi, fi2) || d178Read(t, got) != "binary-v3" {
		t.Fatalf("reinstall must produce a new file with the new content")
	}
	d178NoTemp(t, dest)
}

// Verifies: REQ-INS-001
func TestDO178CInstallBinaryReplacesSymlinkWithoutFollowingIt(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "bin")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dest, "ts-cli")); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(d178Source(t, "new"), dest); err != nil {
		t.Fatal(err)
	}
	if d178Read(t, victim) != "do not touch" {
		t.Fatal("installation wrote through a symlink")
	}
	if fi, _ := os.Lstat(filepath.Join(dest, "ts-cli")); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the symlink must have been replaced by a regular file")
	}
}

// Verifies: REQ-INS-002
func TestDO178CInstallBinaryCreatesAndRefreshesOwnAlias(t *testing.T) {
	src := d178Source(t, "x")
	dest := t.TempDir()

	if err := installBinary(src, dest); err != nil {
		t.Fatal(err)
	}
	if tgt, err := os.Readlink(filepath.Join(dest, "tsc")); err != nil || tgt != "ts-cli" {
		t.Fatalf("tsc alias: %q %v", tgt, err)
	}

	// An alias of ours that points elsewhere (but at a file called ts-cli) is refreshed.
	_ = os.Remove(filepath.Join(dest, "tsc"))
	if err := os.Symlink("/old/place/ts-cli", filepath.Join(dest, "tsc")); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(src, dest); err != nil {
		t.Fatal(err)
	}
	if tgt, _ := os.Readlink(filepath.Join(dest, "tsc")); tgt != "ts-cli" {
		t.Fatalf("own alias not refreshed: %q", tgt)
	}
}

// Verifies: REQ-INS-003
func TestDO178CInstallBinaryNeverReplacesForeignTsc(t *testing.T) {
	cases := map[string]func(t *testing.T, p string){
		"regular file (TypeScript)": func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("typescript"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"symlink to TypeScript": func(t *testing.T, p string) {
			if err := os.Symlink("/usr/lib/node_modules/typescript/bin/tsc", p); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, p string) {
			if err := os.MkdirAll(filepath.Join(p, "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"dangling symlink": func(t *testing.T, p string) {
			if err := os.Symlink("nowhere", p); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			tsc := filepath.Join(dest, "tsc")
			mk(t, tsc)
			before, _ := os.Lstat(tsc)
			var err error
			out, _ := d178Capture(t, func() { err = installBinary(d178Source(t, "new"), dest) })
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.Lstat(tsc)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("foreign tsc was modified (%v)", err)
			}
			if !strings.Contains(out, "skipping tsc alias") {
				t.Fatalf("no warning printed: %q", out)
			}
			if d178Read(t, filepath.Join(dest, "ts-cli")) != "new" {
				t.Fatal("ts-cli must still be installed")
			}
		})
	}
}

// Verifies: REQ-INS-004
func TestDO178CInstallBinaryAliasFailuresAreOnlyWarnings(t *testing.T) {
	t.Run("old alias cannot be removed", func(t *testing.T) {
		dest := t.TempDir()
		_ = os.Symlink("ts-cli", filepath.Join(dest, "tsc"))
		old := removeFn
		removeFn = func(string) error { return errD178Injected }
		t.Cleanup(func() { removeFn = old })
		var err error
		out, _ := d178Capture(t, func() { err = installBinary(d178Source(t, "new"), dest) })
		if err != nil || !strings.Contains(out, "failed to replace existing tsc alias") {
			t.Fatalf("err=%v out=%q", err, out)
		}
		if d178Read(t, filepath.Join(dest, "ts-cli")) != "new" {
			t.Fatal("ts-cli must be installed")
		}
	})
	t.Run("symlink cannot be created", func(t *testing.T) {
		dest := t.TempDir()
		old := symlinkFn
		symlinkFn = func(string, string) error { return errD178Injected }
		t.Cleanup(func() { symlinkFn = old })
		var err error
		out, _ := d178Capture(t, func() { err = installBinary(d178Source(t, "new"), dest) })
		if err != nil || !strings.Contains(out, "failed to create tsc symlink") {
			t.Fatalf("err=%v out=%q", err, out)
		}
		if d178Exists(filepath.Join(dest, "tsc")) {
			t.Fatal("no alias expected")
		}
	})
}

// Verifies: REQ-INS-005
func TestDO178CInstallBinaryFailures(t *testing.T) {
	t.Run("directory cannot be created", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		_ = os.WriteFile(blocker, []byte("x"), 0o644)
		err := installBinary(d178Source(t, "x"), filepath.Join(blocker, "bin"))
		if err == nil || !strings.Contains(err.Error(), "failed to create directory") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("source missing leaves destination and alias alone", func(t *testing.T) {
		dest := t.TempDir()
		_ = os.WriteFile(filepath.Join(dest, "ts-cli"), []byte("old"), 0o755)
		var err error
		d178Capture(t, func() { err = installBinary(filepath.Join(dest, "missing"), dest) })
		if err == nil || !strings.Contains(err.Error(), "failed to copy binary") {
			t.Fatalf("err = %v", err)
		}
		if d178Read(t, filepath.Join(dest, "ts-cli")) != "old" || d178Exists(filepath.Join(dest, "tsc")) {
			t.Fatal("previous installation must be untouched and no alias created")
		}
	})
}

// Verifies: REQ-INS-006
func TestDO178CInstallBinaryWindowsNamesAndAliasCopy(t *testing.T) {
	d178SetGoos(t, "windows")
	src := d178Source(t, "exe")

	dest := t.TempDir()
	d178Capture(t, func() {
		if err := installBinary(src, dest); err != nil {
			t.Error(err)
		}
	})
	for _, n := range []string{"ts-cli.exe", "tsc.exe"} {
		if d178Read(t, filepath.Join(dest, n)) != "exe" {
			t.Fatalf("%s missing or wrong", n)
		}
	}
	if d178Exists(filepath.Join(dest, "ts-cli")) || d178Exists(filepath.Join(dest, "tsc")) {
		t.Fatal("no extension-less files expected on windows")
	}

	// An existing tsc.exe cannot be recognised as ours, so it is never replaced.
	dest2 := t.TempDir()
	_ = os.WriteFile(filepath.Join(dest2, "tsc.exe"), []byte("typescript"), 0o755)
	out, _ := d178Capture(t, func() { _ = installBinary(src, dest2) })
	if d178Read(t, filepath.Join(dest2, "tsc.exe")) != "typescript" || !strings.Contains(out, "skipping tsc alias") {
		t.Fatalf("tsc.exe replaced: %q", out)
	}

	// Failure to copy the alias is only a warning.
	dest3 := t.TempDir()
	d178FaultyTemp(t, func(n int) *d178Faulty {
		if n == 2 {
			return &d178Faulty{failWrite: true}
		}
		return nil
	})
	var err error
	out, _ = d178Capture(t, func() { err = installBinary(src, dest3) })
	if err != nil || !strings.Contains(out, "failed to create tsc alias") || !d178Exists(filepath.Join(dest3, "ts-cli.exe")) {
		t.Fatalf("err=%v out=%q", err, out)
	}
	d178NoTemp(t, dest3)
}

// ─── copyFile ────────────────────────────────────────────────────────────────

// Verifies: REQ-INS-007
func TestDO178CCopyFileIsTempThenRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "ts-cli")
	_ = os.WriteFile(src, []byte("new"), 0o600)
	_ = os.WriteFile(dst, []byte("old"), 0o644)
	before, _ := os.Stat(dst)
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(dst)
	if os.SameFile(before, after) || d178Read(t, dst) != "new" || after.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v content %q", after.Mode(), d178Read(t, dst))
	}
	// empty source is a valid (boundary) case
	_ = os.WriteFile(src, nil, 0o600)
	if err := copyFile(src, dst); err != nil || d178Read(t, dst) != "" {
		t.Fatalf("empty source: %v", err)
	}
	d178NoTemp(t, dir)
}

// Verifies: REQ-INS-007
func TestDO178CCopyFileFailuresKeepDestinationAndLeaveNoTemp(t *testing.T) {
	cases := []struct {
		name  string
		fault *d178Faulty
	}{
		{"write", &d178Faulty{failWrite: true}},
		{"chmod", &d178Faulty{failChmod: true}},
		{"close", &d178Faulty{failClose: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "ts-cli")
			_ = os.WriteFile(src, []byte("new"), 0o644)
			_ = os.WriteFile(dst, []byte("old"), 0o755)
			d178FaultyTemp(t, func(int) *d178Faulty { f := *c.fault; return &f })
			if err := copyFile(src, dst); !errors.Is(err, errD178Injected) {
				t.Fatalf("err = %v", err)
			}
			if d178Read(t, dst) != "old" {
				t.Fatal("destination changed")
			}
			d178NoTemp(t, dir)
		})
	}
	t.Run("read, create-temp and rename failures", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		_ = os.WriteFile(src, []byte("x"), 0o644)
		if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "d")); err == nil {
			t.Fatal("missing source must fail")
		}
		if err := copyFile(src, filepath.Join(dir, "no-dir", "d")); err == nil {
			t.Fatal("missing destination directory must fail")
		}
		target := filepath.Join(dir, "target")
		_ = os.MkdirAll(filepath.Join(target, "sub"), 0o755)
		if err := copyFile(src, target); err == nil {
			t.Fatal("rename over a non-empty directory must fail")
		}
		d178NoTemp(t, dir)
	})
}

// Verifies: REQ-INS-008
func TestDO178CCopyFileResignsOnDarwinOnly(t *testing.T) {
	oldSign := adHocSign
	t.Cleanup(func() { adHocSign = oldSign })

	run := func(goosName string, signErr error) (calls []string, out string, dst string, err error) {
		d178SetGoos(t, goosName)
		dir := t.TempDir()
		dst = filepath.Join(dir, "ts-cli")
		adHocSign = func(p string) error {
			// signing must happen on the temp file, before the rename
			if d178Exists(dst) || d178Read(t, p) != "bin" {
				t.Errorf("sign called at the wrong moment for %s", p)
			}
			calls = append(calls, filepath.Base(p))
			return signErr
		}
		out, _ = d178Capture(t, func() { err = copyFile(d178Source(t, "bin"), dst) })
		return
	}

	calls, _, dst, err := run("darwin", nil)
	if err != nil || len(calls) != 1 || !strings.HasPrefix(calls[0], ".ts-cli-install-") || d178Read(t, dst) != "bin" {
		t.Fatalf("darwin: calls=%v err=%v", calls, err)
	}
	calls, out, dst, err := run("darwin", errD178Injected)
	if err != nil || len(calls) != 1 || d178Read(t, dst) != "bin" ||
		!strings.Contains(out, "could not re-sign") || !strings.Contains(out, "codesign --force --sign - "+dst) {
		t.Fatalf("darwin sign failure must only warn: err=%v out=%q", err, out)
	}
	for _, g := range []string{"linux", "windows"} {
		if calls, _, _, err := run(g, nil); err != nil || len(calls) != 0 {
			t.Fatalf("%s: signing must not run (calls=%v err=%v)", g, calls, err)
		}
	}
}

// Verifies: REQ-INS-008
func TestDO178CAdHocSignRunsCodesign(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "args.log")
	d178Fake(t, bin, "codesign", `printf '%s ' "$@" > '`+log+`'
exit 0`)
	t.Setenv("PATH", bin)
	if err := adHocSign("/some/file"); err != nil {
		t.Fatal(err)
	}
	if got := d178Read(t, log); got != "--force --sign - /some/file " {
		t.Fatalf("codesign argv = %q", got)
	}
	d178Fake(t, bin, "codesign", "exit 1")
	if err := adHocSign("/some/file"); err == nil {
		t.Fatal("a failing codesign must be reported")
	}
	t.Setenv("PATH", t.TempDir())
	if err := adHocSign("/some/file"); err == nil {
		t.Fatal("a missing codesign must be reported")
	}
}

// ─── isOurTscAlias / isInPath ────────────────────────────────────────────────

// Verifies: REQ-INS-009
func TestDO178CIsOurTscAlias(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, target string) string {
		p := filepath.Join(dir, name)
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	file := filepath.Join(dir, "regular")
	_ = os.WriteFile(file, []byte("x"), 0o755)

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"relative ts-cli (exact target)", mk("a", "ts-cli"), true},
		{"absolute path ending in ts-cli (base name only)", mk("b", "/opt/x/ts-cli"), true},
		{"other target", mk("c", "typescript"), false},
		{"target merely containing ts-cli", mk("d", "ts-cli2"), false},
		{"regular file", file, false},
		{"missing", filepath.Join(dir, "missing"), false},
	}
	for _, c := range cases {
		if got := isOurTscAlias(c.path); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	d178SetGoos(t, "windows")
	if isOurTscAlias(cases[0].path) {
		t.Error("on windows nothing is ever considered ours")
	}
}

// Verifies: REQ-INS-010
func TestDO178CIsInPath(t *testing.T) {
	bin := t.TempDir()
	d178Fake(t, bin, "present-cmd", "exit 0")
	t.Setenv("PATH", bin)
	if !isInPath("present-cmd") || isInPath("absent-cmd") || isInPath("") {
		t.Fatal("isInPath wrong")
	}
	if p := filepath.Join(bin, "plain"); os.WriteFile(p, []byte("x"), 0o644) == nil && isInPath("plain") {
		t.Fatal("a non-executable file is not a command")
	}
}

// ─── defaultInstallDir ───────────────────────────────────────────────────────

// Verifies: REQ-INS-011
func TestDO178CDefaultInstallDir(t *testing.T) {
	pf := t.TempDir()
	t.Setenv("ProgramFiles", pf)
	for _, g := range []string{"linux", "darwin", "freebsd"} {
		d178SetGoos(t, g)
		if got := defaultInstallDir(); got != "/usr/local/bin" {
			t.Errorf("%s: %q", g, got)
		}
	}
	d178SetGoos(t, "windows")
	if got := defaultInstallDir(); got != filepath.Join(pf, "ts-cli") {
		t.Errorf("windows: %q", got)
	}
}

// ─── confirmed ───────────────────────────────────────────────────────────────

// Verifies: REQ-INS-012
func TestDO178CConfirmed(t *testing.T) {
	yes := []string{"y\n", "Y\n", "yes\n", "YES\n", "Yes\n", "  y  \n", "\ty\r\n", "y", "yes"}
	no := []string{"", "\n", "n\n", "no\n", "yy\n", "yess\n", "y please\n", "y n\n", "ye\n", " \n", "0\n", "1\n", "-y\n", "y;rm -rf /\n"}
	for _, s := range yes {
		if !confirmed(strings.NewReader(s)) {
			t.Errorf("%q should confirm", s)
		}
	}
	for _, s := range no {
		if confirmed(strings.NewReader(s)) {
			t.Errorf("%q must not confirm", s)
		}
	}
	if !confirmed(strings.NewReader("yes\nno\n")) {
		t.Error("only the first line is read")
	}
	if confirmed(strings.NewReader("no\nyes\n")) {
		t.Error("only the first line is read")
	}
}

// ─── runInstall ──────────────────────────────────────────────────────────────

type d178InstEnv struct {
	binDir, installDir, src string
}

// d178InstallEnv redirects everything runInstall touches. PATH holds only binDir
// (fake tools); addInstallToPath appends the install directory.
func d178InstallEnv(t *testing.T, addInstallToPath bool) *d178InstEnv {
	t.Helper()
	base := t.TempDir()
	e := &d178InstEnv{binDir: filepath.Join(base, "fakebin"), installDir: filepath.Join(base, "dest")}
	if err := os.MkdirAll(e.binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	e.src = filepath.Join(base, "current-binary")
	_ = os.WriteFile(e.src, []byte("the-binary"), 0o755)

	oldExe, oldDir, oldEval := executablePath, installDirFn, evalSymlinks
	executablePath = func() (string, error) { return e.src, nil }
	installDirFn = func() string { return e.installDir }
	t.Cleanup(func() { executablePath, installDirFn, evalSymlinks = oldExe, oldDir, oldEval })

	path := e.binDir
	if addInstallToPath {
		path += string(os.PathListSeparator) + e.installDir
	}
	t.Setenv("PATH", path)
	t.Setenv("SHELL", "/bin/sh")
	return e
}

// Verifies: REQ-INS-013
func TestDO178CRunInstallConfirmation(t *testing.T) {
	cancel := []string{"", "\n", "n\n", "no\n", "yess\n", "y please\n", "maybe\n", "  \n"}
	for _, in := range cancel {
		t.Run("cancel "+strings.TrimSpace(in), func(t *testing.T) {
			e := d178InstallEnv(t, false)
			out, _, err := d178Run(t, NewInstallCommand(), in)
			if err != nil || !strings.Contains(out, "Installation cancelled.") {
				t.Fatalf("err=%v out=%q", err, out)
			}
			if d178Exists(e.installDir) {
				t.Fatal("a cancelled installation must not touch the destination")
			}
		})
	}
	for _, in := range []string{"y\n", "YES\n", " yes \n", "y"} {
		t.Run("accept "+strings.TrimSpace(in), func(t *testing.T) {
			e := d178InstallEnv(t, false)
			out, _, err := d178Run(t, NewInstallCommand(), in)
			if err != nil || !strings.Contains(out, "Installation completed successfully!") {
				t.Fatalf("err=%v out=%q", err, out)
			}
			if d178Read(t, filepath.Join(e.installDir, "ts-cli")) != "the-binary" {
				t.Fatal("binary not installed")
			}
			if tgt, _ := os.Readlink(filepath.Join(e.installDir, "tsc")); tgt != "ts-cli" {
				t.Fatal("alias not installed")
			}
		})
	}
}

// Verifies: REQ-INS-014
func TestDO178CRunInstallReportsCurrentStatus(t *testing.T) {
	ours := func(t *testing.T, e *d178InstEnv) { d178Fake(t, e.binDir, "tsc", `echo "ts-cli alias"`) }
	typescript := func(t *testing.T, e *d178InstEnv) { d178Fake(t, e.binDir, "tsc", `echo "Version 5.4.2"`) }
	tscli := func(t *testing.T, e *d178InstEnv) { d178Fake(t, e.binDir, "ts-cli", "exit 0") }

	cases := []struct {
		name  string
		setup []func(*testing.T, *d178InstEnv)
		want  []string
		deny  []string
	}{
		{"nothing installed", nil,
			[]string{"✗ ts-cli is not in PATH", "✗ tsc alias is not in PATH", "Do you want to proceed with installation?"},
			[]string{"already installed", "reinstall"}},
		{"ts-cli only", []func(*testing.T, *d178InstEnv){tscli},
			[]string{"✓ ts-cli is already in PATH", "ts-cli is already installed, but tsc alias is missing.", "Do you want to proceed"},
			[]string{"reinstall"}},
		{"both ours", []func(*testing.T, *d178InstEnv){tscli, ours},
			[]string{"✓ tsc alias is already in PATH", "Both ts-cli and tsc are already installed.", "Do you want to reinstall?"},
			[]string{"proceed with installation"}},
		{"TypeScript tsc, ts-cli present", []func(*testing.T, *d178InstEnv){tscli, typescript},
			[]string{"tsc alias not available (TypeScript compiler found)", "ts-cli is already installed, but tsc alias is missing."},
			[]string{"Both ts-cli and tsc", "reinstall"}},
		{"our tsc but no ts-cli", []func(*testing.T, *d178InstEnv){ours},
			[]string{"✗ ts-cli is not in PATH", "✓ tsc alias is already in PATH", "Do you want to proceed with installation?"},
			[]string{"already installed", "reinstall"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := d178InstallEnv(t, false)
			for _, s := range c.setup {
				s(t, e)
			}
			out, _, err := d178Run(t, NewInstallCommand(), "n\n")
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in\n%s", w, out)
				}
			}
			for _, w := range c.deny {
				if strings.Contains(out, w) {
					t.Errorf("unexpected %q in\n%s", w, out)
				}
			}
			if !strings.Contains(out, "Current executable: "+e.src) || !strings.Contains(out, "Installation directory: "+e.installDir) {
				t.Errorf("header missing:\n%s", out)
			}
		})
	}
}

// Verifies: REQ-INS-015
func TestDO178CRunInstallVerifiesAndAdvisesAboutPATH(t *testing.T) {
	t.Run("everything in PATH afterwards: no note", func(t *testing.T) {
		d178InstallEnv(t, true)
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range []string{"✓ ts-cli is now available in PATH", "✓ tsc alias is now available in PATH", "You can now use:"} {
			if !strings.Contains(out, w) {
				t.Errorf("missing %q in\n%s", w, out)
			}
		}
		if strings.Contains(out, "Note: You may need to") {
			t.Errorf("unexpected note:\n%s", out)
		}
	})
	t.Run("TypeScript tsc shadows the alias", func(t *testing.T) {
		e := d178InstallEnv(t, true)
		d178Fake(t, e.binDir, "tsc", `echo "Version 5.4.2"`)
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err != nil || !strings.Contains(out, "⚠ tsc alias conflicts with TypeScript compiler") {
			t.Fatalf("err=%v out=%s", err, out)
		}
		if strings.Contains(out, "Note: You may need to") {
			t.Errorf("both commands resolve, no note expected:\n%s", out)
		}
	})
	t.Run("only tsc resolves afterwards: note shown", func(t *testing.T) {
		e := d178InstallEnv(t, false)
		d178Fake(t, e.binDir, "tsc", `echo "ts-cli alias"`)
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err != nil || !strings.Contains(out, "⚠ ts-cli not yet in PATH") || !strings.Contains(out, "✓ tsc alias is now available in PATH") || !strings.Contains(out, "Note: You may need to") {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
	t.Run("only ts-cli resolves afterwards: note shown", func(t *testing.T) {
		e := d178InstallEnv(t, false)
		d178Fake(t, e.binDir, "ts-cli", "exit 0")
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err != nil || !strings.Contains(out, "✓ ts-cli is now available in PATH") || !strings.Contains(out, "⚠ tsc alias not yet in PATH") || !strings.Contains(out, "Note: You may need to") {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
	hints := []struct{ shell, want, deny string }{
		{"/usr/bin/zsh", "~/.zshrc", "~/.bashrc"},
		{"/bin/bash", "~/.bashrc", "~/.zshrc"},
		{"/usr/bin/fish", "", "rc &&"},
		{"", "", "rc &&"},
	}
	for _, h := range hints {
		t.Run("not in PATH, shell "+h.shell, func(t *testing.T) {
			e := d178InstallEnv(t, false)
			t.Setenv("SHELL", h.shell)
			out, _, err := d178Run(t, NewInstallCommand(), "y\n")
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range []string{"⚠ ts-cli not yet in PATH", "⚠ tsc alias not yet in PATH", "Note: You may need to", "Add " + e.installDir + " to your PATH"} {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in\n%s", w, out)
				}
			}
			if h.want != "" && !strings.Contains(out, h.want) {
				t.Errorf("missing %q in\n%s", h.want, out)
			}
			if strings.Contains(out, h.deny) {
				t.Errorf("unexpected %q in\n%s", h.deny, out)
			}
		})
	}
	t.Run("windows gives no shell hint", func(t *testing.T) {
		d178InstallEnv(t, false)
		d178SetGoos(t, "windows")
		t.Setenv("SHELL", "/bin/zsh")
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Note: You may need to") || strings.Contains(out, "zshrc") {
			t.Fatalf("windows output:\n%s", out)
		}
	})
}

// Verifies: REQ-INS-016
func TestDO178CRunInstallFailures(t *testing.T) {
	t.Run("executable path unknown", func(t *testing.T) {
		e := d178InstallEnv(t, false)
		executablePath = func() (string, error) { return "", errD178Injected }
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err == nil || !strings.Contains(err.Error(), "failed to get executable path") || strings.Contains(out, "proceed") || d178Exists(e.installDir) {
			t.Fatalf("err=%v out=%q", err, out)
		}
	})
	t.Run("symlinks cannot be resolved", func(t *testing.T) {
		e := d178InstallEnv(t, false)
		evalSymlinks = func(string) (string, error) { return "", errD178Injected }
		_, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err == nil || !strings.Contains(err.Error(), "failed to resolve symlinks") || d178Exists(e.installDir) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("the executable is resolved through symlinks before copying", func(t *testing.T) {
		e := d178InstallEnv(t, false)
		link := filepath.Join(filepath.Dir(e.src), "link-to-binary")
		if err := os.Symlink(e.src, link); err != nil {
			t.Fatal(err)
		}
		executablePath = func() (string, error) { return link, nil }
		evalSymlinks = filepath.EvalSymlinks
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err != nil || !strings.Contains(out, "Current executable: "+e.src) {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
	t.Run("installation fails", func(t *testing.T) {
		e := d178InstallEnv(t, false)
		blocker := filepath.Join(t.TempDir(), "file")
		_ = os.WriteFile(blocker, []byte("x"), 0o644)
		e.installDir = filepath.Join(blocker, "bin")
		out, _, err := d178Run(t, NewInstallCommand(), "y\n")
		if err == nil || !strings.Contains(err.Error(), "installation failed:") || strings.Contains(out, "Installation completed") {
			t.Fatalf("err=%v out=%q", err, out)
		}
	})
}

// Verifies: REQ-INS-017
func TestDO178CRunInstallNeverReplacesForeignTscInInstallDir(t *testing.T) {
	e := d178InstallEnv(t, true)
	if err := os.MkdirAll(e.installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(e.installDir, "tsc")
	_ = os.WriteFile(foreign, []byte("#!/bin/sh\necho Version 5.4.2\n"), 0o755)
	out, _, err := d178Run(t, NewInstallCommand(), "y\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d178Read(t, foreign), "Version 5.4.2") || !strings.Contains(out, "skipping tsc alias") {
		t.Fatalf("foreign tsc clobbered or not reported:\n%s", out)
	}
	if !strings.Contains(out, "⚠ tsc alias conflicts with TypeScript compiler") {
		t.Fatalf("conflict not reported:\n%s", out)
	}
}

// d178DefaultRunProgram runs the production default of runProgram (saved at init).
var d178DefaultRunProgram = runProgram

// d178WriteCorruptConfig stores an unparsable config.json in the current config dir.
func d178WriteCorruptConfig(t *testing.T) {
	t.Helper()
	dir := os.Getenv("TS_CLI_CONFIG_DIR")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
}

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/util"
)

// Requirements-based tests for uninstall.go (verification/requirements/uninstall.md).

type d178UniEnv struct{ dir, cfg string }

// d178UninstallEnv redirects the install directory and the config directory to
// temp dirs and installs ts-cli plus its alias there.
func d178UninstallEnv(t *testing.T) *d178UniEnv {
	t.Helper()
	base := t.TempDir()
	e := &d178UniEnv{dir: filepath.Join(base, "bin"), cfg: filepath.Join(base, "cfg")}
	for _, d := range []string{e.dir, e.cfg} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(e.dir, "ts-cli"), []byte("bin"), 0o755)
	_ = os.Symlink("ts-cli", filepath.Join(e.dir, "tsc"))
	_ = os.WriteFile(filepath.Join(e.cfg, "config.json"), []byte("{}"), 0o600)

	old := installDirFn
	installDirFn = func() string { return e.dir }
	t.Cleanup(func() { installDirFn = old })
	t.Setenv(util.ConfigDirEnv, e.cfg)
	return e
}

// Verifies: REQ-UNI-001
func TestDO178CUninstallNothingInstalled(t *testing.T) {
	e := d178UninstallEnv(t)
	_ = os.Remove(filepath.Join(e.dir, "ts-cli"))
	_ = os.Remove(filepath.Join(e.dir, "tsc"))
	out, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
	if err != nil || !strings.Contains(out, "ts-cli is not installed in system PATH.") {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if strings.Contains(out, "proceed with uninstallation") {
		t.Fatal("no prompt expected when nothing is installed")
	}
	if !d178Exists(filepath.Join(e.cfg, "config.json")) {
		t.Fatal("configuration must not be removed when nothing is installed")
	}
}

// Verifies: REQ-UNI-002
func TestDO178CUninstallListsWhatItFinds(t *testing.T) {
	e := d178UninstallEnv(t)
	out, _, err := d178Run(t, NewUninstallCommand(), "n\n", "--config")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"Found installed components:",
		"• " + filepath.Join(e.dir, "ts-cli"),
		"• " + filepath.Join(e.dir, "tsc") + " (alias)",
		"• " + e.cfg + " (configuration)",
		"proceed with uninstallation? (y/n): ",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	// without --config the configuration is not mentioned
	out, _, _ = d178Run(t, NewUninstallCommand(), "n\n")
	if strings.Contains(out, "(configuration)") {
		t.Errorf("configuration listed without --config:\n%s", out)
	}
	// with --config but no such directory it is not listed either
	_ = os.RemoveAll(e.cfg)
	out, _, _ = d178Run(t, NewUninstallCommand(), "n\n", "--config")
	if strings.Contains(out, "(configuration)") {
		t.Errorf("missing config dir listed:\n%s", out)
	}
}

// Verifies: REQ-UNI-003
func TestDO178CUninstallRequiresExplicitYes(t *testing.T) {
	for _, in := range []string{"", "\n", "n\n", "no\n", "yess\n", "y please\n", "maybe\n"} {
		e := d178UninstallEnv(t)
		out, _, err := d178Run(t, NewUninstallCommand(), in, "--config")
		if err != nil || !strings.Contains(out, "Uninstallation cancelled.") {
			t.Fatalf("%q: err=%v out=%q", in, err, out)
		}
		for _, p := range []string{filepath.Join(e.dir, "ts-cli"), filepath.Join(e.dir, "tsc"), filepath.Join(e.cfg, "config.json")} {
			if !d178Exists(p) {
				t.Fatalf("%q: %s was removed", in, p)
			}
		}
	}
	for _, in := range []string{"y\n", "YES\n", " yes \n"} {
		e := d178UninstallEnv(t)
		if _, _, err := d178Run(t, NewUninstallCommand(), in); err != nil || d178Exists(filepath.Join(e.dir, "ts-cli")) {
			t.Fatalf("%q: err=%v", in, err)
		}
	}
}

// Verifies: REQ-UNI-004, REQ-UNI-005
func TestDO178CUninstallRemovesBinaryAliasAndConfig(t *testing.T) {
	e := d178UninstallEnv(t)
	out, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(e.dir, "ts-cli"), filepath.Join(e.dir, "tsc"), e.cfg} {
		if d178Exists(p) {
			t.Errorf("%s still exists", p)
		}
		if !strings.Contains(out, "✓ Removed "+p) {
			t.Errorf("no report for %s in\n%s", p, out)
		}
	}
	if !strings.Contains(out, "✓ ts-cli has been uninstalled.") {
		t.Errorf("final message missing:\n%s", out)
	}
}

// Verifies: REQ-UNI-005
func TestDO178CUninstallKeepsConfigurationWithoutFlag(t *testing.T) {
	e := d178UninstallEnv(t)
	if _, _, err := d178Run(t, NewUninstallCommand(), "y\n"); err != nil {
		t.Fatal(err)
	}
	if !d178Exists(filepath.Join(e.cfg, "config.json")) {
		t.Fatal("configuration removed without --config")
	}
	// the flag defaults to false
	if f := NewUninstallCommand().Flags().Lookup("config"); f == nil || f.DefValue != "false" {
		t.Fatalf("flag: %+v", f)
	}
}

// Verifies: REQ-UNI-004
func TestDO178CUninstallLeavesForeignTscAlone(t *testing.T) {
	foreign := map[string]func(p string){
		"regular file":                func(p string) { _ = os.WriteFile(p, []byte("typescript"), 0o755) },
		"symlink to typescript":       func(p string) { _ = os.Symlink("/usr/lib/node_modules/typescript/bin/tsc", p) },
		"symlink to other ts-cli":     func(p string) { _ = os.Symlink("/opt/x/ts-cli", p) },
		"symlink to ts-cli lookalike": func(p string) { _ = os.Symlink("ts-cli2", p) },
	}
	for name, mk := range foreign {
		t.Run(name, func(t *testing.T) {
			e := d178UninstallEnv(t)
			tsc := filepath.Join(e.dir, "tsc")
			_ = os.Remove(tsc)
			mk(tsc)
			out, _, err := d178Run(t, NewUninstallCommand(), "y\n")
			if err != nil {
				t.Fatal(err)
			}
			if !d178Exists(tsc) || strings.Contains(out, tsc) {
				t.Fatalf("foreign tsc touched or listed:\n%s", out)
			}
			if d178Exists(filepath.Join(e.dir, "ts-cli")) {
				t.Fatal("ts-cli should have been removed")
			}
		})
	}
}

// Verifies: REQ-UNI-006
func TestDO178CUninstallPartialInstallations(t *testing.T) {
	t.Run("alias only", func(t *testing.T) {
		e := d178UninstallEnv(t)
		_ = os.Remove(filepath.Join(e.dir, "ts-cli"))
		out, _, err := d178Run(t, NewUninstallCommand(), "y\n")
		if err != nil || d178Exists(filepath.Join(e.dir, "tsc")) || strings.Contains(out, "• "+filepath.Join(e.dir, "ts-cli")+"\n") {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
	t.Run("binary only", func(t *testing.T) {
		e := d178UninstallEnv(t)
		_ = os.Remove(filepath.Join(e.dir, "tsc"))
		out, _, err := d178Run(t, NewUninstallCommand(), "y\n")
		if err != nil || d178Exists(filepath.Join(e.dir, "ts-cli")) || strings.Contains(out, "(alias)") {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
}

// Verifies: REQ-UNI-007
func TestDO178CUninstallRemovalFailures(t *testing.T) {
	t.Run("ts-cli cannot be removed: error mentioning sudo, nothing else removed", func(t *testing.T) {
		e := d178UninstallEnv(t)
		bin := filepath.Join(e.dir, "ts-cli")
		_ = os.Remove(bin)
		// a non-empty directory cannot be removed with os.Remove
		_ = os.MkdirAll(filepath.Join(bin, "sub"), 0o755)
		_, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
		if err == nil || !strings.Contains(err.Error(), "failed to remove "+bin) || !strings.Contains(err.Error(), "try with sudo") {
			t.Fatalf("err = %v", err)
		}
		if !d178Exists(filepath.Join(e.dir, "tsc")) || !d178Exists(e.cfg) {
			t.Fatal("alias and configuration must be left alone after the failure")
		}
	})
	t.Run("alias cannot be removed: warning, the rest continues", func(t *testing.T) {
		e := d178UninstallEnv(t)
		old := removeFn
		removeFn = func(p string) error {
			if filepath.Base(p) == "tsc" {
				return errD178Injected
			}
			return old(p)
		}
		t.Cleanup(func() { removeFn = old })
		out, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
		if err != nil || !strings.Contains(out, "⚠ Failed to remove "+filepath.Join(e.dir, "tsc")) {
			t.Fatalf("err=%v out=%s", err, out)
		}
		if d178Exists(filepath.Join(e.dir, "ts-cli")) || d178Exists(e.cfg) || !strings.Contains(out, "has been uninstalled") {
			t.Fatalf("binary and config should be gone:\n%s", out)
		}
	})
	t.Run("config cannot be removed: warning only", func(t *testing.T) {
		e := d178UninstallEnv(t)
		old := removeAllFn
		removeAllFn = func(string) error { return errD178Injected }
		t.Cleanup(func() { removeAllFn = old })
		out, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
		if err != nil || !strings.Contains(out, "⚠ Failed to remove "+e.cfg) || !strings.Contains(out, "has been uninstalled") {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
}

// Verifies: REQ-UNI-008
func TestDO178CUninstallConfigDirIsTheOneInUse(t *testing.T) {
	t.Run("default is ~/.ts-cli", func(t *testing.T) {
		e := d178UninstallEnv(t)
		home := t.TempDir()
		t.Setenv(util.ConfigDirEnv, "")
		t.Setenv("HOME", home)
		_ = os.MkdirAll(filepath.Join(home, ".ts-cli"), 0o700)
		out, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
		if err != nil || d178Exists(filepath.Join(home, ".ts-cli")) || !strings.Contains(out, filepath.Join(home, ".ts-cli")) {
			t.Fatalf("err=%v out=%s", err, out)
		}
		if !d178Exists(e.cfg) {
			t.Fatal("an unrelated directory was removed")
		}
	})
	t.Run("home unknown: nothing is removed, never a path relative to the cwd", func(t *testing.T) {
		d178UninstallEnv(t)
		t.Setenv(util.ConfigDirEnv, "")
		t.Setenv("HOME", "")
		cwd := t.TempDir()
		t.Chdir(cwd)
		_ = os.MkdirAll(filepath.Join(cwd, ".ts-cli"), 0o700)
		out, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
		if err != nil || !d178Exists(filepath.Join(cwd, ".ts-cli")) || strings.Contains(out, "(configuration)") {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
	t.Run("directory missing is not an error", func(t *testing.T) {
		e := d178UninstallEnv(t)
		_ = os.RemoveAll(e.cfg)
		out, _, err := d178Run(t, NewUninstallCommand(), "y\n", "--config")
		if err != nil || strings.Contains(out, "Failed") {
			t.Fatalf("err=%v out=%s", err, out)
		}
	})
}

// Verifies: REQ-UNI-009
func TestDO178CUninstallWindowsNames(t *testing.T) {
	e := d178UninstallEnv(t)
	d178SetGoos(t, "windows")
	_ = os.Remove(filepath.Join(e.dir, "tsc"))
	_ = os.WriteFile(filepath.Join(e.dir, "ts-cli.exe"), []byte("x"), 0o755)
	_ = os.WriteFile(filepath.Join(e.dir, "tsc.exe"), []byte("x"), 0o755)
	out, _, err := d178Run(t, NewUninstallCommand(), "y\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"ts-cli.exe", "tsc.exe"} {
		if d178Exists(filepath.Join(e.dir, n)) || !strings.Contains(out, "Removed "+filepath.Join(e.dir, n)) {
			t.Errorf("%s not removed:\n%s", n, out)
		}
	}
	if !d178Exists(filepath.Join(e.dir, "ts-cli")) {
		t.Error("the extension-less file is not an installed component on windows")
	}
}

// Verifies: REQ-UNI-010
func TestDO178CFileExists(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	_ = os.WriteFile(f, nil, 0o644)
	_ = os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling"))
	for p, want := range map[string]bool{f: true, dir: true, filepath.Join(dir, "missing"): false, filepath.Join(dir, "dangling"): false, "": false} {
		if fileExists(p) != want {
			t.Errorf("fileExists(%q) != %v", p, want)
		}
	}
}

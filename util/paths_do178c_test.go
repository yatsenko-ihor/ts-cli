package util

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// stubHome makes userHomeDir succeed (returning a temp dir) for the first
// okCalls calls and fail afterwards, and clears TS_CLI_CONFIG_DIR so that the
// home directory is consulted. It returns the home directory and a call counter.
func stubHome(t *testing.T, okCalls int) (string, *int) {
	t.Helper()
	t.Setenv(ConfigDirEnv, "")
	home := t.TempDir()
	calls := 0
	orig := userHomeDir
	userHomeDir = func() (string, error) {
		calls++
		if calls > okCalls {
			return "", errors.New("no home")
		}
		return home, nil
	}
	t.Cleanup(func() { userHomeDir = orig })
	return home, &calls
}

// Verifies: REQ-PTH-001
func TestConfigDirEnvOverride(t *testing.T) {
	_, calls := stubHome(t, 0) // home lookup would fail: must not be consulted
	for _, v := range []string{"/tmp/x", "relative/dir", "/a b/ü"} {
		t.Setenv(ConfigDirEnv, v)
		got, err := ConfigDir()
		if err != nil || got != v {
			t.Errorf("ConfigDir() = %q, %v; want %q", got, err, v)
		}
	}
	if *calls != 0 {
		t.Errorf("home directory consulted %d times despite override", *calls)
	}
}

// Verifies: REQ-PTH-002, REQ-PTH-003
func TestConfigDirDefaultsToHomeDotTsCli(t *testing.T) {
	home, _ := stubHome(t, 10) // sets env to ""
	got, err := ConfigDir()
	if err != nil || got != filepath.Join(home, ".ts-cli") {
		t.Fatalf("ConfigDir() = %q, %v", got, err)
	}
}

// Verifies: REQ-PTH-002
func TestConfigDirDefaultUsesRealHomeLookup(t *testing.T) {
	t.Setenv(ConfigDirEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	got, err := ConfigDir()
	if err != nil || got != filepath.Join(home, ".ts-cli") {
		t.Fatalf("ConfigDir() = %q, %v", got, err)
	}
}

// Verifies: REQ-PTH-004
func TestConfigDirHomeUnavailable(t *testing.T) {
	stubHome(t, 0)
	got, err := ConfigDir()
	if err == nil || got != "" {
		t.Fatalf("ConfigDir() = %q, %v; want empty path and error", got, err)
	}
	if err.Error() == "" {
		t.Fatal("error must carry a message")
	}
}

// Verifies: REQ-PTH-005
func TestConfigDirDoesNotCreateDirectory(t *testing.T) {
	base := t.TempDir()
	want := filepath.Join(base, "not", "yet")
	t.Setenv(ConfigDirEnv, want)
	if got, err := ConfigDir(); err != nil || got != want {
		t.Fatalf("ConfigDir() = %q, %v", got, err)
	}
	if _, err := os.Stat(want); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ConfigDir must not create the directory, stat err = %v", err)
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Fatalf("ConfigDir created %d entries", len(entries))
	}
}

// Verifies: REQ-PTH-006
func TestEnsureConfigDirCreatesWith0700(t *testing.T) {
	want := filepath.Join(t.TempDir(), "a", "b", ".ts-cli")
	t.Setenv(ConfigDirEnv, want)
	got, err := EnsureConfigDir()
	if err != nil || got != want {
		t.Fatalf("EnsureConfigDir() = %q, %v", got, err)
	}
	info, err := os.Stat(want)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatalf("mode = %o, want 0700", info.Mode().Perm())
	}
	parent, _ := os.Stat(filepath.Dir(want))
	if !parent.IsDir() {
		t.Fatal("parents must be created")
	}
}

// Verifies: REQ-PTH-007
func TestEnsureConfigDirIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ConfigDirEnv, dir)
	marker := filepath.Join(dir, "keep")
	if err := os.WriteFile(marker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if got, err := EnsureConfigDir(); err != nil || got != dir {
			t.Fatalf("EnsureConfigDir() = %q, %v", got, err)
		}
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "x" {
		t.Fatalf("existing content disturbed: %q %v", b, err)
	}
}

// Verifies: REQ-PTH-008
func TestEnsureConfigDirPropagatesConfigDirError(t *testing.T) {
	stubHome(t, 0)
	got, err := EnsureConfigDir()
	if err == nil || got != "" {
		t.Fatalf("EnsureConfigDir() = %q, %v; want empty and error", got, err)
	}
}

// Verifies: REQ-PTH-009
func TestEnsureConfigDirFailsWhenFileBlocksPath(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{blocker, filepath.Join(blocker, "sub")} {
		t.Setenv(ConfigDirEnv, p)
		got, err := EnsureConfigDir()
		if err == nil || got != "" {
			t.Errorf("EnsureConfigDir() on %q = %q, %v; want empty and error", p, got, err)
		}
	}
}

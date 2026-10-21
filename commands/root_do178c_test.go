package commands

import (
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Requirements-based tests for root.go (verification/requirements/root.md).

// captureStderr runs fn with os.Stderr redirected and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() { os.Stderr = old }()
		fn()
	}()
	_ = w.Close()
	return <-done
}

// observeExit replaces osExit for the test and records every status.
func observeExit(t *testing.T) *[]int {
	t.Helper()
	var codes []int
	orig := osExit
	osExit = func(c int) { codes = append(codes, c) }
	t.Cleanup(func() { osExit = orig })
	return &codes
}

// Verifies: REQ-ROOT-001
func TestROOTIdentityAndVersion(t *testing.T) {
	root := NewRootCommand()
	if root.Use != "ts-cli" || root.Version != Version {
		t.Fatalf("Use %q Version %q", root.Use, root.Version)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("Version %q is not MAJOR.MINOR.PATCH", Version)
	}
	stdout, _, err := runCobra(t, root, "", "--version")
	if err != nil || strings.TrimSpace(stdout) != "ts-cli version "+Version {
		t.Fatalf("--version: %q %v", stdout, err)
	}
}

// Verifies: REQ-ROOT-002
func TestROOTRegistersAllSubcommands(t *testing.T) {
	var got []string
	for _, c := range NewRootCommand().Commands() {
		got = append(got, c.Name())
	}
	sort.Strings(got)
	want := []string{"account", "install", "interactive", "list", "login", "secrets", "ssh", "uninstall", "up"}
	// cobra adds help (and completion) itself
	var own []string
	for _, n := range got {
		if n != "help" && n != "completion" {
			own = append(own, n)
		}
	}
	if strings.Join(own, ",") != strings.Join(want, ",") {
		t.Fatalf("subcommands %v, want %v", own, want)
	}
	// each is reachable by name, and a fresh root shares no state with the previous one
	a, b := NewRootCommand(), NewRootCommand()
	if a == b || a.Commands()[0] == b.Commands()[0] {
		t.Fatal("NewRootCommand must build independent command trees")
	}
	for _, n := range want {
		if c, _, err := a.Find([]string{n}); err != nil || c.Name() != n {
			t.Fatalf("%s: %v", n, err)
		}
	}
	// the interactive command keeps its aliases
	if c, _, _ := a.Find([]string{"tui"}); c == nil || c.Name() != "interactive" {
		t.Fatal("tui alias lost")
	}
}

// Verifies: REQ-ROOT-003, REQ-ROOT-004
func TestROOTNoSubcommandRunsInteractiveAndReportsErrors(t *testing.T) {
	// no accounts configured: the interactive command's error is printed to stderr and the exit status is 1
	isolate(t)
	codes := observeExit(t)
	root := NewRootCommand()
	var stdout string
	stderr := captureStderr(t, func() {
		var err error
		stdout, _, err = runCobra(t, root, "")
		if err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
	if !strings.HasPrefix(stderr, "Error: no accounts configured") || !strings.Contains(stderr, "ts-cli login") {
		t.Fatalf("stderr %q", stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout %q", stdout)
	}
	if len(*codes) != 1 || (*codes)[0] != 1 {
		t.Fatalf("exit codes %v", *codes)
	}

	// an unreadable configuration is reported the same way and never replaced
	dir := isolate(t)
	bad := corruptConfig(t, dir)
	*codes = nil
	stderr = captureStderr(t, func() { _, _, _ = runCobra(t, NewRootCommand(), "") })
	if !strings.HasPrefix(stderr, "Error: failed to load configuration") || len(*codes) != 1 || (*codes)[0] != 1 {
		t.Fatalf("stderr %q codes %v", stderr, *codes)
	}
	if string(cfgFile(t, dir)) != string(bad) {
		t.Fatal("corrupt config touched")
	}
}

// Verifies: REQ-ROOT-005
func TestROOTSubcommandsAndUnknownWordsDoNotStartInteractiveMode(t *testing.T) {
	isolate(t)
	codes := observeExit(t)

	// a known subcommand runs instead of the default
	stderr := captureStderr(t, func() {
		stdout, _, err := runCobra(t, NewRootCommand(), "", "account", "list")
		if err != nil || !strings.Contains(stdout, "No accounts configured.") {
			t.Errorf("account list: %q %v", stdout, err)
		}
	})
	if stderr != "" || len(*codes) != 0 {
		t.Fatalf("stderr %q codes %v", stderr, *codes)
	}

	// unknown words and flags are command-line errors, not interactive mode
	for _, args := range [][]string{{"frobnicate"}, {"--no-such-flag"}, {"lst"}} {
		var err error
		stderr = captureStderr(t, func() { _, _, err = runCobra(t, NewRootCommand(), "", args...) })
		if err == nil {
			t.Fatalf("%v: expected an error", args)
		}
		if strings.Contains(stderr, "no accounts configured") || len(*codes) != 0 {
			t.Fatalf("%v: interactive mode started (stderr %q codes %v)", args, stderr, *codes)
		}
	}
}

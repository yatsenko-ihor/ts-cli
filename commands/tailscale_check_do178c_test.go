package commands

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Requirements-based tests for tailscale_check.go (verification/requirements/tscheck.md).

// d178NoSleep replaces sleepFn and returns the recorded durations.
func d178NoSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var calls []time.Duration
	old := sleepFn
	sleepFn = func(d time.Duration) { calls = append(calls, d) }
	t.Cleanup(func() { sleepFn = old })
	return &calls
}

// d178StatusTool installs a fake `tailscale` whose `status` prints out and exits with code.
func d178StatusTool(t *testing.T, bin, out string, code int) {
	t.Helper()
	d178Fake(t, bin, "tailscale", `[ "$1" = status ] || exit 99
printf '%s' '`+out+`'
exit `+string(rune('0'+code)))
}

// Verifies: REQ-TSC-001
func TestDO178CCheckStatusRunning(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	for _, out := range []string{"", "100.64.0.1 laptop user@ linux -\n", "logged in as someone", "logged\nout"} {
		d178StatusTool(t, bin, out, 0)
		if ok, msg := CheckTailscaleStatus(); !ok || msg != "" {
			t.Errorf("%q: got %v %q", out, ok, msg)
		}
	}
}

// Verifies: REQ-TSC-002
func TestDO178CCheckStatusNotRunning(t *testing.T) {
	d178SetGoos(t, "linux")
	t.Run("tailscale missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		ok, msg := CheckTailscaleStatus()
		if ok || msg != buildTailscaleNotRunningMessage() || !strings.Contains(msg, "systemctl start tailscaled") {
			t.Fatalf("got %v %q", ok, msg)
		}
	})
	t.Run("tailscale exits non-zero", func(t *testing.T) {
		bin := t.TempDir()
		t.Setenv("PATH", bin)
		d178StatusTool(t, bin, "failed to connect to local tailscaled", 1)
		ok, msg := CheckTailscaleStatus()
		if ok || msg != buildTailscaleNotRunningMessage() {
			t.Fatalf("got %v %q", ok, msg)
		}
	})
}

// Verifies: REQ-TSC-003
func TestDO178CCheckStatusLoggedOut(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	for _, out := range []string{"Logged out.", "LOGGED OUT", "you are logged out of tailscale", "x logged out"} {
		d178StatusTool(t, bin, out, 0)
		ok, msg := CheckTailscaleStatus()
		if ok || msg != "Tailscale daemon is running but you're logged out.\nRun: tailscale up" {
			t.Errorf("%q: got %v %q", out, ok, msg)
		}
	}
	// near misses are not "logged out"
	for _, out := range []string{"logged-out", "loggedout", "logged  out", "log out"} {
		d178StatusTool(t, bin, out, 0)
		if ok, _ := CheckTailscaleStatus(); !ok {
			t.Errorf("%q must not count as logged out", out)
		}
	}
	// a logged-out message on a failing status is still "not running" with the OS message
	d178SetGoos(t, "linux")
	d178StatusTool(t, bin, "Logged out.", 1)
	if ok, msg := CheckTailscaleStatus(); ok || msg != buildTailscaleNotRunningMessage() {
		t.Errorf("failing status: %v %q", ok, msg)
	}
}

// Verifies: REQ-TSC-004
func TestDO178CNotRunningMessagePerOS(t *testing.T) {
	cases := map[string]string{
		"darwin":  "open -a Tailscale",
		"linux":   "sudo systemctl start tailscaled",
		"windows": "Start menu",
		"freebsd": "Please start the Tailscale daemon",
	}
	for g, want := range cases {
		d178SetGoos(t, g)
		got := buildTailscaleNotRunningMessage()
		if !strings.HasPrefix(got, "Tailscale doesn't appear to be running.") || !strings.Contains(got, want) {
			t.Errorf("%s: %q", g, got)
		}
		for g2, other := range cases {
			if g2 != g && strings.Contains(got, other) {
				t.Errorf("%s message contains the %s text %q", g, g2, other)
			}
		}
	}
}

// Verifies: REQ-TSC-005
func TestDO178CAttemptStartWithoutAutomaticSupport(t *testing.T) {
	sleeps := d178NoSleep(t)
	bin := t.TempDir()
	marker := filepath.Join(bin, "ran")
	for _, tool := range []string{"open", "systemctl", "sudo", "service"} {
		d178Fake(t, bin, tool, "echo x > '"+marker+"'")
	}
	t.Setenv("PATH", bin)

	d178SetGoos(t, "linux")
	out, _ := d178Capture(t, func() {
		if attemptToStartTailscale() {
			t.Error("linux: must report not started")
		}
	})
	if !strings.Contains(out, "sudo systemctl start tailscaled") || !strings.Contains(out, "sudo service tailscaled start") {
		t.Errorf("linux instructions missing:\n%s", out)
	}
	d178SetGoos(t, "windows")
	out, _ = d178Capture(t, func() {
		if attemptToStartTailscale() {
			t.Error("windows: must report not started")
		}
	})
	if !strings.Contains(out, "Automatic Tailscale start not supported on this OS") {
		t.Errorf("windows message missing:\n%s", out)
	}
	if d178Exists(marker) || len(*sleeps) != 0 {
		t.Fatal("nothing may be executed and nothing waited for on these systems")
	}
}

// Verifies: REQ-TSC-006
func TestDO178CAttemptStartOnDarwin(t *testing.T) {
	d178SetGoos(t, "darwin")
	sleeps := d178NoSleep(t)

	t.Run("open succeeds and tailscale comes up", func(t *testing.T) {
		*sleeps = (*sleeps)[:0]
		bin := t.TempDir()
		started := filepath.Join(bin, "started")
		d178Fake(t, bin, "open", `[ "$1 $2" = "-a Tailscale" ] || exit 7
echo x > '`+started+`'`)
		d178Fake(t, bin, "tailscale", `[ -e '`+started+`' ]`)
		t.Setenv("PATH", bin)
		var ok bool
		out, _ := d178Capture(t, func() { ok = attemptToStartTailscale() })
		if !ok || !strings.Contains(out, "✅ Tailscale is now running!") || !strings.Contains(out, "Opening Tailscale app on macOS") {
			t.Fatalf("ok=%v\n%s", ok, out)
		}
		if len(*sleeps) != 1 || (*sleeps)[0] != 2*time.Second {
			t.Fatalf("sleeps = %v", *sleeps)
		}
	})
	t.Run("open succeeds but tailscale is still down", func(t *testing.T) {
		bin := t.TempDir()
		d178Fake(t, bin, "open", "exit 0")
		d178Fake(t, bin, "tailscale", "exit 1")
		t.Setenv("PATH", bin)
		var ok bool
		out, _ := d178Capture(t, func() { ok = attemptToStartTailscale() })
		if ok || !strings.Contains(out, "service not yet active") {
			t.Fatalf("ok=%v\n%s", ok, out)
		}
	})
	t.Run("open fails with output", func(t *testing.T) {
		*sleeps = (*sleeps)[:0]
		bin := t.TempDir()
		d178Fake(t, bin, "open", "echo 'Unable to find application'; exit 1")
		t.Setenv("PATH", bin)
		var ok bool
		out, _ := d178Capture(t, func() { ok = attemptToStartTailscale() })
		if ok || !strings.Contains(out, "❌ Failed to start Tailscale") || !strings.Contains(out, "Output: Unable to find application") || len(*sleeps) != 0 {
			t.Fatalf("ok=%v sleeps=%v\n%s", ok, *sleeps, out)
		}
	})
	t.Run("open fails silently / is missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		var ok bool
		out, _ := d178Capture(t, func() { ok = attemptToStartTailscale() })
		if ok || !strings.Contains(out, "❌ Failed to start Tailscale") || strings.Contains(out, "Output:") {
			t.Fatalf("ok=%v\n%s", ok, out)
		}
	})
}

// Verifies: REQ-TSC-007
func TestDO178CWarnIfTailscaleNotRunning(t *testing.T) {
	d178NoSleep(t)
	t.Run("running: silent and nothing attempted", func(t *testing.T) {
		d178SetGoos(t, "darwin")
		bin := t.TempDir()
		marker := filepath.Join(bin, "ran")
		d178Fake(t, bin, "open", "echo x > '"+marker+"'")
		d178StatusTool(t, bin, "ok", 0)
		t.Setenv("PATH", bin)
		out, errOut := d178Capture(t, WarnIfTailscaleNotRunning)
		if out != "" || errOut != "" || d178Exists(marker) {
			t.Fatalf("out=%q err=%q", out, errOut)
		}
	})
	t.Run("not running, cannot be started: warning and manual hint", func(t *testing.T) {
		d178SetGoos(t, "linux")
		t.Setenv("PATH", t.TempDir())
		out, _ := d178Capture(t, WarnIfTailscaleNotRunning)
		for _, w := range []string{"⚠️  Warning: " + buildTailscaleNotRunningMessage(), "uses the Tailscale API", "Please start Tailscale manually and try again."} {
			if !strings.Contains(out, w) {
				t.Errorf("missing %q in\n%s", w, out)
			}
		}
	})
	t.Run("not running, started automatically: no manual hint", func(t *testing.T) {
		d178SetGoos(t, "darwin")
		bin := t.TempDir()
		started := filepath.Join(bin, "started")
		d178Fake(t, bin, "open", "echo x > '"+started+"'")
		d178Fake(t, bin, "tailscale", `[ -e '`+started+`' ]`)
		t.Setenv("PATH", bin)
		out, _ := d178Capture(t, WarnIfTailscaleNotRunning)
		if !strings.Contains(out, "⚠️  Warning:") || !strings.Contains(out, "Tailscale is now running!") || strings.Contains(out, "manually") {
			t.Fatalf("out:\n%s", out)
		}
	})
	t.Run("logged out reports the login hint", func(t *testing.T) {
		d178SetGoos(t, "linux")
		bin := t.TempDir()
		d178StatusTool(t, bin, "Logged out.", 0)
		t.Setenv("PATH", bin)
		out, _ := d178Capture(t, WarnIfTailscaleNotRunning)
		if !strings.Contains(out, "you're logged out") {
			t.Fatalf("out:\n%s", out)
		}
	})
}

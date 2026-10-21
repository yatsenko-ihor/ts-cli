package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Requirements-based tests for up.go (verification/requirements/up.md).
// `tailscale` is a fake script on a private PATH that records its argv (one
// argument per line) and its standard input.

type d178TS struct{ bin, argv, stdin string }

func d178FakeTailscale(t *testing.T, exitCode int) *d178TS {
	t.Helper()
	f := &d178TS{bin: t.TempDir()}
	f.argv = filepath.Join(f.bin, "argv.log")
	f.stdin = filepath.Join(f.bin, "stdin.log")
	d178Fake(t, f.bin, "tailscale", `for a in "$@"; do printf '%s\n' "$a"; done > '`+f.argv+`'
while IFS= read -r line; do printf '%s\n' "$line"; done > '`+f.stdin+`'
echo "tailscale says hi"
exit `+string(rune('0'+exitCode)))
	t.Setenv("PATH", f.bin)
	return f
}

func (f *d178TS) args(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.argv)
	if err != nil {
		t.Fatalf("tailscale was not run: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func d178Eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Verifies: REQ-UP-001
func TestDO178CUpWithoutTailscaleFailsWithInstructions(t *testing.T) {
	for _, g := range []string{"linux", "darwin", "windows", "plan9"} {
		t.Run(g, func(t *testing.T) {
			d178SetGoos(t, g)
			t.Setenv("PATH", t.TempDir())
			out, _, err := d178Run(t, NewUpCommand(), "")
			if err == nil || !strings.Contains(err.Error(), "tailscale command not found.") || !strings.Contains(err.Error(), getInstallInstructions()) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(out, "Starting Tailscale") {
				t.Fatalf("nothing must be started: %q", out)
			}
		})
	}
}

// Verifies: REQ-UP-002
func TestDO178CUpBuildsArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"defaults", nil, []string{"up", "--accept-dns"}},
		{"accept-dns off is passed explicitly", []string{"--accept-dns=false"}, []string{"up", "--accept-dns=false"}},
		{"accept-routes", []string{"--accept-routes"}, []string{"up", "--accept-routes", "--accept-dns"}},
		{"exit node", []string{"--exit-node=exit1"}, []string{"up", "--accept-dns", "--exit-node=exit1"}},
		{"hostname", []string{"--hostname=box"}, []string{"up", "--accept-dns", "--hostname=box"}},
		{"all, fixed order", []string{"--hostname=box", "--exit-node=e", "--accept-dns=false", "--accept-routes"},
			[]string{"up", "--accept-routes", "--accept-dns=false", "--exit-node=e", "--hostname=box"}},
		{"empty values are omitted", []string{"--exit-node=", "--hostname="}, []string{"up", "--accept-dns"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := d178FakeTailscale(t, 0)
			_, _, err := d178Run(t, NewUpCommand(), "", c.args...)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.args(t); !d178Eq(got, c.want) {
				t.Fatalf("argv = %q, want %q", got, c.want)
			}
		})
	}
}

// Verifies: REQ-UP-003
func TestDO178CUpPassesValuesAsSingleArguments(t *testing.T) {
	hostile := []string{
		"a b c", "x;touch /tmp/pwned", "$(touch /tmp/pwned)", "`id`", "--auth-key=tskey-x", "-flag", "a\tb", "ünï", "x'y\"z",
	}
	for _, h := range hostile {
		f := d178FakeTailscale(t, 0)
		if _, _, err := d178Run(t, NewUpCommand(), "", "--hostname="+h, "--exit-node="+h); err != nil {
			t.Fatal(err)
		}
		want := []string{"up", "--accept-dns", "--exit-node=" + h, "--hostname=" + h}
		if got := f.args(t); !d178Eq(got, want) {
			t.Fatalf("%q: argv = %q", h, got)
		}
	}
}

// Verifies: REQ-UP-004
func TestDO178CUpReportsAndSucceeds(t *testing.T) {
	d178FakeTailscale(t, 0)
	out, _, err := d178Run(t, NewUpCommand(), "", "--accept-routes", "--hostname=box")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"Starting Tailscale...",
		"Running: tailscale up --accept-routes --accept-dns --hostname=box",
		"tailscale says hi", // its output is passed through
		"✓ Tailscale is now connected!",
		"ts-cli list", "ts-cli interactive", "ts-cli ssh <device-name>",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
}

// Verifies: REQ-UP-005
func TestDO178CUpFailureOfTailscaleIsAnError(t *testing.T) {
	d178FakeTailscale(t, 3)
	out, _, err := d178Run(t, NewUpCommand(), "")
	if err == nil || !strings.Contains(err.Error(), "failed to start Tailscale") || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out, "now connected") {
		t.Fatalf("success message after a failure:\n%s", out)
	}
}

// Verifies: REQ-UP-006
func TestDO178CUpForwardsStandardInput(t *testing.T) {
	f := d178FakeTailscale(t, 0)
	if _, _, err := d178Run(t, NewUpCommand(), "answer from the user\n"); err != nil {
		t.Fatal(err)
	}
	if got := d178Read(t, f.stdin); got != "answer from the user\n" {
		t.Fatalf("tailscale stdin = %q", got)
	}
}

// Verifies: REQ-UP-007
func TestDO178CUpFlagDefaultsAndMetadata(t *testing.T) {
	cmd := NewUpCommand()
	if cmd.Use != "up" || cmd.RunE == nil {
		t.Fatal("up command not wired")
	}
	want := map[string]string{"accept-routes": "false", "accept-dns": "true", "exit-node": "", "hostname": ""}
	for name, def := range want {
		f := cmd.Flags().Lookup(name)
		if f == nil || f.DefValue != def {
			t.Errorf("flag %s: %+v, want default %q", name, f, def)
		}
	}
}

// Verifies: REQ-UP-008
func TestDO178CGetInstallInstructions(t *testing.T) {
	cases := map[string]string{
		"darwin":  "brew install --cask tailscale",
		"linux":   "https://tailscale.com/download/linux",
		"windows": "https://tailscale.com/download/windows",
		"plan9":   "https://tailscale.com/download",
		"":        "https://tailscale.com/download",
	}
	for g, want := range cases {
		d178SetGoos(t, g)
		got := getInstallInstructions()
		if !strings.Contains(got, want) || !strings.HasPrefix(got, "To install Tailscale") {
			t.Errorf("%q: %q", g, got)
		}
	}
	d178SetGoos(t, "darwin")
	if strings.Contains(getInstallInstructions(), "linux") {
		t.Error("darwin text must not mention linux")
	}
}

// Verifies: REQ-UP-009
func TestDO178CJoinArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""}, {[]string{}, ""}, {[]string{"up"}, "up"}, {[]string{"up", "--a", "--b=c d"}, "up --a --b=c d"}, {[]string{"", ""}, " "},
	}
	for _, c := range cases {
		if got := joinArgs(c.in); got != c.want {
			t.Errorf("joinArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Verifies: REQ-UP-010
func TestDO178CIsTailscaleInstalled(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if isTailscaleInstalled() {
		t.Fatal("empty PATH: not installed")
	}
	_ = os.WriteFile(filepath.Join(bin, "tailscale"), []byte("data"), 0o644)
	if isTailscaleInstalled() {
		t.Fatal("a non-executable file is not an installation")
	}
	d178Fake(t, bin, "tailscale", "exit 0")
	if !isTailscaleInstalled() {
		t.Fatal("executable on PATH: installed")
	}
}

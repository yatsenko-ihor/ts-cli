package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Requirements-based tests for the `ssh` command in ssh.go (verification/requirements/cmdssh.md).
// Shared helpers d178API / d178SaveConfig / d178Config are also used by the
// list and interactive tests.

// d178API is a fake Tailscale API: GET /tailnet/<tailnet>/devices.
type d178API struct {
	srv *httptest.Server
	mu  sync.Mutex
	// devices per tailnet; failing tailnets answer with the given status
	devices map[string][]client.Device
	status  map[string]int
	tokens  map[string][]string // tailnet -> bearer tokens seen
}

func d178NewAPI(t *testing.T) *d178API {
	t.Helper()
	a := &d178API{devices: map[string][]client.Device{}, status: map[string]int{}, tokens: map[string][]string{}}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // tailnet, <name>, devices
		if len(parts) != 3 || parts[0] != "tailnet" || parts[2] != "devices" {
			http.NotFound(w, r)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		tn := parts[1]
		a.tokens[tn] = append(a.tokens[tn], strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if st := a.status[tn]; st != 0 {
			http.Error(w, `{"message":"nope"}`, st)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": a.devices[tn]})
	}))
	restore := client.SetBaseURL(a.srv.URL)
	t.Cleanup(func() { restore(); a.srv.Close() })
	return a
}

func (a *d178API) seen(tn string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.tokens[tn]...)
}

// d178SaveConfig points TS_CLI_CONFIG_DIR at a temp dir and stores the configuration built by fn.
func d178SaveConfig(t *testing.T, fn func(c *config.Config)) {
	t.Helper()
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	if fn == nil {
		return
	}
	if err := config.Update(func(c *config.Config) error { fn(c); return nil }); err != nil {
		t.Fatal(err)
	}
}

const (
	d178KeyA = "tskey-api-AAAA-0123456789abcdef"
	d178KeyB = "tskey-api-BBBB-0123456789abcdef"
)

// d178SSHEnv gives a fake ssh (and optional running tailscale) on a private PATH.
type d178SSHEnv struct{ bin, argv, stdin string }

func d178NewSSHEnv(t *testing.T, sshExit int, tailscaleRunning bool) *d178SSHEnv {
	t.Helper()
	e := &d178SSHEnv{bin: t.TempDir()}
	e.argv = filepath.Join(e.bin, "ssh-argv.log")
	e.stdin = filepath.Join(e.bin, "ssh-stdin.log")
	d178Fake(t, e.bin, "ssh", `for a in "$@"; do printf '%s\n' "$a"; done > '`+e.argv+`'
while IFS= read -r line; do printf '%s\n' "$line"; done > '`+e.stdin+`'
exit `+string(rune('0'+sshExit)))
	if tailscaleRunning {
		d178Fake(t, e.bin, "tailscale", "exit 0")
	}
	t.Setenv("PATH", e.bin)
	return e
}

func (e *d178SSHEnv) args(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(e.argv)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func d178Dev(id, name, host string, addrs ...string) client.Device {
	return client.Device{ID: id, Name: name, Hostname: host, Addresses: addrs, OS: "linux"}
}

func d178OneAccount(c *config.Config) { c.AddAccount("work", d178KeyA, "work.example.com", true) }

func d178Sleeps(t *testing.T) *[]time.Duration { return d178NoSleep(t) }

// Verifies: REQ-CSH-001
func TestDO178CSSHArgumentCount(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", "100.64.0.1")}
	e := d178NewSSHEnv(t, 0, true)

	for _, args := range [][]string{nil, {"a", "b"}} {
		if _, _, err := d178Run(t, NewSSHCommand(), "", args...); err == nil {
			t.Errorf("args %q must be rejected", args)
		}
	}
	_, _, err := d178Run(t, NewSSHCommand(), "", "")
	if err == nil || !strings.Contains(err.Error(), "device name cannot be empty") {
		t.Fatalf("empty name: %v", err)
	}
	if e.args(t) != nil || len(api.seen("work.example.com")) != 0 {
		t.Fatal("nothing may run for rejected arguments")
	}
}

// Verifies: REQ-CSH-001
func TestDO178CSSHEmptyNameNeverMatchesDeviceWithEmptyFields(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{d178Dev("", "", "", "100.64.0.9")}
	e := d178NewSSHEnv(t, 0, true)
	if _, _, err := d178Run(t, NewSSHCommand(), "", ""); err == nil || e.args(t) != nil {
		t.Fatalf("err=%v args=%q", err, e.args(t))
	}
}

// Verifies: REQ-CSH-002
func TestDO178CSSHConfigErrors(t *testing.T) {
	t.Run("corrupt configuration", func(t *testing.T) {
		d178SaveConfig(t, nil)
		dir, _ := util.ConfigDir()
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte("{broken"), 0o600)
		e := d178NewSSHEnv(t, 0, true)
		_, _, err := d178Run(t, NewSSHCommand(), "", "box")
		if err == nil || !strings.Contains(err.Error(), "failed to load configuration") || e.args(t) != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no accounts, even with explicit credentials", func(t *testing.T) {
		d178SaveConfig(t, nil)
		api := d178NewAPI(t)
		e := d178NewSSHEnv(t, 0, true)
		for _, args := range [][]string{{"box"}, {"box", "--api-key", d178KeyA, "--tailnet", "x.com"}} {
			_, _, err := d178Run(t, NewSSHCommand(), "", args...)
			if err == nil || !strings.Contains(err.Error(), "no accounts configured") || !strings.Contains(err.Error(), "ts-cli login") {
				t.Fatalf("%q: err = %v", args, err)
			}
		}
		if e.args(t) != nil || len(api.seen("x.com")) != 0 {
			t.Fatal("nothing may run without accounts")
		}
	})
}

// Verifies: REQ-CSH-003
func TestDO178CSSHExplicitCredentialsOverrideStoredAccounts(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{d178Dev("w1", "box.ts.net", "box", "100.64.0.1")}
	api.devices["other.example.com"] = []client.Device{d178Dev("o1", "box.ts.net", "box", "100.64.0.2")}

	e := d178NewSSHEnv(t, 0, true)
	if _, _, err := d178Run(t, NewSSHCommand(), "", "box", "--api-key", d178KeyB, "--tailnet", "other.example.com"); err != nil {
		t.Fatal(err)
	}
	if got := e.args(t); !d178Eq(got, []string{"100.64.0.2"}) {
		t.Fatalf("ssh argv = %q", got)
	}
	if got := api.seen("other.example.com"); len(got) != 1 || got[0] != d178KeyB {
		t.Fatalf("tokens = %q", got)
	}
	if len(api.seen("work.example.com")) != 0 {
		t.Fatal("stored accounts must not be queried when explicit credentials are given")
	}

	// only one of the two flags: ignored, the stored accounts are used
	for _, args := range [][]string{{"--api-key", d178KeyB}, {"--tailnet", "other.example.com"}} {
		_ = os.Remove(e.argv)
		if _, _, err := d178Run(t, NewSSHCommand(), "", append([]string{"box"}, args...)...); err != nil {
			t.Fatal(err)
		}
		if got := e.args(t); !d178Eq(got, []string{"100.64.0.1"}) {
			t.Fatalf("%q: ssh argv = %q (stored account expected)", args, got)
		}
	}
	// failing explicit credentials
	api.status["other.example.com"] = 401
	_ = os.Remove(e.argv)
	_, _, err := d178Run(t, NewSSHCommand(), "", "box", "--api-key", d178KeyB, "--tailnet", "other.example.com")
	if err == nil || !strings.Contains(err.Error(), "failed to list devices") || e.args(t) != nil {
		t.Fatalf("err = %v", err)
	}
}

// Verifies: REQ-CSH-004
func TestDO178CSSHSearchesAllStoredAccounts(t *testing.T) {
	d178SaveConfig(t, func(c *config.Config) {
		c.AddAccount("one", d178KeyA, "one.example.com", true)
		c.AddAccount("two", d178KeyB, "two.example.com", false)
	})
	api := d178NewAPI(t)
	api.devices["one.example.com"] = []client.Device{d178Dev("a1", "alpha.ts.net", "alpha", "100.64.0.1")}
	api.devices["two.example.com"] = []client.Device{d178Dev("b1", "beta.ts.net", "beta", "100.64.0.2")}
	e := d178NewSSHEnv(t, 0, true)

	if _, _, err := d178Run(t, NewSSHCommand(), "", "beta"); err != nil {
		t.Fatal(err)
	}
	if !d178Eq(e.args(t), []string{"100.64.0.2"}) {
		t.Fatalf("argv = %q", e.args(t))
	}
	// one account failing does not hide the devices of the other
	api.status["one.example.com"] = 500
	_ = os.Remove(e.argv)
	_, stderr, err := d178Run(t, NewSSHCommand(), "", "beta")
	if err != nil || !d178Eq(e.args(t), []string{"100.64.0.2"}) || !strings.Contains(stderr, "one") {
		t.Fatalf("err=%v argv=%q stderr=%q", err, e.args(t), stderr)
	}
	// all accounts failing: no devices
	api.status["two.example.com"] = 500
	_, _, err = d178Run(t, NewSSHCommand(), "", "beta")
	if err == nil || !strings.Contains(err.Error(), "no devices found in any of your configured accounts") {
		t.Fatalf("err = %v", err)
	}
}

// Verifies: REQ-CSH-005
func TestDO178CSSHDeviceLookup(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{
		d178Dev("id-1", "first.ts.net", "shared", "100.64.0.1"),
		d178Dev("id-2", "second.ts.net", "shared", "100.64.0.2"),
		d178Dev("id-3", "third.ts.net", "Third", "100.64.0.3"),
	}
	e := d178NewSSHEnv(t, 0, true)

	cases := []struct {
		query string
		want  string // address, "" = not found
	}{
		{"second.ts.net", "100.64.0.2"}, // by name
		{"third.ts.net", "100.64.0.3"},
		{"Third", "100.64.0.3"},  // by hostname
		{"id-2", "100.64.0.2"},   // by ID
		{"shared", "100.64.0.1"}, // first match wins
		{"third", ""},            // case-sensitive
		{"second", ""},           // no partial match
		{"second.ts.net ", ""},   // no trimming
		{"id-", ""},
		{"nope", ""},
	}
	for _, c := range cases {
		_ = os.Remove(e.argv)
		_, _, err := d178Run(t, NewSSHCommand(), "", c.query)
		if c.want == "" {
			if err == nil || !strings.Contains(err.Error(), "device '"+c.query+"' not found in tailnet") || e.args(t) != nil {
				t.Errorf("%q: err=%v argv=%q", c.query, err, e.args(t))
			}
			continue
		}
		if err != nil || !d178Eq(e.args(t), []string{c.want}) {
			t.Errorf("%q: err=%v argv=%q", c.query, err, e.args(t))
		}
	}
}

// Verifies: REQ-CSH-006
func TestDO178CSSHDeviceWithoutAddress(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{
		d178Dev("n1", "noaddr.ts.net", "noaddr"),
		d178Dev("n2", "two.ts.net", "two", "100.64.0.5", "fd7a:115c:a1e0::5"),
	}
	e := d178NewSSHEnv(t, 0, true)
	_, _, err := d178Run(t, NewSSHCommand(), "", "noaddr")
	if err == nil || !strings.Contains(err.Error(), "device 'noaddr' has no IP addresses") || e.args(t) != nil {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := d178Run(t, NewSSHCommand(), "", "two"); err != nil || !d178Eq(e.args(t), []string{"100.64.0.5"}) {
		t.Fatalf("the first address must be used: err=%v argv=%q", err, e.args(t))
	}
}

// Verifies: REQ-CSH-007
func TestDO178CSSHWarnsWhenTailscaleIsNotRunning(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", "100.64.0.1")}
	sleeps := d178Sleeps(t)

	// running: no warning, no wait
	e := d178NewSSHEnv(t, 0, true)
	out, _, err := d178Run(t, NewSSHCommand(), "", "box")
	if err != nil || strings.Contains(out, "Warning") || len(*sleeps) != 0 {
		t.Fatalf("err=%v sleeps=%v out=%q", err, *sleeps, out)
	}

	// not running: warning, 3 s pause, and ssh is still attempted
	d178SetGoos(t, "linux")
	_ = os.Remove(filepath.Join(e.bin, "tailscale"))
	_ = os.Remove(e.argv)
	out, _, err = d178Run(t, NewSSHCommand(), "", "box")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Warning: Tailscale doesn't appear to be running.") || !strings.Contains(out, "wait 3 seconds") {
		t.Fatalf("warning missing:\n%s", out)
	}
	if len(*sleeps) != 1 || (*sleeps)[0] != 3*time.Second || !d178Eq(e.args(t), []string{"100.64.0.1"}) {
		t.Fatalf("sleeps=%v argv=%q", *sleeps, e.args(t))
	}
}

// Verifies: REQ-CSH-008
func TestDO178CSSHUserSelectionPrecedence(t *testing.T) {
	dev := func(id, host string) client.Device { return d178Dev(id, host+".ts.net", host, "100.64.0.1") }
	cases := []struct {
		name   string
		device client.Device
		cfg    func(c *config.Config)
		args   []string
		want   []string
	}{
		{"no user anywhere", dev("n1", "box"), nil, nil, []string{"100.64.0.1"}},
		{"config default user", dev("n1", "box"), func(c *config.Config) { c.SSHUsername = "defuser" }, nil, []string{"defuser@100.64.0.1"}},
		{"machine credential by ID beats config", dev("n1", "box"), func(c *config.Config) {
			c.SSHUsername = "defuser"
			c.MachineCredentials = map[string]config.MachineAuth{"n1": {Username: "byid"}}
		}, nil, []string{"byid@100.64.0.1"}},
		{"machine credential by hostname when the device has no ID", dev("", "box"), func(c *config.Config) {
			c.SSHUsername = "defuser"
			c.MachineCredentials = map[string]config.MachineAuth{"box": {Username: "byhost"}}
		}, nil, []string{"byhost@100.64.0.1"}},
		{"credential stored under the hostname is ignored when the device has an ID", dev("n1", "box"), func(c *config.Config) {
			c.MachineCredentials = map[string]config.MachineAuth{"box": {Username: "byhost"}}
		}, nil, []string{"100.64.0.1"}},
		{"credential without username falls through to config", dev("n1", "box"), func(c *config.Config) {
			c.SSHUsername = "defuser"
			c.MachineCredentials = map[string]config.MachineAuth{"n1": {Name: "x", PasswordEncrypted: ""}}
		}, nil, []string{"defuser@100.64.0.1"}},
		{"--user beats everything", dev("n1", "box"), func(c *config.Config) {
			c.SSHUsername = "defuser"
			c.MachineCredentials = map[string]config.MachineAuth{"n1": {Username: "byid"}}
		}, []string{"--user=flaguser"}, []string{"flaguser@100.64.0.1"}},
		{"--user empty counts as not given", dev("n1", "box"), func(c *config.Config) { c.SSHUsername = "defuser" },
			[]string{"--user="}, []string{"defuser@100.64.0.1"}},
		{"32-character user (boundary)", dev("n1", "box"), nil, []string{"--user=" + strings.Repeat("a", 32)}, []string{strings.Repeat("a", 32) + "@100.64.0.1"}},
		{"IPv6 address", d178Dev("n1", "box.ts.net", "box", "fd7a:115c:a1e0::1"), nil, []string{"--user=bob"}, []string{"bob@fd7a:115c:a1e0::1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d178SaveConfig(t, func(cf *config.Config) {
				d178OneAccount(cf)
				if c.cfg != nil {
					c.cfg(cf)
				}
			})
			api := d178NewAPI(t)
			api.devices["work.example.com"] = []client.Device{c.device}
			e := d178NewSSHEnv(t, 0, true)
			if _, _, err := d178Run(t, NewSSHCommand(), "", append([]string{c.device.Hostname}, c.args...)...); err != nil {
				t.Fatal(err)
			}
			if got := e.args(t); !d178Eq(got, c.want) {
				t.Fatalf("argv = %q, want %q", got, c.want)
			}
		})
	}
}

// Verifies: REQ-CSH-009
func TestDO178CSSHRejectsHostileUserAndAddress(t *testing.T) {
	badUsers := []string{"-oProxyCommand=touch /tmp/pwned", "-", "a b", "a;b", "$(id)", "a@b", "a\nb", strings.Repeat("a", 33), "ünï"}
	for _, u := range badUsers {
		for _, source := range []string{"flag", "machine", "config"} {
			t.Run(source+" "+u, func(t *testing.T) {
				d178SaveConfig(t, func(c *config.Config) {
					d178OneAccount(c)
					switch source {
					case "machine":
						c.MachineCredentials = map[string]config.MachineAuth{"n1": {Username: u}}
					case "config":
						c.SSHUsername = u
					}
				})
				api := d178NewAPI(t)
				api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", "100.64.0.1")}
				e := d178NewSSHEnv(t, 0, true)
				args := []string{"box"}
				if source == "flag" {
					args = append(args, "--user="+u)
				}
				_, _, err := d178Run(t, NewSSHCommand(), "", args...)
				if err == nil || !strings.Contains(err.Error(), "invalid SSH username") || e.args(t) != nil {
					t.Fatalf("err=%v argv=%q", err, e.args(t))
				}
			})
		}
	}
	badAddrs := []string{"box.example.com", "100.64.0.1 -oProxyCommand=x", "-oProxyCommand=x", "100.64.0.1\n", "100.64.0.256", "", "::g", "100.64.0.1;id"}
	for _, a := range badAddrs {
		t.Run("address "+a, func(t *testing.T) {
			d178SaveConfig(t, d178OneAccount)
			api := d178NewAPI(t)
			api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", a)}
			e := d178NewSSHEnv(t, 0, true)
			_, _, err := d178Run(t, NewSSHCommand(), "", "box")
			if err == nil || !strings.Contains(err.Error(), "not a valid IP") || e.args(t) != nil {
				t.Fatalf("err=%v argv=%q", err, e.args(t))
			}
		})
	}
}

// Verifies: REQ-CSH-010
func TestDO178CSSHRunsSSHInteractively(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", "100.64.0.1")}

	e := d178NewSSHEnv(t, 0, true)
	out, _, err := d178Run(t, NewSSHCommand(), "typed by the user\n", "box", "--user", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Connecting to box.ts.net (100.64.0.1)...") {
		t.Fatalf("banner missing:\n%s", out)
	}
	if got := e.args(t); !d178Eq(got, []string{"bob@100.64.0.1"}) {
		t.Fatalf("argv = %q", got)
	}
	if got := d178Read(t, e.stdin); got != "typed by the user\n" {
		t.Fatalf("ssh stdin = %q", got)
	}
}

// Verifies: REQ-CSH-011
func TestDO178CSSHFailureOfSSHIsAnError(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", "100.64.0.1")}

	d178NewSSHEnv(t, 5, true)
	_, _, err := d178Run(t, NewSSHCommand(), "", "box")
	if err == nil || !strings.Contains(err.Error(), "SSH connection failed") || !strings.Contains(err.Error(), "exit status 5") {
		t.Fatalf("err = %v", err)
	}
	// ssh not installed
	t.Setenv("PATH", t.TempDir())
	d178SetGoos(t, "linux")
	d178Sleeps(t)
	_, _, err = d178Run(t, NewSSHCommand(), "", "box")
	if err == nil || !strings.Contains(err.Error(), "SSH connection failed") {
		t.Fatalf("missing ssh: err = %v", err)
	}
}

// Verifies: REQ-CSH-012
func TestDO178CSSHFlagsAndMetadata(t *testing.T) {
	cmd := NewSSHCommand()
	if cmd.Use != "ssh [device-name-or-hostname]" || cmd.RunE == nil {
		t.Fatal("ssh command not wired")
	}
	for _, n := range []string{"api-key", "tailnet", "user"} {
		if f := cmd.Flags().Lookup(n); f == nil || f.DefValue != "" {
			t.Errorf("flag %s: %+v", n, f)
		}
	}
}

// Verifies: REQ-CSH-009
func TestDO178CBuildSSHTarget(t *testing.T) {
	good := []struct{ user, addr, want string }{
		{"", "100.64.0.1", "100.64.0.1"},
		{"bob", "100.64.0.1", "bob@100.64.0.1"},
		{"a", "fd7a::1", "a@fd7a::1"},
		{strings.Repeat("x", 32), "::1", strings.Repeat("x", 32) + "@::1"},
		{"first.last_1-x", "10.0.0.1", "first.last_1-x@10.0.0.1"},
	}
	for _, g := range good {
		if got, err := buildSSHTarget(g.user, g.addr); err != nil || got != g.want {
			t.Errorf("%q,%q: %q %v", g.user, g.addr, got, err)
		}
	}
	bad := []struct{ user, addr string }{
		{"", ""}, {"", "host"}, {"", "-oProxyCommand=x"}, {"bob", "host"}, {"-o", "100.64.0.1"}, {"a b", "100.64.0.1"}, {strings.Repeat("x", 33), "100.64.0.1"},
	}
	for _, b := range bad {
		if got, err := buildSSHTarget(b.user, b.addr); err == nil || got != "" {
			t.Errorf("%q,%q accepted as %q", b.user, b.addr, got)
		}
	}
}

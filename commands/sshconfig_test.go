package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

func sshcDev(id, name string, addrs ...string) client.Device {
	return client.Device{ID: id, Name: name, Hostname: strings.Split(name, ".")[0], Addresses: addrs, AccountName: "work"}
}

// Verifies: REQ-SSC-001, REQ-SSC-002
func TestAliasAndHostBlocks(t *testing.T) {
	cases := map[string]string{
		"box.tail1.ts.net":      "box",
		"box.tail1.ts.net.":     "box",
		"my laptop\nHost evil":  "my-laptop-Host-evil",
		"-..-":                  "",
		"plain.example.com":     "plain.example.com",
		"under_score-and.dot-1": "under_score-and.dot-1",
	}
	for in, want := range cases {
		if got := aliasOf(client.Device{Name: in}); got != want {
			t.Errorf("aliasOf(%q) = %q, want %q", in, got, want)
		}
	}
	if aliasOf(client.Device{Hostname: "onlyhost"}) != "onlyhost" {
		t.Fatal("hostname is used when there is no name")
	}

	text := buildSSHConfig([]client.Device{
		sshcDev("1", "box.tail1.ts.net", "100.64.0.1"),
		sshcDev("2", "noaddr.tail1.ts.net"),
		sshcDev("3", "badaddr.tail1.ts.net", "not-an-ip"),
		sshcDev("4", "\n", "100.64.0.4"),
	}, &config.Config{}, "")
	if !strings.Contains(text, "Host box\n    HostName 100.64.0.1\n") {
		t.Fatalf("text:\n%s", text)
	}
	if strings.Count(text, "Host ") != 1 || strings.Contains(text, "User") {
		t.Fatalf("only the valid machine, no User line:\n%s", text)
	}
}

// Verifies: REQ-SSC-003
func TestUniqueAliases(t *testing.T) {
	a := sshcDev("1", "pi.tail1.ts.net", "100.64.0.1")
	b := sshcDev("2", "pi.tail2.ts.net", "100.64.0.2")
	b.AccountName = "home"
	c := sshcDev("3", "pi.tail3.ts.net", "100.64.0.3")
	c.AccountName = "home"
	d := sshcDev("4", "pi.tail4.ts.net", "100.64.0.4")
	d.AccountName = ""
	text := buildSSHConfig([]client.Device{a, b, c, d}, &config.Config{}, "")
	for _, want := range []string{"Host pi\n", "Host pi-home\n", "Host pi-home-2\n", "Host pi-2\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
}

// Verifies: REQ-SSC-004
func TestSSHUserSources(t *testing.T) {
	cfg := &config.Config{
		SSHUsername:        "defuser",
		MachineCredentials: map[string]config.MachineAuth{"1": {Username: "saved"}, "host2": {Username: "bad user!"}},
	}
	d1 := sshcDev("1", "a.ts.net", "100.64.0.1")
	d2 := client.Device{Hostname: "host2", Name: "host2", Addresses: []string{"100.64.0.2"}}
	d3 := sshcDev("3", "c.ts.net", "100.64.0.3")
	if got := sshUserFor(d1, cfg, "over"); got != "saved" {
		t.Fatalf("saved wins: %q", got)
	}
	if got := sshUserFor(d2, cfg, "over"); got != "over" {
		t.Fatalf("an invalid saved name is dropped, the override applies: %q", got)
	}
	if got := sshUserFor(d3, cfg, ""); got != "defuser" {
		t.Fatalf("default: %q", got)
	}
	if got := sshUserFor(d3, &config.Config{}, ""); got != "" {
		t.Fatalf("none: %q", got)
	}
	if !strings.Contains(buildSSHConfig([]client.Device{d1}, cfg, ""), "    User saved\n") {
		t.Fatal("User line expected")
	}
}

func sshcSetup(t *testing.T, devs []client.Device) (home string) {
	t.Helper()
	t.Setenv(util.ConfigDirEnv, filepath.Join(t.TempDir(), "cfg"))
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	d178StubList(t, devs, nil)
	return home
}

// Verifies: REQ-SSC-005
func TestSSHConfigCommandPrintsAndRejects(t *testing.T) {
	sshcSetup(t, []client.Device{sshcDev("1", "box.tail1.ts.net", "100.64.0.1"), sshcDev("2", "win.tail1.ts.net", "100.64.0.2")})
	stdout, _, err := d178Run(t, NewSSHConfigCommand(), "", "--name", "box", "--user", "pi")
	if err != nil || !strings.Contains(stdout, "Host box\n    HostName 100.64.0.1\n    User pi\n") || strings.Contains(stdout, "win") {
		t.Fatalf("%q %v", stdout, err)
	}
	if _, _, err := d178Run(t, NewSSHConfigCommand(), "", "--user", "bad user!"); err == nil || !strings.Contains(err.Error(), "invalid --user") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := d178Run(t, NewSSHConfigCommand(), "", "extra"); err == nil {
		t.Fatal("positional arguments are rejected")
	}

	d178StubList(t, nil, os.ErrPermission)
	if out, _, err := d178Run(t, NewSSHConfigCommand(), ""); err == nil || strings.Contains(out, "Host") {
		t.Fatalf("a fetch error is returned: %q %v", out, err)
	}

	t.Setenv(util.ConfigDirEnv, filepath.Join(t.TempDir(), "cfg"))
	if err := os.MkdirAll(os.Getenv(util.ConfigDirEnv), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv(util.ConfigDirEnv), "config.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d178Run(t, NewSSHConfigCommand(), ""); err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("err = %v", err)
	}
}

// Verifies: REQ-SSC-006, REQ-SSC-007
func TestSSHConfigWriteAndInstall(t *testing.T) {
	home := sshcSetup(t, []client.Device{sshcDev("1", "box.tail1.ts.net", "100.64.0.1")})
	target := filepath.Join(os.Getenv(util.ConfigDirEnv), sshConfigFileName)

	stdout, _, err := d178Run(t, NewSSHConfigCommand(), "", "--write")
	if err != nil || !strings.Contains(stdout, "Wrote "+target) || !strings.Contains(stdout, "Include") {
		t.Fatalf("%q %v", stdout, err)
	}
	st, err := os.Stat(target)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); err == nil {
		t.Fatal("--write must not touch ~/.ssh/config")
	}

	sshConfig := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(sshConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sshConfig, []byte("Host old\n    User x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, err = d178Run(t, NewSSHConfigCommand(), "", "--install")
	if err != nil || !strings.Contains(stdout, "Added the Include line") {
		t.Fatalf("%q %v", stdout, err)
	}
	got, _ := os.ReadFile(sshConfig)
	if !strings.HasPrefix(string(got), "Include \""+target+"\"\n\nHost old\n") {
		t.Fatalf("~/.ssh/config:\n%s", got)
	}
	stdout, _, err = d178Run(t, NewSSHConfigCommand(), "", "--install")
	again, _ := os.ReadFile(sshConfig)
	if err != nil || !strings.Contains(stdout, "already includes") || string(again) != string(got) {
		t.Fatalf("second run changes nothing: %q %v", stdout, err)
	}

	// No ~/.ssh/config yet: it is created.
	os.Remove(sshConfig)
	if _, _, err := d178Run(t, NewSSHConfigCommand(), "", "--install"); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(sshConfig); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("created: %v %v", st, err)
	}
}

func TestSSHConfigFileErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	os.WriteFile(file, []byte("x"), 0o600)

	// the directory of the target is a regular file
	if err := writeFileAtomic(filepath.Join(file, "x"), []byte("y")); err == nil {
		t.Fatal("mkdir over a file")
	}
	// unreadable ~/.ssh/config (a directory)
	if _, err := ensureInclude(dir, "/x"); err == nil {
		t.Fatal("reading a directory fails")
	}
	// the temporary file cannot be written: its name is a directory
	if err := os.Mkdir(filepath.Join(dir, "blocked.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "blocked"), []byte("y")); err == nil {
		t.Fatal("write over a directory")
	}
	// rename onto a directory fails
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "sub"), []byte("y")); err == nil {
		t.Fatal("rename over a directory")
	}
	if _, err := os.ReadDir(dir); err != nil {
		t.Fatal(err)
	}

	d178StubList(t, []client.Device{sshcDev("1", "a.ts.net", "100.64.0.1")}, nil)
	// the file cannot be written: the target is a directory
	cfg := filepath.Join(dir, "cfg2")
	t.Setenv(util.ConfigDirEnv, cfg)
	os.MkdirAll(filepath.Join(cfg, sshConfigFileName), 0o700)
	if _, _, err := d178Run(t, NewSSHConfigCommand(), "", "--write"); err == nil || !strings.Contains(err.Error(), "failed to write") {
		t.Fatalf("err = %v", err)
	}
	// ~/.ssh/config cannot be updated: ~/.ssh is a file
	os.RemoveAll(filepath.Join(cfg, sshConfigFileName))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.WriteFile(filepath.Join(home, ".ssh"), []byte("x"), 0o600)
	if _, _, err := d178Run(t, NewSSHConfigCommand(), "", "--install"); err == nil || !strings.Contains(err.Error(), "failed to update") {
		t.Fatalf("err = %v", err)
	}
	// no home directory
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, _, err := d178Run(t, NewSSHConfigCommand(), "", "--install"); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Fatalf("err = %v", err)
	}
}

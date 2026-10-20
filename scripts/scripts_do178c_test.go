package scripts

// Requirements-based tests for scripts/scripts.go (see
// verification/requirements/scripts.md and docs/do178c/README.md).

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/util"
)

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
}

// Verifies: REQ-SCR-001
func TestDO178CDir(t *testing.T) {
	t.Run("is the scripts folder in the config dir and is not created", func(t *testing.T) {
		cfg := t.TempDir()
		t.Setenv(util.ConfigDirEnv, cfg)
		got, err := Dir()
		if err != nil || got != filepath.Join(cfg, "scripts") {
			t.Fatalf("Dir = %q, %v", got, err)
		}
		if _, err := os.Stat(got); !os.IsNotExist(err) {
			t.Fatalf("Dir must not create the folder: %v", err)
		}
	})
	t.Run("config dir unresolvable is an error, also through Load", func(t *testing.T) {
		t.Setenv(util.ConfigDirEnv, "")
		t.Setenv("HOME", "")
		if _, err := Dir(); err == nil {
			t.Fatal("expected error")
		}
		if got, err := Load(); err == nil || got != nil {
			t.Fatalf("Load = %v, %v", got, err)
		}
	})
}

// Verifies: REQ-SCR-002, REQ-SCR-003, REQ-SCR-004, REQ-SCR-005
func TestDO178CLoad(t *testing.T) {
	setup := func(t *testing.T) string {
		cfg := t.TempDir()
		t.Setenv(util.ConfigDirEnv, cfg)
		sdir := filepath.Join(cfg, DirName)
		if err := os.MkdirAll(sdir, 0o700); err != nil {
			t.Fatal(err)
		}
		return sdir
	}
	t.Run("missing directory yields empty list without error", func(t *testing.T) {
		t.Setenv(util.ConfigDirEnv, t.TempDir())
		got, err := Load()
		if err != nil || got != nil {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("empty directory yields empty list", func(t *testing.T) {
		setup(t)
		got, err := Load()
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("scripts path that is a file is an error", func(t *testing.T) {
		cfg := t.TempDir()
		t.Setenv(util.ConfigDirEnv, cfg)
		if err := os.WriteFile(filepath.Join(cfg, DirName), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Load()
		if err == nil || !strings.Contains(err.Error(), "failed to read scripts directory") || got != nil {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("skips directories, dot files and unreadable entries; sorts case-insensitively", func(t *testing.T) {
		sdir := setup(t)
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(os.Mkdir(filepath.Join(sdir, "subdir"), 0o700))
		must(os.WriteFile(filepath.Join(sdir, ".hidden.sh"), []byte("# ts-cli: name=Hidden\n"), 0o600))
		must(os.WriteFile(filepath.Join(sdir, "b.sh"), []byte("# ts-cli: name=bravo\n"), 0o700))
		must(os.WriteFile(filepath.Join(sdir, "a.sh"), []byte("# ts-cli: name=Alpha\n"), 0o700))
		must(os.WriteFile(filepath.Join(sdir, "c.sh"), []byte("echo c\n"), 0o700)) // name = file name
		must(os.WriteFile(filepath.Join(sdir, "Z.sh"), []byte("# ts-cli: name=Zulu\n"), 0o700))
		must(os.Symlink(filepath.Join(sdir, "does-not-exist"), filepath.Join(sdir, "dangling.sh")))

		got, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range got {
			names = append(names, s.Name)
			if s.Path != filepath.Join(sdir, s.File) || !filepath.IsAbs(s.Path) {
				t.Fatalf("path wrong: %+v", s)
			}
		}
		if want := []string{"Alpha", "bravo", "c.sh", "Zulu"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("names = %v, want %v", names, want)
		}
		if string(got[2].Content) != "echo c\n" {
			t.Fatalf("content = %q", got[2].Content)
		}
	})
}

// Verifies: REQ-SCR-006
func TestDO178CParseDefaults(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"nil content", ""},
		{"no metadata", "echo hi\n"},
		{"binary-looking content", "\x00\x01\x02"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Parse("tool.sh", []byte(c.content))
			if s.Name != "tool.sh" || s.File != "tool.sh" || string(s.Content) != c.content ||
				s.Description != "" || s.OS != nil || s.Args != nil || s.Path != "" {
				t.Fatalf("got %+v", s)
			}
		})
	}
}

// Verifies: REQ-SCR-007, REQ-SCR-008, REQ-SCR-009
func TestDO178CParseMetadata(t *testing.T) {
	cases := []struct {
		name    string
		content string
		check   func(Script) string
	}{
		{"hash comment name", "# ts-cli: name=Disk usage\n", func(s Script) string { return want(s.Name, "Disk usage") }},
		{"slash comment name", "// ts-cli: name=Node thing\n", func(s Script) string { return want(s.Name, "Node thing") }},
		{"no space after comment marker", "#ts-cli:name=Tight\n", func(s Script) string { return want(s.Name, "Tight") }},
		{"indented comment", "   \t# ts-cli: name=Indented\n", func(s Script) string { return want(s.Name, "Indented") }},
		{"key is case-insensitive", "# ts-cli: NAME=Upper; Desc=D\n", func(s Script) string { return want(s.Name, "Upper") + want(s.Description, "D") }},
		{"description alias", "# ts-cli: description=Long form\n", func(s Script) string { return want(s.Description, "Long form") }},
		{"desc key", "# ts-cli: desc=Short form\n", func(s Script) string { return want(s.Description, "Short form") }},
		{"empty description clears", "# ts-cli: desc=First\n# ts-cli: desc=\n", func(s Script) string { return want(s.Description, "") }},
		{"empty name keeps file name", "# ts-cli: name=\n", func(s Script) string { return want(s.Name, "f.sh") }},
		{"whitespace-only name keeps file name", "# ts-cli: name=   \n", func(s Script) string { return want(s.Name, "f.sh") }},
		{"later name wins", "# ts-cli: name=One\n# ts-cli: name=Two\n", func(s Script) string { return want(s.Name, "Two") }},
		{"value may contain equals", "# ts-cli: desc=a=b\n", func(s Script) string { return want(s.Description, "a=b") }},
		{"keys combined with semicolons", "# ts-cli: name=N; desc=D; os=linux; args=x\n", func(s Script) string {
			return want(s.Name, "N") + want(s.Description, "D") + want(strings.Join(s.OS, "|"), "linux") + want(strings.Join(s.Args, "|"), "x")
		}},
		{"os lower-cased and split on comma and space", "# ts-cli: os=Linux, macOS  Windows,,\n", func(s Script) string {
			return want(strings.Join(s.OS, "|"), "linux|macos|windows")
		}},
		{"args split on comma and space and keep case", "# ts-cli: args=Path,Depth  Extra\n", func(s Script) string {
			return want(strings.Join(s.Args, "|"), "Path|Depth|Extra")
		}},
		{"list separator comma", "# ts-cli: args=a,b\n", func(s Script) string { return want(strings.Join(s.Args, "|"), "a|b") }},
		{"list separator space", "# ts-cli: args=a b\n", func(s Script) string { return want(strings.Join(s.Args, "|"), "a|b") }},
		{"other punctuation is not a list separator", "# ts-cli: args=a:b\n", func(s Script) string { return want(strings.Join(s.Args, "|"), "a:b") }},
		{"whitespace-only list item (tab) is dropped", "# ts-cli: args=a,\t,b\n", func(s Script) string { return want(strings.Join(s.Args, "|"), "a|b") }},
		{"empty os list", "# ts-cli: os=\n", func(s Script) string { return want(len(s.OS), 0) }},
		{"empty args list", "# ts-cli: args= , ,\n", func(s Script) string { return want(len(s.Args), 0) }},
		{"shebang then header", "#!/bin/sh\n# ts-cli: name=After shebang\n", func(s Script) string { return want(s.Name, "After shebang") }},
		{"plain comment ignored", "# just a comment\n# ts-cli: name=After\n", func(s Script) string { return want(s.Name, "After") }},
		{"other tool prefix ignored", "# ts-clix: name=No\n", func(s Script) string { return want(s.Name, "f.sh") }},
		{"item without equals ignored", "# ts-cli: bogus; name=Kept\n", func(s Script) string { return want(s.Name, "Kept") }},
		{"unknown key ignored", "# ts-cli: colour=red; name=Kept\n", func(s Script) string { return want(s.Name, "Kept") }},
		{"non-comment line carrying the marker ignored", "echo '# ts-cli: name=No'\nts-cli: name=No\n", func(s Script) string { return want(s.Name, "f.sh") }},
		{"windows line endings", "# ts-cli: name=CRLF\r\n", func(s Script) string { return want(s.Name, "CRLF") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if msg := c.check(Parse("f.sh", []byte(c.content))); msg != "" {
				t.Fatal(msg)
			}
		})
	}
}

func want(got, exp any) string {
	if got != exp {
		return fmt.Sprintf("got %#v, want %#v\n", got, exp)
	}
	return ""
}

// Verifies: REQ-SCR-010
func TestDO178CParseScansOnlyFirst30Lines(t *testing.T) {
	filler := func(n int) string { return strings.Repeat("echo x\n", n) }
	cases := []struct {
		name     string
		content  string
		wantName string
	}{
		{"header on line 1", "# ts-cli: name=Found\n", "Found"},
		{"header on line 29", filler(28) + "# ts-cli: name=Found\n", "Found"},
		{"header on line 30 (boundary, last scanned)", filler(29) + "# ts-cli: name=Found\n" + filler(5), "Found"},
		{"header on line 31 (boundary, first ignored)", filler(30) + "# ts-cli: name=Late\n", "f.sh"},
		{"header on line 500", filler(499) + "# ts-cli: name=Late\n", "f.sh"},
		{"file shorter than 30 lines", "echo x\n", "f.sh"},
		{"line above the 1 MiB scanner limit stops parsing safely", strings.Repeat("a", 2<<20) + "\n# ts-cli: name=Late\n", "f.sh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Parse("f.sh", []byte(c.content)).Name; got != c.wantName {
				t.Fatalf("Name = %q, want %q", got, c.wantName)
			}
		})
	}
}

// Verifies: REQ-SCR-011
func TestDO178CSupportsOS(t *testing.T) {
	cases := []struct {
		name     string
		filter   []string
		deviceOS string
		want     bool
	}{
		{"no filter matches anything", nil, "linux", true},
		{"no filter matches empty os", nil, "", true},
		{"empty (non-nil) filter matches anything", []string{}, "plan9", true},
		{"exact match", []string{"linux"}, "linux", true},
		{"device case-insensitive", []string{"linux"}, "LINUX", true},
		{"mismatch", []string{"linux"}, "windows", false},
		{"second entry matches", []string{"windows", "linux"}, "linux", true},
		{"filter darwin, device macOS", []string{"darwin"}, "macOS", true},
		{"filter darwin, device windows", []string{"darwin"}, "windows", false},
		{"filter macos, device darwin", []string{"macos"}, "darwin", true},
		{"filter macos, device windows", []string{"macos"}, "windows", false},
		{"filter linux, device macos is not an alias", []string{"linux"}, "macos", false},
		{"filter linux, device darwin is not an alias", []string{"linux"}, "darwin", false},
		{"filter ios does not match macos", []string{"ios"}, "macos", false},
		{"empty device os vs filter", []string{"linux"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (Script{OS: c.filter}).SupportsOS(c.deviceOS); got != c.want {
				t.Fatalf("SupportsOS(%q) with %v = %v, want %v", c.deviceOS, c.filter, got, c.want)
			}
		})
	}
}

// Verifies: REQ-SCR-012
func TestDO178CHasShebang(t *testing.T) {
	cases := map[string]bool{
		"#!/bin/sh\n": true,
		"#!":          true,
		"#":           false,
		"":            false,
		" #!/bin/sh":  false,
		"\n#!/bin/sh": false,
		"# !/bin/sh":  false,
	}
	for content, want := range cases {
		if got := (Script{Content: []byte(content)}).HasShebang(); got != want {
			t.Errorf("HasShebang(%q) = %v, want %v", content, got, want)
		}
	}
}

// Verifies: REQ-SCR-013
func TestDO178CRemoteCommandShape(t *testing.T) {
	withShebang := Script{Content: []byte("#!/bin/sh\n")}
	plain := Script{Content: []byte("echo\n")}

	t.Run("shebang script is executed directly", func(t *testing.T) {
		got := withShebang.RemoteCommand(nil)
		if !strings.Contains(got, `chmod 700 "$f" && "$f";`) {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("script without shebang runs under sh", func(t *testing.T) {
		got := plain.RemoteCommand(nil)
		if !strings.Contains(got, `chmod 700 "$f" && sh "$f";`) {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("no arguments leaves no trailing blank", func(t *testing.T) {
		if got := withShebang.RemoteCommand([]string{}); !strings.Contains(got, `"$f";`) {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("arguments are quoted and space separated", func(t *testing.T) {
		got := withShebang.RemoteCommand([]string{"a b", "it's"})
		if !strings.Contains(got, `"$f" 'a b' 'it'"'"'s'; rc=$?;`) {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("temp file is private, cleaned up and status propagated", func(t *testing.T) {
		got := plain.RemoteCommand(nil)
		for _, frag := range []string{`mktemp "${TMPDIR:-/tmp}/ts-cli.XXXXXX"`, `|| exit 97`, `cat >"$f"`, `chmod 700 "$f"`, `rc=$?; rm -f "$f"; exit $rc`} {
			if !strings.Contains(got, frag) {
				t.Fatalf("missing %q in %q", frag, got)
			}
		}
	})
	t.Run("script content never appears in the command", func(t *testing.T) {
		s := Script{Content: []byte("#!/bin/sh\necho SECRET-BODY\n")}
		if strings.Contains(s.RemoteCommand([]string{"x"}), "SECRET-BODY") {
			t.Fatal("script content must travel on stdin only")
		}
	})
}

var hostileArgs = []string{
	"plain",
	"a b",
	"it's",
	"'",
	"''",
	`"double"`,
	"$(touch PWNED)",
	"`touch PWNED`",
	"x; touch PWNED",
	"x && touch PWNED",
	"x | touch PWNED",
	"x > PWNED",
	"line1\nline2",
	"tab\there",
	`back\slash`,
	`\`,
	"$HOME ${HOME} $0 $@ $*",
	"*",
	"~",
	"-n",
	"--",
	"-oProxyCommand=x",
	"",
	"unicode é中\U0001F600",
	"!history",
	"%s %d",
}

// Verifies: REQ-SCR-014, REQ-SCR-015, REQ-SCR-016
func TestDO178CShellQuoteLiteralThroughRealShell(t *testing.T) {
	needSh(t)
	for _, arg := range hostileArgs {
		t.Run(arg, func(t *testing.T) {
			work := t.TempDir()
			cmd := exec.Command("sh", "-c", `printf '%s' `+ShellQuote(arg))
			cmd.Dir = work
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != arg {
				t.Fatalf("shell saw %q, want %q", out, arg)
			}
			if entries, _ := os.ReadDir(work); len(entries) != 0 {
				t.Fatalf("quoted argument caused side effects: %v", entries)
			}
		})
	}
	t.Run("exact quoting form", func(t *testing.T) {
		cases := map[string]string{
			"":         `''`,
			"abc":      `'abc'`,
			"it's":     `'it'"'"'s'`,
			"'":        `''"'"''`,
			"a'b'c":    `'a'"'"'b'"'"'c'`,
			"$(x) `y`": "'$(x) `y`'",
		}
		for in, want := range cases {
			if got := ShellQuote(in); got != want {
				t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
			}
		}
	})
	t.Run("all arguments together keep their boundaries", func(t *testing.T) {
		var q []string
		for _, a := range hostileArgs {
			q = append(q, ShellQuote(a))
		}
		work := t.TempDir()
		cmd := exec.Command("sh", "-c", `printf '%s\0' `+strings.Join(q, " "))
		cmd.Dir = work
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		if !reflect.DeepEqual(got, hostileArgs) {
			t.Fatalf("got %q", got)
		}
	})
}

// Verifies: REQ-SCR-013, REQ-SCR-015, REQ-SCR-017
func TestDO178CRemoteCommandRunsLikeTheRemoteHostWould(t *testing.T) {
	needSh(t)

	run := func(t *testing.T, s Script, args []string, env ...string) (stdout string, code int, tmp string) {
		t.Helper()
		tmp = t.TempDir()
		cmd := exec.Command("sh", "-c", s.RemoteCommand(args))
		cmd.Dir = tmp
		cmd.Env = append(os.Environ(), append([]string{"TMPDIR=" + tmp}, env...)...)
		cmd.Stdin = bytes.NewReader(s.Content)
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		code = 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), code, tmp
	}
	argvScript := "printf '%s\\0' \"$@\"\n"

	t.Run("hostile arguments arrive as separate literal arguments (no shebang)", func(t *testing.T) {
		s := Parse("p.sh", []byte(argvScript))
		out, code, tmp := run(t, s, hostileArgs)
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		if got := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00"); !reflect.DeepEqual(got, hostileArgs) {
			t.Fatalf("got %q", got)
		}
		if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
			t.Fatalf("temp file or side effect left behind: %v", entries)
		}
	})
	t.Run("hostile arguments with a shebang script", func(t *testing.T) {
		s := Parse("p.sh", []byte("#!/bin/sh\n"+argvScript))
		out, code, tmp := run(t, s, hostileArgs)
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		if got := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00"); !reflect.DeepEqual(got, hostileArgs) {
			t.Fatalf("got %q", got)
		}
		if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
			t.Fatalf("temp file or side effect left behind: %v", entries)
		}
	})
	t.Run("no arguments", func(t *testing.T) {
		out, code, _ := run(t, Parse("p.sh", []byte("echo n=$#\n")), nil)
		if out != "n=0\n" || code != 0 {
			t.Fatalf("out=%q code=%d", out, code)
		}
	})
	t.Run("exit status of the script is propagated and the file removed", func(t *testing.T) {
		for _, want := range []int{0, 1, 7, 255} {
			s := Parse("p.sh", []byte("exit "+string(rune('0'+want/100%10))+string(rune('0'+want/10%10))+string(rune('0'+want%10))+"\n"))
			_, code, tmp := run(t, s, nil)
			if code != want {
				t.Fatalf("exit %d, want %d", code, want)
			}
			if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
				t.Fatalf("temp file left behind: %v", entries)
			}
		}
	})
	t.Run("temp file is mode 0700 while the script runs", func(t *testing.T) {
		s := Parse("p.sh", []byte(`ls -l "$0" | cut -c1-10`+"\n"))
		out, _, _ := run(t, s, nil)
		if strings.TrimSpace(out) != "-rwx------" {
			t.Fatalf("mode = %q", out)
		}
	})
	t.Run("script body with quotes and dollar signs is not interpreted by the outer shell", func(t *testing.T) {
		body := "echo \"it's $((1+2))\"; echo '$(touch PWNED)'\n"
		out, code, tmp := run(t, Parse("p.sh", []byte(body)), nil)
		if out != "it's 3\n$(touch PWNED)\n" || code != 0 {
			t.Fatalf("out=%q code=%d", out, code)
		}
		if _, err := os.Stat(filepath.Join(tmp, "PWNED")); err == nil {
			t.Fatal("injection executed")
		}
	})
	t.Run("temp directory not usable gives status 97 and the script does not run", func(t *testing.T) {
		s := Parse("p.sh", []byte("echo RAN\n"))
		cmd := exec.Command("sh", "-c", s.RemoteCommand(nil))
		cmd.Env = append(os.Environ(), "TMPDIR=/nonexistent-ts-cli-dir")
		cmd.Stdin = bytes.NewReader(s.Content)
		out, err := cmd.Output()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 97 || strings.Contains(string(out), "RAN") {
			t.Fatalf("err=%v out=%q", err, out)
		}
	})
}

package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/client"
)

func tarGz(t *testing.T, files map[string]string, order ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range order {
		body := files[name]
		typ := byte(tar.TypeReg)
		if strings.HasSuffix(name, "/") {
			typ = tar.TypeDir
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: typ}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("hello"))
		case "/big":
			w.Write([]byte(strings.Repeat("x", 100)))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	if data, err := download(srv.URL+"/ok", 10); err != nil || string(data) != "hello" {
		t.Fatalf("%q %v", data, err)
	}
	for path, want := range map[string]string{"/big": "larger than", "/missing": "404"} {
		if _, err := download(srv.URL+path, 10); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", path, err)
		}
	}
	if _, err := download("http://example.com/x", 10); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("plain http to another host: %v", err)
	}
	if _, err := download("ftp://x", 10); err == nil {
		t.Fatal("ftp")
	}
	if _, err := download("http://127.0.0.1:1/x", 10); err == nil {
		t.Fatal("connection refused")
	}
	// a body cut short is a read error
	cut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("short"))
	}))
	defer cut.Close()
	if _, err := download(cut.URL, 1000); err == nil {
		t.Fatal("truncated body")
	}
}

func TestChecksumFor(t *testing.T) {
	h := strings.Repeat("ab", 32)
	list := h + "  ts-cli_1_linux_amd64.tar.gz\n" + strings.ToUpper(h) + " *other.zip\nshort  x\n\n"
	if got, ok := checksumFor(list, "ts-cli_1_linux_amd64.tar.gz"); !ok || got != h {
		t.Fatalf("%q %v", got, ok)
	}
	if got, ok := checksumFor(list, "other.zip"); !ok || got != h {
		t.Fatalf("binary-mode name and upper case: %q %v", got, ok)
	}
	for _, name := range []string{"x", "missing"} {
		if _, ok := checksumFor(list, name); ok {
			t.Fatalf("%q must not match", name)
		}
	}
}

func TestExtractBinary(t *testing.T) {
	good := tarGz(t, map[string]string{"dir/": "", "dir/README.md": "r", "dir/ts-cli": "BINARY"}, "dir/", "dir/README.md", "dir/ts-cli")
	if data, err := extractBinary(good); err != nil || string(data) != "BINARY" {
		t.Fatalf("%q %v", data, err)
	}
	none := tarGz(t, map[string]string{"dir/README.md": "r", "ts-cli-notes": "x"}, "dir/README.md", "ts-cli-notes")
	if _, err := extractBinary(none); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("no binary: %v", err)
	}
	if _, err := extractBinary([]byte("not gzip")); err == nil || !strings.Contains(err.Error(), "not a valid") {
		t.Fatalf("not gzip: %v", err)
	}
	// a valid gzip stream holding garbage instead of tar
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(bytes.Repeat([]byte("z"), 1024))
	gz.Close()
	if _, err := extractBinary(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("garbage: %v", err)
	}
	// the file's data is cut off
	if _, err := extractBinary(good[:len(good)-30]); err == nil {
		t.Fatal("truncated archive")
	}
	var cutBuf bytes.Buffer
	cgz := gzip.NewWriter(&cutBuf)
	tw := tar.NewWriter(cgz)
	tw.WriteHeader(&tar.Header{Name: "ts-cli", Mode: 0o755, Size: 1000, Typeflag: tar.TypeReg})
	tw.Write([]byte("only a little"))
	cgz.Close() // no tw.Close(): the entry is shorter than its header says
	if _, err := extractBinary(cutBuf.Bytes()); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("short entry: %v", err)
	}
	old := maxBinaryBytes
	maxBinaryBytes = 3
	defer func() { maxBinaryBytes = old }()
	if _, err := extractBinary(good); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("too large: %v", err)
	}
}

type upgradeEnv struct {
	rel     client.Release
	files   map[string][]byte
	exe     string
	archive []byte
}

func upgradeSetup(t *testing.T) *upgradeEnv {
	t.Helper()
	env := &upgradeEnv{files: map[string][]byte{}}
	env.archive = tarGz(t, map[string]string{"ts-cli_9.9.9_linux_amd64/ts-cli": "NEW BINARY"}, "ts-cli_9.9.9_linux_amd64/ts-cli")
	sum := sha256.Sum256(env.archive)
	name := "ts-cli_9.9.9_linux_amd64.tar.gz"
	env.files["https://dl/archive"] = env.archive
	env.files["https://dl/sums"] = []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	env.rel = client.Release{Tag: "v9.9.9", Page: "https://github.com/x/releases/v9.9.9", Assets: []client.ReleaseAsset{
		{Name: name, URL: "https://dl/archive"}, {Name: "checksums.txt", URL: "https://dl/sums"},
	}}
	env.exe = filepath.Join(t.TempDir(), "ts-cli")
	os.WriteFile(env.exe, []byte("OLD BINARY"), 0o755)

	oldDl, oldExe, oldOS, oldArch, oldRel, oldAdHoc := downloadFn, executablePath, goos, goarch, latestReleaseFn, adHocSign
	downloadFn = func(url string, max int64) ([]byte, error) {
		if data, ok := env.files[url]; ok {
			return data, nil
		}
		return nil, errors.New("not found: " + url)
	}
	executablePath = func() (string, error) { return env.exe, nil }
	goos, goarch = "linux", "amd64"
	latestReleaseFn = func() (client.Release, error) { return env.rel, nil }
	adHocSign = func(string) error { return nil }
	oldWrite, oldTmp, hadTmp := writeFileFn, os.Getenv("TMPDIR"), os.Getenv("TMPDIR") != ""
	t.Cleanup(func() {
		downloadFn, executablePath, goos, goarch, latestReleaseFn, adHocSign = oldDl, oldExe, oldOS, oldArch, oldRel, oldAdHoc
		writeFileFn = oldWrite
		if hadTmp {
			os.Setenv("TMPDIR", oldTmp)
		} else {
			os.Unsetenv("TMPDIR")
		}
	})
	return env
}

func TestInstallReleaseSuccess(t *testing.T) {
	env := upgradeSetup(t)
	var out bytes.Buffer
	if err := installRelease(env.rel, &out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(env.exe)
	if string(got) != "NEW BINARY" || !strings.Contains(out.String(), "checksum verified") {
		t.Fatalf("%q / %s", got, out.String())
	}
	if st, _ := os.Stat(env.exe); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}
	// a symlinked executable is replaced where it points
	target := env.exe
	link := filepath.Join(filepath.Dir(env.exe), "link")
	os.Symlink(target, link)
	executablePath = func() (string, error) { return link, nil }
	os.WriteFile(target, []byte("OLD"), 0o755)
	if err := installRelease(env.rel, &out); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "NEW BINARY" {
		t.Fatal("symlink target replaced")
	}
}

func TestInstallReleaseRefusals(t *testing.T) {
	var env *upgradeEnv
	fail := func(want string, mutate func()) {
		t.Run(want, func(t *testing.T) {
			env = upgradeSetup(t)
			mutate()
			err := installRelease(env.rel, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("want %q, got %v", want, err)
			}
			if got, _ := os.ReadFile(env.exe); string(got) != "OLD BINARY" {
				t.Errorf("the old binary must stay, found %q", got)
			}
		})
	}
	fail("cannot replace itself", func() { goos = "windows" })
	fail("no archive for linux/arm64", func() { goarch = "arm64" })
	fail("no checksums.txt", func() { env.rel.Assets = env.rel.Assets[:1] })
	fail("cannot find the running executable", func() { executablePath = func() (string, error) { return "", errors.New("x") } })
	fail("download failed", func() { delete(env.files, "https://dl/archive") })
	fail("checksums.txt failed", func() { delete(env.files, "https://dl/sums") })
	fail("no entry for", func() { env.files["https://dl/sums"] = []byte("") })
	fail("checksum mismatch", func() { env.files["https://dl/archive"] = append([]byte("x"), env.archive...) })
	fail("does not contain ts-cli", func() {
		bad := tarGz(t, map[string]string{"README": "x"}, "README")
		sum := sha256.Sum256(bad)
		env.files["https://dl/archive"] = bad
		env.files["https://dl/sums"] = []byte(hex.EncodeToString(sum[:]) + "  ts-cli_9.9.9_linux_amd64.tar.gz\n")
	})
	fail("no such", func() { os.Setenv("TMPDIR", "/definitely/not/here") })
	fail("disk full", func() {
		writeFileFn = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
		t.Cleanup(func() { writeFileFn = os.WriteFile })
	})
	fail("could not replace", func() {
		missing := filepath.Join(t.TempDir(), "no", "such", "dir", "ts-cli")
		executablePath = func() (string, error) { return missing, nil }
	})
}

func TestUpgradeCommand(t *testing.T) {
	env := upgradeSetup(t)
	oldVer := Version
	t.Cleanup(func() { Version = oldVer })

	Version = "9.9.9"
	stdout, _, err := d178Run(t, NewUpgradeCommand(), "")
	if err != nil || !strings.Contains(stdout, "up to date") {
		t.Fatalf("%q %v", stdout, err)
	}
	Version = "1.0.0"
	stdout, _, err = d178Run(t, NewUpgradeCommand(), "")
	if err != nil || !strings.Contains(stdout, "1.0.0 → v9.9.9") || !strings.Contains(stdout, "upgrade --install") {
		t.Fatalf("%q %v", stdout, err)
	}
	if got, _ := os.ReadFile(env.exe); string(got) != "OLD BINARY" {
		t.Fatal("a plain check changes nothing")
	}
	stdout, _, err = d178Run(t, NewUpgradeCommand(), "", "--install")
	if err != nil || !strings.Contains(stdout, "Installed ts-cli v9.9.9") {
		t.Fatalf("%q %v", stdout, err)
	}
	if got, _ := os.ReadFile(env.exe); string(got) != "NEW BINARY" {
		t.Fatal("installed")
	}
	latestReleaseFn = func() (client.Release, error) { return client.Release{}, errors.New("offline") }
	if _, _, err := d178Run(t, NewUpgradeCommand(), ""); err == nil || err.Error() != "offline" {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := d178Run(t, NewUpgradeCommand(), "", "extra"); err == nil {
		t.Fatal("no arguments")
	}
}

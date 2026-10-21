package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Requirements-based tests for account.go (verification/requirements/account.md).

// ─── helpers shared by the account, login, secrets and root tests ───────────

const (
	keyA = "tskey-api-kAAA-0123456789abcdef"
	keyB = "tskey-api-kBBB-0123456789abcdef"
	keyC = "tskey-api-kCCC-0123456789abcdef"
	keyN = "tskey-api-kNEW-0123456789abcdef"
)

// apiStub is a fake Tailscale API. Status is chosen by bearer token (default
// 401), key records by token.
type apiStub struct {
	mu      sync.Mutex
	hits    int
	tokens  []string
	paths   []string
	status  map[string]int
	expires map[string]string // token -> RFC3339 expiry served by the keys endpoint
}

func startAPI(t *testing.T, status map[string]int, expires map[string]string) *apiStub {
	t.Helper()
	a := &apiStub{status: status, expires: expires}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		a.mu.Lock()
		a.hits++
		a.tokens = append(a.tokens, tok)
		a.paths = append(a.paths, r.URL.Path)
		a.mu.Unlock()
		code, ok := a.status[tok]
		if !ok {
			code = http.StatusUnauthorized
		}
		if strings.HasSuffix(r.URL.Path, "/devices") {
			w.WriteHeader(code)
			if code == http.StatusOK {
				_, _ = w.Write([]byte(`{"devices":[]}`))
			} else {
				_, _ = w.Write([]byte(`{"message":"stub failure"}`))
			}
			return
		}
		if exp, ok := a.expires[tok]; ok {
			_, _ = fmt.Fprintf(w, `{"expires":%q}`, exp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	restore := client.SetBaseURL(srv.URL)
	t.Cleanup(func() { restore(); srv.Close() })
	return a
}

func (a *apiStub) hitCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hits
}

// runCobra runs cmd with separate stdout and stderr.
func runCobra(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	cmd.SetOut(&o)
	cmd.SetErr(&e)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err = cmd.Execute()
	return o.String(), e.String(), err
}

// runAcc runs `account ...` and returns stdout, stderr and the error.
func runAcc(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	return runCobra(t, NewAccountCommand(), stdin, args...)
}

// noSecret fails when any of the texts contains one of the secrets.
func noSecret(t *testing.T, secrets []string, texts ...string) {
	t.Helper()
	for _, s := range secrets {
		for _, x := range texts {
			if strings.Contains(x, s) {
				t.Fatalf("secret %q leaked in %q", s, x)
			}
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(util.ConfigDirEnv, dir)
	t.Setenv("TAILSCALE_API_KEY", "")
	return dir
}

func cfgFile(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadCfg(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func corruptConfig(t *testing.T, dir string) []byte {
	t.Helper()
	bad := []byte("{not json")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), bad, 0600); err != nil {
		t.Fatal(err)
	}
	return bad
}

// seed3 stores home (active), work, third; keys keyA/keyB/keyC.
func seed3(t *testing.T) string {
	t.Helper()
	dir := isolate(t)
	err := config.Update(func(c *config.Config) error {
		c.AddAccount("home", keyA, "home.example.com", true)
		c.AddAccount("work", keyB, "work@example.com", false)
		c.AddAccount("third", keyC, "third.example.com", false)
		c.SSHUsername = "deploy"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// ─── key input ───────────────────────────────────────────────────────────────

// Verifies: REQ-ACC-001, REQ-ACC-003
func TestACCKeyResolvePrecedenceAndSanitising(t *testing.T) {
	cases := []struct {
		name string
		k    keyInput
		in   string
		env  string
		want string
	}{
		{"stdin beats flag and env", keyInput{stdin: true, flag: "flagkey"}, "stdinkey\n", "envkey", "stdinkey"},
		{"flag beats env", keyInput{flag: "flagkey"}, "unused", "envkey", "flagkey"},
		{"env when nothing else", keyInput{}, "unused", "envkey", "envkey"},
		{"nothing given", keyInput{}, "", "", ""},
		{"stdin selected but empty ignores flag", keyInput{stdin: true, flag: "flagkey"}, "", "envkey", ""},
		{"control and NUL removed (stdin)", keyInput{stdin: true}, "ts\x00key\x01-\x1b[31mx\n", "", "tskey-[31mx"},
		{"control removed (flag)", keyInput{flag: " ts\x07key "}, "", "", "tskey"},
		{"control removed (env)", keyInput{}, "", "ts\x01key", "tskey"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("TAILSCALE_API_KEY", c.env)
			got, err := c.k.resolve(strings.NewReader(c.in))
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

// Verifies: REQ-ACC-002
func TestACCKeyResolveStdinBounds(t *testing.T) {
	k := keyInput{stdin: true}
	// only the first line, whitespace and CRLF trimmed
	if got, err := k.resolve(strings.NewReader("  tskey-1  \r\nsecond line\n")); err != nil || got != "tskey-1" {
		t.Fatalf("first line: %q, %v", got, err)
	}
	// EOF without newline is accepted (decision: err == io.EOF)
	if got, err := k.resolve(strings.NewReader("tskey-2")); err != nil || got != "tskey-2" {
		t.Fatalf("no newline: %q, %v", got, err)
	}
	// empty stdin
	if got, err := k.resolve(strings.NewReader("")); err != nil || got != "" {
		t.Fatalf("empty: %q, %v", got, err)
	}
	// hostile: a huge line is cut at 4096 bytes
	got, err := k.resolve(strings.NewReader(strings.Repeat("k", 10000) + "\n"))
	if err != nil || len(got) != 4096 {
		t.Fatalf("long line: len %d, %v", len(got), err)
	}
	// a failing reader is an error and the message names stdin
	_, err = k.resolve(failingReader{})
	if err == nil || !strings.Contains(err.Error(), "failed to read API key from stdin") {
		t.Fatalf("read failure: %v", err)
	}
}

// Verifies: REQ-ACC-002
func TestACCAddFailingStdinIsAnError(t *testing.T) {
	isolate(t)
	api := startAPI(t, map[string]int{keyN: 200}, nil)
	cmd := NewAccountCommand()
	_, _, err := runCobra(t, cmd, "", "add", "--tailnet=x.example.com", "--api-key-stdin")
	if err == nil || !strings.Contains(err.Error(), "API key not provided") {
		t.Fatalf("empty stdin: %v", err)
	}
	// failing stdin through the command (reader replaced after SetIn)
	cmd = NewAccountCommand()
	var o bytes.Buffer
	cmd.SetOut(&o)
	cmd.SetErr(&o)
	cmd.SetIn(failingReader{})
	cmd.SetArgs([]string{"add", "--tailnet=x.example.com", "--api-key-stdin"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "failed to read API key from stdin") {
		t.Fatalf("failing stdin: %v", err)
	}
	if api.hitCount() != 0 {
		t.Fatal("no request without a key")
	}
	// the same for edit
	seed3(t)
	cmd = NewAccountCommand()
	cmd.SetOut(&o)
	cmd.SetErr(&o)
	cmd.SetIn(failingReader{})
	cmd.SetArgs([]string{"edit", "work", "--api-key-stdin"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "failed to read API key from stdin") {
		t.Fatalf("edit failing stdin: %v", err)
	}
}

// ─── add ─────────────────────────────────────────────────────────────────────

// Verifies: REQ-ACC-004, REQ-ACC-005, REQ-ACC-006, REQ-ACC-010
func TestACCAddRejectsBadInputBeforeAnyRequest(t *testing.T) {
	longKey := "tskey-api-" + strings.Repeat("a", 191) // 201 bytes
	okKey200 := "tskey-api-" + strings.Repeat("a", 190)
	cases := []struct {
		name    string
		args    []string
		stdin   string
		wantErr string
	}{
		{"no key", []string{"--tailnet=x.example.com"}, "", "API key not provided"},
		{"key only control chars", []string{"--tailnet=x.example.com", "--api-key=\x01\x02"}, "", "API key not provided"},
		{"tailnet only control chars", []string{"--tailnet=\x01", "--api-key=" + keyN}, "", "tailnet name is required"},
		{"tailnet blank", []string{"--tailnet=  ", "--api-key=" + keyN}, "", "tailnet name is required"},
		{"no key and no tailnet reports the key first", []string{"--tailnet="}, "", "API key not provided"},
		{"name too long (65)", []string{"--tailnet=x.example.com", "--name=" + strings.Repeat("n", 65), "--api-key=" + keyN}, "", "invalid name"},
		{"name with slash", []string{"--tailnet=x.example.com", "--name=a/b", "--api-key=" + keyN}, "", "invalid name"},
		{"name with shell metachar", []string{"--tailnet=x.example.com", "--name=$(id)", "--api-key=" + keyN}, "", "invalid name"},
		{"key too short (19)", []string{"--tailnet=x.example.com", "--api-key=tskey-api-123456789"}, "", "invalid API key"},
		{"key wrong prefix", []string{"--tailnet=x.example.com", "--api-key=abcdefghijklmnopqrstuvwxyz"}, "", "invalid API key"},
		{"key too long (201)", []string{"--tailnet=x.example.com", "--api-key=" + longKey}, "", "invalid API key"},
		{"stdin key too long (cut line)", []string{"--tailnet=x.example.com", "--api-key-stdin"}, strings.Repeat("tskey-", 2000) + "\n", "invalid API key"},
		{"tailnet not email or domain", []string{"--tailnet=nodots", "--api-key=" + keyN}, "", "invalid tailnet"},
		{"tailnet too long", []string{"--tailnet=" + strings.Repeat("a", 250) + ".com", "--api-key=" + keyN}, "", "invalid tailnet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := isolate(t)
			api := startAPI(t, map[string]int{keyN: 200, okKey200: 200}, nil)
			stdout, stderr, err := runAcc(t, c.stdin, append([]string{"add"}, c.args...)...)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
			if api.hitCount() != 0 {
				t.Fatalf("a request was sent for invalid input")
			}
			if _, statErr := os.Stat(filepath.Join(dir, "config.json")); statErr == nil {
				t.Fatal("config must not be created")
			}
			noSecret(t, []string{keyN, "tskey-api-12345"}, stdout, stderr, errText(err))
		})
	}

	// boundaries just inside the limits are accepted and reach the API
	t.Run("boundaries accepted", func(t *testing.T) {
		isolate(t)
		api := startAPI(t, map[string]int{okKey200: 200}, nil)
		name64 := strings.Repeat("n", 64)
		stdout, _, err := runAcc(t, "", "add", "--tailnet=x.example.com", "--name="+name64, "--api-key="+okKey200)
		if err != nil {
			t.Fatal(err)
		}
		if api.hitCount() != 1 || loadCfg(t).Accounts[0].Name != name64 {
			t.Fatalf("hits=%d accounts=%+v", api.hitCount(), loadCfg(t).Accounts)
		}
		noSecret(t, []string{okKey200}, stdout)
	})
}

// Verifies: REQ-ACC-007, REQ-ACC-010
func TestACCAddKeyRejectedByAPIStoresNothing(t *testing.T) {
	for _, code := range []int{401, 403, 500, 404} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := isolate(t)
			startAPI(t, map[string]int{keyN: code}, nil)
			stdout, stderr, err := runAcc(t, "", "add", "--tailnet=x.example.com", "--api-key="+keyN)
			if err == nil || !strings.Contains(err.Error(), "failed to validate API key") {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(stdout, "✓") {
				t.Fatalf("no success marker expected: %q", stdout)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "config.json")); statErr == nil {
				t.Fatal("rejected key must not create a config")
			}
			noSecret(t, []string{keyN}, stdout, stderr, errText(err))
		})
	}
	t.Run("API unreachable", func(t *testing.T) {
		isolate(t)
		srv := httptest.NewServer(http.NotFoundHandler())
		restore := client.SetBaseURL(srv.URL)
		srv.Close() // connection refused
		defer restore()
		_, _, err := runAcc(t, "", "add", "--tailnet=x.example.com", "--api-key="+keyN)
		if err == nil || !strings.Contains(err.Error(), "failed to validate API key") {
			t.Fatalf("err = %v", err)
		}
		if len(loadCfg(t).Accounts) != 0 {
			t.Fatal("nothing may be stored")
		}
	})
}

// Verifies: REQ-ACC-008, REQ-ACC-010
func TestACCAddStoresEncryptedAccount(t *testing.T) {
	dir := isolate(t)
	api := startAPI(t, map[string]int{keyA: 200, keyB: 200}, nil)

	stdout, stderr, err := runAcc(t, "", "add", "--tailnet=home.example.com", "--api-key="+keyA)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Validating API key...", "✓ API key is valid", "✓ Added new account: home.example.com", "✓ Authenticated with tailnet: home.example.com"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	noSecret(t, []string{keyA}, stdout, stderr, string(cfgFile(t, dir)))
	c := loadCfg(t)
	if len(c.Accounts) != 1 || c.Accounts[0].APIKey != keyA || c.Accounts[0].Name != "home.example.com" || !c.Accounts[0].Active {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	if got := api.tokens[0]; got != keyA {
		t.Fatalf("API got token %q", got)
	}
	if !strings.Contains(api.paths[0], "home.example.com") {
		t.Fatalf("path %q", api.paths[0])
	}

	// a second account with a display name does not take over the active flag
	stdout, _, err = runAcc(t, "", "add", "--tailnet=work@example.com", "--name=work", "--api-key="+keyB)
	if err != nil {
		t.Fatal(err)
	}
	c = loadCfg(t)
	if len(c.Accounts) != 2 || c.Accounts[1].Name != "work" || c.Accounts[1].Active || !c.Accounts[0].Active {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	if !strings.Contains(stdout, "✓ Added new account: work") {
		t.Fatalf("stdout %q", stdout)
	}
	noSecret(t, []string{keyA, keyB}, string(cfgFile(t, dir)))
}

// Verifies: REQ-ACC-009, REQ-ACC-010
func TestACCAddExistingTailnetUpdatesInPlace(t *testing.T) {
	dir := isolate(t)
	startAPI(t, map[string]int{keyA: 200, keyN: 200}, nil)
	if _, _, err := runAcc(t, "", "add", "--tailnet=home.example.com", "--name=home", "--api-key="+keyA); err != nil {
		t.Fatal(err)
	}
	// same tailnet, new key, no --name: key replaced, name kept? name defaults to
	// the tailnet, which equals the tailnet, so the stored name is left alone.
	stdout, stderr, err := runAcc(t, "", "add", "--tailnet=home.example.com", "--api-key="+keyN)
	if err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t)
	if len(c.Accounts) != 1 || c.Accounts[0].APIKey != keyN || c.Accounts[0].Name != "home" {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	if !strings.Contains(stdout, "✓ Updated account: home") {
		t.Fatalf("stdout %q", stdout)
	}
	noSecret(t, []string{keyA, keyN}, stdout, stderr, string(cfgFile(t, dir)))

	// explicit different name renames
	if _, _, err := runAcc(t, "", "add", "--tailnet=home.example.com", "--name=house", "--api-key="+keyN); err != nil {
		t.Fatal(err)
	}
	c = loadCfg(t)
	if len(c.Accounts) != 1 || c.Accounts[0].Name != "house" {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
}

// Verifies: REQ-ACC-011, REQ-ACC-010
func TestACCAddFailsWhenConfigCannotBeStored(t *testing.T) {
	dir := isolate(t)
	startAPI(t, map[string]int{keyN: 200}, nil)
	bad := corruptConfig(t, dir)
	stdout, stderr, err := runAcc(t, "", "add", "--tailnet=x.example.com", "--api-key="+keyN)
	if err == nil || !strings.Contains(err.Error(), "failed to store config") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stdout, "Authenticated") {
		t.Fatalf("must not claim success: %q", stdout)
	}
	if got := cfgFile(t, dir); !bytes.Equal(got, bad) {
		t.Fatal("a corrupt config must not be replaced")
	}
	noSecret(t, []string{keyN}, stdout, stderr, errText(err))
}

// Verifies: REQ-ACC-012, REQ-ACC-001, REQ-ACC-010
func TestACCAddFlagsEnvStdinAndArgs(t *testing.T) {
	isolate(t)
	api := startAPI(t, map[string]int{keyA: 200, keyB: 200, keyC: 200}, nil)

	if _, _, err := runAcc(t, "", "add", "--api-key="+keyA); err == nil || !strings.Contains(err.Error(), "tailnet") {
		t.Fatalf("missing --tailnet: %v", err)
	}
	if _, _, err := runAcc(t, "", "add", "--tailnet=a.example.com", "--api-key="+keyA, "extra"); err == nil {
		t.Fatal("positional arguments must be rejected")
	}
	if api.hitCount() != 0 {
		t.Fatal("no request for rejected invocations")
	}

	t.Setenv("TAILSCALE_API_KEY", keyA)
	out1, e1, err := runAcc(t, "", "add", "--tailnet=a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	out2, e2, err := runAcc(t, keyB+"\n", "add", "--tailnet=b.example.com", "--api-key-stdin")
	if err != nil {
		t.Fatal(err)
	}
	out3, e3, err := runAcc(t, "", "add", "--tailnet=c.example.com", "--api-key="+keyC)
	if err != nil {
		t.Fatal(err)
	}
	noSecret(t, []string{keyA, keyB, keyC}, out1, e1, out2, e2, out3, e3)
	c := loadCfg(t)
	if len(c.Accounts) != 3 || c.Accounts[0].APIKey != keyA || c.Accounts[1].APIKey != keyB || c.Accounts[2].APIKey != keyC {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
}

// ─── list ────────────────────────────────────────────────────────────────────

// Verifies: REQ-ACC-013
func TestACCListJSONShapeAndNoKeys(t *testing.T) {
	dir := seed3(t)
	if _, _, err := runAcc(t, "", "hide", "third"); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runAcc(t, "", "list", "--format=json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rows) != 3 || rows[0]["name"] != "home" || rows[0]["active"] != true || rows[2]["hidden"] != true || rows[1]["tailnet"] != "work@example.com" {
		t.Fatalf("rows = %v", rows)
	}
	for _, r := range rows {
		if _, has := r["key"]; has {
			t.Fatalf("key status must be absent without --check: %v", r)
		}
		if _, has := r["expires"]; has {
			t.Fatalf("expires must be absent without --check: %v", r)
		}
	}
	noSecret(t, []string{keyA, keyB, keyC}, stdout, stderr, string(cfgFile(t, dir)))

	// alias and the empty list
	isolate(t)
	stdout, _, err = runAcc(t, "", "ls", "--format=json")
	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("empty list: %q, %v", stdout, err)
	}
}

// Verifies: REQ-ACC-014
func TestACCListErrors(t *testing.T) {
	dir := isolate(t)
	for _, f := range []string{"xml", "JSON", "", "table "} {
		_, _, err := runAcc(t, "", "list", "--format="+f)
		if err == nil || !strings.Contains(err.Error(), "unknown format") {
			t.Fatalf("format %q: %v", f, err)
		}
	}
	bad := corruptConfig(t, dir)
	_, _, err := runAcc(t, "", "list")
	if err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("corrupt config: %v", err)
	}
	// the load error wins over a bad format (config is read first)
	if _, _, err := runAcc(t, "", "list", "--format=xml"); err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("corrupt config + bad format: %v", err)
	}
	if !bytes.Equal(cfgFile(t, dir), bad) {
		t.Fatal("list must not touch a corrupt config")
	}
}

// Verifies: REQ-ACC-015
func TestACCListTable(t *testing.T) {
	seed3(t)
	if _, _, err := runAcc(t, "", "hide", "work"); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runAcc(t, "", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(stdout, "\n")
	if !strings.HasPrefix(lines[0], "NAME") || !strings.Contains(lines[0], "SHOWN") || strings.Contains(lines[0], "API KEY") {
		t.Fatalf("header %q", lines[0])
	}
	if !strings.Contains(lines[2], "home") || !strings.Contains(lines[2], "✓") {
		t.Fatalf("home row %q", lines[2])
	}
	if !strings.Contains(lines[3], "work@example.com") || !strings.Contains(lines[3], "hidden") {
		t.Fatalf("work row %q", lines[3])
	}
	for _, want := range []string{"Total accounts: 3", "Default SSH username: deploy"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	noSecret(t, []string{keyA, keyB, keyC}, stdout, stderr)

	// without SSH username no such line; with no accounts a hint
	if err := config.Update(func(c *config.Config) error { c.SSHUsername = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	stdout, _, _ = runAcc(t, "", "list")
	if strings.Contains(stdout, "Default SSH username") {
		t.Fatalf("stdout %q", stdout)
	}
	isolate(t)
	stdout, _, err = runAcc(t, "", "list")
	if err != nil || !strings.Contains(stdout, "No accounts configured.") || !strings.Contains(stdout, "ts-cli account add --tailnet=") {
		t.Fatalf("empty: %q, %v", stdout, err)
	}
}

// Verifies: REQ-ACC-016, REQ-ACC-013
func TestACCListCheckReportsKeyHealth(t *testing.T) {
	seed3(t)
	exp := time.Now().UTC().Add(10*24*time.Hour + time.Hour).Truncate(time.Second)
	startAPI(t,
		map[string]int{keyA: 200, keyB: 401, keyC: 500},
		map[string]string{keyA: exp.Format(time.RFC3339)})

	stdout, stderr, err := runAcc(t, "", "list", "--check")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"API KEY", "ok: expires in 10 days (" + exp.Format("2006-01-02") + ")", "invalid: key rejected", "unreachable: cannot check"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	noSecret(t, []string{keyA, keyB, keyC}, stdout, stderr)

	stdout, _, err = runAcc(t, "", "list", "--check", "--format=json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []accountJSON
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Key != "ok" || rows[0].Expires != exp.Format(time.RFC3339) || rows[1].Key != "invalid" || rows[1].Expires != "" || rows[2].Key != "unreachable" {
		t.Fatalf("rows = %+v", rows)
	}
	noSecret(t, []string{keyA, keyB, keyC}, stdout)
}

// Verifies: REQ-ACC-016
func TestACCListCheckNoExpiryAndRevoked(t *testing.T) {
	isolate(t)
	if err := config.Update(func(c *config.Config) error {
		c.AddAccount("plain", keyA, "plain.example.com", true)
		c.AddAccount("odd", "tskey-auth-weird-0123456789abcdef", "odd.example.com", false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	startAPI(t, map[string]int{keyA: 200, "tskey-auth-weird-0123456789abcdef": 200}, nil)
	stdout, _, err := runAcc(t, "", "list", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "ok: expiry not readable") || !strings.Contains(stdout, "ok: unknown key format") {
		t.Fatalf("stdout %q", stdout)
	}
}

// Verifies: REQ-ACC-017
func TestACCKeyStatusClassification(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		h          client.Health
		word, note string
	}{
		{"unauthorised wins over everything", client.Health{Unauth: true, Valid: true, HasExpiry: true, Expires: now.Add(time.Hour)}, "invalid", "key rejected"},
		{"not valid, not rejected", client.Health{}, "unreachable", "cannot check"},
		{"expiry in the past", client.Health{Valid: true, HasExpiry: true, Expires: now.Add(-time.Second)}, "invalid", "expired"},
		{"expiry exactly now (boundary)", client.Health{Valid: true, HasExpiry: true, Expires: now}, "invalid", "expired"},
		{"expiry one second ahead", client.Health{Valid: true, HasExpiry: true, Expires: now.Add(time.Second)}, "ok", "expires in 0 days (2026-01-01)"},
		{"expiry in 3.5 days", client.Health{Valid: true, HasExpiry: true, Expires: now.Add(83 * time.Hour)}, "ok", "expires in 3 days (2026-01-04)"},
		{"note when no expiry", client.Health{Valid: true, ExpiryNote: "expiry not readable"}, "ok", "expiry not readable"},
		{"plain valid", client.Health{Valid: true}, "ok", "valid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, n := keyStatus(c.h, now)
			if w != c.word || n != c.note {
				t.Fatalf("got %q %q, want %q %q", w, n, c.word, c.note)
			}
		})
	}
}

// ─── edit ────────────────────────────────────────────────────────────────────

// Verifies: REQ-ACC-018
func TestACCEditRenames(t *testing.T) {
	seed3(t)
	api := startAPI(t, nil, nil)
	stdout, _, err := runAcc(t, "", "edit", "work@example.com", "--name=  job\x01 ")
	if err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t)
	if c.Accounts[1].Name != "job" || c.Accounts[1].APIKey != keyB || !strings.Contains(stdout, "✓ Account updated") {
		t.Fatalf("accounts = %+v out %q", c.Accounts, stdout)
	}
	if api.hitCount() != 0 {
		t.Fatal("a rename alone needs no API request")
	}
	// the alias by tailnet and by (new) name both address the account
	if _, _, err := runAcc(t, "", "edit", "job", "--name=job2"); err != nil {
		t.Fatal(err)
	}
	if loadCfg(t).Accounts[1].Name != "job2" {
		t.Fatal("rename by name failed")
	}
}

// Verifies: REQ-ACC-019
func TestACCEditReplacesKeyAfterValidation(t *testing.T) {
	dir := seed3(t)
	api := startAPI(t, map[string]int{keyN: 200}, nil)
	stdout, stderr, err := runAcc(t, keyN+"\n", "edit", "work", "--api-key-stdin", "--name=job")
	if err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t)
	if c.Accounts[1].APIKey != keyN || c.Accounts[1].Name != "job" {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	if api.tokens[0] != keyN || !strings.Contains(api.paths[0], "work@example.com") {
		t.Fatalf("validated %v against %v", api.tokens, api.paths)
	}
	if !strings.Contains(stdout, "Validating API key...") {
		t.Fatalf("stdout %q", stdout)
	}
	noSecret(t, []string{keyN, keyB}, stdout, stderr, string(cfgFile(t, dir)))

	// key through the flag
	if _, _, err := runAcc(t, "", "edit", "job", "--api-key="+keyN); err != nil {
		t.Fatal(err)
	}
}

// Verifies: REQ-ACC-019
func TestACCEditRejectedKeyLeavesConfigUntouched(t *testing.T) {
	dir := seed3(t)
	startAPI(t, map[string]int{keyN: 403}, nil)
	before := cfgFile(t, dir)
	stdout, stderr, err := runAcc(t, keyN+"\n", "edit", "work", "--api-key-stdin", "--name=job")
	if err == nil || !strings.Contains(err.Error(), "failed to validate API key") {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(before, cfgFile(t, dir)) {
		t.Fatal("config changed although the key was rejected")
	}
	noSecret(t, []string{keyN, keyB}, stdout, stderr, errText(err))
}

// Verifies: REQ-ACC-020
func TestACCEditErrors(t *testing.T) {
	dir := seed3(t)
	api := startAPI(t, map[string]int{keyN: 200}, nil)
	before := cfgFile(t, dir)
	cases := []struct {
		name    string
		stdin   string
		args    []string
		wantErr string
	}{
		{"nothing to change", "", []string{"edit", "work"}, "nothing to change"},
		{"name only blanks", "", []string{"edit", "work", "--name= \x01 "}, "nothing to change"},
		{"empty stdin key", "", []string{"edit", "work", "--api-key-stdin"}, "invalid API key"},
		{"empty stdin key with a name", "\n", []string{"edit", "work", "--api-key-stdin", "--name=job"}, "invalid API key"},
		{"short key", "", []string{"edit", "work", "--api-key=tskey-short"}, "invalid API key"},
		{"key with control chars only", "", []string{"edit", "work", "--api-key=\x01"}, "invalid API key"},
		{"bad name", "", []string{"edit", "work", "--name=a;b"}, "invalid name"},
		{"name too long", "", []string{"edit", "work", "--name=" + strings.Repeat("x", 65)}, "invalid name"},
		{"unknown account", "", []string{"edit", "nope", "--name=job"}, "account 'nope' not found"},
		{"unknown account with key", keyN + "\n", []string{"edit", "nope", "--api-key-stdin"}, "account 'nope' not found"},
		{"duplicate name", "", []string{"edit", "work", "--name=home"}, "already exists"},
		{"duplicate tailnet as name", "", []string{"edit", "work", "--name=home.example.com"}, "already exists"},
		{"no argument", "", []string{"edit"}, "accepts 1 arg"},
		{"two arguments", "", []string{"edit", "a", "b"}, "accepts 1 arg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := runAcc(t, c.stdin, c.args...)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
			if !bytes.Equal(before, cfgFile(t, dir)) {
				t.Fatal("config changed")
			}
			noSecret(t, []string{keyN, keyA, keyB, keyC}, stdout, stderr, errText(err))
		})
	}
	if api.hitCount() != 0 {
		t.Fatalf("API contacted %d times for failing edits", api.hitCount())
	}

	// corrupt configuration
	bad := corruptConfig(t, dir)
	_, _, err := runAcc(t, "", "edit", "work", "--name=job")
	if err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("corrupt: %v", err)
	}
	if !bytes.Equal(bad, cfgFile(t, dir)) {
		t.Fatal("corrupt config must stay")
	}
}

// ─── use ─────────────────────────────────────────────────────────────────────

// fakeTailscale installs an executable named tailscale in a private PATH that
// logs its arguments and exits with code. It returns the log path.
func fakeTailscale(t *testing.T, code int, msg string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\necho %q\nexit %d\n", log, msg, code)
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return log
}

// Verifies: REQ-ACC-022
func TestACCUseSetsActiveAndSwitchesTailscale(t *testing.T) {
	seed3(t)
	log := fakeTailscale(t, 0, "Switched to work")
	stdout, stderr, err := runAcc(t, "", "use", "work")
	if err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t)
	if c.Accounts[0].Active || !c.Accounts[1].Active || c.Accounts[2].Active {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	for _, want := range []string{"✓ Set 'work' as active account", "✓ This computer now uses work@example.com"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("stderr %q", stderr)
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "switch work@example.com" {
		t.Fatalf("tailscale called with %q", b)
	}
	// alias and selection by tailnet
	if _, _, err := runAcc(t, "", "set-active", "home.example.com"); err != nil {
		t.Fatal(err)
	}
	if !loadCfg(t).Accounts[0].Active {
		t.Fatal("alias set-active failed")
	}
}

// Verifies: REQ-ACC-022
func TestACCUseTailscaleFailureIsOnlyAWarning(t *testing.T) {
	seed3(t)
	fakeTailscale(t, 1, "  tailnet not known  ")
	stdout, stderr, err := runAcc(t, "", "use", "work")
	if err != nil {
		t.Fatalf("a tailscale failure must not fail the command: %v", err)
	}
	if !strings.Contains(stderr, "Warning: tailscale switch failed: tailnet not known\n") {
		t.Fatalf("stderr %q", stderr)
	}
	if strings.Contains(stdout, "This computer now uses") || !strings.Contains(stdout, "✓ Set 'work' as active account") {
		t.Fatalf("stdout %q", stdout)
	}
	if !loadCfg(t).Accounts[1].Active {
		t.Fatal("ts-cli's own active account must still change")
	}
}

// Verifies: REQ-ACC-022
func TestACCUseWithoutTailscaleBinary(t *testing.T) {
	seed3(t)
	t.Setenv("PATH", t.TempDir()) // empty: no tailscale
	stdout, stderr, err := runAcc(t, "", "use", "work")
	if err != nil || stderr != "" || strings.Contains(stdout, "This computer") {
		t.Fatalf("out %q err %q %v", stdout, stderr, err)
	}
}

// Verifies: REQ-ACC-021
func TestACCUseErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := isolate(t)
	if _, _, err := runAcc(t, "", "use", "x"); err == nil || !strings.Contains(err.Error(), "no accounts configured") {
		t.Fatalf("empty: %v", err)
	}
	seed3(t)
	before := cfgFile(t, os.Getenv(util.ConfigDirEnv))
	if _, _, err := runAcc(t, "", "use", "nope"); err == nil || !strings.Contains(err.Error(), "account 'nope' not found") {
		t.Fatalf("unknown: %v", err)
	}
	if _, _, err := runAcc(t, "", "use", ""); err == nil {
		t.Fatal("empty name must not match")
	}
	if _, _, err := runAcc(t, "", "use"); err == nil {
		t.Fatal("argument required")
	}
	if !bytes.Equal(before, cfgFile(t, os.Getenv(util.ConfigDirEnv))) {
		t.Fatal("failed use must not change the config")
	}
	_ = dir
}

// ─── hide / show / remove ────────────────────────────────────────────────────

// Verifies: REQ-ACC-023
func TestACCHideAndShow(t *testing.T) {
	seed3(t)
	stdout, _, err := runAcc(t, "", "hide", "work@example.com")
	if err != nil || !strings.Contains(stdout, "✓ Account 'work@example.com' is now hidden") {
		t.Fatalf("hide: %q %v", stdout, err)
	}
	c := loadCfg(t)
	if !c.Accounts[1].Hidden || c.Accounts[0].Hidden || c.Accounts[2].Hidden || !c.Accounts[0].Active {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	stdout, _, err = runAcc(t, "", "show", "work")
	if err != nil || !strings.Contains(stdout, "✓ Account 'work' is now shown") {
		t.Fatalf("show: %q %v", stdout, err)
	}
	if loadCfg(t).Accounts[1].Hidden {
		t.Fatal("show must clear the flag")
	}
	for _, verb := range []string{"hide", "show"} {
		if _, _, err := runAcc(t, "", verb, "nope"); err == nil || !strings.Contains(err.Error(), "account 'nope' not found") {
			t.Fatalf("%s unknown: %v", verb, err)
		}
		if _, _, err := runAcc(t, "", verb, ""); err == nil {
			t.Fatalf("%s empty name must fail", verb)
		}
		if _, _, err := runAcc(t, "", verb); err == nil {
			t.Fatalf("%s needs an argument", verb)
		}
	}
}

// Verifies: REQ-ACC-024
func TestACCRemove(t *testing.T) {
	dir := seed3(t)
	// removing an inactive account keeps the active one and prints no promotion
	stdout, _, err := runAcc(t, "", "rm", "third")
	if err != nil || !strings.Contains(stdout, "✓ Removed account: third") || strings.Contains(stdout, "new active") {
		t.Fatalf("out %q err %v", stdout, err)
	}
	c := loadCfg(t)
	if len(c.Accounts) != 2 || !c.Accounts[0].Active {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	// removing the active account promotes the next one (by tailnet, alias delete)
	stdout, _, err = runAcc(t, "", "delete", "home.example.com")
	if err != nil || !strings.Contains(stdout, "✓ Removed account: home") || !strings.Contains(stdout, "Set 'work' as the new active account") {
		t.Fatalf("out %q err %v", stdout, err)
	}
	c = loadCfg(t)
	if len(c.Accounts) != 1 || !c.Accounts[0].Active || c.Accounts[0].Name != "work" {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	// removing the last account leaves an empty list and no promotion message
	stdout, _, err = runAcc(t, "", "remove", "work")
	if err != nil || strings.Contains(stdout, "new active") {
		t.Fatalf("out %q err %v", stdout, err)
	}
	if len(loadCfg(t).Accounts) != 0 {
		t.Fatal("expected no accounts")
	}
	noSecret(t, []string{keyA, keyB, keyC}, string(cfgFile(t, dir)))
	// nothing left
	if _, _, err := runAcc(t, "", "remove", "work"); err == nil || !strings.Contains(err.Error(), "no accounts configured") {
		t.Fatalf("empty: %v", err)
	}
}

// Verifies: REQ-ACC-024
func TestACCRemoveErrors(t *testing.T) {
	dir := seed3(t)
	before := cfgFile(t, dir)
	for _, arg := range []string{"nope", "", "HOME"} {
		if _, _, err := runAcc(t, "", "remove", arg); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("remove %q: %v", arg, err)
		}
	}
	if _, _, err := runAcc(t, "", "remove"); err == nil {
		t.Fatal("argument required")
	}
	if !bytes.Equal(before, cfgFile(t, dir)) {
		t.Fatal("failed removals must not change the config")
	}
}

// Verifies: REQ-ACC-025
func TestACCCommandWiring(t *testing.T) {
	cmd := NewAccountCommand()
	want := map[string][]string{
		"add": nil, "list": {"ls"}, "edit": nil, "use": {"set-active"}, "hide": nil, "show": nil, "remove": {"rm", "delete"},
	}
	if len(cmd.Commands()) != len(want) {
		t.Fatalf("%d subcommands", len(cmd.Commands()))
	}
	for name, aliases := range want {
		sub, _, err := cmd.Find([]string{name})
		if err != nil || sub.Name() != name {
			t.Fatalf("%s: %v", name, err)
		}
		for _, a := range aliases {
			if s, _, err := cmd.Find([]string{a}); err != nil || s != sub {
				t.Fatalf("alias %s: %v", a, err)
			}
		}
	}
	// the API-key flags exist where a key is accepted, with a warning about the flag
	for _, name := range []string{"add", "edit"} {
		sub, _, _ := cmd.Find([]string{name})
		f := sub.Flags().Lookup("api-key")
		if f == nil || !strings.Contains(f.Usage, "prefer --api-key-stdin") || sub.Flags().Lookup("api-key-stdin") == nil {
			t.Fatalf("%s: key flags missing or unsafe", name)
		}
	}
	// unknown subcommands and flags are errors
	if _, _, err := runAcc(t, "", "frobnicate"); err == nil {
		t.Fatal("unknown subcommand must fail")
	}
}

// Verifies: REQ-ACC-026
func TestACCAddFindsTheAccountByTailnetOnly(t *testing.T) {
	isolate(t)
	startAPI(t, map[string]int{keyA: 200, keyB: 200, keyN: 200}, nil)
	// An account whose display name equals the tailnet being added: that is
	// another account, so adding the tailnet must not overwrite it.
	err := config.Update(func(c *config.Config) error {
		c.AddAccount("shared.example.com", keyA, "other.example.com", true)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = runAcc(t, "", "add", "--tailnet=shared.example.com", "--name=shared", "--api-key="+keyN)
	if err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t)
	if len(c.Accounts) != 2 || c.Accounts[0].Tailnet != "other.example.com" || c.Accounts[0].APIKey != keyA || c.Accounts[1].Tailnet != "shared.example.com" {
		t.Fatalf("the existing account must be untouched and a new one added: %+v", c.Accounts)
	}
}

// Verifies: REQ-ACC-027
func TestACCAddRejectsADisplayNameOfAnotherAccount(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"new tailnet, name of another account", []string{"--tailnet=new.example.com", "--name=work"}, true},
		{"new tailnet, name equal to another account's tailnet", []string{"--tailnet=new.example.com", "--name=third.example.com"}, true},
		{"new tailnet, default name equals another account's name", []string{"--tailnet=home"}, false}, // "home" has no dot: fails tailnet validation instead
		{"existing tailnet renamed to another account's name", []string{"--tailnet=home.example.com", "--name=work"}, true},
		{"existing tailnet keeps its own name", []string{"--tailnet=home.example.com", "--name=home"}, false},
		{"existing tailnet, no name: nothing to clash", []string{"--tailnet=home.example.com"}, false},
		{"new tailnet, unused name", []string{"--tailnet=new.example.com", "--name=fresh"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seed3(t)
			startAPI(t, map[string]int{keyA: 200, keyN: 200}, nil)
			before := loadCfg(t)
			stdout, stderr, err := runAcc(t, "", append([]string{"add", "--api-key=" + keyN}, c.args...)...)
			if c.wantErr {
				if err == nil || !strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "failed to store") {
					t.Fatalf("err = %v", err)
				}
				after := loadCfg(t)
				if len(after.Accounts) != len(before.Accounts) {
					t.Fatalf("nothing may be stored: %+v", after.Accounts)
				}
				for i := range before.Accounts {
					if after.Accounts[i] != before.Accounts[i] {
						t.Fatalf("account %d changed: %+v", i, after.Accounts[i])
					}
				}
				noSecret(t, []string{keyN}, stdout, stderr, err.Error())
			}
		})
	}

	// the default name (the tailnet) clashing with another account's display name
	seed3(t)
	startAPI(t, map[string]int{keyN: 200}, nil)
	err := config.Update(func(c *config.Config) error { return c.UpdateAccount("third", "fresh.example.com", "") })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runAcc(t, "", "add", "--tailnet=fresh.example.com", "--api-key="+keyN); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("default name clash: %v", err)
	}
}

package tui

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// exitError runs a shell that exits with the given code and returns the error.
func exitError(t *testing.T, code int) error {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	err := exec.Command("sh", "-c", "exit "+itoa(code)).Run()
	if err == nil {
		t.Fatalf("expected non-nil error for exit %d", code)
	}
	return err
}

func itoa(i int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i/100%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10)))
}

func TestClassifySSHError(t *testing.T) {
	if got := classifySSHError(nil); got != sshFailureNone {
		t.Fatalf("nil -> %v", got)
	}
	if got := classifySSHError(errors.New("boom")); got != sshFailureExec {
		t.Fatalf("plain error -> %v, want sshFailureExec", got)
	}
	if got := classifySSHError(exitError(t, 5)); got != sshFailureBadPassword {
		t.Fatalf("exit 5 -> %v, want sshFailureBadPassword", got)
	}
	if got := classifySSHError(exitError(t, 255)); got != sshFailureConnection {
		t.Fatalf("exit 255 -> %v, want sshFailureConnection", got)
	}
	// An ordinary non-zero exit of the remote shell must NOT be treated as a
	// credential failure (this used to delete saved passwords).
	for _, code := range []int{1, 2, 127, 130} {
		if got := classifySSHError(exitError(t, code)); got != sshFailureRemoteExit {
			t.Fatalf("exit %d -> %v, want sshFailureRemoteExit", code, got)
		}
	}
}

func TestBuildSSHTarget(t *testing.T) {
	if got, err := buildSSHTarget("", "100.64.0.1"); err != nil || got != "100.64.0.1" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := buildSSHTarget("admin", "fd7a:115c:a1e0::1"); err != nil || got != "admin@fd7a:115c:a1e0::1" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := buildSSHTarget("admin", "not-an-ip; rm -rf /"); err == nil {
		t.Fatal("non-IP address accepted")
	}
	if _, err := buildSSHTarget("-oProxyCommand=evil", "100.64.0.1"); err == nil {
		t.Fatal("option-injection username accepted")
	}
	if _, err := buildSSHTarget("a b", "100.64.0.1"); err == nil {
		t.Fatal("username with space accepted")
	}
}

func TestNewSSHCommandNeverPutsPasswordInArgv(t *testing.T) {
	cmd, cleanup, err := newSSHCommand("user@100.64.0.1", "s3cret-pass", false, "uptime")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "s3cret-pass") {
		t.Fatalf("password leaked into argv: %q", joined)
	}
	for _, e := range cmd.Env {
		if strings.Contains(e, "s3cret-pass") {
			t.Fatalf("password leaked into env: %q", e)
		}
	}
	if !strings.Contains(joined, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("missing host key option: %q", joined)
	}
	if !strings.HasSuffix(joined, "user@100.64.0.1 uptime") {
		t.Fatalf("target/remote args misplaced: %q", joined)
	}

	if _, err := exec.LookPath("sshpass"); err == nil {
		if !strings.Contains(joined, "-d 3") || len(cmd.ExtraFiles) != 1 {
			t.Fatalf("expected sshpass -d 3 with one extra fd, got %q (%d files)", joined, len(cmd.ExtraFiles))
		}
	} else if !strings.HasPrefix(cmd.Args[0], "ssh") {
		t.Fatalf("without sshpass expected plain ssh, got %q", joined)
	}
}

func TestNewSSHCommandBatchModeOnlyWithoutPassword(t *testing.T) {
	cmd, cleanup, err := newSSHCommand("100.64.0.1", "", true, "pwd")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !strings.Contains(strings.Join(cmd.Args, " "), "BatchMode=yes") {
		t.Fatalf("expected BatchMode for non-interactive passwordless run: %v", cmd.Args)
	}

	cmd2, cleanup2, _ := newSSHCommand("100.64.0.1", "", false)
	defer cleanup2()
	if strings.Contains(strings.Join(cmd2.Args, " "), "BatchMode") {
		t.Fatalf("interactive run must not force BatchMode: %v", cmd2.Args)
	}
}

func TestParseLoginNamePicksSelfUser(t *testing.T) {
	status := []byte(`{
	  "Self": {"ID": "n1", "UserID": 222},
	  "Peer": {"k": {"UserID": 111}},
	  "User": {
	    "111": {"ID": 111, "LoginName": "shared-owner@other.com"},
	    "222": {"ID": 222, "LoginName": "me@example.com"}
	  }
	}`)
	if got := parseLoginName(status); got != "me@example.com" {
		t.Fatalf("parseLoginName = %q, want me@example.com", got)
	}
	if got := parseLoginName([]byte(`{"Self":{"UserID":1},"User":{}}`)); got != "" {
		t.Fatalf("expected empty for unknown user, got %q", got)
	}
	if got := parseLoginName([]byte(`not json`)); got != "" {
		t.Fatalf("expected empty for invalid json, got %q", got)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/usr/local/bin/ts-cli"); got != "'/usr/local/bin/ts-cli'" {
		t.Fatalf("got %q", got)
	}
	if got := shellQuote("it's"); got != `'it'"'"'s'` {
		t.Fatalf("got %q", got)
	}
}

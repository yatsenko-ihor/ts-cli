package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/util"
)

func flowModel(t *testing.T, creds map[string]machineCredential, defaultUser string) model {
	t.Helper()
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	devices := []client.Device{{
		ID: "dev-1", Name: "laptop.example.com", Hostname: "laptop",
		Addresses: []string{"100.64.0.1"}, LastSeen: time.Now(),
	}}
	if creds == nil {
		creds = map[string]machineCredential{}
	}
	return model{
		list: deviceList{devices: devices, filteredDevices: devices},
		ssh:  ssh{username: defaultUser, machineCreds: creds},
	}
}

func asModel(t *testing.T, m interface{}) model {
	t.Helper()
	mm, ok := m.(model)
	if !ok {
		t.Fatalf("expected model, got %T", m)
	}
	return mm
}

func TestMachineKeyPrefersIDThenHostname(t *testing.T) {
	if got := machineKey(client.Device{ID: "42", Hostname: "h", Name: "n"}); got != "42" {
		t.Fatalf("got %q", got)
	}
	if got := machineKey(client.Device{Hostname: "h", Name: "n"}); got != "h" {
		t.Fatalf("got %q", got)
	}
}

func TestMigrateCredentialKeysMovesNameToID(t *testing.T) {
	creds := map[string]machineCredential{
		"laptop.example.com": {username: "root", passwordEncrypted: "enc"},
		"other":              {username: "x"},
	}
	devices := []client.Device{{ID: "dev-1", Name: "laptop.example.com", Hostname: "laptop"}}

	if !migrateCredentialKeys(creds, devices) {
		t.Fatal("expected migration to report a change")
	}
	if _, stale := creds["laptop.example.com"]; stale {
		t.Fatal("legacy key should be removed")
	}
	got := creds["dev-1"]
	if got.username != "root" || got.passwordEncrypted != "enc" || got.name != "laptop.example.com" {
		t.Fatalf("migrated cred wrong: %+v", got)
	}
	if _, ok := creds["other"]; !ok {
		t.Fatal("unrelated credentials must be kept")
	}
	if migrateCredentialKeys(creds, devices) {
		t.Fatal("second run must be a no-op")
	}
}

func TestBeginSSHPromptsUsernameWhenNothingSaved(t *testing.T) {
	m := flowModel(t, nil, "")
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m2 := asModel(t, updated)
	if m2.input.mode != inputUsername {
		t.Fatalf("expected username prompt, got mode %v", m2.input.mode)
	}
	if m2.ssh.pending.machineID != "dev-1" || m2.ssh.pending.action != sshActionSession {
		t.Fatalf("pending not initialised: %+v", m2.ssh.pending)
	}
}

func TestBeginSSHWithDefaultUserTriesKeyAuthFirst(t *testing.T) {
	m := flowModel(t, nil, "admin")
	updated, cmd := m.beginSSH(0, sshJob{action: sshActionSession})
	m2 := asModel(t, updated)
	if m2.input.mode != inputNone {
		t.Fatalf("must not prompt for a password before trying key auth, got mode %v", m2.input.mode)
	}
	if cmd == nil {
		t.Fatal("expected an ssh command to run")
	}
	if m2.ssh.pending.username != "admin" || m2.ssh.pending.password != "" {
		t.Fatalf("pending wrong: %+v", m2.ssh.pending)
	}
}

func TestConnectionFailureAsksForPasswordThenSavesOnSuccess(t *testing.T) {
	m := flowModel(t, nil, "admin")
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m = asModel(t, updated)

	// Key-based attempt failed (ssh exit 255).
	updated, _ = m.finishSSHAttempt(sshFailureConnection, errors.New("exit status 255"))
	m = asModel(t, updated)
	if !sshpassAvailable() {
		// Without sshpass the TUI cannot drive a password: expect retry menu.
		if !m.ssh.retryMode {
			t.Fatal("expected retry menu when sshpass is unavailable")
		}
		t.Skip("sshpass not installed; password prompt path not exercised")
	}
	if m.input.mode != inputPassword {
		t.Fatalf("expected password prompt after key auth failure, got %v", m.input.mode)
	}
	if m.notify.sshError == nil {
		t.Fatal("prompt should explain why it is asking")
	}

	// User types a password: nothing may be persisted yet.
	m.ssh.pending.password = "hunter2"
	m.ssh.pending.passwordTried = true
	if _, ok := m.ssh.machineCreds["dev-1"]; ok {
		t.Fatal("credentials must not be stored before success")
	}

	// Connection succeeds: username + encrypted password saved, default user kept.
	updated, cmd := m.finishSSHAttempt(sshFailureNone, nil)
	m = asModel(t, updated)
	cred, ok := m.ssh.machineCreds["dev-1"]
	if !ok || cred.username != "admin" || cred.passwordEncrypted == "" || cred.name != "laptop.example.com" {
		t.Fatalf("credentials not saved on success: %+v", cred)
	}
	if plain, err := util.DecryptPassword(cred.passwordEncrypted); err != nil || plain != "hunter2" {
		t.Fatalf("stored password does not round-trip: %q %v", plain, err)
	}
	if cmd == nil {
		t.Fatal("expected a persistence command")
	}
	if m.ssh.pending.action != sshActionNone {
		t.Fatal("pending attempt should be cleared after success")
	}
}

func TestSavedPasswordIsUsedWithoutPrompting(t *testing.T) {
	m := flowModel(t, nil, "")
	enc, err := util.EncryptPassword("saved-pw")
	if err != nil {
		t.Fatal(err)
	}
	m.ssh.machineCreds["dev-1"] = machineCredential{name: "laptop", username: "root", passwordEncrypted: enc}

	updated, cmd := m.beginSSH(0, sshJob{action: sshActionSession})
	m2 := asModel(t, updated)
	if m2.input.mode != inputNone || cmd == nil {
		t.Fatalf("saved credentials must connect directly; mode=%v cmd=%v", m2.input.mode, cmd != nil)
	}
	if m2.ssh.pending.username != "root" || !m2.ssh.pending.usedSavedPass {
		t.Fatalf("pending wrong: %+v", m2.ssh.pending)
	}
}

func TestRejectedSavedPasswordIsForgottenAndReprompted(t *testing.T) {
	m := flowModel(t, nil, "")
	enc, _ := util.EncryptPassword("old")
	m.ssh.machineCreds["dev-1"] = machineCredential{name: "laptop", username: "root", passwordEncrypted: enc}
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m = asModel(t, updated)

	updated, _ = m.finishSSHAttempt(sshFailureBadPassword, errors.New("exit status 5"))
	m = asModel(t, updated)

	if m.input.mode != inputPassword {
		t.Fatalf("expected password prompt, got %v", m.input.mode)
	}
	cred := m.ssh.machineCreds["dev-1"]
	if cred.passwordEncrypted != "" {
		t.Fatal("rejected saved password must be forgotten")
	}
	if cred.username != "root" {
		t.Fatal("username must be kept when only the password was wrong")
	}
}

func TestRemoteExitKeepsCredentialsAndClearsPending(t *testing.T) {
	m := flowModel(t, nil, "")
	enc, _ := util.EncryptPassword("pw")
	m.ssh.machineCreds["dev-1"] = machineCredential{name: "laptop", username: "root", passwordEncrypted: enc}
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m = asModel(t, updated)

	// Remote shell exited 1: this used to delete the saved password.
	updated, _ = m.finishSSHAttempt(sshFailureRemoteExit, errors.New("exit status 1"))
	m = asModel(t, updated)
	if m.ssh.machineCreds["dev-1"].passwordEncrypted == "" {
		t.Fatal("non-zero remote exit must not delete credentials")
	}
	if m.ssh.retryMode || m.input.mode != inputNone {
		t.Fatal("non-zero remote exit must not open any prompt")
	}
	if m.ssh.pending.action != sshActionNone {
		t.Fatal("pending should be cleared")
	}
}

func TestCancelClearsPendingAndPassword(t *testing.T) {
	m := flowModel(t, nil, "admin")
	m.ssh.pending = pendingSSH{action: sshActionSession, password: "typed"}
	m.input = textInput{mode: inputPassword}
	m = m.cancelSSHAttempt()
	if m.ssh.pending.password != "" || m.ssh.pending.action != sshActionNone || m.input.mode != inputNone {
		t.Fatalf("cancel did not reset state: %+v", m.ssh.pending)
	}
}

func TestProbeSuccessSavesPasswordBeforeSessionStarts(t *testing.T) {
	m := flowModel(t, nil, "pi")
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m = asModel(t, updated)
	m.ssh.pending.password = "typed-pw"
	m.ssh.pending.passwordTried = true

	updated, cmd := m.handleProbe(probeMsg{failure: sshFailureNone})
	m = asModel(t, updated)

	cred := m.ssh.machineCreds["dev-1"]
	if cred.username != "pi" || cred.passwordEncrypted == "" {
		t.Fatalf("password must be saved as soon as the probe succeeds: %+v", cred)
	}
	if plain, err := util.DecryptPassword(cred.passwordEncrypted); err != nil || plain != "typed-pw" {
		t.Fatalf("stored password does not round-trip: %q %v", plain, err)
	}
	if !m.ssh.pending.verified || cmd == nil {
		t.Fatal("session must start with the pending attempt marked verified")
	}
}

func TestSessionEndingWith255AfterVerifiedProbeKeepsCredentials(t *testing.T) {
	m := flowModel(t, nil, "pi")
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m = asModel(t, updated)
	m.ssh.pending.password = "typed-pw"
	updated, _ = m.handleProbe(probeMsg{failure: sshFailureNone})
	m = asModel(t, updated)

	// The established session is dropped: ssh exits 255.
	updated, _ = m.finishSSHAttempt(sshFailureConnection, errors.New("exit status 255"))
	m = asModel(t, updated)

	if m.ssh.retryMode || m.input.mode != inputNone {
		t.Fatal("a dropped session after a verified login must not open the retry menu or a prompt")
	}
	if m.ssh.machineCreds["dev-1"].passwordEncrypted == "" {
		t.Fatal("saved password must survive a dropped session")
	}
	if m.ssh.pending.action != sshActionNone {
		t.Fatal("pending should be cleared")
	}
}

func TestProbeFailureReprompts(t *testing.T) {
	m := flowModel(t, nil, "pi")
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m = asModel(t, updated)
	m.ssh.pending.password = "wrong"

	updated, _ = m.handleProbe(probeMsg{failure: sshFailureBadPassword, err: errors.New("exit status 5")})
	m = asModel(t, updated)
	if m.input.mode != inputPassword {
		t.Fatalf("wrong password must re-open the password prompt, got %v", m.input.mode)
	}
	if _, ok := m.ssh.machineCreds["dev-1"]; ok {
		t.Fatal("nothing may be saved when the probe fails")
	}
}

func TestSuccessfulConnectionRemembersLastMachine(t *testing.T) {
	m := flowModel(t, nil, "pi")
	updated, _ := m.beginSSH(0, sshJob{action: sshActionSession})
	m = asModel(t, updated)
	updated, _ = m.finishSSHAttempt(sshFailureNone, nil)
	m = asModel(t, updated)
	if m.lastMachine != "dev-1" {
		t.Fatalf("lastMachine = %q", m.lastMachine)
	}
}

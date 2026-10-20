# Requirements: SSH credential flow (REQ-FLW) - tui/sshflow.go

Scope: the state machine that decides when credentials are asked for, saved or forgotten. Intent: key-based login is
tried first; a password is asked for only after that fails; a typed password is verified (probe) before it is saved;
a rejected saved password is forgotten; nothing is persisted before success; cancelling clears every secret; an
ordinary non-zero exit of the remote command never deletes credentials.

## Helpers

- **REQ-FLW-001** machineKey shall return the device ID, or the Hostname when the ID is empty (empty string when both are empty).
- **REQ-FLW-002** displayName shall return the device Name, or the Hostname when the Name is empty.
- **REQ-FLW-003** sshpassAvailable shall report true exactly when an `sshpass` executable is on PATH.
- **REQ-FLW-004** sshJob.label shall return the script name for a script action with a script, the command for a command action, and an empty string otherwise (script action without script, session, none).

## Starting an attempt

- **REQ-FLW-005** beginSSH shall do nothing (same model, no command, existing error kept) for a device index below 0 or at or above the number of listed devices.
- **REQ-FLW-006** beginSSH shall replace the pending attempt by a fresh one holding the action, device index, machine key, display name, command, script and arguments of the request, discarding any password, flags and username of a previous attempt, and shall clear the previous SSH error.
- **REQ-FLW-007** beginSSH shall, when the device belongs to another tailnet than the one the daemon is logged into, return the account-switch command and not continue the flow (no prompt, no process); the pending attempt shall be kept for after the switch.
- **REQ-FLW-008** needsAccountSwitch shall be false when the device tailnet or the daemon login is unknown or when they denote the same tailnet (case-insensitive, `user@` prefix accepted), and true otherwise.
- **REQ-FLW-009** continueSSH shall do nothing when the pending attempt has no action.
- **REQ-FLW-010** continueSSH shall resolve the username in this order: the username already in the attempt, the username saved for the machine, the default username; when none is known it shall open the username prompt and start nothing.
- **REQ-FLW-011** continueSSH shall, when the attempt asks for the password next and a username is known, open the password prompt, clear that flag and start nothing; when the username is unknown the username is asked first and the flag is kept.

## Running an attempt

- **REQ-FLW-012** runPendingAttempt shall, for a device index outside the device list, clear the pending attempt, report "device list changed; please select the device again" and start nothing.
- **REQ-FLW-013** runPendingAttempt shall use the password typed for the attempt in preference to the saved one, use the saved password when none was typed, and recompute `usedSavedPass` on every call (true only when the saved password is used).
- **REQ-FLW-014** For an interactive session, runPendingAttempt shall run the credential probe first when a password was typed, is not yet verified and sshpass is available; in every other case it shall start the interactive session directly. A password shall reach the process only through sshpass (never argv).
- **REQ-FLW-015** For a command or script action, runPendingAttempt shall start the streamed job with the resolved password, register it as the single machine run (running, output visible, label of the job) and return the commands that wait for its output and tick.
- **REQ-FLW-016** When the streamed job cannot be started, runPendingAttempt shall report "could not start ssh", clear the pending attempt, start no run and save nothing.
- **REQ-FLW-017** runPendingAttempt shall do nothing for an attempt whose action is none or unknown.

## Lookups

- **REQ-FLW-018** savedPassword shall return the decrypted password of a machine, and an empty string when the machine is unknown, has no stored password, the stored value is not valid base64, or cannot be decrypted (tampered), and when the credential map is nil.
- **REQ-FLW-019** passwordForDevice shall return the password typed for the attempt in progress when it belongs to the same machine, otherwise the saved password, otherwise an empty string.
- **REQ-FLW-020** usernameForDevice shall return the username saved for the machine, falling back to the default username when the machine has none.

## Outcome of an attempt

- **REQ-FLW-021** finishSSHAttempt shall do nothing when there is no pending attempt, for every failure class.
- **REQ-FLW-022** finishSSHAttempt shall treat sshFailureConnection of a verified interactive session as the end of an established session (sshFailureRemoteExit, message "connection closed: ..."), keeping the saved credentials; an unverified session, a verified non-session action or a different failure class shall not be reinterpreted.
- **REQ-FLW-023** On sshFailureNone or sshFailureRemoteExit finishSSHAttempt shall persist the credentials (REQ-FLW-029..031), clear the pending attempt, open no prompt or retry menu and return the persistence commands; only a remote exit of an interactive session shall report "remote session ended with an error".
- **REQ-FLW-024** On sshFailureBadPassword after a saved password was used, finishSSHAttempt shall delete the stored password (keeping name and username), persist that deletion, report that the saved password was rejected, clear the typed password, mark the password as tried and open the password prompt, keeping the attempt pending; when no stored credential exists it shall do the same without persisting anything.
- **REQ-FLW-025** On sshFailureBadPassword after a typed password, finishSSHAttempt shall persist nothing, leave stored credentials untouched, clear the typed password, mark the password as tried, report "password rejected for user@machine; try again" and open the password prompt.
- **REQ-FLW-026** On sshFailureConnection finishSSHAttempt shall persist nothing and keep the attempt pending, and shall ask for a password (with an explanatory message) only when no password was tried, none is present, no saved password was used and sshpass is available.
- **REQ-FLW-027** On sshFailureConnection in every other situation finishSSHAttempt shall open the retry menu with the cursor at 0 and report the error.
- **REQ-FLW-028** On sshFailureExec, or any unknown failure, finishSSHAttempt shall report "could not start ssh: ...", clear the pending attempt, open no prompt and save nothing.

## Persisting credentials

- **REQ-FLW-029** persistSuccessfulCredentials shall store the machine display name, the username and, only when one was typed, the password encrypted with util.EncryptPassword, both in memory and in config.json (creating the credential map when nil); the plaintext password shall appear nowhere in config.json. When encryption fails it shall not store any password, shall still store name and username, and shall report that the password could not be encrypted.
- **REQ-FLW-030** persistSuccessfulCredentials shall write the machine credential only when the name, the username or the password changed; a stored password shall stay untouched when none was typed.
- **REQ-FLW-031** persistSuccessfulCredentials shall set and store the default username only when none is set and the attempt has a username, and shall always remember the machine as the last one used.

## Probe and cancel

- **REQ-FLW-032** handleProbe shall ignore a probe result when the pending attempt is not an interactive session (cancelled meanwhile): no change, no command.
- **REQ-FLW-033** handleProbe shall, for a probe that succeeded (sshFailureNone or sshFailureRemoteExit), persist the credentials, mark the attempt verified and start the interactive session; for any other failure it shall hand over to finishSSHAttempt and save nothing; when the device index is no longer valid it shall cancel the attempt and save nothing.
- **REQ-FLW-034** cancelSSHAttempt shall clear the pending attempt including the typed password and all flags, the retry menu, the input prompt (mode, text, cursor, masking) and the SSH error, without modifying the receiver's own copy.

## Completion of streamed jobs

- **REQ-FLW-035** handleRunDone shall ignore the completion of a job that no machine run owns (nil or stale job): no state change, no history, no command.
- **REQ-FLW-036** For a single-machine run that connected (killed, sshFailureNone or sshFailureRemoteExit) handleRunDone shall settle the machine run, record the run in history, clear the SSH error and finish the attempt as a success (credentials persisted, pending cleared).
- **REQ-FLW-037** For a single-machine run that did not connect (connection failure, bad password, exec failure) handleRunDone shall settle the machine run, record no history, save no credentials and hand the failure to finishSSHAttempt (prompt or retry menu or error).
- **REQ-FLW-038** For a multi-machine run handleRunDone shall settle the machine and continue the multi-run logic without touching the interactive credential flow (pending attempt, prompts) and without saving or forgetting credentials on failure.

## Credential key migration

- **REQ-FLW-039** migrateCredentialKeys shall move credentials stored under a device's Name or Hostname to its stable key (ID, else Hostname), setting the display name, keep an already existing entry of the stable key and drop the legacy entry, never touch entries whose legacy name is empty or equal to the key, skip devices without any key, keep unrelated entries, and be idempotent.
- **REQ-FLW-040** migrateCredentialKeys shall fill in a missing display name of an entry of the stable key, keep an existing one, and return true exactly when it changed anything.

## MC/DC

Compound decisions of tui/sshflow.go and the independence pairs (test names are `TestFunc/subtest`, or `TestFunc (case)` when the function loops without subtests).

1. beginSSH: `index < 0 || index >= len(filteredDevices)`.
   - `index < 0`: `TestDO178CBeginSSHIndexBounds` (index -1) vs (index 0, last assertion of the same test).
   - `index >= len`: `TestDO178CBeginSSHIndexBounds` (index 1) vs (index 0).
2. needsAccountSwitch: `device.AccountTailnet == "" || tailscaleActiveAccount == ""` (`TestDO178CAccountSwitch`).
   - device tailnet empty: `device tailnet unknown` (false) vs `different tailnet` (true).
   - active account empty: `daemon login unknown` (false) vs `different tailnet` (true).
3. continueSSH: `cred, ok := machineCreds[id]; ok && cred.username != ""` (`TestDO178CContinueSSHUsernameResolution`).
   - ok: `machine credential beats default` vs `default when machine has no credential`.
   - cred.username != "": `machine credential beats default` vs `credential with empty username falls back to default`.
4. runPendingAttempt: `p.deviceIndex < 0 || p.deviceIndex >= len(...)`.
   - `< 0`: `TestDO178CRunPendingAttemptStaleDeviceIndex` (index -1) vs a valid index in `TestDO178CRunPendingAttemptSessionRouting`.
   - `>= len`: same test (index 1 with one device) vs the valid index.
5. runPendingAttempt: `p.password == ""` and `saved != ""` (nested ifs, outcome = saved password used), `TestDO178CRunPendingAttemptSessionRouting`.
   - `p.password == ""`: `saved password only: session without probe` vs `typed password beats saved one in the probe`.
   - `saved != ""`: `saved password only: session without probe` vs `no typed password, none saved: session`.
6. runPendingAttempt: `p.password != "" && !p.verified && sshpassAvailable()` (outcome true = probe), `TestDO178CRunPendingAttemptSessionRouting`. Base: `typed, unverified, sshpass: probe first`.
   - `p.password != ""`: base vs `no typed password, none saved: session`.
   - `!p.verified`: base vs `typed, verified: straight to session`.
   - `sshpassAvailable()`: base vs `typed, unverified, no sshpass: straight to session`.
7. finishSSHAttempt: `p.verified && p.action == sshActionSession && failure == sshFailureConnection`, `TestDO178CVerifiedSessionDroppedWith255`. Base: `verified + session + 255: remote exit, credentials kept`.
   - verified: base vs `not verified: a 255 is a login failure`.
   - action == session: base vs `verified but a command, not a session: a 255 is a connection failure`.
   - failure == connection: base vs `verified session but bad password stays a bad password`.
8. finishSSHAttempt: `failure == sshFailureRemoteExit && p.action == sshActionSession`, `TestDO178CFinishSuccessPersistsAndClears`. Base: `remote exit, session reports the error`.
   - failure == remote exit: base vs `none, session`.
   - action == session: base vs `remote exit, command stays quiet`.
9. finishSSHAttempt: `!p.passwordTried && p.password == "" && !p.usedSavedPass && sshpassAvailable()` (outcome true = password prompt), `TestDO178CFinishConnectionFailure`. Base: `first key failure with sshpass: ask for a password`.
   - `!passwordTried`: base vs `password already tried: retry menu`.
   - `password == ""`: base vs `a typed password is present: retry menu`.
   - `!usedSavedPass`: base vs `a saved password was used: retry menu`.
   - `sshpassAvailable()`: base vs `no sshpass: retry menu`.
10. finishSSHAttempt switch `case sshFailureNone, sshFailureRemoteExit` (each value alone selects the success branch): `TestDO178CFinishSuccessPersistsAndClears` (`none, session` and `remote exit, session reports the error`) vs `TestDO178CFinishConnectionFailure` (connection) / `TestDO178CFinishExecFailure`.
11. finishSSHAttempt bad password: `p.usedSavedPass` and `cred, ok := machineCreds[id]` (nested): `TestDO178CFinishBadSavedPasswordIsForgotten/saved password rejected...` (stored, forgotten) vs `.../used saved password but no stored credential` (ok false) vs `TestDO178CFinishBadTypedPasswordIsNotSaved` (usedSavedPass false).
12. persistSuccessfulCredentials: `m.ssh.username == "" && p.username != ""`, `TestDO178CPersistSuccessfulCredentials`. Base: `new machine: ...`.
    - `username == ""`: base vs `only the username changed` (default already "old").
    - `p.username != ""`: base vs `default username is only set when empty and the attempt had one`.
13. persistSuccessfulCredentials: `changed` is the OR of three independent comparisons (`cred.name != p.machineName`, `cred.username != p.username`, `p.password != ""` with successful encryption). Pairs: `nothing new learned: only the last-machine hint is written` (all false, no write) vs `only the name changed`, `only the username changed`, `only the password changed` (one true each, write).
14. handleProbe: `msg.failure != sshFailureNone && msg.failure != sshFailureRemoteExit` (outcome true = failure path), `TestDO178CHandleProbeOutcomes`.
    - `!= None`: `success 0` vs `failure 2` (connection).
    - `!= RemoteExit`: `success 3` vs `failure 2`.
15. handleProbe: `p.deviceIndex < 0 || p.deviceIndex >= len(...)`: `device list changed while probing` (index 5 and index -1) vs `success 0` (index 0).
16. handleRunDone: `res.killed || failure == None || failure == RemoteExit` (outcome true = connected), `TestDO178CHandleRunDoneSingleMachine`.
    - killed: `killed by the user, even though flagged as connection failure` vs `connection failure`.
    - None: `clean exit` vs `connection failure`.
    - RemoteExit: `remote exit non-zero` vs `connection failure`.
17. migrateCredentialKeys: `legacy == "" || legacy == key`, `TestDO178CMigrateCredentialKeys`.
    - `legacy == ""`: `empty legacy names are skipped` vs `legacy name moved to id with display name`.
    - `legacy == key`: `legacy equal to key is not touched` vs `no id: key is hostname, legacy name moves onto it`.
18. migrateCredentialKeys: `c, ok := creds[key]; ok && c.name == ""`.
    - ok: `missing display name is filled in` vs `unrelated credentials are kept`.
    - `c.name == ""`: `missing display name is filled in` vs `existing display name is kept`.
19. Single-condition decisions covered both ways: `needsAccountSwitch` sameTailnet, `p.action == sshActionNone` in continueSSH and finishSSHAttempt, `p.username == ""`, `p.askPasswordNext`, `err != nil` of startRemoteJob / decryption / encryption, `p.deviceIndex` in handleProbe, `p.action != sshActionSession` in handleProbe, `p.usedSavedPass`, `m.run.multi`, `mr == nil`, `key == ""` and `old, ok`/`exists` in migrateCredentialKeys, `j.script != nil` in label, `m.ssh.machineCreds == nil`, `cred.passwordEncrypted == ""` in savedPassword.

## Derived requirements / unreachable code

Derived behaviour (not asked for by a user need, each covered by a test above):

- displayName falls back to Hostname; machineKey falls back to Hostname (REQ-FLW-001/002).
- A probe that ends in sshFailureRemoteExit counts as successful login (REQ-FLW-033): `true` returning non-zero still proves the credentials worked.
- A killed run counts as connected, so stopping a run never costs the user a credential or a history entry (REQ-FLW-036).
- A failure to encrypt the typed password does not stop the other credentials from being saved (REQ-FLW-029).
- The saved password of a machine is only deleted when it was the one used and sshpass answered "wrong password" (exit 5); every other failure leaves it alone (REQ-FLW-024/025/026).
- The pending attempt of a bad password keeps `passwordTried`, so a second key failure goes to the retry menu instead of prompting forever (REQ-FLW-026/027).

Test seam added to non-test code (behaviour identical): `encryptPassword` (defaults to util.EncryptPassword) in tui/sshflow.go, used to provoke encryption failure, which util cannot be made to produce from this package.

Unreachable code: none. The `return m, nil` at the end of runPendingAttempt and the `default` branches are reached with an action of none/unknown and an unknown failure value (REQ-FLW-017, REQ-FLW-028); they are defensive but testable by calling the function directly. 100 % statement coverage and every condition true and false under gobco.

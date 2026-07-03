# ts-cli SSH Credential System

## Default Username

The global SSH username (`ssh_username` in config) serves as the default for new machines:

1. **First use**: No username saved → app prompts for username
2. **After first successful connection**: Username is saved globally as `ssh_username`
3. **Subsequent connections to new machines**: The saved `ssh_username` is used as default
4. **Per-machine override**: Each machine can have its own username stored in `machine_credentials`

The default username is **not** the OS username. It's the value saved after the first successful SSH connection. All machines share this default until overridden per-machine.

### Username Resolution Order

1. Per-machine credential username (if saved for this machine)
2. Global `ssh_username` (saved after first successful connection)
3. Prompt user to enter username

## Password Saving

Passwords are stored **per-machine** — each machine has its own unique encrypted password.

### How It Works

1. User selects a machine and presses `s` to SSH
2. If per-machine credentials exist (username + password) → connect directly using `sshpass`
3. If no credentials → prompt for password in-app
4. Password is **saved to disk immediately** after entry (before SSH session starts)
5. SSH session launches with saved credentials
6. If connection fails → saved credentials are deleted, retry menu appears
7. If connection succeeds → nothing extra needed (already saved)

### Why Save Before Connect

Credentials are persisted **before** the SSH session starts because:
- If the user closes the terminal during an SSH session, the app never gets a "success" callback
- Saving first ensures credentials survive terminal closure
- Bad credentials are cleaned up on failure anyway

### Storage Format

```json
{
  "ssh_username": "pi",
  "machine_credentials": {
    "box-057": {
      "username": "pi",
      "password": "<AES-256-GCM encrypted, base64 encoded>"
    },
    "server1": {
      "username": "admin",
      "password": "<AES-256-GCM encrypted, base64 encoded>"
    }
  }
}
```

### Encryption

- Algorithm: AES-256-GCM
- Key derivation: SHA-256 of `"ts-cli:{os_username}:{uid}:{hostname}"`
- The key is machine-bound — passwords can only be decrypted on the same machine by the same OS user
- Stored as base64 with random nonce prepended to ciphertext

### Connection Flow Summary

```
Saved per-machine creds → connect directly using that machine's username + password
No per-machine creds, global username exists → prompt for password only
Nothing saved → prompt for username, then password
Connection fails → delete saved creds, retry menu (change user/pass/both)
Connection succeeds → credentials already saved, nothing extra needed
```

### Managing Saved Credentials

- Press `o` → Options menu → "Machines with saved passwords"
- Shows list of machines with saved credentials
- Press `d` to delete credentials for a machine
- Press `Esc` to go back

## Icons Reference

| Icon | Context | Meaning |
|------|---------|---------|
| 🔑 | Device list | Device key will expire in the future |
| 🔒 | Device list | Device key has expired |
| 🔑 | Machine credentials list | Key-based auth (no password stored, username only) |

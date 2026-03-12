# Security Audit Report - ts-cli

Date: 2024
Auditor: Security Review
Version: 0.1.0 (pre-release)

## Executive Summary

This security audit examines the ts-cli application for potential security vulnerabilities. The application handles sensitive data (Tailscale API keys) and executes system commands, making security critical.

## Critical Areas Reviewed

### 1. API Key Storage & File Permissions ✅ GOOD

**Finding**: Config file properly secured

- Config directory: `~/.ts-cli/` with 0700 permissions (owner only)
- Config file: `config.json` with 0600 permissions (owner read/write only)
- API keys stored in plaintext but with proper file permissions

**Recommendation**: Current implementation is acceptable for a CLI tool. For enhanced security, consider:

- Key encryption using OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service)
- This would require additional dependencies and complexity

**Status**: ✅ Acceptable for v0.1.0

---

### 2. Command Injection Prevention ⚠️ NEEDS ATTENTION

**Finding**: Most command executions are safe, but some need validation

**Safe Implementations**:

```go
// Good: Arguments passed separately
exec.Command("ssh", sshTarget)
exec.Command("tailscale", "status", "--json")
exec.Command("tailscale", "switch", accountName)
```

**Potential Issues**:

1. **Username validation missing** (`tui/model.go` line 1021)
    - User-provided SSH username not validated
    - Could contain special characters
    - Risk: Low (passed as separate argument, not shell interpolation)
    - Recommendation: Add validation

2. **Script path in fmt.Sprintf** (`tui/model.go` line 1500)
    ```go
    exec.Command("bash", "-c", fmt.Sprintf("%s; rm -f %s", scriptPath, scriptPath))
    ```

    - Risk: Low (scriptPath from os.CreateTemp, not user-controlled)
    - Recommendation: Use separate arguments where possible

**Recommendation**: Add input validation for SSH username

**Status**: ⚠️ Minor fixes needed

---

### 3. Input Validation ⚠️ NEEDS IMPROVEMENT

**Finding**: Limited input validation on user-provided data

**Missing Validation**:

1. SSH Username
    - No regex validation
    - Should match `^[a-zA-Z0-9._-]+$`
2. Tailnet/Account Names
    - No validation on format
    - Should validate against expected patterns

3. API Keys
    - No format validation
    - Should check basic format before storage

**Recommendation**: Add validation functions

**Status**: ⚠️ Needs implementation

---

### 4. Error Messages & Information Disclosure ✅ GOOD

**Finding**: Error messages don't leak sensitive information

- API keys not logged in error messages
- Stack traces not exposed to end users
- Errors are descriptive but not overly verbose

**Status**: ✅ Good

---

### 5. SSH Connection Security ✅ GOOD

**Finding**: SSH connections properly handled

- Uses system SSH client (inherits user's SSH config and keys)
- No password handling in application
- SSH target validated by Tailscale API response

**Potential Improvement**:

- Add option to specify SSH options (StrictHostKeyChecking, etc.)
- Document SSH config requirements

**Status**: ✅ Good

---

### 6. Dependencies & Supply Chain 🔍 TO REVIEW

**Finding**: Need to audit dependencies for known vulnerabilities

**Action Items**:

- Run `go list -m all` to list dependencies
- Check for CVEs in dependencies
- Consider using `govulncheck` tool

**Status**: 🔍 Requires review

---

### 7. Temporary File Handling ⚠️ NEEDS IMPROVEMENT

**Finding**: Temporary files created but cleanup could be more robust

**Issue**: `tui/model.go` - Add account script

```go
tmpFile, err := os.CreateTemp("", "ts-cli-add-account-*.sh")
// Script executed with: bash -c "$scriptPath; rm -f $scriptPath"
```

**Concern**:

- Cleanup relies on script execution
- If script fails early, temp file may persist
- Permissions set to 0755 (executable by anyone who can read it)

**Recommendation**:

- Use `defer os.Remove(scriptPath)` for guaranteed cleanup
- Consider using 0700 permissions instead of 0755

**Status**: ⚠️ Needs improvement

---

### 8. Race Conditions 📝 LOW RISK

**Finding**: Minimal concurrent operations

- TUI runs in single goroutine mostly
- Config file read/write not protected by locks
- Risk: Low (single user, CLI tool)

**Recommendation**: Document that concurrent access not supported

**Status**: 📝 Acceptable for v0.1.0

---

### 9. Privilege Escalation 🔍 TO VERIFY

**Finding**: Application runs with user privileges

- No sudo/elevated permissions required
- Except: Linux systemd start (commands/tailscale_check.go line 80)

**Issue**:

```go
cmd = exec.Command("sudo", "systemctl", "start", "tailscaled")
```

**Concern**:

- Prompts for sudo password
- User must have sudo access
- Alternative: Instruct user to run command manually

**Recommendation**:

- Remove sudo command execution
- Provide instructions for users to run manually
- Critical security principle: Don't execute sudo from applications

**Status**: 🔍 Requires fix - CRITICAL

---

## Prioritized Recommendations

### HIGH PRIORITY (v0.1.0)

1. ✅ **Remove sudo execution** - Never execute sudo from application
    - File: `commands/tailscale_check.go` line 80
    - Action: Remove sudo command, provide user instructions instead

2. **Add input validation** - Validate user inputs
    - SSH username validation
    - Account/tailnet name validation
    - API key format validation

3. **Improve temp file handling** - More robust cleanup
    - Use defer for cleanup
    - Reduce file permissions to 0700

### MEDIUM PRIORITY (v0.2.0)

4. **Dependency audit** - Check for CVEs
    - Run govulncheck
    - Update vulnerable dependencies

5. **Document security considerations**
    - SSH configuration requirements
    - File permission requirements
    - Concurrent access limitations

### LOW PRIORITY (Future)

6. **Keychain integration** - Enhanced key storage
    - OS keychain for API keys
    - Reduces plaintext key storage

## Summary

Overall security posture: **GOOD with minor fixes needed**

The application follows good security practices in most areas:

- ✅ Proper file permissions
- ✅ No obvious command injection vulnerabilities
- ✅ Good error handling
- ⚠️ Needs input validation
- 🔍 Critical: Remove sudo execution

**Before v0.1.0 release**: Fix HIGH PRIORITY issues (especially sudo removal)

## Sign-off

This audit provides reasonable assurance that the application follows security best practices for a CLI tool handling sensitive credentials. The recommended fixes should be implemented before the v0.1.0 release.

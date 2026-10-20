package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Tailscale daemon interaction functions
// These functions handle communication with the local Tailscale daemon

// switchTailscaleAccount switches the active Tailscale account
func switchTailscaleAccount(accountName string) error {
	cmd := exec.Command("tailscale", "switch", accountName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to switch account: %w (output: %s)", err, string(output))
	}
	return nil
}

// switchAccountForSSH returns a command that switches Tailscale account and prepares for SSH
func (m model) switchAccountForSSH(deviceIndex int, accountName string) tea.Cmd {
	return tea.Sequence(
		tea.Println(fmt.Sprintf("\n🔄 Switching to account: %s", accountName)),
		func() tea.Msg {
			err := switchTailscaleAccount(accountName)
			if err == nil {
				tea.Println(fmt.Sprintf("✓ Switched to account: %s", accountName))
			}
			return accountSwitchedMsg{
				accountName:    accountName,
				err:            err,
				proceedWithSSH: true,
				deviceIndex:    deviceIndex,
			}
		},
	)
}

// runTailscaleUp runs 'tailscale up' command
func (m model) runTailscaleUp() tea.Cmd {
	return func() tea.Msg {
		tailscaleCmd := exec.Command("tailscale", "up")
		return tea.ExecProcess(tailscaleCmd, func(err error) tea.Msg {
			if err != nil {
				return tailscaleUpMsg{err: err}
			}
			return tailscaleUpMsg{err: nil}
		})()
	}
}

// shellQuote single-quotes s for safe interpolation into a POSIX shell script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// checkLocalTailscaleStatus checks if Tailscale is running locally
func checkLocalTailscaleStatus() (bool, string) {
	cmd := exec.Command("tailscale", "status")
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Tailscale command failed - daemon might not be running or not installed
		return false, "Tailscale daemon is not running"
	}

	// Check if output indicates we're not connected
	outputStr := string(output)
	if strings.Contains(strings.ToLower(outputStr), "logged out") {
		return false, "You are logged out from Tailscale"
	}

	// Tailscale is running and connected
	return true, ""
}

// getRealTailscaleAccount gets the currently active account from Tailscale daemon
func getRealTailscaleAccount() string {
	cmd := exec.Command("tailscale", "status", "--json")
	output, err := cmd.CombinedOutput()

	if err != nil {
		// If tailscale is not running or not installed, return unknown
		return "<not connected>"
	}

	// The status JSON lists every known user under "User" (keyed by ID);
	// the current login is the entry referenced by Self.UserID. Taking the
	// first "LoginName" in the document could return a shared-node owner.
	if login := parseLoginName(output); login != "" {
		return login
	}

	// Fallback: try to get it from regular status output
	cmd = exec.Command("tailscale", "status")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return "<not connected>"
	}

	// The status output typically shows the account email
	// Parse the first line to extract the account
	// Format is: "IP  hostname  account@domain  OS  status"
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 && len(lines[0]) > 0 {
		fields := strings.Fields(lines[0])
		// The account is in the 3rd field (index 2)
		if len(fields) >= 3 {
			account := fields[2]
			// Verify it looks like an account (contains @)
			if strings.Contains(account, "@") {
				return account
			}
		}
	}

	return "<not connected>"
}

// tailscaleStatus is the subset of `tailscale status --json` we need.
type tailscaleStatus struct {
	Self struct {
		UserID json.Number `json:"UserID"`
	} `json:"Self"`
	User map[string]struct {
		LoginName string `json:"LoginName"`
	} `json:"User"`
}

// parseLoginName extracts the current user's LoginName from status JSON.
// Returns "" when it cannot be determined.
func parseLoginName(statusJSON []byte) string {
	var st tailscaleStatus
	if err := json.Unmarshal(statusJSON, &st); err != nil {
		return ""
	}
	if u, ok := st.User[st.Self.UserID.String()]; ok {
		return u.LoginName
	}
	return ""
}

// checkIfInstallNeeded checks if ts-cli is properly installed in PATH
func checkIfInstallNeeded() (bool, bool) {
	execPath, err := os.Executable()
	if err != nil {
		return false, false
	}

	// Get the symlink target if this is a symlink
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		realPath = execPath
	}

	// Check if we're running from a typical PATH location
	// Common locations: /usr/local/bin, /usr/bin, ~/bin, ~/.local/bin
	pathLocations := []string{
		"/usr/local/bin",
		"/usr/bin",
		filepath.Join(os.Getenv("HOME"), "bin"),
		filepath.Join(os.Getenv("HOME"), ".local", "bin"),
	}

	inPath := false
	for _, loc := range pathLocations {
		if strings.HasPrefix(realPath, loc+"/") {
			inPath = true
			break
		}
	}

	if inPath {
		// Already in PATH, no suggestion needed
		return false, false
	}

	// Not in PATH - check if there's a broken installation
	// (ts-cli found in PATH but not the binary we're running from)
	if foundPath, err := exec.LookPath("ts-cli"); err == nil && foundPath != "" {
		if resolved, err := filepath.EvalSymlinks(foundPath); err == nil {
			foundPath = resolved
		}
		if foundPath != realPath {
			return true, true // Show suggestion, installation is broken
		}
	}

	// Not in PATH and no broken installation found
	return true, false
}

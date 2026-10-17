package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// runAddAccount prompts user to add a new account via login command
func (m model) runAddAccount() tea.Cmd {
	return func() tea.Msg {
		cmd := m.createAddAccountScript()
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return addAccountMsg{err: err}
			}
			return addAccountMsg{err: nil}
		})()
	}
}

// createAddAccountScript creates an interactive script for adding a new account
func (m model) createAddAccountScript() *exec.Cmd {
	// Get the path to the current ts-cli executable
	execPath, err := os.Executable()
	if err != nil {
		// Fallback to assuming ts-cli is in PATH
		execPath = "ts-cli"
	}

	if runtime.GOOS == "windows" {
		// The guided flow is a bash script; there is no cmd.exe equivalent yet.
		return exec.Command("cmd", "/c",
			"echo Adding accounts from the TUI is not supported on Windows. && "+
				"echo Run: set TAILSCALE_API_KEY=... ^&^& ts-cli login --tailnet=^<name^> && pause")
	}

	// Create an interactive shell script that guides the user.
	// No `set -e`: the login failure branch below must stay reachable.
	script := fmt.Sprintf(`#!/bin/bash

# Colors for better UX
BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

clear
echo -e "${BLUE}============================================${NC}"
echo -e "${BLUE}       Add New Tailscale Account${NC}"
echo -e "${BLUE}============================================${NC}"
echo ""
echo -e "${YELLOW}To add a new account, you need:${NC}"
echo "  1. Your tailnet name (e.g., example.com)"
echo "  2. A Tailscale API key"
echo ""
echo -e "${YELLOW}To generate an API key:${NC}"
echo "  1. Visit: https://login.tailscale.com/admin/settings/keys"
echo "  2. Click 'Generate API key'"
echo "  3. Give it a description (e.g., 'ts-cli')"
echo "  4. Copy the key (starts with 'tskey-api-')"
echo ""
echo "Press Enter to continue (or Ctrl+C to cancel)..."
read

# Prompt for tailnet
echo ""
echo -e "${BLUE}Enter your tailnet name:${NC}"
echo -n "Tailnet: "
read TAILNET

if [ -z "$TAILNET" ]; then
    echo -e "${YELLOW}Tailnet name cannot be empty. Exiting.${NC}"
    sleep 2
    exit 1
fi

# Prompt for API key
echo ""
echo -e "${BLUE}Enter your Tailscale API key:${NC}"
echo -n "API Key: "
read -s API_KEY
echo ""

if [ -z "$API_KEY" ]; then
    echo -e "${YELLOW}API key cannot be empty. Exiting.${NC}"
    sleep 2
    exit 1
fi

# Run the login command. The key is passed through the environment so it
# never shows up in the process list (argv is readable by every local user).
echo ""
echo -e "${BLUE}Validating and saving account...${NC}"
if TAILSCALE_API_KEY="$API_KEY" %s login --tailnet="$TAILNET"; then
    echo ""
    echo -e "${GREEN}✓ Account added successfully!${NC}"
    echo ""
    echo "Press Enter to return to interactive mode..."
    read
else
    echo ""
    echo -e "${YELLOW}Failed to add account. Press Enter to continue...${NC}"
    read
    exit 1
fi
`, shellQuote(execPath))

	// Create temp script file
	tmpFile, err := os.CreateTemp("", "ts-cli-add-account-*.sh")
	if err != nil {
		// Fallback to simpler approach
		return exec.Command("sh", "-c", fmt.Sprintf("echo 'Failed to create script: %v'; sleep 2", err))
	}

	if _, err := tmpFile.WriteString(script); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return exec.Command("sh", "-c", fmt.Sprintf("echo 'Failed to write script: %v'; sleep 2", err))
	}

	// Use 0700 (user execute only) for better security
	if err := tmpFile.Chmod(0700); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return exec.Command("sh", "-c", fmt.Sprintf("echo 'Failed to write script: %v'; sleep 2", err))
	}

	scriptPath := tmpFile.Name()
	tmpFile.Close()

	// Run the script, then remove it
	return exec.Command("bash", "-c", fmt.Sprintf("%s; rm -f %s", shellQuote(scriptPath), shellQuote(scriptPath)))
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

package tui

import (
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

// runTailscaleDown runs 'tailscale down' to disconnect from the network
func (m model) runTailscaleDown() tea.Cmd {
	return func() tea.Msg {
		tailscaleCmd := exec.Command("tailscale", "down")
		return tea.ExecProcess(tailscaleCmd, func(err error) tea.Msg {
			if err != nil {
				return tailscaleDownMsg{err: err}
			}
			return tailscaleDownMsg{err: nil}
		})()
	}
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

	// Create an interactive shell script that guides the user
	script := fmt.Sprintf(`#!/bin/bash
set -e

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

# Run the login command
echo ""
echo -e "${BLUE}Validating and saving account...${NC}"
%s login --tailnet="$TAILNET" --api-key="$API_KEY"

if [ $? -eq 0 ]; then
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
`, execPath)

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

	// Create a command that runs the script and then deletes it
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", scriptPath)
	} else {
		// Use bash to run the script, then remove it
		cmd = exec.Command("bash", "-c", fmt.Sprintf("%s; rm -f %s", scriptPath, scriptPath))
	}

	return cmd
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

	// Parse JSON to get the account email
	// The status JSON contains a "Self" object with "UserProfile" that has "LoginName"
	// For simplicity, let's extract it using string parsing
	outputStr := string(output)

	// Look for "LoginName" field in JSON
	// Note: JSON is formatted with whitespace, so we need flexible parsing
	loginNamePattern := `"LoginName"`
	if idx := strings.Index(outputStr, loginNamePattern); idx != -1 {
		// Find the colon after "LoginName"
		afterKey := outputStr[idx+len(loginNamePattern):]
		colonIdx := strings.Index(afterKey, ":")
		if colonIdx != -1 {
			// Find the opening quote
			afterColon := afterKey[colonIdx+1:]
			quoteIdx := strings.Index(afterColon, `"`)
			if quoteIdx != -1 {
				// Find the closing quote
				afterQuote := afterColon[quoteIdx+1:]
				endQuoteIdx := strings.Index(afterQuote, `"`)
				if endQuoteIdx != -1 {
					return afterQuote[:endQuoteIdx]
				}
			}
		}
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
	// Look for ts-cli in PATH
	pathCmd := exec.Command("which", "ts-cli")
	output, err := pathCmd.Output()
	if err == nil && len(output) > 0 {
		// Found ts-cli in PATH, but we're not running from there

		// This might be a broken installation
		foundPath := strings.TrimSpace(string(output))
		if foundPath != realPath {
			return true, true // Show suggestion, installation is broken
		}
	}

	// Not in PATH and no broken installation found
	return true, false
}

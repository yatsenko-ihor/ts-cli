package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// NewUninstallCommand creates the uninstall command
func NewUninstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall ts-cli from system PATH",
		Long: `Remove ts-cli binary and tsc alias from system PATH.

This command will:
  - Remove ts-cli from /usr/local/bin (Unix/macOS) or C:\Program Files\ts-cli (Windows)
  - Remove the 'tsc' symlink/alias
  - Optionally remove configuration (~/.ts-cli/)`,
		RunE: runUninstall,
	}

	cmd.Flags().Bool("config", false, "Also remove configuration directory (~/.ts-cli/)")

	return cmd
}

func runUninstall(cmd *cobra.Command, args []string) error {
	removeConfig, _ := cmd.Flags().GetBool("config")

	// Determine installation directory based on OS
	var installDir string
	switch runtime.GOOS {
	case "windows":
		installDir = filepath.Join(os.Getenv("ProgramFiles"), "ts-cli")
	default:
		installDir = "/usr/local/bin"
	}

	tsCliPath := filepath.Join(installDir, "ts-cli")
	tscPath := filepath.Join(installDir, "tsc")

	if runtime.GOOS == "windows" {
		tsCliPath += ".exe"
		tscPath += ".exe"
	}

	// Check what's installed
	tsCliExists := fileExists(tsCliPath)
	tscExists := false

	if runtime.GOOS != "windows" {
		// Check if tsc is our symlink
		if target, err := os.Readlink(tscPath); err == nil && target == "ts-cli" {
			tscExists = true
		}
	} else {
		tscExists = fileExists(tscPath)
	}

	if !tsCliExists && !tscExists {
		fmt.Println("ts-cli is not installed in system PATH.")
		return nil
	}

	fmt.Println("Found installed components:")
	if tsCliExists {
		fmt.Printf("  • %s\n", tsCliPath)
	}
	if tscExists {
		fmt.Printf("  • %s (alias)\n", tscPath)
	}
	if removeConfig {
		homeDir, _ := os.UserHomeDir()
		configDir := filepath.Join(homeDir, ".ts-cli")
		if fileExists(configDir) {
			fmt.Printf("  • %s (configuration)\n", configDir)
		}
	}

	fmt.Print("\nDo you want to proceed with uninstallation? (y/n): ")

	var response string
	_, _ = fmt.Scanln(&response) // EOF/empty input falls through to "cancelled"
	response = strings.ToLower(strings.TrimSpace(response))

	if response != "y" && response != "yes" {
		fmt.Println("Uninstallation cancelled.")
		return nil
	}

	// Remove ts-cli binary
	if tsCliExists {
		if err := os.Remove(tsCliPath); err != nil {
			return fmt.Errorf("failed to remove %s: %w (try with sudo)", tsCliPath, err)
		}
		fmt.Printf("✓ Removed %s\n", tsCliPath)
	}

	// Remove tsc alias
	if tscExists {
		if err := os.Remove(tscPath); err != nil {
			fmt.Printf("⚠ Failed to remove %s: %v\n", tscPath, err)
		} else {
			fmt.Printf("✓ Removed %s\n", tscPath)
		}
	}

	// Remove config directory if requested
	if removeConfig {
		homeDir, _ := os.UserHomeDir()
		configDir := filepath.Join(homeDir, ".ts-cli")
		if fileExists(configDir) {
			if err := os.RemoveAll(configDir); err != nil {
				fmt.Printf("⚠ Failed to remove %s: %v\n", configDir, err)
			} else {
				fmt.Printf("✓ Removed %s\n", configDir)
			}
		}
	}

	fmt.Println("\n✓ ts-cli has been uninstalled.")
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

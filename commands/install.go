package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// NewInstallCommand creates the install command
func NewInstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install ts-cli to system PATH",
		Long: `Install ts-cli binary to system PATH for easy access from anywhere.

This command will:
  - Copy the ts-cli binary to /usr/local/bin (Unix/macOS) or C:\Program Files\ts-cli (Windows)
  - Create a symlink 'tsc' as a shorter alias
  - Verify installation by checking if commands are available`,
		RunE: runInstall,
	}

	return cmd
}

func runInstall(cmd *cobra.Command, args []string) error {
	// Get the path of the currently running executable
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary path
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	fmt.Printf("Current executable: %s\n\n", execPath)

	// Check current installation status
	tsCliAlreadyInstalled := isInPath("ts-cli")
	tscAlreadyInstalled := false

	fmt.Println("Current status:")
	if tsCliAlreadyInstalled {
		fmt.Println("  ✓ ts-cli is already in PATH")
	} else {
		fmt.Println("  ✗ ts-cli is not in PATH")
	}

	if isInPath("tsc") {
		// Check if it's our tsc or TypeScript compiler
		tscPath, _ := exec.LookPath("tsc")
		if tscPath != "" {
			// Try to determine if it's TypeScript's tsc
			output, _ := exec.Command("tsc", "--version").CombinedOutput()
			if strings.Contains(string(output), "Version") {
				fmt.Println("  ✗ tsc alias not available (TypeScript compiler found)")
			} else {
				fmt.Println("  ✓ tsc alias is already in PATH")
				tscAlreadyInstalled = true
			}
		}
	} else {
		fmt.Println("  ✗ tsc alias is not in PATH")
	}

	fmt.Println()

	// If both are already installed, ask if user wants to reinstall
	if tsCliAlreadyInstalled && tscAlreadyInstalled {
		fmt.Println("Both ts-cli and tsc are already installed.")
	} else if tsCliAlreadyInstalled {
		fmt.Println("ts-cli is already installed, but tsc alias is missing.")
	}

	// Determine installation directory based on OS
	var installDir string
	switch runtime.GOOS {
	case "windows":
		installDir = filepath.Join(os.Getenv("ProgramFiles"), "ts-cli")
	default: // Unix-like systems (macOS, Linux)
		installDir = "/usr/local/bin"
	}

	fmt.Printf("Installation directory: %s\n", installDir)

	// Ask for confirmation
	if tsCliAlreadyInstalled && tscAlreadyInstalled {
		fmt.Print("Do you want to reinstall? (y/n): ")
	} else {
		fmt.Print("Do you want to proceed with installation? (y/n): ")
	}

	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))

	if response != "y" && response != "yes" {
		fmt.Println("Installation cancelled.")
		return nil
	}

	// Perform installation
	if err := installBinary(execPath, installDir); err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	fmt.Println("\n✓ Installation completed successfully!")

	// Verify installation
	fmt.Println("\nVerifying installation...")
	tsCliInPath := isInPath("ts-cli")
	tscInPath := isInPath("tsc")

	if tsCliInPath {
		fmt.Println("✓ ts-cli is now available in PATH")
	} else {
		fmt.Println("⚠ ts-cli not yet in PATH (may require new terminal session)")
	}

	if tscInPath {
		// Check if it's our tsc or TypeScript compiler
		tscPath, _ := exec.LookPath("tsc")
		if tscPath != "" {
			output, _ := exec.Command("tsc", "--version").CombinedOutput()
			if strings.Contains(string(output), "Version") {
				fmt.Println("⚠ tsc alias conflicts with TypeScript compiler")
			} else {
				fmt.Println("✓ tsc alias is now available in PATH")
			}
		}
	} else {
		fmt.Println("⚠ tsc alias not yet in PATH (may require new terminal session)")
	}

	// Provide instructions if not in PATH
	if !tsCliInPath || !tscInPath {
		fmt.Println("\n📝 Note: You may need to:")
		fmt.Println("   1. Open a new terminal session, or")
		fmt.Printf("   2. Add %s to your PATH\n", installDir)

		// Provide shell-specific instructions
		if runtime.GOOS != "windows" {
			shell := os.Getenv("SHELL")
			if strings.Contains(shell, "zsh") {
				fmt.Println("   3. Run: echo 'export PATH=\"$PATH:" + installDir + "\"' >> ~/.zshrc && source ~/.zshrc")
			} else if strings.Contains(shell, "bash") {
				fmt.Println("   3. Run: echo 'export PATH=\"$PATH:" + installDir + "\"' >> ~/.bashrc && source ~/.bashrc")
			}
		}
	}

	fmt.Println("\nYou can now use:")
	fmt.Println("  ts-cli [command]")
	fmt.Println("  tsc [command]")

	return nil
}

func isInPath(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

func installBinary(srcPath, destDir string) error {
	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	destPath := filepath.Join(destDir, "ts-cli")
	tscPath := filepath.Join(destDir, "tsc")

	// On Windows, add .exe extension
	if runtime.GOOS == "windows" {
		destPath += ".exe"
		tscPath += ".exe"
	}

	// Copy the binary
	fmt.Printf("Copying %s to %s...\n", srcPath, destPath)
	if err := copyFile(srcPath, destPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Make executable (Unix-like systems)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(destPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	// Create symlink/copy for tsc alias
	fmt.Printf("Creating tsc alias at %s...\n", tscPath)

	// Remove existing tsc if it's ours
	if _, err := os.Lstat(tscPath); err == nil {
		os.Remove(tscPath)
	}

	if runtime.GOOS == "windows" {
		// On Windows, create a copy instead of symlink
		if err := copyFile(destPath, tscPath); err != nil {
			fmt.Printf("Warning: failed to create tsc alias: %v\n", err)
		}
	} else {
		// On Unix-like systems, create a symlink
		if err := os.Symlink("ts-cli", tscPath); err != nil {
			fmt.Printf("Warning: failed to create tsc symlink: %v\n", err)
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	// Read source file
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// Write to destination
	return os.WriteFile(dst, data, 0755)
}

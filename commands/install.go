package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// Test seams: package-level variables whose defaults give the production
// behaviour. They let the tests redirect the installation directory, fake the
// operating system and inject failures without touching /usr/local/bin.
var (
	goos           = runtime.GOOS
	installDirFn   = defaultInstallDir
	executablePath = os.Executable
	evalSymlinks   = filepath.EvalSymlinks
	removeFn       = os.Remove
	symlinkFn      = os.Symlink
	createTemp     = func(dir, pattern string) (tempFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
)

// tempFile is the part of *os.File that copyFile uses.
type tempFile interface {
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Close() error
	Name() string
}

// defaultInstallDir is where `install` puts the binary and `uninstall`
// looks for it.
func defaultInstallDir() string {
	if goos == "windows" {
		return filepath.Join(os.Getenv("ProgramFiles"), "ts-cli")
	}
	return "/usr/local/bin"
}

// confirmed reads one line from in and reports whether it is exactly "y" or
// "yes" (any case, surrounding blanks ignored). Anything else, including EOF,
// an empty line or "y and more", means no.
func confirmed(in io.Reader) bool {
	line, _ := bufio.NewReader(in).ReadString('\n') // EOF returns the partial line
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// tscIsTypeScript reports whether the `tsc` found on PATH is the TypeScript compiler.
func tscIsTypeScript() bool {
	output, _ := exec.Command("tsc", "--version").CombinedOutput()
	return strings.Contains(string(output), "Version")
}

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
	execPath, err := executablePath()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary path
	execPath, err = evalSymlinks(execPath)
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
		if tscIsTypeScript() {
			fmt.Println("  ✗ tsc alias not available (TypeScript compiler found)")
		} else {
			fmt.Println("  ✓ tsc alias is already in PATH")
			tscAlreadyInstalled = true
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
	installDir := installDirFn()

	fmt.Printf("Installation directory: %s\n", installDir)

	// Ask for confirmation
	if tsCliAlreadyInstalled && tscAlreadyInstalled {
		fmt.Print("Do you want to reinstall? (y/n): ")
	} else {
		fmt.Print("Do you want to proceed with installation? (y/n): ")
	}

	if !confirmed(cmd.InOrStdin()) {
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
		if tscIsTypeScript() {
			fmt.Println("⚠ tsc alias conflicts with TypeScript compiler")
		} else {
			fmt.Println("✓ tsc alias is now available in PATH")
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
		if goos != "windows" {
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
	if goos == "windows" {
		destPath += ".exe"
		tscPath += ".exe"
	}

	// Copy the binary
	fmt.Printf("Copying %s to %s...\n", srcPath, destPath)
	if err := copyFile(srcPath, destPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// copyFile already created the file with mode 0755.

	// Create symlink/copy for tsc alias
	fmt.Printf("Creating tsc alias at %s...\n", tscPath)

	// Only replace an existing tsc when it is our own symlink. Anything
	// else (typically the TypeScript compiler) must be left alone.
	if _, err := os.Lstat(tscPath); err == nil {
		if !isOurTscAlias(tscPath) {
			fmt.Printf("Warning: %s already exists and is not a ts-cli alias; skipping tsc alias.\n", tscPath)
			return nil
		}
		if err := removeFn(tscPath); err != nil {
			fmt.Printf("Warning: failed to replace existing tsc alias: %v\n", err)
			return nil
		}
	}

	if goos == "windows" {
		// On Windows, create a copy instead of symlink
		if err := copyFile(destPath, tscPath); err != nil {
			fmt.Printf("Warning: failed to create tsc alias: %v\n", err)
		}
	} else {
		// On Unix-like systems, create a symlink
		if err := symlinkFn("ts-cli", tscPath); err != nil {
			fmt.Printf("Warning: failed to create tsc symlink: %v\n", err)
		}
	}

	return nil
}

// isOurTscAlias reports whether path is the tsc alias created by ts-cli.
// On Unix that is a symlink pointing at "ts-cli"; on Windows the alias is a
// copy of the binary, which cannot be told apart from anything else, so an
// existing file is never considered ours.
func isOurTscAlias(path string) bool {
	if goos == "windows" {
		return false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false
	}
	return filepath.Base(target) == "ts-cli"
}

// adHocSign gives the installed binary a fresh ad-hoc code signature. It is a
// variable so tests do not need codesign.
var adHocSign = func(path string) error {
	return exec.Command("codesign", "--force", "--sign", "-", path).Run()
}

// copyFile installs src at dst as a new file. It writes a temporary file next
// to dst and renames it over dst, so dst always gets a new inode.
//
// Why not just overwrite: on macOS the kernel caches a binary's code
// signature per file. Rewriting an already-executed binary in place leaves a
// stale cache entry and the next launch is killed by the kernel
// ("zsh: killed ts-cli") although the bytes are fine. A new inode avoids that,
// and the ad-hoc re-signature below covers copies that lost their signature.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	tmp, err := createTemp(filepath.Dir(dst), ".ts-cli-install-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0755); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if goos == "darwin" {
		// Best effort: an unsigned arm64 binary would be killed on launch.
		if err := adHocSign(tmpName); err != nil {
			fmt.Printf("Warning: could not re-sign %s (%v); if it is killed on launch run: codesign --force --sign - %s\n", dst, err, dst)
		}
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

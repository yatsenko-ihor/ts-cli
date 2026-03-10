package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ihor/ts-cli/client"
)

// LoginCommand implements the login command
type LoginCommand struct{}

// Help returns the help text for the login command
func (c *LoginCommand) Help() string {
	helpText := `
Usage: ts-cli login [options]

  Validate and store the Tailscale API key for authentication.
  The API key can be provided via the TAILSCALE_API_KEY environment variable
  or using the --api-key flag.

Options:

  --api-key=<key>     Tailscale API key (overrides TAILSCALE_API_KEY env var)
  --tailnet=<name>    Tailnet name (e.g., example.com or user@example.com)
                      If not provided, will use the tailnet from your API key

Example:

  export TAILSCALE_API_KEY=tskey-api-xxxxx
  ts-cli login --tailnet=example.com

  OR

  ts-cli login --api-key=tskey-api-xxxxx --tailnet=example.com
`
	return strings.TrimSpace(helpText)
}

// Synopsis returns a short synopsis of the login command
func (c *LoginCommand) Synopsis() string {
	return "Validate and configure Tailscale API authentication"
}

// Run executes the login command
func (c *LoginCommand) Run(args []string) int {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	var apiKey string
	var tailnet string

	flags.StringVar(&apiKey, "api-key", "", "Tailscale API key")
	flags.StringVar(&tailnet, "tailnet", "", "Tailnet name")

	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %s\n", err)
		return 1
	}

	// Try to get API key from flag first, then environment variable
	if apiKey == "" {
		apiKey = os.Getenv("TAILSCALE_API_KEY")
	}

	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: API key not provided.\n")
		fmt.Fprintf(os.Stderr, "Set TAILSCALE_API_KEY environment variable or use --api-key flag.\n")
		return 1
	}

	// If tailnet not provided, try to use a default or prompt
	if tailnet == "" {
		fmt.Fprintf(os.Stderr, "Error: Tailnet name is required.\n")
		fmt.Fprintf(os.Stderr, "Use --tailnet flag to specify your tailnet name.\n")
		return 1
	}

	// Validate the API key
	fmt.Println("Validating API key...")
	apiClient := client.NewClient(apiKey)

	if err := apiClient.ValidateAPIKey(tailnet); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to validate API key: %s\n", err)
		return 1
	}

	fmt.Println("✓ API key is valid")

	// Store the configuration locally
	if err := c.storeConfig(apiKey, tailnet); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to store config locally: %s\n", err)
		fmt.Println("You can still use the API key via environment variable.")
		return 0
	}

	fmt.Println("✓ Configuration saved successfully")
	fmt.Printf("✓ Authenticated with tailnet: %s\n", tailnet)

	return 0
}

// storeConfig stores the API configuration locally
func (c *LoginCommand) storeConfig(apiKey, tailnet string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".ts-cli")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configFile := filepath.Join(configDir, "config")
	content := fmt.Sprintf("TAILSCALE_API_KEY=%s\nTAILNET=%s\n", apiKey, tailnet)

	if err := os.WriteFile(configFile, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

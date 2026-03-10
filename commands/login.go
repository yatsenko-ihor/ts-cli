package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ihor/ts-cli/client"
	"github.com/spf13/cobra"
)

// NewLoginCommand creates the login command
func NewLoginCommand() *cobra.Command {
	var apiKey string
	var tailnet string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Validate and configure Tailscale API authentication",
		Long: `Validate and store the Tailscale API key for authentication.
The API key can be provided via the TAILSCALE_API_KEY environment variable
or using the --api-key flag.`,
		Example: `  # Using environment variable
  export TAILSCALE_API_KEY=tskey-api-xxxxx
  ts-cli login --tailnet=example.com

  # Using flag
  ts-cli login --api-key=tskey-api-xxxxx --tailnet=example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Try to get API key from flag first, then environment variable
			if apiKey == "" {
				apiKey = os.Getenv("TAILSCALE_API_KEY")
			}

			if apiKey == "" {
				return fmt.Errorf("API key not provided.\nSet TAILSCALE_API_KEY environment variable or use --api-key flag")
			}

			// If tailnet not provided, return error
			if tailnet == "" {
				return fmt.Errorf("tailnet name is required.\nUse --tailnet flag to specify your tailnet name")
			}

			// Validate the API key
			fmt.Println("Validating API key...")
			apiClient := client.NewClient(apiKey)

			if err := apiClient.ValidateAPIKey(tailnet); err != nil {
				return fmt.Errorf("failed to validate API key: %w", err)
			}

			fmt.Println("✓ API key is valid")

			// Store the configuration locally
			if err := storeConfig(apiKey, tailnet); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to store config locally: %s\n", err)
				fmt.Println("You can still use the API key via environment variable.")
				return nil
			}

			fmt.Println("✓ Configuration saved successfully")
			fmt.Printf("✓ Authenticated with tailnet: %s\n", tailnet)

			return nil
		},
	}

	// Define flags
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key (overrides TAILSCALE_API_KEY env var)")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (e.g., example.com or user@example.com)")
	cmd.MarkFlagRequired("tailnet")

	return cmd
}

// storeConfig stores the API configuration locally
func storeConfig(apiKey, tailnet string) error {
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

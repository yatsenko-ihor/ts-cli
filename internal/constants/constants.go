// Package constants provides centralized configuration values and constants
// used throughout the ts-cli application, adhering to the DRY principle.
package constants

import "time"

// Application metadata
const (
	// APP_VERSION represents the current version of ts-cli
	APP_VERSION = "0.1.0"

	// APP_NAME is the application name
	APP_NAME = "ts-cli"

	// APP_SHORT_DESC is the short description of the application
	APP_SHORT_DESC = "Tailscale CLI - Manage Tailscale devices via API"

	// APP_LONG_DESC is the detailed description of the application
	APP_LONG_DESC = "A command-line interface tool for managing Tailscale devices and resources via the Tailscale REST API."
)

// API Configuration
const (
	// API_BASE_URL is the base URL for Tailscale API
	API_BASE_URL = "https://api.tailscale.com/api/v2"

	// API_TIMEOUT is the default timeout for API requests
	API_TIMEOUT = 30 * time.Second

	// API_KEY_ENV_VAR is the environment variable name for API key
	API_KEY_ENV_VAR = "TAILSCALE_API_KEY"
)

// File System Paths
const (
	// CONFIG_DIR_NAME is the name of the configuration directory
	CONFIG_DIR_NAME = ".ts-cli"

	// CONFIG_FILE_NAME is the name of the configuration file
	CONFIG_FILE_NAME = "config.json"

	// HISTORY_FILE_NAME is the name of the command history file
	HISTORY_FILE_NAME = "history.json"

	// OLD_CONFIG_FILE_NAME is the name of the legacy configuration file
	OLD_CONFIG_FILE_NAME = "config"

	// CONFIG_VERSION is the current configuration format version
	CONFIG_VERSION = "1.0"
)

// File Permissions
const (
	// DIR_PERMISSION is the default permission for directories
	DIR_PERMISSION = 0700

	// FILE_PERMISSION is the default permission for configuration files
	FILE_PERMISSION = 0600
)

// Output Formatting
const (
	// FORMAT_TABLE represents table output format
	FORMAT_TABLE = "table"

	// FORMAT_JSON represents JSON output format
	FORMAT_JSON = "json"

	// MAX_OUTPUT_LENGTH limits the output size stored in history
	MAX_OUTPUT_LENGTH = 1024

	// OUTPUT_TRUNCATE_SUFFIX is appended to truncated output
	OUTPUT_TRUNCATE_SUFFIX = "... (truncated)"
)

// UI Constants
const (
	// TUI_MIN_HEIGHT is the minimum height required for TUI
	TUI_MIN_HEIGHT = 30

	// TUI_MIN_WIDTH is the minimum width for optimal TUI display
	TUI_MIN_WIDTH = 80
)

// HTTP Status Codes (for clarity in error handling)
const (
	HTTP_STATUS_OK           = 200
	HTTP_STATUS_UNAUTHORIZED = 401
	HTTP_STATUS_FORBIDDEN    = 403
	HTTP_STATUS_BAD_REQUEST  = 400
)

// Old Config Keys (for migration)
const (
	OLD_CONFIG_KEY_API_KEY      = "TAILSCALE_API_KEY"
	OLD_CONFIG_KEY_TAILNET      = "TAILNET"
	OLD_CONFIG_KEY_SSH_USERNAME = "SSH_USERNAME"
)

// Error Messages
const (
	ERR_NO_ACCOUNTS_CONFIGURED = "no accounts configured.\nRun 'ts-cli login --tailnet=<name>' first to add an account"
	ERR_INVALID_API_KEY        = "invalid API key or insufficient permissions"
	ERR_NO_DEVICES_FOUND       = "no devices found in any of your configured accounts"
	ERR_API_KEY_NOT_PROVIDED   = "API key not provided.\nSet TAILSCALE_API_KEY environment variable or use --api-key flag"
	ERR_TAILNET_REQUIRED       = "tailnet name is required.\nUse --tailnet flag to specify your tailnet name"
)

// Log Messages
const (
	LOG_VALIDATING_API_KEY = "Validating API key..."
	LOG_API_KEY_VALID      = "✓ API key is valid"
	LOG_FETCHING_DEVICES   = "Fetching devices from all configured accounts..."
	LOG_UPDATED_ACCOUNT    = "✓ Updated account: %s"
	LOG_ADDED_ACCOUNT      = "✓ Added account: %s"
	LOG_SAVED_CONFIG       = "✓ Configuration saved successfully"
)

package util

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidateSSHUsername validates an SSH username
// Username should contain only alphanumeric characters, dots, underscores, and hyphens
func ValidateSSHUsername(username string) error {
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	if len(username) > 32 {
		return fmt.Errorf("username too long (max 32 characters)")
	}

	// Username should match standard Unix username pattern
	validUsername := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !validUsername.MatchString(username) {
		return fmt.Errorf("username contains invalid characters (only alphanumeric, dots, underscores, and hyphens allowed)")
	}

	// Username should not start with a hyphen
	if strings.HasPrefix(username, "-") {
		return fmt.Errorf("username cannot start with a hyphen")
	}

	return nil
}

// ValidateTailnet validates a Tailnet name
// Tailnet is typically an email or domain
func ValidateTailnet(tailnet string) error {
	if tailnet == "" {
		return fmt.Errorf("tailnet cannot be empty")
	}

	if len(tailnet) > 253 {
		return fmt.Errorf("tailnet too long (max 253 characters)")
	}

	// Basic validation: should contain @ or be a valid domain
	if !strings.Contains(tailnet, "@") && !strings.Contains(tailnet, ".") {
		return fmt.Errorf("tailnet should be an email or domain (e.g., user@example.com or example.com)")
	}

	return nil
}

// ValidateAccountName validates an account name
// Account name is a user-friendly identifier
func ValidateAccountName(name string) error {
	if name == "" {
		return fmt.Errorf("account name cannot be empty")
	}

	if len(name) > 64 {
		return fmt.Errorf("account name too long (max 64 characters)")
	}

	// Allow alphanumeric, spaces, dots, underscores, and hyphens
	validName := regexp.MustCompile(`^[a-zA-Z0-9 ._-]+$`)
	if !validName.MatchString(name) {
		return fmt.Errorf("account name contains invalid characters")
	}

	return nil
}

// ValidateAPIKey validates a Tailscale API key format
// Tailscale API keys typically start with "tskey-" or "tskey-api-"
func ValidateAPIKey(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	if len(apiKey) < 20 {
		return fmt.Errorf("API key too short (seems invalid)")
	}

	if len(apiKey) > 200 {
		return fmt.Errorf("API key too long (max 200 characters)")
	}

	// Check for common prefixes
	validPrefixes := []string{"tskey-", "tskey-api-", "tskey-auth-"}
	hasValidPrefix := false
	for _, prefix := range validPrefixes {
		if strings.HasPrefix(apiKey, prefix) {
			hasValidPrefix = true
			break
		}
	}

	if !hasValidPrefix {
		return fmt.Errorf("API key should start with 'tskey-' or 'tskey-api-' (Tailscale API key format)")
	}

	return nil
}

// SanitizeInput removes potentially dangerous characters from input
// This is a defense-in-depth measure
func SanitizeInput(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")
	
	// Remove control characters except tab, newline, carriage return
	var sanitized strings.Builder
	for _, r := range input {
		if r >= 32 || r == '\t' || r == '\n' || r == '\r' {
			sanitized.WriteRune(r)
		}
	}
	
	return strings.TrimSpace(sanitized.String())
}

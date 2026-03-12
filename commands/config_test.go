package commands

import (
	"testing"
)

// TestConfigSetActiveAccount tests the SetActiveAccount functionality
func TestConfigSetActiveAccount(t *testing.T) {
	config := &Config{
		Accounts: []Account{
			{Name: "personal", APIKey: "key1", Tailnet: "personal.com", Active: true},
			{Name: "work", APIKey: "key2", Tailnet: "work.com", Active: false},
			{Name: "test", APIKey: "key3", Tailnet: "test.com", Active: false},
		},
	}

	// Test setting a different account as active
	success := config.SetActiveAccount("work.com")
	if !success {
		t.Error("Expected SetActiveAccount to return true for existing account")
	}

	if !config.Accounts[1].Active {
		t.Error("Expected 'work' account to be active")
	}

	if config.Accounts[0].Active {
		t.Error("Expected 'personal' account to be inactive")
	}

	if config.Accounts[2].Active {
		t.Error("Expected 'test' account to be inactive")
	}
}

// TestConfigSetActiveAccountNonExistent tests setting a non-existent account as active
func TestConfigSetActiveAccountNonExistent(t *testing.T) {
	config := &Config{
		Accounts: []Account{
			{Name: "personal", APIKey: "key1", Tailnet: "personal.com", Active: true},
		},
	}

	// Try to set a non-existent account as active
	success := config.SetActiveAccount("doesnotexist.com")
	if success {
		t.Error("Expected SetActiveAccount to return false for non-existent account")
	}

	// The original account should still be active
	if !config.Accounts[0].Active {
		t.Error("Expected 'personal' account to remain active when setting non-existent account")
	}
}

// TestConfigGetActiveAccount tests retrieving the active account
func TestConfigGetActiveAccount(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected *Account
	}{
		{
			name: "single active account",
			config: &Config{
				Accounts: []Account{
					{Name: "personal", APIKey: "key1", Tailnet: "personal.com", Active: true},
					{Name: "work", APIKey: "key2", Tailnet: "work.com", Active: false},
				},
			},
			expected: &Account{Name: "personal", APIKey: "key1", Tailnet: "personal.com", Active: true},
		},
		{
			name: "no active account",
			config: &Config{
				Accounts: []Account{
					{Name: "personal", APIKey: "key1", Tailnet: "personal.com", Active: false},
					{Name: "work", APIKey: "key2", Tailnet: "work.com", Active: false},
				},
			},
			expected: nil,
		},
		{
			name: "empty accounts",
			config: &Config{
				Accounts: []Account{},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetActiveAccount()
			
			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil account, got %v", result)
				}
			} else {
				if result == nil {
					t.Error("Expected non-nil account, got nil")
				} else if result.Name != tt.expected.Name {
					t.Errorf("Expected account name %s, got %s", tt.expected.Name, result.Name)
				}
			}
		})
	}
}

// TestConfigGetAccountByTailnet tests retrieving account by tailnet
func TestConfigGetAccountByTailnet(t *testing.T) {
	config := &Config{
		Accounts: []Account{
			{Name: "personal", APIKey: "key1", Tailnet: "personal.com", Active: true},
			{Name: "work", APIKey: "key2", Tailnet: "work.com", Active: false},
		},
	}

	// Test finding existing account
	account := config.GetAccountByTailnet("work.com")
	if account == nil {
		t.Error("Expected to find work account")
	} else if account.Name != "work" {
		t.Errorf("Expected account name 'work', got %s", account.Name)
	}

	// Test finding non-existent account
	account = config.GetAccountByTailnet("nonexistent.com")
	if account != nil {
		t.Error("Expected nil for non-existent tailnet")
	}
}

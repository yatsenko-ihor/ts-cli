package util

import (
	"strings"
	"testing"
)

func TestValidateSSHUsername(t *testing.T) {
	valid := []string{"root", "ubuntu", "john.doe", "svc_account", "a-b", "user123", strings.Repeat("a", 32)}
	for _, u := range valid {
		if err := ValidateSSHUsername(u); err != nil {
			t.Errorf("ValidateSSHUsername(%q) unexpected error: %v", u, err)
		}
	}

	invalid := map[string]string{
		"":                      "empty",
		"-oProxyCommand=evil":   "leading hyphen (ssh option injection)",
		"user name":             "space",
		"user;rm -rf /":         "shell metacharacters",
		"user@host":             "at sign",
		"user$(id)":             "command substitution",
		"user\n":                "newline",
		strings.Repeat("a", 33): "too long",
		"юзер":                  "non-ascii",
	}
	for u, why := range invalid {
		if err := ValidateSSHUsername(u); err == nil {
			t.Errorf("ValidateSSHUsername(%q) should fail (%s)", u, why)
		}
	}
}

func TestValidateTailnet(t *testing.T) {
	for _, tn := range []string{"example.com", "user@example.com", "my-corp.github"} {
		if err := ValidateTailnet(tn); err != nil {
			t.Errorf("ValidateTailnet(%q): %v", tn, err)
		}
	}
	for _, tn := range []string{"", "nodots", strings.Repeat("a", 254) + ".com"} {
		if err := ValidateTailnet(tn); err == nil {
			t.Errorf("ValidateTailnet(%q) should fail", tn)
		}
	}
}

func TestValidateAPIKey(t *testing.T) {
	if err := ValidateAPIKey("tskey-api-kXXXXXXXXXXXXXXXXXXXXXXXX"); err != nil {
		t.Errorf("valid key rejected: %v", err)
	}
	for _, k := range []string{"", "tskey-api-short", "notakey-xxxxxxxxxxxxxxxxxxxxxxxx", "tskey-api-" + strings.Repeat("x", 200)} {
		if err := ValidateAPIKey(k); err == nil {
			t.Errorf("ValidateAPIKey(%q) should fail", k)
		}
	}
}

func TestSanitizeInput(t *testing.T) {
	// NUL and ESC are dropped, interior tab is kept, surrounding whitespace trimmed.
	got := SanitizeInput("  ls\x00\t-la\x1b\n ")
	want := "ls\t-la"
	if got != want {
		t.Fatalf("SanitizeInput = %q, want %q", got, want)
	}
}

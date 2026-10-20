package util

import (
	"strings"
	"testing"
)

type valCase struct {
	name string
	in   string
	ok   bool
}

func runValCases(t *testing.T, fn func(string) error, cases []valCase) {
	t.Helper()
	for _, c := range cases {
		err := fn(c.in)
		if c.ok && err != nil {
			t.Errorf("%s: %q unexpectedly rejected: %v", c.name, c.in, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: %q unexpectedly accepted", c.name, c.in)
		}
	}
}

// Verifies: REQ-VAL-001
func TestValidateSSHUsernameEmpty(t *testing.T) {
	runValCases(t, ValidateSSHUsername, []valCase{{"empty", "", false}, {"one char", "a", true}})
}

// Verifies: REQ-VAL-002
func TestValidateSSHUsernameLengthBoundary(t *testing.T) {
	runValCases(t, ValidateSSHUsername, []valCase{
		{"31", strings.Repeat("a", 31), true},
		{"32", strings.Repeat("a", 32), true},
		{"33", strings.Repeat("a", 33), false},
		{"1000", strings.Repeat("a", 1000), false},
	})
}

// Verifies: REQ-VAL-003
func TestValidateSSHUsernameRejectsHostileCharacters(t *testing.T) {
	var cases []valCase
	for _, s := range []string{
		"user name", "user;id", "user|id", "user&id", "user`id`", "user$(id)", "user$HOME",
		"user@host", "user\n", "user\r", "user\t", "user\x00", "us/er", "us\\er", "user'", `user"`,
		"юзер", "user‮", "user*", "user=x", "user:x", "user,x", "user\x7f",
	} {
		cases = append(cases, valCase{"hostile", s, false})
	}
	runValCases(t, ValidateSSHUsername, cases)
}

// Verifies: REQ-VAL-004
func TestValidateSSHUsernameRejectsLeadingHyphen(t *testing.T) {
	runValCases(t, ValidateSSHUsername, []valCase{
		{"option injection", "-oProxyCommand=evil", false},
		{"only hyphen", "-", false},
		{"double hyphen", "--help", false},
		{"leading hyphen letter", "-a", false},
		{"inner hyphen", "a-", true},
		{"inner hyphen 2", "a-b", true},
	})
}

// Verifies: REQ-VAL-005
func TestValidateSSHUsernameAcceptsOrdinaryNames(t *testing.T) {
	var cases []valCase
	for _, s := range []string{"root", "ubuntu", "john.doe", "svc_account", "a-b", "user123", "_x", ".x", "A.B_c-d", "0"} {
		cases = append(cases, valCase{"ordinary", s, true})
	}
	runValCases(t, ValidateSSHUsername, cases)
}

// Verifies: REQ-VAL-006
func TestValidateTailnetEmpty(t *testing.T) {
	runValCases(t, ValidateTailnet, []valCase{{"empty", "", false}, {"minimal", ".", true}})
}

// Verifies: REQ-VAL-007
func TestValidateTailnetLengthBoundary(t *testing.T) {
	mk := func(n int) string { return strings.Repeat("a", n-4) + ".com" }
	runValCases(t, ValidateTailnet, []valCase{
		{"252", mk(252), true},
		{"253", mk(253), true},
		{"254", mk(254), false},
		{"1000", mk(1000), false},
	})
}

// Verifies: REQ-VAL-008
func TestValidateTailnetRequiresAtOrDot(t *testing.T) {
	runValCases(t, ValidateTailnet, []valCase{
		{"neither", "nodots", false},
		{"neither dash", "my-corp", false},
		{"at only", "user@nodots", true},
		{"dot only", "example.com", true},
		{"both", "user@example.com", true},
	})
}

// Verifies: REQ-VAL-008
func TestValidateTailnetMCDC(t *testing.T) {
	// D2 = !Contains("@") && !Contains("."); true means reject.
	reject := func(s string) bool { return ValidateTailnet(s) != nil }
	if !reject("nodots") { // A=T B=T -> reject
		t.Fatal("base case must be rejected")
	}
	if reject("user@nodots") { // A flips only -> accept
		t.Error("condition A has no independent effect")
	}
	if reject("example.com") { // B flips only -> accept
		t.Error("condition B has no independent effect")
	}
}

// Verifies: REQ-VAL-009
func TestValidateAccountNameEmpty(t *testing.T) {
	runValCases(t, ValidateAccountName, []valCase{{"empty", "", false}, {"one", "a", true}})
}

// Verifies: REQ-VAL-010
func TestValidateAccountNameLengthBoundary(t *testing.T) {
	runValCases(t, ValidateAccountName, []valCase{
		{"63", strings.Repeat("a", 63), true},
		{"64", strings.Repeat("a", 64), true},
		{"65", strings.Repeat("a", 65), false},
	})
}

// Verifies: REQ-VAL-011
func TestValidateAccountNameCharacterSet(t *testing.T) {
	runValCases(t, ValidateAccountName, []valCase{
		{"plain", "Work Account", true},
		{"punct", "my.acct_1-b", true},
		{"slash", "a/b", false},
		{"semicolon", "a;b", false},
		{"newline", "a\nb", false},
		{"tab", "a\tb", false},
		{"nul", "a\x00b", false},
		{"unicode", "аккаунт", false},
		{"at", "a@b", false},
	})
}

// Verifies: REQ-VAL-012
func TestValidateAPIKeyEmpty(t *testing.T) {
	runValCases(t, ValidateAPIKey, []valCase{{"empty", "", false}})
}

func apiKey(n int) string { return "tskey-" + strings.Repeat("x", n-6) }

// Verifies: REQ-VAL-013
func TestValidateAPIKeyMinLength(t *testing.T) {
	runValCases(t, ValidateAPIKey, []valCase{
		{"19", apiKey(19), false},
		{"20", apiKey(20), true},
		{"21", apiKey(21), true},
	})
}

// Verifies: REQ-VAL-014
func TestValidateAPIKeyMaxLength(t *testing.T) {
	runValCases(t, ValidateAPIKey, []valCase{
		{"199", apiKey(199), true},
		{"200", apiKey(200), true},
		{"201", apiKey(201), false},
	})
}

// Verifies: REQ-VAL-015
func TestValidateAPIKeyPrefix(t *testing.T) {
	pad := strings.Repeat("x", 20)
	runValCases(t, ValidateAPIKey, []valCase{
		{"tskey-", "tskey-" + pad, true},
		{"tskey-api-", "tskey-api-" + pad, true},
		{"tskey-auth-", "tskey-auth-" + pad, true},
		{"no prefix", pad + pad, false},
		{"upper case", "TSKEY-" + pad, false},
		{"partial", "tskey" + pad, false},
		{"prefix in middle", pad + "tskey-" + pad, false},
		{"leading space", " tskey-" + pad, false},
	})
}

// Verifies: REQ-VAL-016
func TestSanitizeInputRemovesNul(t *testing.T) {
	for in, want := range map[string]string{"a\x00b": "ab", "\x00\x00": "", "ab": "ab", "\x00a\x00": "a"} {
		if got := SanitizeInput(in); got != want {
			t.Errorf("SanitizeInput(%q) = %q, want %q", in, got, want)
		}
	}
}

// Verifies: REQ-VAL-017
func TestSanitizeInputRemovesControlCharacters(t *testing.T) {
	for in, want := range map[string]string{
		"a\x01b":      "ab",
		"a\x1bb":      "ab", // escape
		"a\x1fb":      "ab", // limit-1 of printable
		"a\x07\x08b":  "ab",
		"a\tb\nc\rd":  "a\tb\nc\rd",
		"a\x0bb\x0cc": "abc", // VT and FF are controls, not preserved
	} {
		if got := SanitizeInput(in); got != want {
			t.Errorf("SanitizeInput(%q) = %q, want %q", in, got, want)
		}
	}
}

// Verifies: REQ-VAL-018
func TestSanitizeInputKeepsPrintable(t *testing.T) {
	for _, in := range []string{"a b", "x y z", "!~", "юнікод", "key 🔑", "a\x7fb", "a\u0085b"} {
		if got := SanitizeInput(in); got != in {
			t.Errorf("SanitizeInput(%q) = %q, want unchanged", in, got)
		}
	}
	if got := SanitizeInput("a\x20b"); got != "a b" { // boundary U+0020 kept
		t.Errorf("space (U+0020) must be kept, got %q", got)
	}
}

// Verifies: REQ-VAL-017, REQ-VAL-018
func TestSanitizeInputMCDC(t *testing.T) {
	// D1 = r >= 32 || r == '\t' || r == '\n' || r == '\r'; keep when true.
	kept := func(r rune) bool { return SanitizeInput("x"+string(r)+"x") == "x"+string(r)+"x" }
	if kept(0x01) {
		t.Fatal("U+0001 (all conditions false) must be dropped")
	}
	if !kept('a') { // A flips
		t.Error("condition r>=32 has no independent effect")
	}
	if !kept('\t') { // B flips
		t.Error("condition r=='\\t' has no independent effect")
	}
	if !kept('\n') { // C flips
		t.Error("condition r=='\\n' has no independent effect")
	}
	if !kept('\r') { // D flips
		t.Error("condition r=='\\r' has no independent effect")
	}
	if kept(0x1f) || !kept(0x20) {
		t.Error("boundary U+001F/U+0020 wrong")
	}
}

// Verifies: REQ-VAL-019
func TestSanitizeInputTrims(t *testing.T) {
	for in, want := range map[string]string{
		"  a  ":         "a",
		"\n\ta b\r\n":   "a b",
		"":              "",
		"   ":           "",
		"\x00\x01 \x02": "",
		"\x01 a \x02":   "a", // controls removed first, then trimmed
		" a\x00 b ":     "a b",
		"a  b":          "a  b", // interior white space kept
	} {
		if got := SanitizeInput(in); got != want {
			t.Errorf("SanitizeInput(%q) = %q, want %q", in, got, want)
		}
	}
}

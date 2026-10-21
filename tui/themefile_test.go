//go:build !windows

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Requirements-based tests for custom theme files (verification/requirements/themefile.md).

func writeTheme(t *testing.T, dir, file, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func resetThemes(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		customThemes, customThemeOrder, themeProblems = map[string]theme{}, nil, nil
		applyTheme(themeLight)
	})
}

// Verifies: REQ-THM-001
func TestParseColor(t *testing.T) {
	ok := map[string]string{"#fff": "#fff", "#FFAA00": "#ffaa00", " #0a0b0c ": "#0a0b0c", "0": "0", "255": "255", "42": "42"}
	for in, want := range ok {
		c, set, err := parseColor(in)
		if err != nil || !set || string(c) != want {
			t.Errorf("parseColor(%q) = %q %v %v, want %q", in, c, set, err, want)
		}
	}
	if _, set, err := parseColor("  "); set || err != nil {
		t.Error("blank keeps the base colour without an error")
	}
	for _, bad := range []string{"256", "999", "#ggg", "#12345", "#1234567", "red", "rgb(1,2,3)", "-1", "#", "1e2"} {
		if _, _, err := parseColor(bad); err == nil {
			t.Errorf("parseColor(%q) must fail", bad)
		}
	}
}

// Verifies: REQ-THM-002
func TestBuildThemeOverridesTheBase(t *testing.T) {
	th, err := buildTheme(themeFile{Base: "dark", Bg: "#000000", Accent: "#ff0000", InputBg: "17"})
	if err != nil {
		t.Fatal(err)
	}
	if th.bg != "#000000" || th.accent != "#ff0000" || th.inputBg != "17" {
		t.Fatalf("overrides lost: %+v", th)
	}
	if th.text != themes[themeDark].text || th.online != themes[themeDark].online {
		t.Fatal("colours that are not overridden must come from the base")
	}
	// default base is light
	th, err = buildTheme(themeFile{Text: "#111111"})
	if err != nil || th.border != themes[themeLight].border || th.text != "#111111" {
		t.Fatalf("default base: %+v %v", th, err)
	}
	// every field is wired to its own key
	all := themeFile{Base: "light", Bg: "#000001", Text: "#000002", Title: "#000003", Accent: "#000004", Muted: "#000005",
		Border: "#000006", BorderDim: "#000007", BorderFocus: "#000008", Error: "#000009", Search: "#00000a",
		Success: "#00000b", Info: "#00000c", Header: "#00000d", HeaderStrong: "#00000e", Highlight: "#00000f",
		Online: "#000010", Offline: "#000011", Warn: "#000012", Dim: "#000013", InputFg: "#000014", InputBg: "#000015"}
	th, err = buildTheme(all)
	if err != nil {
		t.Fatal(err)
	}
	got := []lipgloss.Color{th.bg, th.text, th.title, th.accent, th.muted, th.border, th.borderDim, th.borderFocus, th.errc, th.search,
		th.success, th.info, th.header, th.headerStrong, th.highlight, th.online, th.offline, th.warn, th.dim, th.inputFg, th.inputBg}
	seen := map[lipgloss.Color]bool{}
	for _, c := range got {
		if seen[c] || !strings.HasPrefix(string(c), "#0000") {
			t.Fatalf("each key must set its own field: %v", got)
		}
		seen[c] = true
	}
	// errors
	if _, err := buildTheme(themeFile{Base: "nope"}); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Errorf("unknown base: %v", err)
	}
	if _, err := buildTheme(themeFile{Accent: "blue"}); err == nil || !strings.Contains(err.Error(), "accent") {
		t.Errorf("a bad colour must name its key: %v", err)
	}
}

// Verifies: REQ-THM-003
func TestLoadCustomThemesAcceptsGoodAndReportsBadFiles(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "ocean.json", `{"base":"dark","bg":"#0b1e2d","accent":"#4fc3f7"}`)
	writeTheme(t, dir, "my_theme-2.json", `{}`)
	writeTheme(t, dir, "notes.txt", `ignored`)
	writeTheme(t, dir, "Bad Name.json", `{}`)
	writeTheme(t, dir, "dark.json", `{}`)
	writeTheme(t, dir, "broken.json", `{not json`)
	writeTheme(t, dir, "typo.json", `{"acent":"#fff"}`)
	writeTheme(t, dir, "color.json", `{"accent":"nope"}`)
	writeTheme(t, dir, "base.json", `{"base":"zzz"}`)
	writeTheme(t, dir, "big.json", `{"base":"`+strings.Repeat("x", maxThemeFileLen)+`"}`)
	if err := os.Mkdir(filepath.Join(dir, "folder.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "unreadable.json")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "dangling.json")); err != nil {
		t.Fatal(err)
	}

	got, problems := loadCustomThemes(dir)
	if len(got) != 2 || got["ocean"].bg != "#0b1e2d" || got["ocean"].text != themes[themeDark].text {
		t.Fatalf("loaded = %v", got)
	}
	if _, ok := got["my_theme-2"]; !ok {
		t.Fatal("underscore and digits are allowed in names")
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"Bad Name.json: the file name", "dark.json: \"dark\" is a built-in", "broken.json: invalid JSON",
		"typo.json: invalid JSON", "color.json: accent", "base.json: base \"zzz\"", "big.json: the file is larger", "unreadable.json: cannot read", "dangling.json: open",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems lack %q:\n%s", want, joined)
		}
	}
	if len(problems) != 9 {
		t.Errorf("one message per bad file expected, got %d:\n%s", len(problems), joined)
	}

	// a missing directory is normal
	if got, problems := loadCustomThemes(filepath.Join(dir, "missing")); len(got) != 0 || problems != nil {
		t.Fatalf("missing dir: %v %v", got, problems)
	}
}

// Verifies: REQ-THM-004
func TestCustomThemesJoinTheCycleAndCanBeSelected(t *testing.T) {
	resetThemes(t)
	cfg := t.TempDir()
	t.Setenv(util.ConfigDirEnv, cfg)
	writeTheme(t, filepath.Join(cfg, themesDirName), "zeta.json", `{"base":"dark","accent":"#112233"}`)
	writeTheme(t, filepath.Join(cfg, themesDirName), "alpha.json", `{"accent":"#445566"}`)
	loadThemes()

	names := themeNames()
	want := append(append([]string(nil), themeOrder...), "alpha", "zeta")
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", names, want)
	}
	// the cycle visits built-ins, then custom themes in name order, then wraps
	n := themeLight
	var visited []string
	for range names {
		visited = append(visited, n)
		n = nextTheme(n)
	}
	if strings.Join(visited, ",") != strings.Join(want, ",") || n != themeLight {
		t.Fatalf("cycle = %v then %s", visited, n)
	}
	if nextTheme("unknown") != themeLight {
		t.Fatal("an unknown name restarts the cycle")
	}

	if got := loadUISettings(map[string]string{"theme": "zeta"}).theme; got != "zeta" {
		t.Fatalf("a stored custom theme must load, got %q", got)
	}
	applyTheme("zeta")
	if cur.accent != "#112233" || cur.bg != themes[themeDark].bg {
		t.Fatalf("applyTheme must use the custom palette: %+v", cur)
	}
	if loadUISettings(map[string]string{"theme": "deleted-theme"}).theme != themeLight {
		t.Fatal("a stored theme whose file is gone falls back to light")
	}
}

// Verifies: REQ-THM-005
func TestSettingsWindowReportsCustomThemesAndProblems(t *testing.T) {
	resetThemes(t)
	cfg := t.TempDir()
	t.Setenv(util.ConfigDirEnv, cfg)
	dir := filepath.Join(cfg, themesDirName)
	writeTheme(t, dir, "ok.json", `{}`)
	m := model{set: loadUISettings(nil), width: 120}
	loadThemes()
	out := m.renderSettings()
	if !strings.Contains(out, "1 loaded") || strings.Contains(out, "⚠") {
		t.Fatalf("one good theme, no problems:\n%s", out)
	}

	for i := 0; i < 5; i++ {
		writeTheme(t, dir, "bad"+string(rune('a'+i))+".json", `{nope`)
	}
	loadThemes()
	out = m.renderSettings()
	if strings.Count(out, "⚠") != 3 || !strings.Contains(out, "and 2 more problems") {
		t.Fatalf("three problems are listed, the rest counted:\n%s", out)
	}

	// no themes directory: nothing is shown
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	loadThemes()
	if strings.Contains(m.renderSettings(), "custom themes") {
		t.Fatal("no custom themes, no line")
	}
}

// Verifies: REQ-THM-006
func TestLoadThemesWithoutAConfigDirectoryIsHarmless(t *testing.T) {
	resetThemes(t)
	if os.Getenv("HOME") == "" {
		t.Skip()
	}
	t.Setenv(util.ConfigDirEnv, "")
	t.Setenv("HOME", "")
	loadThemes()
	if len(customThemes) != 0 || themeProblems != nil {
		t.Fatalf("%v %v", customThemes, themeProblems)
	}
}

package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Custom themes from files. Every ~/.ts-cli/themes/<name>.json is a theme
// called <name>. A file names a built-in theme as its "base" (default
// "light") and overrides any colours; colours it leaves out come from the
// base. Colours are "#rgb", "#rrggbb" or an ANSI number 0-255. A bad file is
// skipped and reported in the settings window; it never stops the program.

const (
	themesDirName   = "themes"
	maxThemeFileLen = 64 << 10
)

var (
	customThemes     = map[string]theme{}
	customThemeOrder []string // names of customThemes, sorted
	themeProblems    []string // why files were skipped, for the settings window
)

// themeFile is the JSON shape. Unknown keys are an error so typos show up.
type themeFile struct {
	Base string `json:"base"`

	Bg           string `json:"bg"`
	Text         string `json:"text"`
	Title        string `json:"title"`
	Accent       string `json:"accent"`
	Muted        string `json:"muted"`
	Border       string `json:"border"`
	BorderDim    string `json:"border_dim"`
	BorderFocus  string `json:"border_focus"`
	Error        string `json:"error"`
	Search       string `json:"search"`
	Success      string `json:"success"`
	Info         string `json:"info"`
	Header       string `json:"header"`
	HeaderStrong string `json:"header_strong"`
	Highlight    string `json:"highlight"`
	Online       string `json:"online"`
	Offline      string `json:"offline"`
	Warn         string `json:"warn"`
	Dim          string `json:"dim"`
	InputFg      string `json:"input_fg"`
	InputBg      string `json:"input_bg"`
}

var (
	themeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	hexColorRe  = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	ansiColorRe = regexp.MustCompile(`^[0-9]{1,3}$`)
)

// parseColor validates one colour value. An empty string means "keep the
// base's colour".
func parseColor(s string) (lipgloss.Color, bool, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", false, nil
	case hexColorRe.MatchString(s):
		return lipgloss.Color(strings.ToLower(s)), true, nil
	case ansiColorRe.MatchString(s):
		if n, _ := strconv.Atoi(s); n <= 255 {
			return lipgloss.Color(s), true, nil
		}
	}
	return "", false, fmt.Errorf("%q is not a colour (use #rrggbb, #rgb or 0-255)", s)
}

// buildTheme applies the overrides of f to its base theme.
func buildTheme(f themeFile) (theme, error) {
	base := f.Base
	if base == "" {
		base = themeLight
	}
	t, ok := themes[base]
	if !ok {
		return theme{}, fmt.Errorf("base %q is not a built-in theme (%s)", base, strings.Join(themeOrder, ", "))
	}
	fields := []struct {
		key string
		val string
		dst *lipgloss.Color
	}{
		{"bg", f.Bg, &t.bg}, {"text", f.Text, &t.text}, {"title", f.Title, &t.title},
		{"accent", f.Accent, &t.accent}, {"muted", f.Muted, &t.muted}, {"border", f.Border, &t.border},
		{"border_dim", f.BorderDim, &t.borderDim}, {"border_focus", f.BorderFocus, &t.borderFocus},
		{"error", f.Error, &t.errc}, {"search", f.Search, &t.search}, {"success", f.Success, &t.success},
		{"info", f.Info, &t.info}, {"header", f.Header, &t.header}, {"header_strong", f.HeaderStrong, &t.headerStrong},
		{"highlight", f.Highlight, &t.highlight}, {"online", f.Online, &t.online}, {"offline", f.Offline, &t.offline},
		{"warn", f.Warn, &t.warn}, {"dim", f.Dim, &t.dim}, {"input_fg", f.InputFg, &t.inputFg}, {"input_bg", f.InputBg, &t.inputBg},
	}
	for _, fl := range fields {
		c, set, err := parseColor(fl.val)
		if err != nil {
			return theme{}, fmt.Errorf("%s: %w", fl.key, err)
		}
		if set {
			*fl.dst = c
		}
	}
	return t, nil
}

// loadCustomThemes reads every *.json in dir. It returns the valid themes and
// one message per skipped file.
func loadCustomThemes(dir string) (map[string]theme, []string) {
	out := map[string]theme{}
	var problems []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out, nil // no themes directory is normal
	}
	for _, e := range entries {
		file := e.Name()
		if e.IsDir() || !strings.HasSuffix(file, ".json") {
			continue
		}
		name := strings.TrimSuffix(file, ".json")
		fail := func(format string, args ...any) {
			problems = append(problems, file+": "+fmt.Sprintf(format, args...))
		}
		if !themeNameRe.MatchString(name) {
			fail("the file name must be 1-32 characters of a-z, 0-9, - or _ (lower case)")
			continue
		}
		if _, builtin := themes[name]; builtin {
			fail("%q is a built-in theme name; pick another file name", name)
			continue
		}
		data, err := readLimited(filepath.Join(dir, file))
		if err != nil {
			fail("%v", err)
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		var f themeFile
		if err := dec.Decode(&f); err != nil {
			fail("invalid JSON: %v", err)
			continue
		}
		t, err := buildTheme(f)
		if err != nil {
			fail("%v", err)
			continue
		}
		out[name] = t
	}
	return out, problems
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxThemeFileLen+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the file: %w", err)
	}
	if len(data) > maxThemeFileLen {
		return nil, fmt.Errorf("the file is larger than %d bytes", maxThemeFileLen)
	}
	return data, nil
}

// loadThemes (re)loads the custom themes from ~/.ts-cli/themes.
func loadThemes() {
	customThemes, themeProblems = map[string]theme{}, nil
	customThemeOrder = nil
	dir, err := util.ConfigDir()
	if err != nil {
		return
	}
	customThemes, themeProblems = loadCustomThemes(filepath.Join(dir, themesDirName))
	for name := range customThemes {
		customThemeOrder = append(customThemeOrder, name)
	}
	sort.Strings(customThemeOrder)
}

package tui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
)

// Personal data kept only in config.json: pinned machines (shown at the top
// of the list, key *), a note per machine (key N), and small helpers on the
// list: y/Y copy the address or name, E exports the visible list.

// Personal is what NewModel takes from the configuration besides credentials.
type Personal struct {
	Pinned []string
	Notes  map[string]string
}

// pinMark is shown before the name of a pinned machine.
const pinMark = "★ "

func (m model) isPinned(d client.Device) bool { return m.pinned[machineKey(d)] }

// togglePin pins or unpins the highlighted machine; the cursor stays on it.
func (m model) togglePin() (tea.Model, tea.Cmd) {
	d, ok := m.targetDevice()
	if !ok {
		return m, nil
	}
	key := machineKey(d)
	pinned := make(map[string]bool, len(m.pinned)+1)
	for k, v := range m.pinned {
		pinned[k] = v
	}
	if pinned[key] {
		delete(pinned, key)
		m.notify.info = "Unpinned " + displayName(d)
	} else {
		pinned[key] = true
		m.notify.info = "Pinned " + displayName(d) + " to the top"
	}
	m.pinned = pinned
	m.refilterKeepCursor()
	return m, storePinned(key, pinned[key])
}

func storePinned(key string, on bool) tea.Cmd {
	return func() tea.Msg {
		return personalSavedMsg{err: config.Update(func(cfg *config.Config) error {
			cfg.SetPinned(key, on)
			return nil
		})}
	}
}

func storeNote(key, note string) tea.Cmd {
	return func() tea.Msg {
		return personalSavedMsg{err: config.Update(func(cfg *config.Config) error {
			cfg.SetNote(key, note)
			return nil
		})}
	}
}

// personalSavedMsg is the result of writing pins or notes to config.json.
type personalSavedMsg struct{ err error }

// promptNote asks for the note of the highlighted machine.
func (m model) promptNote() (tea.Model, tea.Cmd) {
	d, ok := m.targetDevice()
	if !ok {
		return m, nil
	}
	key := machineKey(d)
	current := m.notes[key]
	m.input = textInput{
		mode: inputText, label: "Note for " + shortHost(displayName(d)), value: current, cursor: len([]rune(current)),
		hint: "free text, stored only in config.json; empty removes it",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			note := strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(v))
			notes := make(map[string]string, len(m.notes)+1)
			for k, n := range m.notes {
				notes[k] = n
			}
			if note == "" {
				delete(notes, key)
			} else {
				notes[key] = note
			}
			m.notes = notes
			return m, storeNote(key, note)
		},
	}
	return m, nil
}

// copyAddress copies the highlighted machine's IP (or its name).
func (m model) copyAddress(name bool) (tea.Model, tea.Cmd) {
	d, ok := m.targetDevice()
	if !ok {
		return m, nil
	}
	text := ""
	switch {
	case name:
		text = shortHost(displayName(d))
	case len(d.Addresses) > 0:
		text = d.Addresses[0]
	default:
		m.notify.sshError = fmt.Errorf("%s has no IP address", displayName(d))
		return m, nil
	}
	return m, copyTextToClipboard(text)
}

// exportRow is one machine of an export.
type exportRow struct {
	Account  string   `json:"account"`
	Name     string   `json:"name"`
	Hostname string   `json:"hostname"`
	Address  string   `json:"address"`
	OS       string   `json:"os"`
	Tags     []string `json:"tags"`
	Online   bool     `json:"online"`
	LastSeen string   `json:"last_seen"`
	Note     string   `json:"note"`
}

func (m model) exportRows() []exportRow {
	rows := make([]exportRow, 0, len(m.list.filteredDevices))
	for _, d := range m.list.filteredDevices {
		addr := ""
		if len(d.Addresses) > 0 {
			addr = d.Addresses[0]
		}
		seen := ""
		if !d.LastSeen.IsZero() {
			seen = d.LastSeen.UTC().Format(time.RFC3339)
		}
		tags := d.Tags
		if tags == nil {
			tags = []string{}
		}
		rows = append(rows, exportRow{
			Account: d.AccountName, Name: displayName(d), Hostname: d.Hostname, Address: addr, OS: d.OS,
			Tags: tags, Online: m.isReachable(d), LastSeen: seen, Note: m.notes[machineKey(d)],
		})
	}
	return rows
}

// formulaSafe keeps a spreadsheet from running a cell that starts with = + - @.
func formulaSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// renderExport produces the file content: JSON for a .json path, CSV otherwise.
func renderExport(rows []exportRow, path string) ([]byte, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return json.MarshalIndent(rows, "", "  ")
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"account", "name", "hostname", "address", "os", "tags", "online", "last_seen", "note"})
	for _, r := range rows {
		_ = w.Write([]string{formulaSafe(r.Account), formulaSafe(r.Name), formulaSafe(r.Hostname), r.Address, formulaSafe(r.OS),
			formulaSafe(strings.Join(r.Tags, " ")), fmt.Sprint(r.Online), r.LastSeen, formulaSafe(r.Note)})
	}
	w.Flush()
	return []byte(b.String()), w.Error()
}

// promptExport asks where to save the visible machines.
func (m model) promptExport() (tea.Model, tea.Cmd) {
	if len(m.list.filteredDevices) == 0 {
		return m, nil
	}
	def := "~/ts-cli-machines.csv"
	m.input = textInput{
		mode: inputText, label: fmt.Sprintf("Export %d machines to", len(m.list.filteredDevices)), value: def, cursor: len([]rune(def)),
		path: pathAny,
		hint: "a .json path writes JSON, anything else CSV; the visible list, with notes",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			path := expandHome(v)
			if path == "" {
				return m, nil
			}
			data, err := renderExport(m.exportRows(), path)
			if err == nil {
				err = writeExport(path, data)
			}
			if err != nil {
				m.notify.sshError = fmt.Errorf("export failed: %w", err)
				return m, nil
			}
			m.notify.info = fmt.Sprintf("✓ Exported %d machines to %s", len(m.list.filteredDevices), path)
			return m, nil
		},
	}
	return m, nil
}

// writeExport writes data with mode 0600 (it lists the whole tailnet).
func writeExport(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Update notice: once a day ts-cli asks GitHub for the latest release (one
// unauthenticated GET, nothing about you is sent) and shows a line when a
// newer version exists. F12 → "Check for updates" switches it off. The answer
// is cached in update_check.json next to config.json.

const updateCheckEvery = 24 * time.Hour

// latestRelease is the GitHub request; a seam for tests.
var latestRelease = client.LatestRelease

type updateCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Tag       string    `json:"tag"`
}

// updateMsg carries the latest known release tag (empty when unknown).
type updateMsg struct{ tag string }

// updateCheckCmd returns the cached tag when it is fresh, else asks GitHub
// and caches the answer. Failures are silent: no notice is better than noise.
func updateCheckCmd() tea.Cmd {
	return func() tea.Msg {
		dir, err := util.ConfigDir()
		if err != nil {
			return updateMsg{}
		}
		file := filepath.Join(dir, "update_check.json")
		var cache updateCache
		if data, err := os.ReadFile(file); err == nil && json.Unmarshal(data, &cache) == nil &&
			cache.Tag != "" && time.Since(cache.CheckedAt) < updateCheckEvery {
			return updateMsg{tag: cache.Tag}
		}
		rel, err := latestRelease()
		if err != nil {
			return updateMsg{}
		}
		if data, err := json.Marshal(updateCache{CheckedAt: time.Now(), Tag: rel.Tag}); err == nil {
			if os.MkdirAll(dir, 0o700) == nil {
				_ = os.WriteFile(file, data, 0o600)
			}
		}
		return updateMsg{tag: rel.Tag}
	}
}

// handleUpdate shows the notice when the release is newer than this version.
func (m model) handleUpdate(msg updateMsg) (tea.Model, tea.Cmd) {
	if msg.tag != "" && client.CompareVersions(m.version, msg.tag) < 0 {
		m.notify.info = fmt.Sprintf("⬆ ts-cli %s is available (you have %s) · run: ts-cli upgrade", msg.tag, m.version)
	}
	return m, nil
}

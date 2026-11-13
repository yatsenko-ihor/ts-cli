package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Monitoring: a latency history per machine, and an optional background
// monitor (F12 → "Monitor machines") that pings the visible machines in turn
// and says when one stops or starts answering.

const (
	monitorInterval = 30 * time.Second
	monitorFirst    = 2 * time.Second
	monitorBatch    = 6 // machines pinged per round
	maxSamples      = 60
	alertAfterFails = 2 // consecutive failures before a machine is reported down
	maxAlerts       = 20
)

// sample is one ping result.
type sample struct {
	at  time.Time
	lat time.Duration
	ok  bool
}

// monitorTickMsg starts one monitor round; seq tells the loops of earlier
// on/off switches apart.
type monitorTickMsg struct{ seq int }

func tickMonitor(seq int, after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg { return monitorTickMsg{seq: seq} })
}

// recordSample keeps the last maxSamples results of a machine.
func (m *model) recordSample(key string, ok bool, lat time.Duration) {
	if m.samples == nil {
		m.samples = map[string][]sample{}
	}
	s := append(m.samples[key], sample{at: time.Now(), lat: lat, ok: ok})
	if len(s) > maxSamples {
		s = s[len(s)-maxSamples:]
	}
	m.samples[key] = s
}

// sparkBars are the eight block heights of a sparkline.
var sparkBars = []rune("▁▂▃▄▅▆▇█")

// sparkline draws the last n samples: bar height is the latency relative to
// the slowest of them, "×" is a failed ping.
func sparkline(samples []sample, n int) string {
	if len(samples) > n {
		samples = samples[len(samples)-n:]
	}
	var slowest time.Duration
	for _, s := range samples {
		if s.ok && s.lat > slowest {
			slowest = s.lat
		}
	}
	var b strings.Builder
	for _, s := range samples {
		switch {
		case !s.ok:
			b.WriteRune('×')
		case slowest == 0:
			b.WriteRune(sparkBars[0])
		default:
			b.WriteRune(sparkBars[int(s.lat*time.Duration(len(sparkBars)-1)/slowest)])
		}
	}
	return b.String()
}

// latencyText summarises a machine's history: "▁▂▂▅× 12ms (min 3 · avg 9 · max 40) · 1 of 5 failed".
func latencyText(samples []sample) string {
	if len(samples) == 0 {
		return ""
	}
	var sum, lo, hi time.Duration
	ok := 0
	for _, s := range samples {
		if !s.ok {
			continue
		}
		if ok == 0 || s.lat < lo {
			lo = s.lat
		}
		if s.lat > hi {
			hi = s.lat
		}
		sum += s.lat
		ok++
	}
	text := sparkline(samples, 24)
	if ok == 0 {
		return text + fmt.Sprintf(" no reply (%d tries)", len(samples))
	}
	ms := func(d time.Duration) string { return fmt.Sprintf("%d", d.Round(time.Millisecond).Milliseconds()) }
	text += fmt.Sprintf(" %sms (min %s · avg %s · max %s)", ms(samples[len(samples)-1].lat), ms(lo), ms(sum/time.Duration(ok)), ms(hi))
	if failed := len(samples) - ok; failed > 0 {
		text += fmt.Sprintf(" · %d of %d failed", failed, len(samples))
	}
	return text
}

// deviceByKey finds a machine of the list by its key.
func (m model) deviceByKey(key string) (client.Device, bool) {
	for _, d := range m.list.devices {
		if machineKey(d) == key {
			return d, true
		}
	}
	return client.Device{}, false
}

// pushAlert records an event and shows it on the main screen.
func (m *model) pushAlert(text string) {
	m.alerts = append(m.alerts, time.Now().Format("15:04")+" "+text)
	if len(m.alerts) > maxAlerts {
		m.alerts = m.alerts[len(m.alerts)-maxAlerts:]
	}
	m.notify.info = text
}

// noteMonitorResult turns a monitor ping into an alert when a machine that
// answered keeps failing, or when one reported down answers again.
func (m *model) noteMonitorResult(msg pingMsg, wasReachable bool) {
	if m.failStreak == nil {
		m.failStreak, m.downAlerted, m.failedFromUp = map[string]int{}, map[string]bool{}, map[string]bool{}
	}
	name := shortHost(msg.name)
	if msg.ok {
		m.failStreak[msg.key] = 0
		if m.downAlerted[msg.key] {
			delete(m.downAlerted, msg.key)
			m.pushAlert("✓ " + name + " is answering again")
		}
		return
	}
	m.failStreak[msg.key]++
	if m.failStreak[msg.key] == 1 {
		m.failedFromUp[msg.key] = wasReachable // the state before the streak began
	}
	if m.failStreak[msg.key] >= alertAfterFails && m.failedFromUp[msg.key] && !m.downAlerted[msg.key] {
		m.downAlerted[msg.key] = true
		m.pushAlert("⚠ " + name + " stopped answering")
	}
}

// startMonitor (re)starts the monitor loop when the setting is on.
func (m model) startMonitor() (model, tea.Cmd) {
	m.monitorSeq++
	if !m.set.monitor {
		return m, nil
	}
	return m, tickMonitor(m.monitorSeq, monitorFirst)
}

// handleMonitorTick pings the next machines in turn and schedules the next round.
func (m model) handleMonitorTick(msg monitorTickMsg) (tea.Model, tea.Cmd) {
	if !m.set.monitor || msg.seq != m.monitorSeq {
		return m, nil
	}
	cmds := []tea.Cmd{tickMonitor(m.monitorSeq, monitorInterval)}
	devs := m.list.filteredDevices
	started, last := 0, -1
	for i := 0; i < len(devs) && started < monitorBatch; i++ {
		idx := (m.monitorNext + i) % len(devs)
		d := devs[idx]
		key := machineKey(d)
		if !m.canPing(d) || m.pinging[key] || m.pingIsFresh(d) {
			continue
		}
		// Machines the API calls offline are skipped unless they are being watched.
		if !isDeviceOnline(d) && !m.downAlerted[key] && m.ping[key] != pingOK {
			continue
		}
		if m.pinging == nil {
			m.pinging = map[string]bool{}
		}
		m.pinging[key] = true
		cmds = append(cmds, pingDevice(d, pingMonitor, manualPingWait))
		started++
		last = idx
	}
	if last >= 0 {
		m.monitorNext = (last + 1) % len(devs)
	}
	return m, tea.Batch(cmds...)
}

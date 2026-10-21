package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Connection details of a machine: how traffic reaches it (direct or through
// a DERP relay) and how fast. Two sources:
//
//   - `tailscale ping` (Machine tab → "Ping with details"): measured now.
//   - `tailscale status --json`: what the local daemon knows about every peer
//     of the active tailnet without sending anything (path, traffic, whether
//     the peer offers Tailscale SSH). Loaded at start and on reload.

// ─── tailscale ping ──────────────────────────────────────────────────────────

// pingDetail summarises one `tailscale ping --c N` run.
type pingDetail struct {
	ok       bool          // at least one pong
	direct   bool          // the last pong came over a direct path
	via      string        // direct endpoint, or relay region
	latency  time.Duration // of the last pong
	pongs    int
	directAt int // 1-based index of the first direct pong, 0 = never
	err      string
	at       time.Time
}

var pongRe = regexp.MustCompile(`^pong from \S+ \(([^)]+)\) via (.+?) in (\S+)$`)

// parsePingOutput reads the output of `tailscale ping`:
//
//	pong from box (100.64.0.5) via DERP(fra) in 45ms
//	pong from box (100.64.0.5) via 192.168.1.20:41641 in 3ms
func parsePingOutput(out string) pingDetail {
	var d pingDetail
	for _, line := range strings.Split(out, "\n") {
		m := pongRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		lat, err := time.ParseDuration(m[3])
		if err != nil {
			continue
		}
		d.ok = true
		d.pongs++
		d.latency = lat
		via := m[2]
		if region, found := strings.CutPrefix(via, "DERP("); found {
			d.direct, d.via = false, strings.TrimSuffix(region, ")")
			continue
		}
		d.direct, d.via = true, via
		if d.directAt == 0 {
			d.directAt = d.pongs
		}
	}
	return d
}

// text is the one-line description.
func (d pingDetail) text() string {
	switch {
	case d.err != "":
		return "no reply: " + d.err
	case !d.ok:
		return "no reply"
	case d.direct && d.directAt > 1:
		return fmt.Sprintf("direct %s · %s (after %d pings via relay)", d.via, roundLatency(d.latency), d.directAt-1)
	case d.direct:
		return fmt.Sprintf("direct %s · %s", d.via, roundLatency(d.latency))
	}
	return fmt.Sprintf("relay %s · %s (no direct path yet)", d.via, roundLatency(d.latency))
}

func roundLatency(l time.Duration) string {
	if l < time.Millisecond {
		return "<1ms"
	}
	return l.Round(time.Millisecond).String()
}

// pingDetailCount is how many pings are sent at most (it stops at the first
// direct pong).
const pingDetailCount = 5

// tailscalePingN is a test seam.
var tailscalePingN = func(ctx context.Context, addr string, count int) ([]byte, error) {
	return exec.CommandContext(ctx, "tailscale", "ping", "--c", strconv.Itoa(count), "--timeout", "3s", addr).CombinedOutput()
}

// pingDetailMsg is the result of a detailed ping.
type pingDetailMsg struct {
	key, name string
	detail    pingDetail
}

func pingDetailCmd(device client.Device) tea.Cmd {
	key, name := machineKey(device), displayName(device)
	return func() tea.Msg {
		fail := func(err string) tea.Msg {
			return pingDetailMsg{key: key, name: name, detail: pingDetail{err: err, at: time.Now()}}
		}
		if len(device.Addresses) == 0 {
			return fail("no IP address")
		}
		addr := device.Addresses[0]
		if net.ParseIP(addr) == nil {
			return fail(fmt.Sprintf("invalid address %q", addr))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		out, err := tailscalePingN(ctx, addr, pingDetailCount)
		d := parsePingOutput(string(out))
		d.at = time.Now()
		if !d.ok {
			switch {
			case isNotFound(err):
				d.err = "tailscale CLI not found in PATH"
			case err != nil:
				d.err = firstLine(string(out), err)
			}
		}
		return pingDetailMsg{key: key, name: name, detail: d}
	}
}

func isNotFound(err error) bool {
	_, ok := err.(*exec.Error)
	return ok
}

func firstLine(out string, fallback error) string {
	line := strings.TrimSpace(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return fallback.Error()
	}
	return line
}

// ─── tailscale status ────────────────────────────────────────────────────────

// peerInfo is what the local daemon reports about one peer.
type peerInfo struct {
	online, active, ssh, exitNode bool
	curAddr, relay                string
	rx, tx                        int64
	handshake                     time.Time
}

type statusJSON struct {
	Peer map[string]struct {
		TailscaleIPs  []string  `json:"TailscaleIPs"`
		Online        bool      `json:"Online"`
		Active        bool      `json:"Active"`
		ExitNode      bool      `json:"ExitNode"`
		CurAddr       string    `json:"CurAddr"`
		Relay         string    `json:"Relay"`
		RxBytes       int64     `json:"RxBytes"`
		TxBytes       int64     `json:"TxBytes"`
		LastHandshake time.Time `json:"LastHandshake"`
		SSHHostKeys   []string  `json:"SSHHostKeys"`
	} `json:"Peer"`
}

// parsePeers indexes the peers of `tailscale status --json` by each of
// their Tailscale IP addresses.
func parsePeers(data []byte) (map[string]peerInfo, error) {
	var st statusJSON
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	out := map[string]peerInfo{}
	for _, p := range st.Peer {
		info := peerInfo{
			online: p.Online, active: p.Active, exitNode: p.ExitNode,
			curAddr: p.CurAddr, relay: p.Relay, rx: p.RxBytes, tx: p.TxBytes,
			handshake: p.LastHandshake, ssh: len(p.SSHHostKeys) > 0,
		}
		for _, ip := range p.TailscaleIPs {
			out[ip] = info
		}
	}
	return out, nil
}

// pathText says how the daemon currently reaches the peer.
func (p peerInfo) pathText() string {
	switch {
	case p.curAddr != "":
		return "direct " + p.curAddr
	case p.relay != "":
		return "relay " + p.relay
	case !p.online:
		return "offline"
	}
	return "idle (no traffic yet)"
}

// trafficText is "↓ 1.2 MB ↑ 340 kB", or "" when nothing was transferred.
func (p peerInfo) trafficText() string {
	if p.rx == 0 && p.tx == 0 {
		return ""
	}
	return "↓ " + humanBytes(p.rx) + "  ↑ " + humanBytes(p.tx)
}

func humanBytes(n int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + " " + units[i]
}

// peersMsg is the result of loading the peer status.
type peersMsg struct {
	peers map[string]peerInfo
	err   error
}

// tailscaleStatusJSON is a test seam.
var tailscaleStatusJSON = func() ([]byte, error) {
	return exec.Command("tailscale", "status", "--json").Output()
}

func loadPeersCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := tailscaleStatusJSON()
		if err != nil {
			return peersMsg{err: err}
		}
		peers, err := parsePeers(data)
		return peersMsg{peers: peers, err: err}
	}
}

// peerFor returns the daemon's view of device, if it has one.
func (m model) peerFor(d client.Device) (peerInfo, bool) {
	if len(d.Addresses) == 0 {
		return peerInfo{}, false
	}
	p, ok := m.peers[d.Addresses[0]]
	return p, ok
}

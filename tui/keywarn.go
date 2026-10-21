package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

// Device key reminders. A device whose key expires drops off the tailnet
// until someone logs in again, so keys that are expired or about to expire
// are flagged in the list and in a line on the main screen.

// deviceKeyState classifies d's key at now. Devices with key expiry disabled
// or without an expiry date are neither.
func deviceKeyState(d client.Device, now time.Time) (expired, soon bool) {
	if d.KeyExpiryDisabled || d.Expires.IsZero() {
		return false, false
	}
	if !d.Expires.After(now) {
		return true, false
	}
	return false, d.Expires.Sub(now) < keyWarnDays*24*time.Hour
}

// deviceKeyWarning is the main-screen line about device keys of the visible
// accounts, or "" when none is expired or expiring.
func (m model) deviceKeyWarning(now time.Time) string {
	expired, soon := 0, 0
	var firstName string
	var firstAt time.Time
	for _, d := range m.list.devices {
		if m.acct.accountHidden(d.AccountName) {
			continue
		}
		e, s := deviceKeyState(d, now)
		switch {
		case e:
			expired++
		case s:
			soon++
		default:
			continue
		}
		if firstAt.IsZero() || d.Expires.Before(firstAt) {
			firstAt, firstName = d.Expires, shortHost(displayName(d))
		}
	}
	if expired == 0 && soon == 0 {
		return ""
	}
	var parts []string
	if expired > 0 {
		parts = append(parts, fmt.Sprintf("%d expired", expired))
	}
	if soon > 0 {
		parts = append(parts, fmt.Sprintf("%d expire within %d days", soon, keyWarnDays))
	}
	when := "expired " + ago(firstAt)
	if firstAt.After(now) {
		when = fmt.Sprintf("in %dd", int(firstAt.Sub(now).Hours()/24))
	}
	return fmt.Sprintf("⚠ Device keys: %s (soonest: %s %s)  — Machine tab can disable expiry", strings.Join(parts, ", "), firstName, when)
}

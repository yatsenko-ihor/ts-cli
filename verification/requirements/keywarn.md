# Requirements: device key reminders (REQ-DKW) - tui/keywarn.go

- **REQ-DKW-001** A device key shall count as expired when its expiry date is not after now, and as expiring soon when it expires within 14 days; devices with key expiry disabled or without an expiry date shall be neither.
- **REQ-DKW-002** The main screen shall show one warning line counting expired and soon-expiring device keys of the visible accounts (hidden accounts and disabled expiry excluded), naming the machine with the earliest expiry and when; it shall be empty when nothing is expired or expiring.
- **REQ-DKW-003** The list shall flag both states: `exp` in the warning colour for soon and in the error colour for expired in the symbol style, ⏳ and 🔒 in the emoji style, and nothing for a key far from expiry.

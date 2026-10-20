package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// baseURL is the Tailscale API root. It is a variable so tests can point the
// client at an httptest server.
var baseURL = "https://api.tailscale.com/api/v2"

// errInvalidKey is returned when the API rejects the key (401/403). Callers
// classify on it with errors.Is rather than on server-supplied text.
var errInvalidKey = errors.New("invalid API key or insufficient permissions")

// maxResponseBytes caps how much of an API response we are willing to
// buffer; device lists are small, so this is purely a safety limit.
const maxResponseBytes = 16 << 20

// Client represents a Tailscale API client
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// Device represents a Tailscale device
type Device struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Hostname      string    `json:"hostname"`
	ClientVersion string    `json:"clientVersion"`
	OS            string    `json:"os"`
	Created       time.Time `json:"created"`
	LastSeen      time.Time `json:"lastSeen"`
	// ConnectedToControl: the device recently kept a TCP connection to the
	// Tailscale control server. The API omits lastSeen while this is true.
	ConnectedToControl        bool      `json:"connectedToControl"`
	Expires                   time.Time `json:"expires"`
	Authorized                bool      `json:"authorized"`
	IsExternal                bool      `json:"isExternal"`
	UpdateAvailable           bool      `json:"updateAvailable"`
	BlocksIncomingConnections bool      `json:"blocksIncomingConnections"`
	KeyExpiryDisabled         bool      `json:"keyExpiryDisabled"`
	Addresses                 []string  `json:"addresses"`
	Tags                      []string  `json:"tags,omitempty"`
	// Additional fields for multi-account support
	AccountName    string `json:"-"` // Not from API, added locally
	AccountTailnet string `json:"-"` // Not from API, added locally
}

// DevicesResponse represents the response from the devices API
type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Message string `json:"message"`
}

// NewClient creates a new Tailscale API client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doRequest performs an HTTP request to the Tailscale API
func (c *Client) doRequest(method, path string) (*http.Response, error) {
	url := fmt.Sprintf("%s%s", baseURL, path)

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use Bearer token authentication
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

// devicesPath builds the devices endpoint path for a tailnet. The tailnet is
// user input (an email or domain) and is path-escaped so characters such as
// '/' or '?' cannot alter the endpoint.
func devicesPath(tailnet string) string {
	return "/tailnet/" + url.PathEscape(tailnet) + "/devices"
}

// readBody reads at most maxResponseBytes of the response body.
func readBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
}

// apiError turns a non-2xx response into an error.
func apiError(resp *http.Response) error {
	body, _ := readBody(resp)
	var errResp ErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		return fmt.Errorf("API error: %s", errResp.Message)
	}
	return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
}

// ValidateAPIKey checks if the API key is valid by making a test request
func (c *Client) ValidateAPIKey(tailnet string) error {
	resp, err := c.doRequest("GET", devicesPath(tailnet))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errInvalidKey
	}

	if resp.StatusCode >= 400 {
		return apiError(resp)
	}

	return nil
}

// ListDevices retrieves all devices in the tailnet
func (c *Client) ListDevices(tailnet string) ([]Device, error) {
	resp, err := c.doRequest("GET", devicesPath(tailnet))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var devicesResp DevicesResponse
	if err := json.Unmarshal(body, &devicesResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return devicesResp.Devices, nil
}

// AccountDevices represents devices from a specific account
type AccountDevices struct {
	AccountName string
	Tailnet     string
	Devices     []Device
	Error       error
}

// ListDevicesFromAccounts retrieves devices from multiple accounts.
// Per-account failures are reported on stderr; use
// ListDevicesFromAccountsWithErrors when stderr is not available (TUI).
func ListDevicesFromAccounts(accounts []AccountInfo) []Device {
	devices, errs := ListDevicesFromAccountsWithErrors(accounts)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}
	return devices
}

// ListDevicesFromAccountsWithErrors retrieves devices from multiple accounts
// in parallel and returns the merged list together with one error per
// account that could not be queried.
func ListDevicesFromAccountsWithErrors(accounts []AccountInfo) ([]Device, []error) {
	var allDevices []Device
	var errs []error

	results := make(chan AccountDevices, len(accounts))

	for _, account := range accounts {
		go func(acc AccountInfo) {
			client := NewClient(acc.APIKey)
			devices, err := client.ListDevices(acc.Tailnet)
			results <- AccountDevices{
				AccountName: acc.Name,
				Tailnet:     acc.Tailnet,
				Devices:     devices,
				Error:       err,
			}
		}(account)
	}

	for i := 0; i < len(accounts); i++ {
		result := <-results
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("failed to fetch devices from account %s: %w", result.AccountName, result.Error))
			continue
		}

		for j := range result.Devices {
			result.Devices[j].AccountName = result.AccountName
			result.Devices[j].AccountTailnet = result.Tailnet
		}

		allDevices = append(allDevices, result.Devices...)
	}

	return allDevices, errs
}

// AccountInfo represents basic account information for device querying
type AccountInfo struct {
	Name    string
	APIKey  string
	Tailnet string
	Active  bool
	Hidden  bool
}

// KeyInfo is what the API reports about an API access token.
type KeyInfo struct {
	Expires time.Time
	Revoked bool
	Invalid bool
}

// APIKeyID extracts the key ID from a "tskey-api-<id>-<secret>" token. It
// returns "" for tokens of another shape.
func APIKeyID(apiKey string) string {
	rest, ok := strings.CutPrefix(apiKey, "tskey-api-")
	if !ok {
		return ""
	}
	id, _, ok := strings.Cut(rest, "-")
	if !ok {
		return ""
	}
	return id
}

// KeyInfo reads the expiry of the API key itself. Not every token may read
// its own record, so callers treat an error as "unknown", not as invalid.
func (c *Client) KeyInfo(tailnet, keyID string) (KeyInfo, error) {
	resp, err := c.doRequest("GET", "/tailnet/"+url.PathEscape(tailnet)+"/keys/"+url.PathEscape(keyID))
	if err != nil {
		return KeyInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return KeyInfo{}, apiError(resp)
	}
	body, err := readBody(resp)
	if err != nil {
		return KeyInfo{}, err
	}
	var raw struct {
		Expires time.Time `json:"expires"`
		Revoked time.Time `json:"revoked"`
		Invalid bool      `json:"invalid"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return KeyInfo{}, fmt.Errorf("failed to parse response: %w", err)
	}
	return KeyInfo{Expires: raw.Expires, Revoked: !raw.Revoked.IsZero(), Invalid: raw.Invalid}, nil
}

// Health is the result of checking one account's API key.
type Health struct {
	Valid      bool      // the key can list devices
	Err        error     // why it is not valid (or the network error)
	Unauth     bool      // the API rejected the key (401/403)
	Expires    time.Time // zero when unknown
	HasExpiry  bool
	ExpiryNote string // why the expiry is unknown, if it is
}

// CheckAccount verifies the key and, best effort, reads its expiry.
func CheckAccount(acc AccountInfo) Health {
	c := NewClient(acc.APIKey)
	h := Health{}
	if err := c.ValidateAPIKey(acc.Tailnet); err != nil {
		h.Err = err
		h.Unauth = errors.Is(err, errInvalidKey)
		return h
	}
	h.Valid = true
	id := APIKeyID(acc.APIKey)
	if id == "" {
		h.ExpiryNote = "unknown key format"
		return h
	}
	info, err := c.KeyInfo(acc.Tailnet, id)
	switch {
	case err != nil:
		h.ExpiryNote = "expiry not readable"
	case info.Revoked || info.Invalid:
		h.Valid = false
		h.Unauth = true
		h.Err = fmt.Errorf("API key is revoked or expired")
	case !info.Expires.IsZero():
		h.Expires, h.HasExpiry = info.Expires, true
	}
	return h
}

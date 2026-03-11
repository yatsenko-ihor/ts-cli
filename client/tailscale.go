package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	baseURL = "https://api.tailscale.com/api/v2"
)

// Client represents a Tailscale API client
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// Device represents a Tailscale device
type Device struct {
	ID                        string    `json:"id"`
	Name                      string    `json:"name"`
	Hostname                  string    `json:"hostname"`
	ClientVersion             string    `json:"clientVersion"`
	OS                        string    `json:"os"`
	Created                   time.Time `json:"created"`
	LastSeen                  time.Time `json:"lastSeen"`
	Authorized                bool      `json:"authorized"`
	IsExternal                bool      `json:"isExternal"`
	UpdateAvailable           bool      `json:"updateAvailable"`
	BlocksIncomingConnections bool      `json:"blocksIncomingConnections"`
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

// ValidateAPIKey checks if the API key is valid by making a test request
func (c *Client) ValidateAPIKey(tailnet string) error {
	resp, err := c.doRequest("GET", fmt.Sprintf("/tailnet/%s/devices", tailnet))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("invalid API key or insufficient permissions")
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("API error: %s", errResp.Message)
		}
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListDevices retrieves all devices in the tailnet
func (c *Client) ListDevices(tailnet string) ([]Device, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/tailnet/%s/devices", tailnet))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
			return nil, fmt.Errorf("API error: %s", errResp.Message)
		}
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
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

// ListDevicesFromAccounts retrieves devices from multiple accounts
func ListDevicesFromAccounts(accounts []AccountInfo) []Device {
	var allDevices []Device

	// Use a channel to collect results
	results := make(chan AccountDevices, len(accounts))

	// Query each account in parallel
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

	// Collect results
	for i := 0; i < len(accounts); i++ {
		result := <-results
		if result.Error != nil {
			// Log error but continue with other accounts
			fmt.Fprintf(os.Stderr, "Warning: Failed to fetch devices from account %s: %v\n", result.AccountName, result.Error)
			continue
		}

		// Tag each device with its account info
		for j := range result.Devices {
			result.Devices[j].AccountName = result.AccountName
			result.Devices[j].AccountTailnet = result.Tailnet
		}

		allDevices = append(allDevices, result.Devices...)
	}

	return allDevices
}

// AccountInfo represents basic account information for device querying
type AccountInfo struct {
	Name    string
	APIKey  string
	Tailnet string
	Active  bool
}

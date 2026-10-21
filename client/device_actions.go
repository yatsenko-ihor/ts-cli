package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Actions on one device (Tailscale API v2 /device/{id}/...). They need an API
// key that may write devices; a read-only key gets a 403 which is returned as
// the API's own message.

func devicePath(deviceID, suffix string) string {
	return "/device/" + url.PathEscape(deviceID) + suffix
}

// deviceAction sends the request and turns any non-2xx answer into an error.
func (c *Client) deviceAction(method, deviceID, suffix string, payload any) error {
	if deviceID == "" {
		return fmt.Errorf("device has no ID")
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	resp, err := c.doBody(method, devicePath(deviceID, suffix), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return apiError(resp)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	return nil
}

// SetKeyExpiryDisabled turns key expiry off (disabled=true: the device key
// never expires) or on again.
func (c *Client) SetKeyExpiryDisabled(deviceID string, disabled bool) error {
	return c.deviceAction(http.MethodPost, deviceID, "/key", map[string]bool{"keyExpiryDisabled": disabled})
}

// ExpireKey expires the device key now: the device must log in again.
func (c *Client) ExpireKey(deviceID string) error {
	return c.deviceAction(http.MethodPost, deviceID, "/expire", nil)
}

// RenameDevice sets the device's machine name (a DNS label, or a full name
// that the API normalises).
func (c *Client) RenameDevice(deviceID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	return c.deviceAction(http.MethodPost, deviceID, "/name", map[string]string{"name": name})
}

// SetTags replaces the device's tags (each "tag:name"). An empty list is
// refused: removing every tag turns a tagged device into a user device,
// which the API does not allow.
func (c *Client) SetTags(deviceID string, tags []string) error {
	if len(tags) == 0 {
		return fmt.Errorf("at least one tag is required")
	}
	for _, t := range tags {
		if !strings.HasPrefix(t, "tag:") || len(t) == len("tag:") {
			return fmt.Errorf("invalid tag %q (use tag:name)", t)
		}
	}
	return c.deviceAction(http.MethodPost, deviceID, "/tags", map[string][]string{"tags": tags})
}

// DeleteDevice removes the device from the tailnet.
func (c *Client) DeleteDevice(deviceID string) error {
	return c.deviceAction(http.MethodDelete, deviceID, "", nil)
}

// NormalizeTags turns user input ("server, tag:db,web") into API tags
// ("tag:server", "tag:db", "tag:web"), dropping empties and duplicates.
func NormalizeTags(input string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if !strings.HasPrefix(part, "tag:") {
			part = "tag:" + part
		}
		if !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

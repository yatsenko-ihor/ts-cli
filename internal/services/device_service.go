// Package services provides business logic layer, separating concerns
// from presentation (commands) and data access (client), following SOLID principles.
package services

import (
	"fmt"
	"os"
	"sync"

	"github.com/ihor/ts-cli/client"
)

// DeviceService handles device-related business logic
type DeviceService struct {
	client *client.Client
}

// NewDeviceService creates a new device service instance
func NewDeviceService(apiKey string) *DeviceService {
	return &DeviceService{
		client: client.NewClient(apiKey),
	}
}

// AccountDeviceResult represents the result of fetching devices from an account
type AccountDeviceResult struct {
	AccountName string
	Tailnet     string
	Devices     []client.Device
	Error       error
}

// ListDevices retrieves all devices for a specific tailnet
func (s *DeviceService) ListDevices(tailnet string) ([]client.Device, error) {
	devices, err := s.client.ListDevices(tailnet)
	if err != nil {
		return nil, fmt.Errorf("failed to list devices from tailnet %s: %w", tailnet, err)
	}
	return devices, nil
}

// ListDevicesFromMultipleAccounts fetches devices from multiple accounts concurrently
// and aggregates the results. This demonstrates the Single Responsibility Principle
// by handling only multi-account device aggregation logic.
func (s *DeviceService) ListDevicesFromMultipleAccounts(accounts []client.AccountInfo) []client.Device {
	if len(accounts) == 0 {
		return []client.Device{}
	}
	var (
		allDevices []client.Device
		mu         sync.Mutex
		wg         sync.WaitGroup
	)
	// Query each account concurrently
	for _, account := range accounts {
		wg.Add(1)
		go func(acc client.AccountInfo) {
			defer wg.Done()
			deviceService := NewDeviceService(acc.APIKey)
			devices, err := deviceService.ListDevices(acc.Tailnet)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to fetch devices from account %s: %v\n",
					acc.Name, err)
				return
			}
			// Tag each device with its account info
			for i := range devices {
				devices[i].AccountName = acc.Name
				devices[i].AccountTailnet = acc.Tailnet
			}
			mu.Lock()
			allDevices = append(allDevices, devices...)
			mu.Unlock()
		}(account)
	}
	wg.Wait()
	return allDevices
}

// FindDeviceByIdentifier locates a device by name, hostname, or ID
// This centralizes the device lookup logic following DRY principle.
func (s *DeviceService) FindDeviceByIdentifier(devices []client.Device, identifier string) *client.Device {
	for i := range devices {
		device := &devices[i]
		if device.Name == identifier ||
			device.Hostname == identifier ||
			device.ID == identifier {
			return device
		}
	}
	return nil
}

// ValidateAPIKey checks if the provided API key is valid for the given tailnet
func (s *DeviceService) ValidateAPIKey(tailnet string) error {
	if err := s.client.ValidateAPIKey(tailnet); err != nil {
		return fmt.Errorf("API key validation failed: %w", err)
	}
	return nil
}

// GetDevicePrimaryAddress returns the primary IP address of a device
func (s *DeviceService) GetDevicePrimaryAddress(device *client.Device) (string, error) {
	if device == nil {
		return "", fmt.Errorf("device is nil")
	}
	if len(device.Addresses) == 0 {
		return "", fmt.Errorf("device '%s' has no IP addresses", device.Name)
	}
	return device.Addresses[0], nil
}


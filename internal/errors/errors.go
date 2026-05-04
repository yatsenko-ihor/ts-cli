// Package errors provides centralized error handling and custom error types
// Following the Open/Closed Principle - open for extension, closed for modification
package errors

import (
	"errors"
	"fmt"
)

// ErrorType represents different categories of errors
type ErrorType string

const (
	// ErrorTypeValidation represents validation errors
	ErrorTypeValidation ErrorType = "VALIDATION_ERROR"
	// ErrorTypeAPI represents API-related errors
	ErrorTypeAPI ErrorType = "API_ERROR"
	// ErrorTypeConfig represents configuration errors
	ErrorTypeConfig ErrorType = "CONFIG_ERROR"
	// ErrorTypeNetwork represents network-related errors
	ErrorTypeNetwork ErrorType = "NETWORK_ERROR"
	// ErrorTypeNotFound represents resource not found errors
	ErrorTypeNotFound ErrorType = "NOT_FOUND_ERROR"
	// ErrorTypePermission represents permission-related errors
	ErrorTypePermission ErrorType = "PERMISSION_ERROR"
)

// AppError represents a structured application error
type AppError struct {
	Type    ErrorType
	Message string
	Err     error
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

// Unwrap returns the wrapped error
func (e *AppError) Unwrap() error {
	return e.Err
}

// NewValidationError creates a new validation error
func NewValidationError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeValidation,
		Message: message,
		Err:     err,
	}
}

// NewAPIError creates a new API error
func NewAPIError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeAPI,
		Message: message,
		Err:     err,
	}
}

// NewConfigError creates a new configuration error
func NewConfigError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeConfig,
		Message: message,
		Err:     err,
	}
}

// NewNetworkError creates a new network error
func NewNetworkError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeNetwork,
		Message: message,
		Err:     err,
	}
}

// NewNotFoundError creates a new not found error
func NewNotFoundError(resourceType, identifier string) *AppError {
	return &AppError{
		Type:    ErrorTypeNotFound,
		Message: fmt.Sprintf("%s '%s' not found", resourceType, identifier),
		Err:     nil,
	}
}

// NewPermissionError creates a new permission error
func NewPermissionError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypePermission,
		Message: message,
		Err:     err,
	}
}

// IsValidationError checks if the error is a validation error
func IsValidationError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Type == ErrorTypeValidation
}

// IsAPIError checks if the error is an API error
func IsAPIError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Type == ErrorTypeAPI
}

// IsConfigError checks if the error is a configuration error
func IsConfigError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Type == ErrorTypeConfig
}

// IsNotFoundError checks if the error is a not found error
func IsNotFoundError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Type == ErrorTypeNotFound
}

// IsPermissionError checks if the error is a permission error
func IsPermissionError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Type == ErrorTypePermission
}

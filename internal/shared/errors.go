// Ported from packages/adapter-shared/src/errors.ts @ 6adca36 (chat v4.40.0).
// Divergences: TS Error subclasses → pointer struct types with Error();
// errors.As matches the hierarchy (specialized types Unwrap to *AdapterError).
// Optional TS fields are zero values ("" / 0 / nil). Messages keep the
// upstream wording the tests pin, including capitalized defaults.
package shared

import "strconv"

// AdapterError is the base adapter error.
type AdapterError struct {
	Adapter string
	Code    string
	Name    string
	Msg     string
}

func (e *AdapterError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

// NewAdapterError constructs an AdapterError. code "" is omitted.
func NewAdapterError(message, adapter, code string) *AdapterError {
	return &AdapterError{Adapter: adapter, Code: code, Name: "AdapterError", Msg: message}
}

// AdapterRateLimitError is thrown when a platform rate limit is hit.
type AdapterRateLimitError struct {
	*AdapterError
	RetryAfter int // 0 = unset
}

func (e *AdapterRateLimitError) Unwrap() error { return e.AdapterError }

// NewAdapterRateLimitError constructs a rate-limit error. retryAfter 0 is omitted.
func NewAdapterRateLimitError(adapter string, retryAfter int) *AdapterRateLimitError {
	msg := "Rate limited by " + adapter
	if retryAfter != 0 {
		msg += ", retry after " + strconv.Itoa(retryAfter) + "s"
	}
	return &AdapterRateLimitError{
		AdapterError: &AdapterError{Adapter: adapter, Code: "RATE_LIMITED", Name: "AdapterRateLimitError", Msg: msg},
		RetryAfter:   retryAfter,
	}
}

// AuthenticationError is thrown when credentials are invalid or expired.
type AuthenticationError struct {
	*AdapterError
}

func (e *AuthenticationError) Unwrap() error { return e.AdapterError }

// NewAuthenticationError constructs an auth error. message "" uses the default.
func NewAuthenticationError(adapter, message string) *AuthenticationError {
	if message == "" {
		message = "Authentication failed for " + adapter
	}
	return &AuthenticationError{
		AdapterError: &AdapterError{Adapter: adapter, Code: "AUTH_FAILED", Name: "AuthenticationError", Msg: message},
	}
}

// ResourceNotFoundError is thrown when a requested resource does not exist.
type ResourceNotFoundError struct {
	*AdapterError
	ResourceType string
	ResourceID   string // "" = unset
}

func (e *ResourceNotFoundError) Unwrap() error { return e.AdapterError }

// NewResourceNotFoundError constructs a not-found error. resourceID "" is omitted.
func NewResourceNotFoundError(adapter, resourceType, resourceID string) *ResourceNotFoundError {
	idPart := ""
	if resourceID != "" {
		idPart = " '" + resourceID + "'"
	}
	return &ResourceNotFoundError{
		AdapterError: &AdapterError{
			Adapter: adapter,
			Code:    "NOT_FOUND",
			Name:    "ResourceNotFoundError",
			Msg:     resourceType + idPart + " not found in " + adapter,
		},
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}
}

// PermissionError is thrown when the bot lacks a required permission.
type PermissionError struct {
	*AdapterError
	Action        string
	RequiredScope string // "" = unset
}

func (e *PermissionError) Unwrap() error { return e.AdapterError }

// NewPermissionError constructs a permission error. requiredScope "" is omitted.
func NewPermissionError(adapter, action, requiredScope string) *PermissionError {
	scopePart := ""
	if requiredScope != "" {
		scopePart = " (requires: " + requiredScope + ")"
	}
	return &PermissionError{
		AdapterError: &AdapterError{
			Adapter: adapter,
			Code:    "PERMISSION_DENIED",
			Name:    "PermissionError",
			Msg:     "Permission denied: cannot " + action + " in " + adapter + scopePart,
		},
		Action:        action,
		RequiredScope: requiredScope,
	}
}

// ValidationError is thrown when input data is invalid.
type ValidationError struct {
	*AdapterError
}

func (e *ValidationError) Unwrap() error { return e.AdapterError }

// NewValidationError constructs a validation error.
func NewValidationError(adapter, message string) *ValidationError {
	return &ValidationError{
		AdapterError: &AdapterError{Adapter: adapter, Code: "VALIDATION_ERROR", Name: "ValidationError", Msg: message},
	}
}

// NetworkError is thrown on a network or connectivity issue.
type NetworkError struct {
	*AdapterError
	OriginalError error
}

func (e *NetworkError) Unwrap() []error {
	if e == nil {
		return nil
	}
	var errs []error
	if e.AdapterError != nil {
		errs = append(errs, e.AdapterError)
	}
	if e.OriginalError != nil {
		errs = append(errs, e.OriginalError)
	}
	return errs
}

// NewNetworkError constructs a network error. message "" uses the default.
func NewNetworkError(adapter, message string, original error) *NetworkError {
	if message == "" {
		message = "Network error communicating with " + adapter
	}
	return &NetworkError{
		AdapterError:  &AdapterError{Adapter: adapter, Code: "NETWORK_ERROR", Name: "NetworkError", Msg: message},
		OriginalError: original,
	}
}

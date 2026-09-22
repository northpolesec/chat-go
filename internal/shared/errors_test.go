package shared

import (
	"errors"
	"testing"

	"github.com/shoenig/test/must"
)

func TestAdapterError(t *testing.T) {
	t.Parallel()

	t.Run("creates error with message, adapter, and code", func(t *testing.T) {
		t.Parallel()
		err := NewAdapterError("Something failed", "slack", "CUSTOM_CODE")
		must.Eq(t, "Something failed", err.Error())
		must.Eq(t, "slack", err.Adapter)
		must.Eq(t, "CUSTOM_CODE", err.Code)
		must.Eq(t, "AdapterError", err.Name)
	})

	t.Run("is an instance of Error", func(t *testing.T) {
		t.Parallel()
		var err error = NewAdapterError("test", "slack", "")
		must.Error(t, err)
	})

	t.Run("works without code", func(t *testing.T) {
		t.Parallel()
		err := NewAdapterError("test", "teams", "")
		must.Eq(t, "", err.Code)
	})
}

func TestAdapterRateLimitError(t *testing.T) {
	t.Parallel()

	t.Run("creates error with retry after", func(t *testing.T) {
		t.Parallel()
		err := NewAdapterRateLimitError("slack", 30)
		must.Eq(t, "Rate limited by slack, retry after 30s", err.Error())
		must.Eq(t, "slack", err.Adapter)
		must.Eq(t, "RATE_LIMITED", err.Code)
		must.Eq(t, 30, err.RetryAfter)
		must.Eq(t, "AdapterRateLimitError", err.Name)
	})

	t.Run("creates error without retry after", func(t *testing.T) {
		t.Parallel()
		err := NewAdapterRateLimitError("teams", 0)
		must.Eq(t, "Rate limited by teams", err.Error())
		must.Eq(t, 0, err.RetryAfter)
	})

	t.Run("is an instance of AdapterError", func(t *testing.T) {
		t.Parallel()
		err := NewAdapterRateLimitError("slack", 0)
		var ae *AdapterError
		must.True(t, errors.As(err, &ae))
	})
}

func TestAuthenticationError(t *testing.T) {
	t.Parallel()

	t.Run("creates error with custom message", func(t *testing.T) {
		t.Parallel()
		err := NewAuthenticationError("slack", "Token expired")
		must.Eq(t, "Token expired", err.Error())
		must.Eq(t, "slack", err.Adapter)
		must.Eq(t, "AUTH_FAILED", err.Code)
		must.Eq(t, "AuthenticationError", err.Name)
	})

	t.Run("creates error with default message", func(t *testing.T) {
		t.Parallel()
		err := NewAuthenticationError("teams", "")
		must.Eq(t, "Authentication failed for teams", err.Error())
	})

	t.Run("is an instance of AdapterError", func(t *testing.T) {
		t.Parallel()
		err := NewAuthenticationError("slack", "")
		var ae *AdapterError
		must.True(t, errors.As(err, &ae))
	})
}

func TestResourceNotFoundError(t *testing.T) {
	t.Parallel()

	t.Run("creates error with resource type and id", func(t *testing.T) {
		t.Parallel()
		err := NewResourceNotFoundError("slack", "channel", "C123456")
		must.Eq(t, "channel 'C123456' not found in slack", err.Error())
		must.Eq(t, "slack", err.Adapter)
		must.Eq(t, "NOT_FOUND", err.Code)
		must.Eq(t, "channel", err.ResourceType)
		must.Eq(t, "C123456", err.ResourceID)
		must.Eq(t, "ResourceNotFoundError", err.Name)
	})

	t.Run("creates error without resource id", func(t *testing.T) {
		t.Parallel()
		err := NewResourceNotFoundError("teams", "user", "")
		must.Eq(t, "user not found in teams", err.Error())
		must.Eq(t, "", err.ResourceID)
	})

	t.Run("is an instance of AdapterError", func(t *testing.T) {
		t.Parallel()
		err := NewResourceNotFoundError("slack", "thread", "")
		var ae *AdapterError
		must.True(t, errors.As(err, &ae))
	})
}

func TestPermissionError(t *testing.T) {
	t.Parallel()

	t.Run("creates error with action and scope", func(t *testing.T) {
		t.Parallel()
		err := NewPermissionError("slack", "send messages", "chat:write")
		must.Eq(t, "Permission denied: cannot send messages in slack (requires: chat:write)", err.Error())
		must.Eq(t, "slack", err.Adapter)
		must.Eq(t, "PERMISSION_DENIED", err.Code)
		must.Eq(t, "send messages", err.Action)
		must.Eq(t, "chat:write", err.RequiredScope)
		must.Eq(t, "PermissionError", err.Name)
	})

	t.Run("creates error without scope", func(t *testing.T) {
		t.Parallel()
		err := NewPermissionError("teams", "delete messages", "")
		must.Eq(t, "Permission denied: cannot delete messages in teams", err.Error())
		must.Eq(t, "", err.RequiredScope)
	})

	t.Run("is an instance of AdapterError", func(t *testing.T) {
		t.Parallel()
		err := NewPermissionError("gchat", "test", "")
		var ae *AdapterError
		must.True(t, errors.As(err, &ae))
	})
}

func TestValidationError(t *testing.T) {
	t.Parallel()

	t.Run("creates error with message", func(t *testing.T) {
		t.Parallel()
		err := NewValidationError("slack", "Message text exceeds 40000 characters")
		must.Eq(t, "Message text exceeds 40000 characters", err.Error())
		must.Eq(t, "slack", err.Adapter)
		must.Eq(t, "VALIDATION_ERROR", err.Code)
		must.Eq(t, "ValidationError", err.Name)
	})

	t.Run("is an instance of AdapterError", func(t *testing.T) {
		t.Parallel()
		err := NewValidationError("teams", "Invalid")
		var ae *AdapterError
		must.True(t, errors.As(err, &ae))
	})
}

func TestNetworkError(t *testing.T) {
	t.Parallel()

	t.Run("creates error with custom message", func(t *testing.T) {
		t.Parallel()
		err := NewNetworkError("slack", "Connection timeout after 30s", nil)
		must.Eq(t, "Connection timeout after 30s", err.Error())
		must.Eq(t, "slack", err.Adapter)
		must.Eq(t, "NETWORK_ERROR", err.Code)
		must.Eq(t, "NetworkError", err.Name)
	})

	t.Run("creates error with default message", func(t *testing.T) {
		t.Parallel()
		err := NewNetworkError("gchat", "", nil)
		must.Eq(t, "Network error communicating with gchat", err.Error())
	})

	t.Run("can wrap original error", func(t *testing.T) {
		t.Parallel()
		original := errors.New("ECONNREFUSED")
		err := NewNetworkError("teams", "Connection refused", original)
		must.Eq(t, original, err.OriginalError)
	})

	t.Run("is an instance of AdapterError", func(t *testing.T) {
		t.Parallel()
		err := NewNetworkError("slack", "", nil)
		var ae *AdapterError
		must.True(t, errors.As(err, &ae))
	})
}

func TestErrorHierarchy(t *testing.T) {
	t.Parallel()

	t.Run("all errors extend AdapterError", func(t *testing.T) {
		t.Parallel()
		errs := []error{
			NewAdapterRateLimitError("slack", 0),
			NewAuthenticationError("slack", ""),
			NewResourceNotFoundError("slack", "test", ""),
			NewPermissionError("slack", "test", ""),
			NewValidationError("slack", "test"),
			NewNetworkError("slack", "", nil),
		}
		for _, err := range errs {
			var ae *AdapterError
			must.True(t, errors.As(err, &ae))
			must.Error(t, err)
		}
	})

	t.Run("can be caught by adapter name", func(t *testing.T) {
		t.Parallel()
		var slackErrors []*AdapterError
		err := NewAdapterRateLimitError("slack", 30)
		var ae *AdapterError
		if errors.As(err, &ae) && ae.Adapter == "slack" {
			slackErrors = append(slackErrors, ae)
		}
		must.Eq(t, 1, len(slackErrors))
		must.Eq(t, "slack", slackErrors[0].Adapter)
	})

	t.Run("can be caught by error code", func(t *testing.T) {
		t.Parallel()
		var rateLimitErrors []*AdapterError
		errs := []error{
			NewAdapterRateLimitError("slack", 0),
			NewAuthenticationError("teams", ""),
			NewAdapterRateLimitError("gchat", 0),
		}
		for _, err := range errs {
			var ae *AdapterError
			if errors.As(err, &ae) && ae.Code == "RATE_LIMITED" {
				rateLimitErrors = append(rateLimitErrors, ae)
			}
		}
		must.Eq(t, 2, len(rateLimitErrors))
	})
}

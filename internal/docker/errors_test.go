package docker

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestIsPermissionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("some random error"),
			expected: false,
		},
		{
			name:     "permission denied lowercase",
			err:      errors.New("permission denied"),
			expected: true,
		},
		{
			name:     "permission denied mixed case",
			err:      errors.New("Permission Denied"),
			expected: true,
		},
		{
			name:     "docker socket permission error",
			err:      errors.New("dial unix /var/run/docker.sock: connect: permission denied"),
			expected: true,
		},
		{
			name:     "access denied",
			err:      errors.New("access denied"),
			expected: true,
		},
		{
			name:     "wrapped permission error",
			err:      fmt.Errorf("creating docker client: %w", errors.New("permission denied")),
			expected: true,
		},
		{
			name:     "os.ErrPermission",
			err:      os.ErrPermission,
			expected: true,
		},
		{
			name:     "wrapped os.ErrPermission",
			err:      fmt.Errorf("file access: %w", os.ErrPermission),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPermissionError(tt.err)
			if result != tt.expected {
				t.Errorf("IsPermissionError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestWrapPermissionError(t *testing.T) {
	t.Run("wraps permission error", func(t *testing.T) {
		originalErr := errors.New("permission denied while connecting to docker")
		wrapped := WrapPermissionError(originalErr)

		var permErr *PermissionError
		if !errors.As(wrapped, &permErr) {
			t.Errorf("expected wrapped error to be PermissionError, got %T", wrapped)
		}
	})

	t.Run("does not wrap non-permission error", func(t *testing.T) {
		originalErr := errors.New("connection timeout")
		result := WrapPermissionError(originalErr)

		var permErr *PermissionError
		if errors.As(result, &permErr) {
			t.Error("expected non-permission error to not be wrapped")
		}

		if result != originalErr {
			t.Error("expected original error to be returned unchanged")
		}
	})
}

func TestPermissionError_Error(t *testing.T) {
	originalErr := errors.New("dial unix /var/run/docker.sock: connect: permission denied")
	permErr := &PermissionError{Err: originalErr}

	if permErr.Error() != originalErr.Error() {
		t.Errorf("expected Error() to return original message, got %q", permErr.Error())
	}
}

func TestPermissionError_Unwrap(t *testing.T) {
	originalErr := errors.New("permission denied")
	permErr := &PermissionError{Err: originalErr}

	unwrapped := permErr.Unwrap()
	if unwrapped != originalErr {
		t.Errorf("expected Unwrap() to return original error")
	}
}

func TestPermissionError_Help(t *testing.T) {
	permErr := &PermissionError{Err: errors.New("permission denied")}
	help := permErr.Help()

	if help == "" {
		t.Error("expected Help() to return non-empty string")
	}

	switch runtime.GOOS {
	case "linux":
		if !contains(help, "docker group") {
			t.Error("expected Linux help to mention docker group")
		}
		if !contains(help, "usermod") {
			t.Error("expected Linux help to mention usermod command")
		}
		if !contains(help, "newgrp") {
			t.Error("expected Linux help to mention newgrp command")
		}
	case "darwin":
		if !contains(help, "Docker Desktop") {
			t.Error("expected macOS help to mention Docker Desktop")
		}
	}
}

func TestFormatError(t *testing.T) {
	t.Run("formats permission error with help", func(t *testing.T) {
		originalErr := errors.New("permission denied")
		permErr := &PermissionError{Err: originalErr}

		formatted := FormatError(permErr)

		if !contains(formatted, originalErr.Error()) {
			t.Error("expected formatted error to contain original message")
		}
	})

	t.Run("returns plain message for non-permission error", func(t *testing.T) {
		err := errors.New("connection timeout")
		formatted := FormatError(err)

		if formatted != err.Error() {
			t.Errorf("expected plain error message, got %q", formatted)
		}
	})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

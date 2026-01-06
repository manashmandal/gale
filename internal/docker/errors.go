package docker

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

type PermissionError struct {
	Err error
}

func (e *PermissionError) Error() string {
	return e.Err.Error()
}

func (e *PermissionError) Unwrap() error {
	return e.Err
}

func (e *PermissionError) Help() string {
	if runtime.GOOS == "linux" {
		return `Docker permission denied. To fix this:

  1. Add your user to the docker group:
     sudo usermod -aG docker $USER

  2. Apply the new group (choose one):
     - Run: newgrp docker
     - Or log out and log back in

  3. Verify with: docker ps

Alternatively, run gale with sudo (not recommended for regular use).`
	}

	if runtime.GOOS == "darwin" {
		return `Docker permission denied. Ensure Docker Desktop is running and your user has access.`
	}

	return `Docker permission denied. Ensure Docker is running and you have permission to access it.`
}

func IsPermissionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	permissionIndicators := []string{
		"permission denied",
		"access denied",
		"connect: permission denied",
		"dial unix /var/run/docker.sock: connect: permission denied",
	}

	for _, indicator := range permissionIndicators {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(indicator)) {
			return true
		}
	}

	return errors.Is(err, os.ErrPermission)
}

func WrapPermissionError(err error) error {
	if IsPermissionError(err) {
		return &PermissionError{Err: err}
	}
	return err
}

func FormatError(err error) string {
	var permErr *PermissionError
	if errors.As(err, &permErr) {
		return fmt.Sprintf("%s\n\n%s", permErr.Error(), permErr.Help())
	}
	return err.Error()
}

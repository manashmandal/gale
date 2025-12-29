package runner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/native"
)

func NewClient(cfg *config.Config, logger *slog.Logger) (Client, error) {
	switch cfg.Runner.Mode {
	case "native":
		return newNativeClient(cfg, logger)
	case "docker", "":
		return newDockerClient(cfg, logger)
	default:
		return nil, fmt.Errorf("unknown runner mode: %s (use 'docker' or 'native')", cfg.Runner.Mode)
	}
}

func newDockerClient(cfg *config.Config, logger *slog.Logger) (Client, error) {
	dockerClient, err := docker.NewClient(cfg.Docker.Host)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	logger.Info("ensuring runner image is available", "image", cfg.Runner.Image)
	if err := dockerClient.EnsureImage(context.Background(), cfg.Runner.Image); err != nil {
		dockerClient.Close()
		return nil, fmt.Errorf("ensuring runner image: %w", err)
	}

	logger.Info("using docker runner mode", "image", cfg.Runner.Image)
	return NewDockerAdapter(dockerClient), nil
}

func newNativeClient(cfg *config.Config, logger *slog.Logger) (Client, error) {
	nativeClient, err := native.NewClient("")
	if err != nil {
		return nil, fmt.Errorf("creating native client: %w", err)
	}

	// Each runner downloads its own binary to its isolated directory
	// No shared cache - eliminates race conditions during parallel runner creation
	logger.Info("using native runner mode (each runner downloads its own binary)")
	return NewNativeAdapter(nativeClient), nil
}

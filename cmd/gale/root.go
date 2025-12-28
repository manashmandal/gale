package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	cfgFile  string
	logLevel string
)

var rootCmd = &cobra.Command{
	Use:   "gale",
	Short: "JIT autoscaler for GitHub Actions self-hosted runners",
	Long: `Gale is a Just-In-Time autoscaler for GitHub Actions self-hosted runners.

It monitors your GitHub repositories for queued jobs and dynamically
spawns Docker-based runners on demand.

Quick start:
  gale init           # Interactive setup
  gale start          # Start the autoscaler
  gale status         # Check current status`,
}

func getDefaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(homeDir, ".gale", "config.yaml")
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", getDefaultConfigPath(), "config file path")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "", "log level (debug, info, warn, error)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

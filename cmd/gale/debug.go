package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/spf13/cobra"
)

type DebugInfo struct {
	Timestamp   string            `json:"timestamp"`
	Version     string            `json:"version"`
	System      SystemInfo        `json:"system"`
	Config      ConfigInfo        `json:"config"`
	Runners     []RunnerDebugInfo `json:"runners"`
	Directories DirInfo           `json:"directories"`
}

type SystemInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	NumCPU   int    `json:"num_cpu"`
	Hostname string `json:"hostname"`
}

type ConfigInfo struct {
	Path       string   `json:"path"`
	Exists     bool     `json:"exists"`
	RunnerMode string   `json:"runner_mode"`
	MaxRunners int      `json:"max_runners"`
	Labels     []string `json:"labels"`
}

type RunnerDebugInfo struct {
	ID        string `json:"id"`
	Dir       string `json:"dir"`
	DirExists bool   `json:"dir_exists"`
	HasLog    bool   `json:"has_log"`
	LogTail   string `json:"log_tail,omitempty"`
}

type DirInfo struct {
	BaseDir     string   `json:"base_dir"`
	CacheExists bool     `json:"cache_exists"`
	WorkDirs    []string `json:"work_dirs"`
	TotalWorkMB float64  `json:"total_work_mb"`
}

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Dump debug information for troubleshooting",
	Long: `Collects and displays debug information useful for troubleshooting runner issues.

Includes:
  - System information
  - Configuration details
  - Runner directories and logs
  - Recent runner activity`,
	RunE: runDebug,
}

func init() {
	rootCmd.AddCommand(debugCmd)
}

func runDebug(cmd *cobra.Command, args []string) error {
	homeDir, _ := os.UserHomeDir()
	baseDir := filepath.Join(homeDir, ".gale", "native-runners")

	hostname, _ := os.Hostname()

	info := DebugInfo{
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   version,
		System: SystemInfo{
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
			NumCPU:   runtime.NumCPU(),
			Hostname: hostname,
		},
		Config:      getConfigDebugInfo(),
		Runners:     getRunnerDebugInfo(baseDir),
		Directories: getDirDebugInfo(baseDir),
	}

	output, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling debug info: %w", err)
	}

	fmt.Println(string(output))
	return nil
}

func getConfigDebugInfo() ConfigInfo {
	info := ConfigInfo{
		Path: cfgFile,
	}

	if _, err := os.Stat(cfgFile); err == nil {
		info.Exists = true
		if cfg, err := config.Load(cfgFile); err == nil {
			info.RunnerMode = cfg.Runner.Mode
			info.MaxRunners = cfg.Scaler.MaxRunners
			info.Labels = cfg.Runner.Labels
		}
	}

	return info
}

func getRunnerDebugInfo(baseDir string) []RunnerDebugInfo {
	var runners []RunnerDebugInfo

	workDir := filepath.Join(baseDir, "work")
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return runners
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		runnerDir := filepath.Join(workDir, entry.Name())
		logPath := filepath.Join(runnerDir, "runner.log")

		r := RunnerDebugInfo{
			ID:        entry.Name(),
			Dir:       runnerDir,
			DirExists: true,
		}

		if logContent, err := os.ReadFile(logPath); err == nil {
			r.HasLog = true
			r.LogTail = getTail(string(logContent), 50)
		}

		runners = append(runners, r)
	}

	return runners
}

func getDirDebugInfo(baseDir string) DirInfo {
	info := DirInfo{
		BaseDir: baseDir,
	}

	cacheDir := filepath.Join(baseDir, "cache")
	if _, err := os.Stat(cacheDir); err == nil {
		info.CacheExists = true
	}

	workDir := filepath.Join(baseDir, "work")
	entries, _ := os.ReadDir(workDir)
	for _, entry := range entries {
		if entry.IsDir() {
			info.WorkDirs = append(info.WorkDirs, entry.Name())
		}
	}

	info.TotalWorkMB = getDirSizeMB(workDir)

	return info
}

func getTail(content string, lines int) string {
	allLines := strings.Split(content, "\n")
	if len(allLines) <= lines {
		return content
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

func getDirSizeMB(path string) float64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return float64(size) / (1024 * 1024)
}

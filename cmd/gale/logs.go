package main

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var (
	followLogs bool
	tailLines  int
	clearLogs  bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View gale daemon logs",
	Long: `View logs from the gale daemon (webhook or start).

Examples:
  gale logs              # Show recent logs
  gale logs -f           # Follow logs in real-time
  gale logs -n 100       # Show last 100 lines
  gale logs --clear      # Clear log file`,
	RunE: runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&followLogs, "follow", "f", false, "follow log output in real-time")
	logsCmd.Flags().IntVarP(&tailLines, "lines", "n", 50, "number of lines to show")
	logsCmd.Flags().BoolVar(&clearLogs, "clear", false, "clear the log file")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	logPath := GetLogFilePath()

	if clearLogs {
		if err := os.Truncate(logPath, 0); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No log file exists")
				return nil
			}
			return fmt.Errorf("clearing log file: %w", err)
		}
		fmt.Printf("Cleared log file: %s\n", logPath)
		return nil
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Printf("No log file found at %s\n", logPath)
		fmt.Println("Start a daemon with 'gale webhook --daemon' or 'gale start --daemon' to generate logs")
		return nil
	}

	if followLogs {
		return followLogFile(logPath)
	}

	return tailLogFile(logPath, tailLines)
}

func tailLogFile(path string, lines int) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer file.Close()

	allLines, err := readAllLines(file)
	if err != nil {
		return fmt.Errorf("reading log file: %w", err)
	}

	start := 0
	if len(allLines) > lines {
		start = len(allLines) - lines
	}

	for _, line := range allLines[start:] {
		fmt.Println(line)
	}

	return nil
}

func readAllLines(r io.Reader) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func followLogFile(path string) error {
	if err := tailLogFile(path, tailLines); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(path); err != nil {
		return fmt.Errorf("watching log file: %w", err)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer file.Close()

	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seeking to end: %w", err)
	}

	reader := bufio.NewReader(file)

	fmt.Println("\n--- Following logs (Ctrl+C to stop) ---")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						break
					}
					fmt.Print(line)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return fmt.Errorf("watcher error: %w", err)
		}
	}
}

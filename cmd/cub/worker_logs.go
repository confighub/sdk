// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var workerLogsCmd = &cobra.Command{
	Use:           "logs <worker-slug>",
	Short:         "View logs from a worker",
	Long:          getCommandHelp(`View logs from a worker. Use -f to follow/tail the logs in real-time.`, ""),
	Args:          cobra.ExactArgs(1),
	RunE:          workerLogsCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var workerLogsArgs struct {
	follow bool
	lines  int
}

func init() {
	workerLogsCmd.Flags().BoolVarP(&workerLogsArgs.follow, "follow", "f", false, "Follow log output (like tail -f)")
	workerLogsCmd.Flags().IntVarP(&workerLogsArgs.lines, "tail", "n", 0, "Number of lines to show from the end of the logs (0 = all)")
	workerCmd.AddCommand(workerLogsCmd)
}

func workerLogsCmdRun(cmd *cobra.Command, args []string) error {
	workerSlug := args[0]
	confighubDir := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR)
	logFile := filepath.Join(confighubDir, "worker", "log", fmt.Sprintf("%s.log", workerSlug))

	// Check if log file exists
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return fmt.Errorf("no log file found for worker '%s'", workerSlug)
	}

	file, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	if workerLogsArgs.lines > 0 {
		// Read last N lines
		if err := printLastNLines(file, workerLogsArgs.lines); err != nil {
			return err
		}
	} else {
		// Print entire file
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading log file: %w", err)
		}
	}

	// If follow mode, tail the file
	if workerLogsArgs.follow {
		return followLogFile(file)
	}

	return nil
}

// printLastNLines reads and prints the last N lines from a file
func printLastNLines(file *os.File, n int) error {
	// Read all lines into a circular buffer
	lines := make([]string, 0, n)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if len(lines) < n {
			lines = append(lines, line)
		} else {
			// Shift and add (circular buffer behavior)
			lines = append(lines[1:], line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log file: %w", err)
	}

	// Print all lines we collected
	for _, line := range lines {
		fmt.Println(line)
	}

	return nil
}

// followLogFile tails the log file in real-time
func followLogFile(file *os.File) error {
	// Seek to end of file
	if _, err := file.Seek(0, 2); err != nil {
		return fmt.Errorf("failed to seek to end of file: %w", err)
	}

	scanner := bufio.NewScanner(file)
	for {
		if scanner.Scan() {
			fmt.Println(scanner.Text())
		} else {
			// No more data available, sleep and retry
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("error reading log file: %w", err)
			}
			time.Sleep(100 * time.Millisecond)

			// Create a new scanner to continue reading
			// (bufio.Scanner doesn't have a way to retry after EOF)
			scanner = bufio.NewScanner(file)
		}
	}
}

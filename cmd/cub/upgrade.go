// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade cub to the latest version",
	Long:  `Download and install the latest version of cub and cub-worker-run binaries`,
	RunE:  upgradeCmdRun,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

func upgradeCmdRun(cmd *cobra.Command, args []string) error {
	// Detect OS and architecture
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Only support darwin and linux
	if osName != "darwin" && osName != "linux" {
		return fmt.Errorf("unsupported operating system: %s", osName)
	}

	// TODO: Version check

	// Determine binary location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	binDir := filepath.Join(homeDir, CONFIGHUB_DIR, "bin")
	cubPath := filepath.Join(binDir, "cub")
	cubWorkerPath := filepath.Join(binDir, "cub-worker-run")

	// Check if binaries exist
	if _, err := os.Stat(cubPath); os.IsNotExist(err) {
		return fmt.Errorf("cub binary not found at %s", cubPath)
	}

	// Download URLs
	bucket := "confighub-downloads-6554c6cc"
	cubURL := fmt.Sprintf("https://%s.s3.amazonaws.com/public/cub-%s-%s-latest", bucket, osName, arch)
	cubWorkerURL := fmt.Sprintf("https://%s.s3.amazonaws.com/public/cub-worker-run-%s-%s-latest", bucket, osName, arch)

	// Download binaries to temp files
	tprint("Downloading cub binary...")
	cubTempPath := filepath.Join(binDir, ".cub.tmp")
	if err := downloadFile(cubTempPath, cubURL); err != nil {
		return fmt.Errorf("failed to download cub binary: %w", err)
	}
	defer os.Remove(cubTempPath)

	tprint("Downloading cub-worker-run binary...")
	cubWorkerTempPath := filepath.Join(binDir, ".cub-worker-run.tmp")
	if err := downloadFile(cubWorkerTempPath, cubWorkerURL); err != nil {
		return fmt.Errorf("failed to download cub-worker-run binary: %w", err)
	}
	defer os.Remove(cubWorkerTempPath)

	// Set executable permissions on temp files
	if err := os.Chmod(cubTempPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions on cub binary: %w", err)
	}
	if err := os.Chmod(cubWorkerTempPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions on cub-worker-run binary: %w", err)
	}

	// Atomic replacement of binaries
	tprint("Installing new binaries...")

	// Replace cub binary
	cubBackupPath := cubPath + ".old"
	if err := os.Rename(cubPath, cubBackupPath); err != nil {
		// If rename fails, binary might not exist or we don't have permissions
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to backup current cub binary: %w", err)
		}
	} else {
		defer func() {
			// Clean up backup on success
			os.Remove(cubBackupPath)
		}()
	}

	if err := os.Rename(cubTempPath, cubPath); err != nil {
		// Try to restore backup if rename fails
		if _, err2 := os.Stat(cubBackupPath); err2 == nil {
			os.Rename(cubBackupPath, cubPath)
		}
		return fmt.Errorf("failed to install new cub binary: %w", err)
	}

	// Replace cub-worker-run binary
	cubWorkerBackupPath := cubWorkerPath + ".old"
	if err := os.Rename(cubWorkerPath, cubWorkerBackupPath); err != nil {
		// If rename fails, binary might not exist (which is okay for cub-worker-run)
		if !os.IsNotExist(err) {
			// Only log warning, don't fail the upgrade
			tprint("Warning: could not backup cub-worker-run: %v", err)
		}
	} else {
		defer func() {
			// Clean up backup on success
			os.Remove(cubWorkerBackupPath)
		}()
	}

	if err := os.Rename(cubWorkerTempPath, cubWorkerPath); err != nil {
		// Try to restore backup if rename fails
		if _, err2 := os.Stat(cubWorkerBackupPath); err2 == nil {
			os.Rename(cubWorkerBackupPath, cubWorkerPath)
		}
		// Don't fail if cub-worker-run fails, just warn
		tprint("Warning: failed to install new cub-worker-run binary: %v", err)
	}

	tprint("Successfully upgraded cub")
	tprint("Please restart any running cub processes to use the new version")

	return nil
}

func downloadFile(filepath string, url string) error {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	// Make request
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

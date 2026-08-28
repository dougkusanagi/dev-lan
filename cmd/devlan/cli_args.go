package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func parseGlobalArgs(args []string) (string, []string, error) {
	dataDir := defaultDataDir()
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--data-dir":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", nil, fmt.Errorf("--data-dir exige um caminho")
			}
			dataDir = args[i+1]
			i++
		case "--version", "-v", "--help", "-h":
			remaining = append(remaining, args[i])
		default:
			remaining = append(remaining, args[i])
		}
	}
	return filepath.Clean(dataDir), remaining, nil
}

func defaultDataDir() string {
	if configured := strings.TrimSpace(os.Getenv("DEVLAN_HOME")); configured != "" {
		return configured
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "DevLAN")
	}
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/etc/devlan/windows-data-dir"); err == nil {
			if configured := strings.TrimSpace(string(data)); configured != "" {
				return filepath.Clean(configured)
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".devlan")
	}
	return ".devlan"
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	linuxDirectoryName   = "agent-context-probe"
	desktopDirectoryName = "Agent Context Probe"
)

type StandardPaths struct {
	ConfigDir  string
	ConfigFile string
	DataDir    string
	Database   string
	BackupDir  string
}

func ResolveStandardPaths() (StandardPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return StandardPaths{}, fmt.Errorf("resolve user home directory: %w", err)
	}
	return standardPathsFor(runtime.GOOS, home, os.Getenv)
}

func standardPathsFor(platform, home string, getenv func(string) string) (StandardPaths, error) {
	if strings.TrimSpace(home) == "" {
		return StandardPaths{}, fmt.Errorf("user home directory is empty")
	}

	var configDir, dataDir string
	switch platform {
	case "darwin":
		configDir = filepath.Join(home, "Library", "Application Support", desktopDirectoryName)
		dataDir = configDir
	case "windows":
		configRoot := strings.TrimSpace(getenv("APPDATA"))
		if configRoot == "" {
			configRoot = platformJoin(platform, home, "AppData", "Roaming")
		}
		dataRoot := strings.TrimSpace(getenv("LOCALAPPDATA"))
		if dataRoot == "" {
			dataRoot = platformJoin(platform, home, "AppData", "Local")
		}
		configDir = platformJoin(platform, configRoot, desktopDirectoryName)
		dataDir = platformJoin(platform, dataRoot, desktopDirectoryName)
	default:
		configRoot := absoluteEnvironmentPath(getenv("XDG_CONFIG_HOME"))
		if configRoot == "" {
			configRoot = filepath.Join(home, ".config")
		}
		dataRoot := absoluteEnvironmentPath(getenv("XDG_DATA_HOME"))
		if dataRoot == "" {
			dataRoot = filepath.Join(home, ".local", "share")
		}
		configDir = filepath.Join(configRoot, linuxDirectoryName)
		dataDir = filepath.Join(dataRoot, linuxDirectoryName)
	}

	return StandardPaths{
		ConfigDir:  configDir,
		ConfigFile: platformJoin(platform, configDir, "config.yaml"),
		DataDir:    dataDir,
		Database:   platformJoin(platform, dataDir, "requests.db"),
		BackupDir:  platformJoin(platform, dataDir, "backups"),
	}, nil
}

func absoluteEnvironmentPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return ""
	}
	return filepath.Clean(value)
}

func platformJoin(platform string, elements ...string) string {
	if platform != "windows" {
		return filepath.Join(elements...)
	}
	if len(elements) == 0 {
		return ""
	}
	joined := strings.TrimRight(elements[0], `\/`)
	for _, element := range elements[1:] {
		trimmed := strings.Trim(element, `\/`)
		if trimmed == "" {
			continue
		}
		joined += `\` + trimmed
	}
	return joined
}

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolatedStandardPaths(t *testing.T) StandardPaths {
	t.Helper()
	root := t.TempDir()
	return StandardPaths{
		ConfigDir:  filepath.Join(root, "config"),
		ConfigFile: filepath.Join(root, "config", "config.yaml"),
		DataDir:    filepath.Join(root, "data"),
		Database:   filepath.Join(root, "data", "requests.db"),
		BackupDir:  filepath.Join(root, "data", "backups"),
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ACP_CONFIG", "ACP_DATA_DIR", "ACP_HOST", "ACP_PORT", "ACP_ACCESS_TOKEN",
		"PORT", "DB_PATH", "READ_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT",
		"ANTHROPIC_FORWARD_URL", "ANTHROPIC_VERSION", "ANTHROPIC_MAX_RETRIES",
		"OPENAI_BASE_URL", "OPENAI_API_KEY",
	} {
		t.Setenv(key, "")
	}
}

func validConfig() *Config {
	return &Config{
		Server:  ServerConfig{Host: "127.0.0.1", Port: "3001"},
		Storage: StorageConfig{},
	}
}

func TestConfigRejectsNegativeCaptureLimit(t *testing.T) {
	cfg := validConfig()
	cfg.Storage.MaxCaptureBytes = -1
	if err := cfg.validate(); err == nil {
		t.Fatal("expected a negative capture limit to fail validation")
	}
}

func TestConfigAcceptsUnlimitedCapture(t *testing.T) {
	cfg := validConfig()
	if err := cfg.validate(); err != nil {
		t.Fatalf("zero should enable unlimited capture: %v", err)
	}
}

func TestConfigRejectsNegativeRawDisplayLimits(t *testing.T) {
	tests := []struct {
		name string
		web  WebConfig
	}{
		{
			name: "request",
			web:  WebConfig{RawRequestMaxDisplayChars: -1},
		},
		{
			name: "response",
			web:  WebConfig{RawResponseMaxDisplayChars: -1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Web = tt.web
			if err := cfg.validate(); err == nil {
				t.Fatal("expected a negative raw display limit to fail validation")
			}
		})
	}
}

func TestConfigAcceptsUnlimitedRawDisplay(t *testing.T) {
	cfg := validConfig()
	cfg.Web = WebConfig{
		RawRequestMaxDisplayChars:  0,
		RawResponseMaxDisplayChars: 0,
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("zero should enable unlimited raw display: %v", err)
	}
}

func TestDefaultConfigurationBindsToLoopback(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	configPath := filepath.Join(t.TempDir(), "missing.yaml")

	cfg, err := LoadWithOptions(LoadOptions{ConfigPath: configPath, StandardPaths: &standard})
	if err == nil {
		t.Fatal("explicit missing config must fail")
	}

	cfg, err = LoadWithOptions(LoadOptions{StandardPaths: &standard})
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != "3001" {
		t.Fatalf("unexpected default address: %s:%s", cfg.Server.Host, cfg.Server.Port)
	}
	if cfg.Storage.DBPath != standard.Database {
		t.Fatalf("default database path = %s, want %s", cfg.Storage.DBPath, standard.Database)
	}
}

func TestRemoteHostRequiresAccessToken(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Host = "0.0.0.0"
	if err := cfg.validate(); err == nil {
		t.Fatal("remote host without an access token must fail")
	}
	cfg.Server.AccessToken = "0123456789abcdef"
	if err := cfg.validate(); err != nil {
		t.Fatalf("remote host with an access token should pass: %v", err)
	}
}

func TestLoadPriorityIsFlagsThenEnvironmentThenFile(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`server:
  host: 127.0.0.1
  port: 4100
  timeouts:
    read: 2m
storage:
  db_path: from-file.db
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("PORT", "4200")
	t.Setenv("READ_TIMEOUT", "3m")
	t.Setenv("DB_PATH", "from-env.db")

	cfg, err := LoadWithOptions(LoadOptions{
		ConfigPath:    configPath,
		DataDir:       filepath.Join(t.TempDir(), "data"),
		Port:          "4300",
		StandardPaths: &standard,
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Port != "4300" {
		t.Fatalf("flag port did not win: %s", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 3*time.Minute {
		t.Fatalf("environment timeout did not win: %s", cfg.Server.ReadTimeout)
	}
	if filepath.Base(cfg.Storage.DBPath) != "requests.db" {
		t.Fatalf("data directory override did not win: %s", cfg.Storage.DBPath)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("read-only resolution changed config mode to %o", got)
	}
}

func TestMalformedExplicitConfigFails(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("server: ["), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadWithOptions(LoadOptions{ConfigPath: configPath, StandardPaths: &standard}); err == nil {
		t.Fatal("malformed config must fail")
	}
}

func TestResolutionPrepareSecuresConfigWithoutChangingItDuringResolve(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 3001\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolution, err := Resolve(LoadOptions{ConfigPath: configPath, StandardPaths: &standard})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if info, _ := os.Stat(configPath); info.Mode().Perm() != 0o644 {
		t.Fatal("Resolve must not modify config permissions")
	}
	if err := resolution.Prepare(); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if info, _ := os.Stat(configPath); info.Mode().Perm() != 0o600 {
		t.Fatalf("prepared config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestEnvironmentAndCLIConfigSources(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	envConfig := filepath.Join(t.TempDir(), "env.yaml")
	cliConfig := filepath.Join(t.TempDir(), "cli.yaml")
	for path, port := range map[string]string{envConfig: "4100", cliConfig: "4200"} {
		if err := os.WriteFile(path, []byte("server:\n  port: "+port+"\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	t.Setenv("ACP_CONFIG", envConfig)

	environment, err := Resolve(LoadOptions{StandardPaths: &standard})
	if err != nil {
		t.Fatalf("resolve environment config: %v", err)
	}
	if environment.ConfigSource != "environment" || environment.Config.Server.Port != "4100" {
		t.Fatalf("unexpected environment config resolution: %+v", environment)
	}

	cli, err := Resolve(LoadOptions{ConfigPath: cliConfig, StandardPaths: &standard})
	if err != nil {
		t.Fatalf("resolve CLI config: %v", err)
	}
	if cli.ConfigSource != "cli" || cli.Config.Server.Port != "4200" {
		t.Fatalf("CLI config did not win: %+v", cli)
	}
}

func TestStandardConfigIsSelectedWhenPresent(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	if err := os.MkdirAll(standard.ConfigDir, 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(standard.ConfigFile, []byte("server:\n  port: 4300\n"), 0o600); err != nil {
		t.Fatalf("write standard config: %v", err)
	}

	resolution, err := Resolve(LoadOptions{StandardPaths: &standard})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.ConfigSource != "standard" || resolution.ConfigPath != standard.ConfigFile {
		t.Fatalf("standard config was not selected: %+v", resolution)
	}
	if resolution.Config.Server.Port != "4300" {
		t.Fatalf("standard config value was not loaded: %s", resolution.Config.Server.Port)
	}
}

func TestRelativeDatabasePathUsesConfigDirectory(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("storage:\n  db_path: state/requests.db\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolution, err := Resolve(LoadOptions{ConfigPath: configPath, StandardPaths: &standard})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(configDir, "state", "requests.db")
	if resolution.Config.Storage.DBPath != want {
		t.Fatalf("database path = %s, want %s", resolution.Config.Storage.DBPath, want)
	}
}

func TestEnvironmentAliasesHaveDeterministicPriority(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	t.Setenv("PORT", "4100")
	t.Setenv("ACP_PORT", "4200")
	t.Setenv("ACP_DATA_DIR", filepath.Join(t.TempDir(), "data-dir"))
	directDatabase := filepath.Join(t.TempDir(), "direct", "requests.db")
	t.Setenv("DB_PATH", directDatabase)

	resolution, err := Resolve(LoadOptions{StandardPaths: &standard})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Config.Server.Port != "4200" {
		t.Fatalf("ACP_PORT did not override PORT: %s", resolution.Config.Server.Port)
	}
	if resolution.Config.Storage.DBPath != directDatabase {
		t.Fatalf("DB_PATH did not override ACP_DATA_DIR: %s", resolution.Config.Storage.DBPath)
	}
}

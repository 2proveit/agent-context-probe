package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Providers ProvidersConfig `yaml:"providers"`
	Storage   StorageConfig   `yaml:"storage"`
	Web       WebConfig       `yaml:"web"`
	Subagents SubagentsConfig `yaml:"subagents"`
	Anthropic AnthropicConfig
}

type ServerConfig struct {
	Host        string         `yaml:"host"`
	Port        string         `yaml:"port"`
	AccessToken string         `yaml:"access_token"`
	Timeouts    TimeoutsConfig `yaml:"timeouts"`
	// Legacy fields
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type TimeoutsConfig struct {
	Read  string `yaml:"read"`
	Write string `yaml:"write"`
	Idle  string `yaml:"idle"`
}

type ProvidersConfig struct {
	Anthropic AnthropicProviderConfig `yaml:"anthropic"`
	OpenAI    OpenAIProviderConfig    `yaml:"openai"`
}

type AnthropicProviderConfig struct {
	BaseURL    string `yaml:"base_url"`
	Version    string `yaml:"version"`
	MaxRetries int    `yaml:"max_retries"`
}

type OpenAIProviderConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

type AnthropicConfig struct {
	BaseURL    string
	Version    string
	MaxRetries int
}

type StorageConfig struct {
	RequestsDir     string `yaml:"requests_dir"`
	DBPath          string `yaml:"db_path"`
	BackupDir       string `yaml:"backup_dir"`
	MaxCaptureBytes int64  `yaml:"max_capture_bytes"`
}

type WebConfig struct {
	ShowRawStreamEvents        bool `yaml:"show_raw_stream_events"`
	RawRequestMaxDisplayChars  int  `yaml:"raw_request_max_display_chars"`
	RawResponseMaxDisplayChars int  `yaml:"raw_response_max_display_chars"`
}

type SubagentsConfig struct {
	Enable   bool              `yaml:"enable"`
	Mappings map[string]string `yaml:"mappings"`
}

type LoadOptions struct {
	ConfigPath    string
	DataDir       string
	Host          string
	Port          string
	StandardPaths *StandardPaths
}

type Resolution struct {
	Config         *Config
	Standard       StandardPaths
	ConfigPath     string
	ConfigSource   string
	DataDir        string
	DatabaseSource string
	HostSource     string
	PortSource     string
}

func Load() (*Config, error) {
	return LoadWithOptions(LoadOptions{})
}

func LoadWithOptions(options LoadOptions) (*Config, error) {
	resolution, err := Resolve(options)
	if err != nil {
		return nil, err
	}
	return resolution.Config, nil
}

func Resolve(options LoadOptions) (*Resolution, error) {
	var standard StandardPaths
	var standardErr error
	if options.StandardPaths != nil {
		standard = *options.StandardPaths
	} else {
		standard, standardErr = ResolveStandardPaths()
	}

	cfg := &Config{
		Server: ServerConfig{
			Host:         "127.0.0.1",
			Port:         "3001",
			ReadTimeout:  600 * time.Second,
			WriteTimeout: 600 * time.Second,
			IdleTimeout:  600 * time.Second,
		},
		Providers: ProvidersConfig{
			Anthropic: AnthropicProviderConfig{
				BaseURL:    "https://api.anthropic.com",
				Version:    "2023-06-01",
				MaxRetries: 3,
			},
			OpenAI: OpenAIProviderConfig{
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "",
			},
		},
		Storage: StorageConfig{
			DBPath:          standard.Database,
			MaxCaptureBytes: 10 * 1024 * 1024,
		},
		Subagents: SubagentsConfig{
			Enable:   false,
			Mappings: make(map[string]string),
		},
	}

	resolution := &Resolution{
		Config:         cfg,
		Standard:       standard,
		ConfigSource:   "default",
		DataDir:        standard.DataDir,
		DatabaseSource: "default",
		HostSource:     "default",
		PortSource:     "default",
	}

	configPath, source, explicit, err := resolveConfigPath(options.ConfigPath, standard.ConfigFile)
	if err != nil {
		return nil, err
	}
	if configPath != "" {
		if err := cfg.loadFromFile(configPath); err != nil {
			return nil, fmt.Errorf("load config %s: %w", configPath, err)
		}
		resolution.ConfigPath = configPath
		resolution.ConfigSource = source
		if cfg.Server.Host != "127.0.0.1" {
			resolution.HostSource = "config"
		}
		if cfg.Server.Port != "3001" {
			resolution.PortSource = "config"
		}
		if cfg.Storage.DBPath != standard.Database {
			resolution.DatabaseSource = "config"
			if !filepath.IsAbs(cfg.Storage.DBPath) {
				cfg.Storage.DBPath = filepath.Join(filepath.Dir(configPath), cfg.Storage.DBPath)
			}
		}
	} else if explicit {
		selected := options.ConfigPath
		if selected == "" {
			selected = os.Getenv("ACP_CONFIG")
		}
		return nil, fmt.Errorf("config file %s does not exist", selected)
	}

	cfg.applyFileDurations()

	if envHost := os.Getenv("ACP_HOST"); envHost != "" {
		cfg.Server.Host = envHost
		resolution.HostSource = "environment"
	}
	if envPort := firstEnvironment("ACP_PORT", "PORT"); envPort != "" {
		cfg.Server.Port = envPort
		resolution.PortSource = "environment"
	}
	if envToken := os.Getenv("ACP_ACCESS_TOKEN"); envToken != "" {
		cfg.Server.AccessToken = envToken
	}
	if envTimeout := os.Getenv("READ_TIMEOUT"); envTimeout != "" {
		cfg.Server.ReadTimeout = getDuration("READ_TIMEOUT", cfg.Server.ReadTimeout)
	}
	if envTimeout := os.Getenv("WRITE_TIMEOUT"); envTimeout != "" {
		cfg.Server.WriteTimeout = getDuration("WRITE_TIMEOUT", cfg.Server.WriteTimeout)
	}
	if envTimeout := os.Getenv("IDLE_TIMEOUT"); envTimeout != "" {
		cfg.Server.IdleTimeout = getDuration("IDLE_TIMEOUT", cfg.Server.IdleTimeout)
	}

	// Override Anthropic settings
	if envURL := os.Getenv("ANTHROPIC_FORWARD_URL"); envURL != "" {
		cfg.Providers.Anthropic.BaseURL = envURL
	}
	if envVersion := os.Getenv("ANTHROPIC_VERSION"); envVersion != "" {
		cfg.Providers.Anthropic.Version = envVersion
	}
	if envRetries := os.Getenv("ANTHROPIC_MAX_RETRIES"); envRetries != "" {
		cfg.Providers.Anthropic.MaxRetries = getInt("ANTHROPIC_MAX_RETRIES", cfg.Providers.Anthropic.MaxRetries)
	}

	// Override OpenAI settings
	if envURL := os.Getenv("OPENAI_BASE_URL"); envURL != "" {
		cfg.Providers.OpenAI.BaseURL = envURL
	}
	if envKey := os.Getenv("OPENAI_API_KEY"); envKey != "" {
		cfg.Providers.OpenAI.APIKey = envKey
	}

	// Override storage settings
	if envPath := os.Getenv("DB_PATH"); envPath != "" {
		cfg.Storage.DBPath, err = absolutePath(envPath)
		if err != nil {
			return nil, fmt.Errorf("resolve DB_PATH: %w", err)
		}
		resolution.DatabaseSource = "environment"
	} else if envDataDir := os.Getenv("ACP_DATA_DIR"); envDataDir != "" {
		resolvedDataDir, err := absolutePath(envDataDir)
		if err != nil {
			return nil, fmt.Errorf("resolve ACP_DATA_DIR: %w", err)
		}
		cfg.Storage.DBPath = filepath.Join(resolvedDataDir, "requests.db")
		resolution.DatabaseSource = "environment"
	}

	if options.DataDir != "" {
		resolvedDataDir, err := absolutePath(options.DataDir)
		if err != nil {
			return nil, fmt.Errorf("resolve --data-dir: %w", err)
		}
		cfg.Storage.DBPath = filepath.Join(resolvedDataDir, "requests.db")
		resolution.DatabaseSource = "cli"
	}
	if options.Host != "" {
		cfg.Server.Host = options.Host
		resolution.HostSource = "cli"
	}
	if options.Port != "" {
		cfg.Server.Port = options.Port
		resolution.PortSource = "cli"
	}

	if cfg.Storage.DBPath == "" {
		if standardErr != nil {
			return nil, fmt.Errorf("standard data directory is unavailable: %w; use --data-dir, DB_PATH, or storage.db_path", standardErr)
		}
		return nil, fmt.Errorf("standard data directory is unavailable; use --data-dir, DB_PATH, or storage.db_path")
	}
	if !filepath.IsAbs(cfg.Storage.DBPath) {
		cfg.Storage.DBPath, err = absolutePath(cfg.Storage.DBPath)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
	}
	resolution.DataDir = filepath.Dir(cfg.Storage.DBPath)
	if cfg.Storage.BackupDir == "" {
		cfg.Storage.BackupDir = filepath.Join(resolution.DataDir, "backups")
	} else if !filepath.IsAbs(cfg.Storage.BackupDir) {
		base := resolution.DataDir
		if resolution.ConfigPath != "" {
			base = filepath.Dir(resolution.ConfigPath)
		}
		cfg.Storage.BackupDir = filepath.Join(base, cfg.Storage.BackupDir)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// Sync legacy Anthropic config with new structure
	cfg.Anthropic = AnthropicConfig{
		BaseURL:    cfg.Providers.Anthropic.BaseURL,
		Version:    cfg.Providers.Anthropic.Version,
		MaxRetries: cfg.Providers.Anthropic.MaxRetries,
	}

	return resolution, nil
}

func resolveConfigPath(requested, standard string) (path, source string, explicit bool, err error) {
	if requested != "" {
		resolved, err := absolutePath(requested)
		if err != nil {
			return "", "", true, err
		}
		if _, err := os.Stat(resolved); err == nil {
			return resolved, "cli", true, nil
		} else if os.IsNotExist(err) {
			return "", "cli", true, nil
		} else {
			return "", "cli", true, err
		}
	}
	if environment := strings.TrimSpace(os.Getenv("ACP_CONFIG")); environment != "" {
		resolved, err := absolutePath(environment)
		if err != nil {
			return "", "", true, err
		}
		if _, err := os.Stat(resolved); err == nil {
			return resolved, "environment", true, nil
		} else if os.IsNotExist(err) {
			return "", "environment", true, nil
		} else {
			return "", "environment", true, err
		}
	}
	if standard != "" {
		if _, err := os.Stat(standard); err == nil {
			return standard, "standard", false, nil
		} else if !os.IsNotExist(err) {
			return "", "standard", false, err
		}
	}
	return "", "default", false, nil
}

func secureConfigFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure config file %s: %w", path, err)
	}
	return nil
}

func (r *Resolution) Prepare() error {
	if r.ConfigPath == "" {
		return nil
	}
	return secureConfigFile(r.ConfigPath)
}

func firstEnvironment(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func absolutePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	return filepath.Abs(value)
}

func (c *Config) applyFileDurations() {
	if duration, err := time.ParseDuration(c.Server.Timeouts.Read); err == nil && c.Server.Timeouts.Read != "" {
		c.Server.ReadTimeout = duration
	}
	if duration, err := time.ParseDuration(c.Server.Timeouts.Write); err == nil && c.Server.Timeouts.Write != "" {
		c.Server.WriteTimeout = duration
	}
	if duration, err := time.ParseDuration(c.Server.Timeouts.Idle); err == nil && c.Server.Timeouts.Idle != "" {
		c.Server.IdleTimeout = duration
	}
}

func (c *Config) validate() error {
	host := strings.Trim(strings.TrimSpace(c.Server.Host), "[]")
	if host == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	if port, err := strconv.Atoi(c.Server.Port); err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if c.Server.RequiresAccessToken() && len(c.Server.AccessToken) < 16 {
		return fmt.Errorf("server.access_token must contain at least 16 characters when server.host is not loopback")
	}
	if c.Storage.MaxCaptureBytes < 0 {
		return fmt.Errorf("storage.max_capture_bytes must be zero or greater")
	}
	if c.Web.RawRequestMaxDisplayChars < 0 {
		return fmt.Errorf("web.raw_request_max_display_chars must be zero or greater")
	}
	if c.Web.RawResponseMaxDisplayChars < 0 {
		return fmt.Errorf("web.raw_response_max_display_chars must be zero or greater")
	}
	return nil
}

func (c ServerConfig) RequiresAccessToken() bool {
	host := strings.Trim(strings.TrimSpace(c.Host), "[]")
	return !isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Config) loadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, c)
}

func getDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}

	return duration
}

func getInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return intValue
}

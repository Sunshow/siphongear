package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Server   ServerConfig   `koanf:"server"`
	Database DatabaseConfig `koanf:"database"`
	Auth     AuthConfig     `koanf:"auth"`
	Log      LogConfig      `koanf:"log"`
	Browser  BrowserConfig  `koanf:"browser"`
	Runner   RunnerConfig   `koanf:"runner"`
	History  HistoryConfig  `koanf:"history"`
}

type ServerConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

type DatabaseConfig struct {
	Driver string `koanf:"driver"` // sqlite | mysql | postgres
	DSN    string `koanf:"dsn"`
}

type AuthConfig struct {
	JWTSecret    string `koanf:"jwt_secret"`
	MasterKey    string `koanf:"master_key"` // 32 bytes hex/base64
	InitUsername string `koanf:"init_username"`
	InitPassword string `koanf:"init_password"`
	TokenTTLHrs  int    `koanf:"token_ttl_hrs"`
}

type LogConfig struct {
	Level  string `koanf:"level"`
	Pretty bool   `koanf:"pretty"`
}

type BrowserConfig struct {
	WSURL string `koanf:"ws_url"`
}

type RunnerConfig struct {
	MaxConcurrency int `koanf:"max_concurrency"`
	DefaultTimeout int `koanf:"default_timeout"` // seconds
}

type HistoryConfig struct {
	RetentionDays int `koanf:"retention_days"`
}

func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, err
		}
	}

	if err := k.Load(env.Provider("SIPHON_", ".", func(s string) string {
		key := strings.TrimPrefix(s, "SIPHON_")
		key = strings.ToLower(key)
		key = strings.ReplaceAll(key, "__", ".")
		return key
	}), nil); err != nil {
		return nil, err
	}

	cfg := &Config{History: HistoryConfig{RetentionDays: 7}}
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, err
	}
	setDefaults(cfg)
	if cfg.History.RetentionDays <= 0 {
		return nil, fmt.Errorf("history.retention_days must be a positive integer")
	}
	return cfg, nil
}

func setDefaults(c *Config) {
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 7080
	}
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Database.DSN == "" {
		c.Database.DSN = "data/siphongear.db"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Auth.TokenTTLHrs == 0 {
		c.Auth.TokenTTLHrs = 24 * 7
	}
	if c.Runner.MaxConcurrency == 0 {
		c.Runner.MaxConcurrency = 4
	}
	if c.Runner.DefaultTimeout == 0 {
		c.Runner.DefaultTimeout = 60
	}
}

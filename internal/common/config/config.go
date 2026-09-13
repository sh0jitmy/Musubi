// Copyright 2026 Musubi Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Author: sh0jitmy

// Package config provides application configuration loading and environment overrides.
package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config represents full system configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Backup    BackupConfig    `yaml:"backup"`
	Retention RetentionConfig `yaml:"retention"`
}

// ServerConfig defines HTTP and SNMP listener ports.
type ServerConfig struct {
	Port     string `yaml:"port"`
	TrapPort string `yaml:"trap_port"`
}

// DatabaseConfig defines driver and connection string.
type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// BackupConfig defines periodic backup worker options.
type BackupConfig struct {
	Enabled        bool   `yaml:"enabled"`
	IntervalHours  int    `yaml:"interval_hours"`
	RetentionCount int    `yaml:"retention_count"`
	Directory      string `yaml:"directory"`
}

// RetentionConfig defines log purge settings.
type RetentionConfig struct {
	IntervalHours int `yaml:"interval_hours"`
	Days          int `yaml:"days"`
}

// DefaultConfig returns safe standalone defaults (SQLite-first, Docker-free).
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:     "8080",
			TrapPort: "162",
		},
		Database: DatabaseConfig{
			Driver: "sqlite3",
			DSN:    "musubi.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		},
		Backup: BackupConfig{
			Enabled:        true,
			IntervalHours:  24,
			RetentionCount: 7,
			Directory:      "./data/backups",
		},
		Retention: RetentionConfig{
			IntervalHours: 24,
			Days:          30,
		},
	}
}

// Load loads configuration from an optional file path and applies environment variable overrides.
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// If no path given, check CONFIG_PATH or default "./config.yaml"
	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
	}
	if configPath == "" && fileExists("config.yaml") {
		configPath = "config.yaml"
	}

	if configPath != "" {
		//nolint:gosec // user specified config file path
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file '%s': %w", configPath, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse yaml config '%s': %w", configPath, err)
		}
	}

	// Environment variable overrides
	if p := os.Getenv("PORT"); p != "" {
		cfg.Server.Port = p
	}
	if tp := os.Getenv("SNMP_TRAP_PORT"); tp != "" {
		cfg.Server.TrapPort = tp
	}
	if d := os.Getenv("DATABASE_DRIVER"); d != "" {
		cfg.Database.Driver = d
	}
	if dsn := os.Getenv("DATABASE_DSN"); dsn != "" {
		cfg.Database.DSN = dsn
	}
	if be := os.Getenv("BACKUP_ENABLED"); be != "" {
		cfg.Backup.Enabled = (be == "true" || be == "1" || be == "yes")
	}
	if bih := os.Getenv("BACKUP_INTERVAL_HOURS"); bih != "" {
		if val, err := strconv.Atoi(bih); err == nil && val > 0 {
			cfg.Backup.IntervalHours = val
		}
	}
	if brc := os.Getenv("BACKUP_RETENTION_COUNT"); brc != "" {
		if val, err := strconv.Atoi(brc); err == nil && val > 0 {
			cfg.Backup.RetentionCount = val
		}
	}
	if bdir := os.Getenv("BACKUP_DIR"); bdir != "" {
		cfg.Backup.Directory = bdir
	}
	if rih := os.Getenv("RETENTION_INTERVAL_HOURS"); rih != "" {
		if val, err := strconv.Atoi(rih); err == nil && val > 0 {
			cfg.Retention.IntervalHours = val
		}
	}
	if rd := os.Getenv("RETENTION_DAYS"); rd != "" {
		if val, err := strconv.Atoi(rd); err == nil && val > 0 {
			cfg.Retention.Days = val
		}
	}

	return cfg, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

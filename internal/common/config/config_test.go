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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Default(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	assert.Equal(t, "8080", cfg.Server.Port)
	assert.Equal(t, "162", cfg.Server.TrapPort)
	assert.Equal(t, "sqlite3", cfg.Database.Driver)
	assert.True(t, cfg.Backup.Enabled)
	assert.Equal(t, 24, cfg.Backup.IntervalHours)
	assert.Equal(t, 7, cfg.Backup.RetentionCount)
}

func TestConfig_LoadWithYamlAndEnv(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "musubi_cfg_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	//nolint:gosec // test mock dsn without real credentials
	yamlContent := `
server:
  port: "9090"
  trap_port: "1162"
database:
  driver: "postgres"
  dsn: "postgres://localhost:5432/musubi"
backup:
  enabled: false
  interval_hours: 12
  retention_count: 14
  directory: "/tmp/custom_backups"
retention:
  interval_hours: 48
  days: 60
`
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0600))

	// 1. Load from YAML
	cfg, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "9090", cfg.Server.Port)
	assert.Equal(t, "1162", cfg.Server.TrapPort)
	assert.Equal(t, "postgres", cfg.Database.Driver)
	assert.False(t, cfg.Backup.Enabled)
	assert.Equal(t, 12, cfg.Backup.IntervalHours)
	assert.Equal(t, 14, cfg.Backup.RetentionCount)
	assert.Equal(t, "/tmp/custom_backups", cfg.Backup.Directory)
	assert.Equal(t, 48, cfg.Retention.IntervalHours)
	assert.Equal(t, 60, cfg.Retention.Days)

	// 2. Override with environment variables
	t.Setenv("PORT", "7070")
	t.Setenv("DATABASE_DRIVER", "sqlite3")
	t.Setenv("BACKUP_ENABLED", "true")
	t.Setenv("BACKUP_RETENTION_COUNT", "20")

	cfgOverridden, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "7070", cfgOverridden.Server.Port)
	assert.Equal(t, "sqlite3", cfgOverridden.Database.Driver)
	assert.True(t, cfgOverridden.Backup.Enabled)
	assert.Equal(t, 20, cfgOverridden.Backup.RetentionCount)
}

func TestConfig_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := Load("/nonexistent/path/to/config.yaml")
	assert.Error(t, err)
}

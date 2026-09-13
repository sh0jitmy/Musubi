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

package database

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sh0jitmy/musubi/ent"
)

// BackupManifest contains metadata about the backup archive.
type BackupManifest struct {
	Version     string         `json:"version"`
	Timestamp   time.Time      `json:"timestamp"`
	Driver      string         `json:"driver,omitempty"`
	TableCounts map[string]int `json:"table_counts"`
	Checksum    string         `json:"checksum"`
}

// BackupUser represents the serializable User entity including password hash.
type BackupUser struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

// BackupCredentialProfile preserves sensitive passphrases across backups.
type BackupCredentialProfile struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	SecLevel       string    `json:"sec_level"`
	Community      string    `json:"community"`
	Username       string    `json:"username"`
	AuthProtocol   string    `json:"auth_protocol"`
	AuthPassphrase string    `json:"auth_passphrase"`
	PrivProtocol   string    `json:"priv_protocol"`
	PrivPassphrase string    `json:"priv_passphrase"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// BackupTarget represents the serializable Target entity.
type BackupTarget struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	Status        string            `json:"status"`
	Labels        map[string]string `json:"labels"`
	CredentialID  string            `json:"credential_id"`
	PollingConfig map[string]any    `json:"polling_config"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// BackupScenario represents the serializable Scenario entity.
type BackupScenario struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	CurrentVersion int       `json:"current_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// BackupScenarioVersion represents the serializable ScenarioVersion entity.
type BackupScenarioVersion struct {
	ID           string         `json:"id"`
	ScenarioID   string         `json:"scenario_id"`
	Version      int            `json:"version"`
	DslYaml      string         `json:"dsl_yaml"`
	InputsSchema map[string]any `json:"inputs_schema"`
	TargetNames  []string       `json:"target_names"`
	CreatedAt    time.Time      `json:"created_at"`
}

// BackupJob represents the serializable Job entity.
type BackupJob struct {
	ID              string         `json:"id"`
	ScenarioID      string         `json:"scenario_id"`
	ScenarioVersion int            `json:"scenario_version"`
	Status          string         `json:"status"`
	DynamicInputs   map[string]any `json:"dynamic_inputs"`
	LockedTargets   []string       `json:"locked_targets"`
	TriggeredBy     string         `json:"triggered_by"`
	StartedAt       *time.Time     `json:"started_at"`
	FinishedAt      *time.Time     `json:"finished_at"`
	CreatedAt       time.Time      `json:"created_at"`
}

// BackupJobStep represents the serializable JobStep entity.
type BackupJobStep struct {
	ID           string         `json:"id"`
	JobID        string         `json:"job_id"`
	StepID       string         `json:"step_id"`
	StepOrder    int            `json:"step_order"`
	StepType     string         `json:"step_type"`
	Status       string         `json:"status"`
	ResultOutput map[string]any `json:"result_output"`
	Error        string         `json:"error"`
	ExecutedAt   time.Time      `json:"executed_at"`
}

// BackupStateTransitionLog represents the serializable StateTransitionLog entity.
type BackupStateTransitionLog struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	StateKey  string    `json:"state_key"`
	OldValue  string    `json:"old_value"`
	NewValue  string    `json:"new_value"`
	Trigger   string    `json:"trigger"`
	CreatedAt time.Time `json:"created_at"`
}

// BackupAuditLog represents the serializable AuditLog entity.
type BackupAuditLog struct {
	ID         string         `json:"id"`
	Action     string         `json:"action"`
	UserID     string         `json:"user_id"`
	Role       string         `json:"role"`
	IP         string         `json:"ip"`
	TargetID   string         `json:"target_id"`
	ScenarioID string         `json:"scenario_id"`
	Diff       map[string]any `json:"diff"`
	CreatedAt  time.Time      `json:"created_at"`
}

// BackupData contains structured dumps of all database entities.
type BackupData struct {
	Users               []BackupUser               `json:"users"`
	CredentialProfiles  []BackupCredentialProfile  `json:"credential_profiles"`
	Targets             []BackupTarget             `json:"targets"`
	Scenarios           []BackupScenario           `json:"scenarios"`
	ScenarioVersions    []BackupScenarioVersion    `json:"scenario_versions"`
	Jobs                []BackupJob                `json:"jobs"`
	JobSteps            []BackupJobStep            `json:"job_steps"`
	StateTransitionLogs []BackupStateTransitionLog `json:"state_transition_logs"`
	AuditLogs           []BackupAuditLog           `json:"audit_logs"`
}

// BackupResult describes a successfully generated backup archive.
type BackupResult struct {
	Filename    string         `json:"filename"`
	DownloadURL string         `json:"download_url"`
	FilePath    string         `json:"file_path"`
	TableCounts map[string]int `json:"table_counts"`
	Checksum    string         `json:"checksum"`
	CreatedAt   time.Time      `json:"created_at"`
}

// RestoreResult describes the result of restoring a backup archive.
type RestoreResult struct {
	Restored    bool           `json:"restored"`
	TablesCount int            `json:"tables_count"`
	TableCounts map[string]int `json:"table_counts"`
}

// CreateBackupArchive exports all database entities into a compressed .tar.gz archive with a SHA-256 checksum.
func CreateBackupArchive(ctx context.Context, client *ent.Client, backupDir string) (*BackupResult, error) {
	if backupDir == "" {
		backupDir = "./data/backups"
	}
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	data := BackupData{
		Users:               make([]BackupUser, 0),
		CredentialProfiles:  make([]BackupCredentialProfile, 0),
		Targets:             make([]BackupTarget, 0),
		Scenarios:           make([]BackupScenario, 0),
		ScenarioVersions:    make([]BackupScenarioVersion, 0),
		Jobs:                make([]BackupJob, 0),
		JobSteps:            make([]BackupJobStep, 0),
		StateTransitionLogs: make([]BackupStateTransitionLog, 0),
		AuditLogs:           make([]BackupAuditLog, 0),
	}

	// 1. Users
	users, err := client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query users for backup: %w", err)
	}
	for _, u := range users {
		data.Users = append(data.Users, BackupUser{
			ID:           u.ID,
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
		})
	}

	// 2. CredentialProfiles
	creds, err := client.CredentialProfile.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query credentials for backup: %w", err)
	}
	for _, c := range creds {
		data.CredentialProfiles = append(data.CredentialProfiles, BackupCredentialProfile{
			ID:             c.ID,
			Name:           c.Name,
			Version:        c.Version,
			SecLevel:       c.SecLevel,
			Community:      c.Community,
			Username:       c.Username,
			AuthProtocol:   c.AuthProtocol,
			AuthPassphrase: c.AuthPassphrase,
			PrivProtocol:   c.PrivProtocol,
			PrivPassphrase: c.PrivPassphrase,
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
		})
	}

	// 3. Targets
	targets, err := client.Target.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query targets for backup: %w", err)
	}
	for _, t := range targets {
		data.Targets = append(data.Targets, BackupTarget{
			ID:            t.ID,
			Name:          t.Name,
			Description:   t.Description,
			Host:          t.Host,
			Port:          t.Port,
			Status:        t.Status,
			Labels:        t.Labels,
			CredentialID:  t.CredentialID,
			PollingConfig: t.PollingConfig,
			CreatedAt:     t.CreatedAt,
			UpdatedAt:     t.UpdatedAt,
		})
	}

	// 4. Scenarios
	scenarios, err := client.Scenario.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query scenarios for backup: %w", err)
	}
	for _, s := range scenarios {
		data.Scenarios = append(data.Scenarios, BackupScenario{
			ID:             s.ID,
			Name:           s.Name,
			Description:    s.Description,
			CurrentVersion: s.CurrentVersion,
			CreatedAt:      s.CreatedAt,
			UpdatedAt:      s.UpdatedAt,
		})
	}

	// 5. ScenarioVersions
	versions, err := client.ScenarioVersion.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query scenario versions for backup: %w", err)
	}
	for _, v := range versions {
		data.ScenarioVersions = append(data.ScenarioVersions, BackupScenarioVersion{
			ID:           v.ID,
			ScenarioID:   v.ScenarioID,
			Version:      v.Version,
			DslYaml:      v.DslYaml,
			InputsSchema: v.InputsSchema,
			TargetNames:  v.TargetNames,
			CreatedAt:    v.CreatedAt,
		})
	}

	// 6. Jobs
	jobs, err := client.Job.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs for backup: %w", err)
	}
	for _, j := range jobs {
		data.Jobs = append(data.Jobs, BackupJob{
			ID:              j.ID,
			ScenarioID:      j.ScenarioID,
			ScenarioVersion: j.ScenarioVersion,
			Status:          j.Status,
			DynamicInputs:   j.DynamicInputs,
			LockedTargets:   j.LockedTargets,
			TriggeredBy:     j.TriggeredBy,
			StartedAt:       j.StartedAt,
			FinishedAt:      j.FinishedAt,
			CreatedAt:       j.CreatedAt,
		})
	}

	// 7. JobSteps
	steps, err := client.JobStep.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query job steps for backup: %w", err)
	}
	for _, js := range steps {
		data.JobSteps = append(data.JobSteps, BackupJobStep{
			ID:           js.ID,
			JobID:        js.JobID,
			StepID:       js.StepID,
			StepOrder:    js.StepOrder,
			StepType:     js.StepType,
			Status:       js.Status,
			ResultOutput: js.ResultOutput,
			Error:        js.Error,
			ExecutedAt:   js.ExecutedAt,
		})
	}

	// 8. StateTransitionLogs
	stLogs, err := client.StateTransitionLog.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query state transition logs for backup: %w", err)
	}
	for _, st := range stLogs {
		data.StateTransitionLogs = append(data.StateTransitionLogs, BackupStateTransitionLog{
			ID:        st.ID,
			Target:    st.Target,
			StateKey:  st.StateKey,
			OldValue:  st.OldValue,
			NewValue:  st.NewValue,
			Trigger:   st.Trigger,
			CreatedAt: st.CreatedAt,
		})
	}

	// 9. AuditLogs
	auditLogs, err := client.AuditLog.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs for backup: %w", err)
	}
	for _, a := range auditLogs {
		data.AuditLogs = append(data.AuditLogs, BackupAuditLog{
			ID:         a.ID,
			Action:     a.Action,
			UserID:     a.UserID,
			Role:       a.Role,
			IP:         a.IP,
			TargetID:   a.TargetID,
			ScenarioID: a.ScenarioID,
			Diff:       a.Diff,
			CreatedAt:  a.CreatedAt,
		})
	}

	tableCounts := map[string]int{
		"users":                 len(data.Users),
		"credential_profiles":   len(data.CredentialProfiles),
		"targets":               len(data.Targets),
		"scenarios":             len(data.Scenarios),
		"scenario_versions":     len(data.ScenarioVersions),
		"jobs":                  len(data.Jobs),
		"job_steps":             len(data.JobSteps),
		"state_transition_logs": len(data.StateTransitionLogs),
		"audit_logs":            len(data.AuditLogs),
	}

	dataBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize backup data: %w", err)
	}

	hash := sha256.Sum256(dataBytes)
	checksum := hex.EncodeToString(hash[:])

	now := time.Now().UTC()
	manifest := BackupManifest{
		Version:     "1.0.0",
		Timestamp:   now,
		TableCounts: tableCounts,
		Checksum:    checksum,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize backup manifest: %w", err)
	}

	// Generate tar.gz
	filename := fmt.Sprintf("musubi-backup-%s.tar.gz", now.Format("20060102-150405"))
	archivePath := filepath.Join(backupDir, filename)

	//nolint:gosec // backup archive path generated internally
	f, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Write manifest.json
	if err := writeTarEntry(tw, "manifest.json", manifestBytes); err != nil {
		return nil, err
	}
	// Write data.json
	if err := writeTarEntry(tw, "data.json", dataBytes); err != nil {
		return nil, err
	}
	// Write checksum.sha256
	checksumFileContent := fmt.Sprintf("%s  data.json\n", checksum)
	if err := writeTarEntry(tw, "checksum.sha256", []byte(checksumFileContent)); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return &BackupResult{
		Filename:    filename,
		DownloadURL: fmt.Sprintf("/v1/system/downloads/%s", filename),
		FilePath:    archivePath,
		TableCounts: tableCounts,
		Checksum:    checksum,
		CreatedAt:   now,
	}, nil
}

func writeTarEntry(tw *tar.Writer, name string, content []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0600,
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}
	if _, err := tw.Write(content); err != nil {
		return fmt.Errorf("failed to write tar content for %s: %w", name, err)
	}
	return nil
}

// RestoreBackupArchive unpacks a .tar.gz backup archive, verifies SHA-256 integrity,
// and applies full system restoration in a single transaction.
func RestoreBackupArchive(ctx context.Context, client *ent.Client, archivePath string) (*RestoreResult, error) {
	//nolint:gosec // archive file path provided by user or resolved internally
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open backup archive: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() {
		_ = gr.Close()
	}()

	tr := tar.NewReader(gr)

	var (
		manifestBytes []byte
		dataBytes     []byte
		checksumBytes []byte
	)

	for {
		hdr, trErr := tr.Next()
		if trErr == io.EOF {
			break
		}
		if trErr != nil {
			return nil, fmt.Errorf("failed to read tar entry: %w", trErr)
		}

		var buf bytes.Buffer
		// Limit reading to 500MB to prevent decompression bombs
		if _, cErr := io.CopyN(&buf, tr, 500*1024*1024); cErr != nil && cErr != io.EOF {
			return nil, fmt.Errorf("failed to read content for %s: %w", hdr.Name, cErr)
		}

		switch hdr.Name {
		case "manifest.json":
			manifestBytes = buf.Bytes()
		case "data.json":
			dataBytes = buf.Bytes()
		case "checksum.sha256":
			checksumBytes = buf.Bytes()
		}
	}

	if len(dataBytes) == 0 {
		return nil, fmt.Errorf("invalid backup archive: missing data.json")
	}

	// Verify SHA-256 checksum
	hash := sha256.Sum256(dataBytes)
	actualChecksum := hex.EncodeToString(hash[:])

	if len(manifestBytes) > 0 {
		var manifest BackupManifest
		if mErr := json.Unmarshal(manifestBytes, &manifest); mErr == nil && manifest.Checksum != "" {
			if manifest.Checksum != actualChecksum {
				return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", manifest.Checksum, actualChecksum)
			}
		}
	}

	if len(checksumBytes) > 0 {
		csText := strings.TrimSpace(string(checksumBytes))
		parts := strings.Fields(csText)
		if len(parts) > 0 && parts[0] != actualChecksum {
			return nil, fmt.Errorf("checksum.sha256 verification failed: expected %s, got %s", parts[0], actualChecksum)
		}
	}

	var data BackupData
	if dErr := json.Unmarshal(dataBytes, &data); dErr != nil {
		return nil, fmt.Errorf("failed to unmarshal backup data: %w", dErr)
	}

	// Execute restore within a single transaction
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction for restore: %w", err)
	}

	// Rollback handler on error
	rollback := func(cause error) (*RestoreResult, error) {
		_ = tx.Rollback()
		return nil, cause
	}

	// 1. Purge existing records in reverse dependency order
	if _, err := tx.JobStep.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean job_steps: %w", err))
	}
	if _, err := tx.Job.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean jobs: %w", err))
	}
	if _, err := tx.ScenarioVersion.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean scenario_versions: %w", err))
	}
	if _, err := tx.Scenario.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean scenarios: %w", err))
	}
	if _, err := tx.Target.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean targets: %w", err))
	}
	if _, err := tx.CredentialProfile.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean credential_profiles: %w", err))
	}
	if _, err := tx.StateTransitionLog.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean state_transition_logs: %w", err))
	}
	if _, err := tx.AuditLog.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean audit_logs: %w", err))
	}
	if _, err := tx.User.Delete().Exec(ctx); err != nil {
		return rollback(fmt.Errorf("failed to clean users: %w", err))
	}

	// 2. Insert records in dependency order
	// Users
	for _, u := range data.Users {
		_, err := tx.User.Create().
			SetUsername(u.Username).
			SetPasswordHash(u.PasswordHash).
			Save(ctx)
		if err != nil {
			return rollback(fmt.Errorf("failed to restore user '%s': %w", u.Username, err))
		}
	}

	// CredentialProfiles
	for _, c := range data.CredentialProfiles {
		b := tx.CredentialProfile.Create().
			SetID(c.ID).
			SetName(c.Name).
			SetVersion(c.Version).
			SetSecLevel(c.SecLevel).
			SetCommunity(c.Community).
			SetUsername(c.Username).
			SetAuthProtocol(c.AuthProtocol).
			SetAuthPassphrase(c.AuthPassphrase).
			SetPrivProtocol(c.PrivProtocol).
			SetPrivPassphrase(c.PrivPassphrase).
			SetCreatedAt(c.CreatedAt).
			SetUpdatedAt(c.UpdatedAt)
		if _, err := b.Save(ctx); err != nil {
			return rollback(fmt.Errorf("failed to restore credential '%s': %w", c.Name, err))
		}
	}

	// Targets
	for _, t := range data.Targets {
		b := tx.Target.Create().
			SetID(t.ID).
			SetName(t.Name).
			SetDescription(t.Description).
			SetHost(t.Host).
			SetPort(t.Port).
			SetStatus(t.Status).
			SetCredentialID(t.CredentialID).
			SetCreatedAt(t.CreatedAt).
			SetUpdatedAt(t.UpdatedAt)
		if t.Labels != nil {
			b.SetLabels(t.Labels)
		}
		if t.PollingConfig != nil {
			b.SetPollingConfig(t.PollingConfig)
		}
		if _, err := b.Save(ctx); err != nil {
			return rollback(fmt.Errorf("failed to restore target '%s': %w", t.Name, err))
		}
	}

	// Scenarios
	for _, s := range data.Scenarios {
		_, err := tx.Scenario.Create().
			SetID(s.ID).
			SetName(s.Name).
			SetDescription(s.Description).
			SetCurrentVersion(s.CurrentVersion).
			SetCreatedAt(s.CreatedAt).
			SetUpdatedAt(s.UpdatedAt).
			Save(ctx)
		if err != nil {
			return rollback(fmt.Errorf("failed to restore scenario '%s': %w", s.Name, err))
		}
	}

	// ScenarioVersions
	for _, sv := range data.ScenarioVersions {
		b := tx.ScenarioVersion.Create().
			SetID(sv.ID).
			SetScenarioID(sv.ScenarioID).
			SetVersion(sv.Version).
			SetDslYaml(sv.DslYaml).
			SetCreatedAt(sv.CreatedAt)
		if sv.InputsSchema != nil {
			b.SetInputsSchema(sv.InputsSchema)
		}
		if sv.TargetNames != nil {
			b.SetTargetNames(sv.TargetNames)
		}
		if _, err := b.Save(ctx); err != nil {
			return rollback(fmt.Errorf("failed to restore scenario version '%s-v%d': %w", sv.ScenarioID, sv.Version, err))
		}
	}

	// Jobs
	for _, j := range data.Jobs {
		b := tx.Job.Create().
			SetID(j.ID).
			SetScenarioID(j.ScenarioID).
			SetScenarioVersion(j.ScenarioVersion).
			SetStatus(j.Status).
			SetTriggeredBy(j.TriggeredBy).
			SetCreatedAt(j.CreatedAt)
		if j.DynamicInputs != nil {
			b.SetDynamicInputs(j.DynamicInputs)
		}
		if j.LockedTargets != nil {
			b.SetLockedTargets(j.LockedTargets)
		}
		if j.StartedAt != nil {
			b.SetStartedAt(*j.StartedAt)
		}
		if j.FinishedAt != nil {
			b.SetFinishedAt(*j.FinishedAt)
		}
		if _, err := b.Save(ctx); err != nil {
			return rollback(fmt.Errorf("failed to restore job '%s': %w", j.ID, err))
		}
	}

	// JobSteps
	for _, js := range data.JobSteps {
		b := tx.JobStep.Create().
			SetID(js.ID).
			SetJobID(js.JobID).
			SetStepID(js.StepID).
			SetStepOrder(js.StepOrder).
			SetStepType(js.StepType).
			SetStatus(js.Status).
			SetError(js.Error).
			SetExecutedAt(js.ExecutedAt)
		if js.ResultOutput != nil {
			b.SetResultOutput(js.ResultOutput)
		}
		if _, err := b.Save(ctx); err != nil {
			return rollback(fmt.Errorf("failed to restore job step '%s': %w", js.ID, err))
		}
	}

	// StateTransitionLogs
	for _, st := range data.StateTransitionLogs {
		_, err := tx.StateTransitionLog.Create().
			SetID(st.ID).
			SetTarget(st.Target).
			SetStateKey(st.StateKey).
			SetOldValue(st.OldValue).
			SetNewValue(st.NewValue).
			SetTrigger(st.Trigger).
			SetCreatedAt(st.CreatedAt).
			Save(ctx)
		if err != nil {
			return rollback(fmt.Errorf("failed to restore state transition log '%s': %w", st.ID, err))
		}
	}

	// AuditLogs
	for _, a := range data.AuditLogs {
		b := tx.AuditLog.Create().
			SetID(a.ID).
			SetAction(a.Action).
			SetUserID(a.UserID).
			SetRole(a.Role).
			SetIP(a.IP).
			SetTargetID(a.TargetID).
			SetScenarioID(a.ScenarioID).
			SetCreatedAt(a.CreatedAt)
		if a.Diff != nil {
			b.SetDiff(a.Diff)
		}
		if _, err := b.Save(ctx); err != nil {
			return rollback(fmt.Errorf("failed to restore audit log '%s': %w", a.ID, err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit restore transaction: %w", err)
	}

	tableCounts := map[string]int{
		"users":                 len(data.Users),
		"credential_profiles":   len(data.CredentialProfiles),
		"targets":               len(data.Targets),
		"scenarios":             len(data.Scenarios),
		"scenario_versions":     len(data.ScenarioVersions),
		"jobs":                  len(data.Jobs),
		"job_steps":             len(data.JobSteps),
		"state_transition_logs": len(data.StateTransitionLogs),
		"audit_logs":            len(data.AuditLogs),
	}

	return &RestoreResult{
		Restored:    true,
		TablesCount: 9,
		TableCounts: tableCounts,
	}, nil
}

// RotateBackups removes archives in backupDir exceeding retentionCount (oldest first).
func RotateBackups(backupDir string, retentionCount int) (int, error) {
	if retentionCount <= 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read backup dir: %w", err)
	}

	type backupFileInfo struct {
		path    string
		modTime time.Time
	}

	var archives []backupFileInfo
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "musubi-backup-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			info, err := e.Info()
			if err == nil {
				archives = append(archives, backupFileInfo{
					path:    filepath.Join(backupDir, e.Name()),
					modTime: info.ModTime(),
				})
			}
		}
	}

	if len(archives) <= retentionCount {
		return 0, nil
	}

	// Sort newest first
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].modTime.After(archives[j].modTime)
	})

	deletedCount := 0
	for i := retentionCount; i < len(archives); i++ {
		if err := os.Remove(archives[i].path); err == nil {
			deletedCount++
			slog.Info("Rotated old backup archive", "path", archives[i].path)
		}
	}

	return deletedCount, nil
}

// StartBackgroundBackup initiates an in-process, cron-free periodic backup worker.
func StartBackgroundBackup(ctx context.Context, client *ent.Client, interval time.Duration, backupDir string, retentionCount int) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if retentionCount <= 0 {
		retentionCount = 7
	}
	if backupDir == "" {
		backupDir = "./data/backups"
	}

	slog.Info("Starting in-process Scheduled Backup Worker",
		"interval", interval.String(),
		"backup_dir", backupDir,
		"retention_count", retentionCount,
	)

	//nolint:gosec // background worker manages independent ticker lifecycle
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopping Scheduled Backup Worker...")
				return
			case <-ticker.C:
				bCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				res, err := CreateBackupArchive(bCtx, client, backupDir)
				cancel()
				if err != nil {
					slog.Error("Scheduled backup failed", "error", err)
				} else {
					slog.Info("Scheduled backup created successfully",
						"filename", res.Filename,
						"checksum", res.Checksum,
					)
					if rotated, rErr := RotateBackups(backupDir, retentionCount); rErr != nil {
						slog.Warn("Failed to rotate backups", "error", rErr)
					} else if rotated > 0 {
						slog.Info("Backup rotation completed", "deleted_archives", rotated)
					}
				}
			}
		}
	}()
}

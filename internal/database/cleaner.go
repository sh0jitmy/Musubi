// Copyright 2026 [Copyright Holder]
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
// Author: [YOUR_NAME]

package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sh0jitmy/musubi/ent"
	"github.com/sh0jitmy/musubi/ent/auditlog"
	"github.com/sh0jitmy/musubi/ent/job"
	"github.com/sh0jitmy/musubi/ent/statetransitionlog"
)

// PurgeResult summarizes the number of deleted records across tables.
type PurgeResult struct {
	StateTransitionLogsDeleted int       `json:"state_transition_logs_deleted"`
	JobsDeleted                int       `json:"jobs_deleted"`
	AuditLogsDeleted           int       `json:"audit_logs_deleted"`
	CutoffTime                 time.Time `json:"cutoff_time"`
	RetentionDays              int       `json:"retention_days"`
}

// PurgeExpiredRecords deletes records older than retentionDays across state_transition_logs, jobs, and audit_logs.
func PurgeExpiredRecords(ctx context.Context, client *ent.Client, retentionDays int) (*PurgeResult, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	res := &PurgeResult{
		CutoffTime:    cutoff,
		RetentionDays: retentionDays,
	}

	// 1. Purge state_transition_logs
	stDeleted, err := client.StateTransitionLog.Delete().
		Where(statetransitionlog.CreatedAtLT(cutoff)).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to purge state transition logs: %w", err)
	}
	res.StateTransitionLogsDeleted = stDeleted

	// 2. Purge finished/old jobs
	jobDeleted, err := client.Job.Delete().
		Where(job.CreatedAtLT(cutoff)).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to purge old jobs: %w", err)
	}
	res.JobsDeleted = jobDeleted

	// 3. Purge old audit logs
	auditDeleted, err := client.AuditLog.Delete().
		Where(auditlog.CreatedAtLT(cutoff)).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to purge audit logs: %w", err)
	}
	res.AuditLogsDeleted = auditDeleted

	return res, nil
}

// StartBackgroundCleaner starts an in-process, cross-platform background worker that periodically purges expired records.
func StartBackgroundCleaner(ctx context.Context, client *ent.Client, interval time.Duration, retentionDays int) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if retentionDays <= 0 {
		retentionDays = 30
	}

	slog.Info("Starting in-process Log Retention Cleaner Worker",
		"interval", interval.String(),
		"retention_days", retentionDays,
	)

	//nolint:gosec // background worker uses independent context for cleanup execution
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopping Log Retention Cleaner Worker...")
				return
			case <-ticker.C:
				purgeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				res, err := PurgeExpiredRecords(purgeCtx, client, retentionDays)
				cancel()
				if err != nil {
					slog.Error("Log Retention Cleaner run failed", "error", err)
				} else {
					slog.Info("Log Retention Cleaner run completed successfully",
						"state_transition_logs_deleted", res.StateTransitionLogsDeleted,
						"jobs_deleted", res.JobsDeleted,
						"audit_logs_deleted", res.AuditLogsDeleted,
						"cutoff", res.CutoffTime.Format(time.RFC3339),
					)
				}
			}
		}
	}()
}

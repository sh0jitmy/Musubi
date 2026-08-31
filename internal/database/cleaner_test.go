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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleaner_PurgeAndBackgroundWorker(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := NewClient(ctx, "sqlite3", "file:cleaner_test?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer func() {
		_ = client.Close()
	}()

	// Insert test old records
	pastTime := time.Now().Add(-60 * 24 * time.Hour)

	_, err = client.StateTransitionLog.Create().
		SetID("st-old-1").
		SetTarget("spine1").
		SetStateKey("IF-MIB::ifOperStatus.1").
		SetOldValue("down").
		SetNewValue("up").
		SetTrigger("TRAP").
		SetCreatedAt(pastTime).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.StateTransitionLog.Create().
		SetID("st-recent-1").
		SetTarget("spine1").
		SetStateKey("IF-MIB::ifOperStatus.1").
		SetOldValue("up").
		SetNewValue("down").
		SetTrigger("TRAP").
		SetCreatedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.Job.Create().
		SetID("job-old-1").
		SetScenarioID("sc-1").
		SetStatus("SUCCESS").
		SetCreatedAt(pastTime).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.AuditLog.Create().
		SetID("audit-old-1").
		SetAction("target.delete").
		SetCreatedAt(pastTime).
		Save(ctx)
	require.NoError(t, err)

	// Purge records older than 30 days
	res, err := PurgeExpiredRecords(ctx, client, 30)
	require.NoError(t, err)
	assert.Equal(t, 1, res.StateTransitionLogsDeleted)
	assert.Equal(t, 1, res.JobsDeleted)
	assert.Equal(t, 1, res.AuditLogsDeleted)

	// Remaining records
	stCount, err := client.StateTransitionLog.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, stCount)

	// Test background cleaner worker startup and shutdown
	workerCtx, workerCancel := context.WithCancel(context.Background())
	StartBackgroundCleaner(workerCtx, client, 10*time.Millisecond, 30)
	time.Sleep(50 * time.Millisecond)
	workerCancel()
}

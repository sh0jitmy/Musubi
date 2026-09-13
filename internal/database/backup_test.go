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
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sh0jitmy/musubi/ent/credentialprofile"
	"github.com/sh0jitmy/musubi/ent/target"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupAndRestore_FullLifecycle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "musubi_backup_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	client, err := NewClient(ctx, "sqlite3", "file:backup_lifecycle_test?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// 1. Seed initial data
	err = SeedAdminUser(ctx, client)
	require.NoError(t, err)

	cred, err := client.CredentialProfile.Create().
		SetID("cred-test-1").
		SetName("v3-test").
		SetVersion("v3").
		SetSecLevel("authPriv").
		SetUsername("admin").
		SetAuthProtocol("SHA256").
		SetAuthPassphrase("secret-auth-pass").
		SetPrivProtocol("AES128").
		SetPrivPassphrase("secret-priv-pass").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.Target.Create().
		SetID("tg-test-1").
		SetName("spine1").
		SetHost("192.0.2.10").
		SetPort(161).
		SetStatus("ONLINE").
		SetCredentialID(cred.ID).
		SetLabels(map[string]string{"env": "test"}).
		Save(ctx)
	require.NoError(t, err)

	sc, err := client.Scenario.Create().
		SetID("sc-test-1").
		SetName("bgp-check").
		SetDescription("BGP status test scenario").
		SetCurrentVersion(1).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ScenarioVersion.Create().
		SetID("scv-test-1").
		SetScenarioID(sc.ID).
		SetVersion(1).
		SetDslYaml("name: bgp-check\nsteps: []\n").
		Save(ctx)
	require.NoError(t, err)

	job, err := client.Job.Create().
		SetID("job-test-1").
		SetScenarioID(sc.ID).
		SetScenarioVersion(1).
		SetStatus("SUCCESS").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.JobStep.Create().
		SetID("step-test-1").
		SetJobID(job.ID).
		SetStepID("s1").
		SetStepOrder(1).
		SetStepType("action").
		SetStatus("SUCCESS").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.StateTransitionLog.Create().
		SetID("st-test-1").
		SetTarget("spine1").
		SetStateKey("IF-MIB::ifOperStatus.1").
		SetOldValue("down").
		SetNewValue("up").
		SetTrigger("TRAP").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.AuditLog.Create().
		SetID("audit-test-1").
		SetAction("target.create").
		SetUserID("admin").
		Save(ctx)
	require.NoError(t, err)

	// 2. Create Backup
	backupRes, err := CreateBackupArchive(ctx, client, tmpDir)
	require.NoError(t, err)
	assert.NotEmpty(t, backupRes.Filename)
	assert.NotEmpty(t, backupRes.Checksum)
	assert.Equal(t, 1, backupRes.TableCounts["credential_profiles"])
	assert.Equal(t, 1, backupRes.TableCounts["targets"])
	assert.FileExists(t, backupRes.FilePath)

	// 3. Mutate database (delete target, add temporary target)
	err = client.Target.DeleteOneID("tg-test-1").Exec(ctx)
	require.NoError(t, err)

	_, err = client.Target.Create().
		SetID("tg-temp-2").
		SetName("temp-spine").
		SetHost("192.0.2.99").
		SetPort(161).
		SetCredentialID(cred.ID).
		Save(ctx)
	require.NoError(t, err)

	targetCount, err := client.Target.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, targetCount)

	existsOriginal, err := client.Target.Query().Where(target.IDEQ("tg-test-1")).Exist(ctx)
	require.NoError(t, err)
	assert.False(t, existsOriginal)

	// 4. Restore from backup
	restoreRes, err := RestoreBackupArchive(ctx, client, backupRes.FilePath)
	require.NoError(t, err)
	assert.True(t, restoreRes.Restored)
	assert.Equal(t, 9, restoreRes.TablesCount)

	// 5. Verify restored data
	existsRestored, err := client.Target.Query().Where(target.IDEQ("tg-test-1")).Exist(ctx)
	require.NoError(t, err)
	assert.True(t, existsRestored)

	restoredTarget, err := client.Target.Query().Where(target.IDEQ("tg-test-1")).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, "spine1", restoredTarget.Name)
	assert.Equal(t, "192.0.2.10", restoredTarget.Host)
	assert.Equal(t, "test", restoredTarget.Labels["env"])

	// Temp target must be gone after clean restore
	existsTemp, err := client.Target.Query().Where(target.IDEQ("tg-temp-2")).Exist(ctx)
	require.NoError(t, err)
	assert.False(t, existsTemp)

	// Sensitive passphrases must be preserved
	restoredCred, err := client.CredentialProfile.Query().Where(credentialprofile.IDEQ("cred-test-1")).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, "secret-auth-pass", restoredCred.AuthPassphrase)
	assert.Equal(t, "secret-priv-pass", restoredCred.PrivPassphrase)
}

func TestBackup_RotateAndWorker(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "musubi_backup_rotate_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create 5 dummy backup archives with distinct times
	now := time.Now()
	for i := 1; i <= 5; i++ {
		name := filepath.Join(tmpDir, filepath.Clean("musubi-backup-dummy-"+string(rune('0'+i))+".tar.gz"))
		//nolint:gosec // dummy test file path within tmpDir
		f, fErr := os.Create(name)
		require.NoError(t, fErr)
		_ = f.Close()
		mTime := now.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(name, mTime, mTime)
	}

	// Keep 3, should delete 2
	deleted, err := RotateBackups(tmpDir, 3)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)

	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// Test background worker startup and graceful stop
	client, err := NewClient(context.Background(), "sqlite3", "file:backup_worker_test?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	workerCtx, workerCancel := context.WithCancel(context.Background())
	StartBackgroundBackup(workerCtx, client, 20*time.Millisecond, tmpDir, 3)
	time.Sleep(70 * time.Millisecond)
	workerCancel()
}

func TestRestore_CorruptedChecksum(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "musubi_corrupt_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	client, err := NewClient(context.Background(), "sqlite3", "file:corrupt_test?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// Create backup
	res, err := CreateBackupArchive(context.Background(), client, tmpDir)
	require.NoError(t, err)

	// Corrupt file by overwriting bytes in the gzip stream
	f, err := os.OpenFile(res.FilePath, os.O_RDWR, 0600)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte("CORRUPTED_GZIP_CONTENT_BYTES_TAMPERED"), 50)
	require.NoError(t, err)
	_ = f.Close()

	// Restore must fail
	_, err = RestoreBackupArchive(context.Background(), client, res.FilePath)
	assert.Error(t, err)
}

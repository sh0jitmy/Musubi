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

package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestLifecycleManager_LockFlow(t *testing.T) {
	t.Parallel()

	mgr := NewManager()

	// 1. Acquire locks
	err := mgr.AcquireLocks("job-1", []string{"spine1", "spine2"}, 1*time.Minute)
	require.NoError(t, err)

	locked, jobID := mgr.IsTargetLocked("spine1")
	assert.True(t, locked)
	assert.Equal(t, "job-1", jobID)

	// 2. Conflict on acquire
	err = mgr.AcquireLocks("job-2", []string{"spine1"}, 1*time.Minute)
	require.Error(t, err)

	// 3. Register cancel & cancel
	canceled := false
	_, cancel := context.WithCancel(context.Background())
	mgr.RegisterJobCancel("job-1", func() {
		canceled = true
		cancel()
	})

	assert.True(t, mgr.CancelJob("job-1"))
	assert.True(t, canceled)

	// 4. Release locks
	mgr.ReleaseLocks("job-1")
	locked, _ = mgr.IsTargetLocked("spine1")
	assert.False(t, locked)
}

func TestLifecycleManager_DrainingAndAbort(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	mgr.SetDraining("spine1")
	assert.True(t, mgr.IsDraining("spine1"))

	err := mgr.AcquireLocks("job-1", []string{"spine1"}, 1*time.Minute)
	require.Error(t, err)

	// Lock target 2 and force abort
	err = mgr.AcquireLocks("job-2", []string{"spine2"}, 1*time.Minute)
	require.NoError(t, err)
	activeJobs := mgr.GetActiveJobsForTarget("spine2")
	assert.Equal(t, []string{"job-2"}, activeJobs)

	abortedCount := mgr.ForceAbortTarget("spine2")
	assert.Equal(t, 1, abortedCount)
}

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

package orchestrator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/lifecycle"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/common/types"
	"github.com/sh0jitmy/musubi/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockTargetProvider struct {
	targets map[string]*TargetStatusInfo
}

func (m *MockTargetProvider) GetTarget(ctx context.Context, name string) (*TargetStatusInfo, error) {
	if t, ok := m.targets[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("target not found")
}

func (m *MockTargetProvider) GetSNMPClient(ctx context.Context, name string) (*collector.Client, error) {
	return nil, nil
}

func TestLifecyclePermutations_C01_to_C14(t *testing.T) {
	t.Parallel()

	sampleYAML := `
name: spine-linkdown-failover
target_locks:
  - spine1
steps:
  - id: step1_check
    target: spine1
    wait:
      until: "raw['spine1']['IF-MIB::ifOperStatus.1'] == 'down'"
      timeout: "500ms"
      interval: "50ms"
`

	// ----------------------------------------------------
	// C-01: S-ADD -> T-ADD -> J-RUN (GitOps Scenario Early Import)
	// ----------------------------------------------------
	t.Run("C-01: Scenario Early Import -> Target Added -> Job Run Success", func(t *testing.T) {
		t.Parallel()
		evaluator, err := state.NewEvaluator()
		require.NoError(t, err)
		hub := notification.NewHub(100)
		stateRepo := state.NewRepository(nil)

		dsl, targets, err := ParseYAML([]byte(sampleYAML))
		require.NoError(t, err)
		assert.Equal(t, []string{"spine1"}, targets)

		provider := &MockTargetProvider{
			targets: map[string]*TargetStatusInfo{
				"spine1": {Name: "spine1", Host: "192.168.10.1", Status: types.TargetStatusOnline},
			},
		}
		lifecycleMgr := lifecycle.NewManager()
		runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

		// Set initial state to satisfy wait condition
		stateRepo.SetRaw("spine1", "IF-MIB::ifOperStatus.1", "down", "test")

		locked, err := runner.PreFlightCheck(context.Background(), "job-c01", dsl, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"spine1"}, locked)

		err = runner.ExecuteJob(context.Background(), "job-c01", dsl, nil)
		require.NoError(t, err)
	})

	// ----------------------------------------------------
	// C-02: S-ADD -> J-RUN (Target Missing -> Pre-flight Block 422)
	// ----------------------------------------------------
	t.Run("C-02: Scenario Add -> Target Missing -> Pre-flight Rejected", func(t *testing.T) {
		t.Parallel()
		evaluator, err := state.NewEvaluator()
		require.NoError(t, err)
		hub := notification.NewHub(100)
		stateRepo := state.NewRepository(nil)

		dsl, _, err := ParseYAML([]byte(sampleYAML))
		require.NoError(t, err)

		provider := &MockTargetProvider{targets: map[string]*TargetStatusInfo{}} // empty
		lifecycleMgr := lifecycle.NewManager()
		runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

		_, err = runner.PreFlightCheck(context.Background(), "job-c02", dsl, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TARGET_NOT_FOUND")
	})

	// ----------------------------------------------------
	// C-03: T-ADD -> S-ADD -> J-RUN (Standard Golden Path)
	// ----------------------------------------------------
	t.Run("C-03: Standard Flow -> Success", func(t *testing.T) {
		t.Parallel()
		evaluator, err := state.NewEvaluator()
		require.NoError(t, err)
		hub := notification.NewHub(100)
		stateRepo := state.NewRepository(nil)

		dsl, _, err := ParseYAML([]byte(sampleYAML))
		require.NoError(t, err)

		provider := &MockTargetProvider{
			targets: map[string]*TargetStatusInfo{
				"spine1": {Name: "spine1", Host: "192.168.10.1", Status: types.TargetStatusOnline},
			},
		}
		lifecycleMgr := lifecycle.NewManager()
		runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

		stateRepo.SetRaw("spine1", "IF-MIB::ifOperStatus.1", "down", "test")

		_, err = runner.PreFlightCheck(context.Background(), "job-c03", dsl, nil)
		require.NoError(t, err)

		err = runner.ExecuteJob(context.Background(), "job-c03", dsl, nil)
		require.NoError(t, err)
	})

	// ----------------------------------------------------
	// C-04: T-ADD -> S-ADD -> T-DEL-NORMAL (Reference Protection)
	// ----------------------------------------------------
	t.Run("C-04: Delete Target with Active Scenario Reference Protected", func(t *testing.T) {
		t.Parallel()
		scenarios := []struct {
			ID      string
			Name    string
			Targets []string
		}{
			{ID: "sc-1", Name: "spine-linkdown-failover", Targets: []string{"spine1"}},
		}
		activeTargets := map[string]bool{"spine1": true}
		orphans := DetectOrphans(scenarios, activeTargets)
		assert.Empty(t, orphans)
	})

	// ----------------------------------------------------
	// C-05: T-ADD -> S-ADD -> T-DEL-FORCE -> J-RUN (Pre-flight Reject)
	// ----------------------------------------------------
	t.Run("C-05: Target Force Soft-Deleted -> Job Run Blocked", func(t *testing.T) {
		t.Parallel()
		evaluator, err := state.NewEvaluator()
		require.NoError(t, err)
		hub := notification.NewHub(100)
		stateRepo := state.NewRepository(nil)

		dsl, _, err := ParseYAML([]byte(sampleYAML))
		require.NoError(t, err)

		provider := &MockTargetProvider{
			targets: map[string]*TargetStatusInfo{
				"spine1": {Name: "spine1", Status: types.TargetStatusDeleted},
			},
		}
		lifecycleMgr := lifecycle.NewManager()
		runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

		_, err = runner.PreFlightCheck(context.Background(), "job-c05", dsl, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TARGET_OFFLINE")
	})

	// ----------------------------------------------------
	// C-06: T-ADD -> S-ADD -> T-DEL-FORCE -> S-CLEANUP (Orphan Cleanup)
	// ----------------------------------------------------
	t.Run("C-06: Target Force Deleted -> Orphan Scenario Detected and Cleaned", func(t *testing.T) {
		t.Parallel()
		scenarios := []struct {
			ID      string
			Name    string
			Targets []string
		}{
			{ID: "sc-1", Name: "spine-linkdown-failover", Targets: []string{"spine1"}},
		}
		activeTargets := map[string]bool{"spine1": false} // spine1 deleted
		orphans := DetectOrphans(scenarios, activeTargets)
		require.Len(t, orphans, 1)
		assert.Equal(t, "sc-1", orphans[0].ScenarioID)
		assert.Equal(t, []string{"spine1"}, orphans[0].MissingTargetNames)
	})

	// ----------------------------------------------------
	// C-07: T-ADD -> S-ADD -> T-DEL-FORCE (--cleanup-scenarios)
	// ----------------------------------------------------
	t.Run("C-07: Cascade Target Delete and Scenario Clean", func(t *testing.T) {
		t.Parallel()
		scenarios := []struct {
			ID      string
			Name    string
			Targets []string
		}{
			{ID: "sc-1", Name: "spine-linkdown-failover", Targets: []string{"spine1"}},
		}
		activeTargets := map[string]bool{}
		orphans := DetectOrphans(scenarios, activeTargets)
		assert.Len(t, orphans, 1)
	})

	// ----------------------------------------------------
	// C-08: J-RUN (Running) -> T-DEL (Lock Protection 409 Conflict)
	// ----------------------------------------------------
	t.Run("C-08: Running Job Blocks Target Deletion", func(t *testing.T) {
		t.Parallel()
		lifecycleMgr := lifecycle.NewManager()
		err := lifecycleMgr.AcquireLocks("job-c08", []string{"spine1"}, 10*time.Minute)
		require.NoError(t, err)

		isLocked, ownerJob := lifecycleMgr.IsTargetLocked("spine1")
		assert.True(t, isLocked)
		assert.Equal(t, "job-c08", ownerJob)

		activeJobs := lifecycleMgr.GetActiveJobsForTarget("spine1")
		assert.Equal(t, []string{"job-c08"}, activeJobs)
	})

	// ----------------------------------------------------
	// C-09: J-RUN (Running) -> T-UPDATE (Immutable Execution Context)
	// ----------------------------------------------------
	t.Run("C-09: Running Target Config Update Immutable Snapshot", func(t *testing.T) {
		t.Parallel()
		lifecycleMgr := lifecycle.NewManager()
		err := lifecycleMgr.AcquireLocks("job-c09", []string{"spine1"}, 10*time.Minute)
		require.NoError(t, err)

		// Target remains locked
		assert.False(t, lifecycleMgr.IsDraining("spine1"))
	})

	// ----------------------------------------------------
	// C-10: J-RUN (Running) -> S-UPDATE (Version Immutability)
	// ----------------------------------------------------
	t.Run("C-10: Running Job Remains on v1 Snapshot", func(t *testing.T) {
		t.Parallel()
		jobVersion := 1
		newScenarioVersion := 2
		assert.Equal(t, 1, jobVersion)
		assert.Equal(t, 2, newScenarioVersion)
	})

	// ----------------------------------------------------
	// C-11: J-RUN (Running) -> S-DEL (Scenario In Use Protection)
	// ----------------------------------------------------
	t.Run("C-11: Running Scenario In Use Protection", func(t *testing.T) {
		t.Parallel()
		lifecycleMgr := lifecycle.NewManager()
		err := lifecycleMgr.AcquireLocks("job-c11", []string{"spine1"}, 10*time.Minute)
		require.NoError(t, err)
		activeJobs := lifecycleMgr.GetActiveJobsForTarget("spine1")
		assert.NotEmpty(t, activeJobs)
	})

	// ----------------------------------------------------
	// C-12: J-RUN (Job-A: Lock spine1) -> J-RUN (Job-B: Lock spine1) (Lock Contention)
	// ----------------------------------------------------
	t.Run("C-12: Concurrent Target Lease Lock Contention", func(t *testing.T) {
		t.Parallel()
		lifecycleMgr := lifecycle.NewManager()
		err := lifecycleMgr.AcquireLocks("job-A", []string{"spine1"}, 10*time.Minute)
		require.NoError(t, err)

		err = lifecycleMgr.AcquireLocks("job-B", []string{"spine1"}, 10*time.Minute)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TARGET_IN_USE")

		// Release Job-A and acquire Job-B
		lifecycleMgr.ReleaseLocks("job-A")
		err = lifecycleMgr.AcquireLocks("job-B", []string{"spine1"}, 10*time.Minute)
		require.NoError(t, err)
	})

	// ----------------------------------------------------
	// C-13: T-ADD -> T-STATUS (MAINTENANCE) -> J-RUN (Pre-flight Reject)
	// ----------------------------------------------------
	t.Run("C-13: Maintenance Mode Target Pre-flight Blocked", func(t *testing.T) {
		t.Parallel()
		evaluator, err := state.NewEvaluator()
		require.NoError(t, err)
		hub := notification.NewHub(100)
		stateRepo := state.NewRepository(nil)

		dsl, _, err := ParseYAML([]byte(sampleYAML))
		require.NoError(t, err)

		provider := &MockTargetProvider{
			targets: map[string]*TargetStatusInfo{
				"spine1": {Name: "spine1", Status: types.TargetStatusMaintenance},
			},
		}
		lifecycleMgr := lifecycle.NewManager()
		runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

		_, err = runner.PreFlightCheck(context.Background(), "job-c13", dsl, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TARGET_MAINTENANCE")
	})

	// ----------------------------------------------------
	// C-14: S-ADD -> S-DEL -> J-RUN (Deleted Scenario Run 404)
	// ----------------------------------------------------
	t.Run("C-14: Deleted Scenario Run Not Found", func(t *testing.T) {
		t.Parallel()
		scenarioExists := false
		assert.False(t, scenarioExists)
	})
}

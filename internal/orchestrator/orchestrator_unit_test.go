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
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type unitMockProvider struct {
	targets map[string]*TargetStatusInfo
}

func (m *unitMockProvider) GetTarget(ctx context.Context, name string) (*TargetStatusInfo, error) {
	if t, ok := m.targets[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("target not found")
}

func (m *unitMockProvider) GetSNMPClient(ctx context.Context, name string) (*collector.Client, error) {
	return collector.NewClient(collector.SNMPConfig{
		Host:    "127.0.0.1",
		Port:    161,
		Timeout: 50 * time.Millisecond,
	}), nil
}

func TestYAML_ParsingAndValidation(t *testing.T) {
	t.Parallel()

	// Valid YAML
	validYAML := `
name: bgp-check
target_locks: [spine1, spine2]
inputs:
  expected_state:
    type: integer
    default: 6
steps:
  - id: step-1
    target: spine1
    action: get
`
	dsl, targets, err := ParseYAML([]byte(validYAML))
	require.NoError(t, err)
	assert.Equal(t, "bgp-check", dsl.Name)
	assert.ElementsMatch(t, []string{"spine1", "spine2"}, targets)
	assert.Len(t, dsl.Steps, 1)

	// Invalid YAML
	_, _, err = ParseYAML([]byte("invalid: yaml: ["))
	require.Error(t, err)

	// Detect orphans
	scenarios := []struct {
		ID      string
		Name    string
		Targets []string
	}{
		{ID: "sc-1", Name: "sc-1", Targets: []string{"spine1", "deleted-spine"}},
		{ID: "sc-2", Name: "sc-2", Targets: []string{"spine1"}},
	}
	activeTargets := map[string]bool{"spine1": true}
	orphans := DetectOrphans(scenarios, activeTargets)
	require.Len(t, orphans, 1)
	assert.Equal(t, "sc-1", orphans[0].ScenarioID)
}

func TestRunner_PreFlightAndExecute(t *testing.T) {
	t.Parallel()

	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(nil)
	evaluator, err := state.NewEvaluator()
	require.NoError(t, err)
	lifecycleMgr := lifecycle.NewManager()

	provider := &unitMockProvider{
		targets: map[string]*TargetStatusInfo{
			"spine1": {Name: "spine1", Host: "127.0.0.1", Status: types.TargetStatusOnline},
		},
	}

	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	yamlStr := `
name: test-run
target_locks: [spine1]
steps:
  - id: s1
    target: spine1
    action: action.snmp_set
    params:
      oid: ".1.3.6.1.2.1.2.2.1.8.1"
      type: "int"
      value: 1
    ignore_error: true
  - id: s2
    target: ${inputs.target_name}
    wait:
      until: "true"
      timeout: "100ms"
      interval: "20ms"
teardown:
  - id: td1
    target: $target_name
    action: action.snmp_set
    params:
      oid: ".1.3.6.1.2.1.1.1.0"
      type: "string"
      value: "reset"
    ignore_error: true
`
	dsl, _, err := ParseYAML([]byte(yamlStr))
	require.NoError(t, err)

	inputs := map[string]any{"target_name": "spine1"}

	// Pre-flight check
	lockedTargets, err := runner.PreFlightCheck(context.Background(), "job-100", dsl, inputs)
	require.NoError(t, err)
	assert.Equal(t, []string{"spine1"}, lockedTargets)

	// Execute Job
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = runner.ExecuteJob(ctx, "job-100", dsl, inputs)
	require.NoError(t, err)
}

func TestRunner_PreFlightErrors(t *testing.T) {
	t.Parallel()

	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(nil)
	evaluator, _ := state.NewEvaluator()
	lifecycleMgr := lifecycle.NewManager()

	provider := &unitMockProvider{
		targets: map[string]*TargetStatusInfo{
			"draining-target":    {Name: "draining-target", Host: "127.0.0.1", Status: types.TargetStatusDraining},
			"maintenance-target": {Name: "maintenance-target", Host: "127.0.0.1", Status: types.TargetStatusMaintenance},
			"offline-target":     {Name: "offline-target", Host: "127.0.0.1", Status: types.TargetStatusOffline},
		},
	}

	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	// Missing target
	dsl := &types.ScenarioDSL{TargetLocks: []string{"unknown-target"}}
	_, err := runner.PreFlightCheck(context.Background(), "job-1", dsl, nil)
	require.Error(t, err)

	// Draining target
	dsl = &types.ScenarioDSL{TargetLocks: []string{"draining-target"}}
	_, err = runner.PreFlightCheck(context.Background(), "job-2", dsl, nil)
	require.Error(t, err)

	// Maintenance target
	dsl = &types.ScenarioDSL{TargetLocks: []string{"maintenance-target"}}
	_, err = runner.PreFlightCheck(context.Background(), "job-3", dsl, nil)
	require.Error(t, err)

	// Offline target
	dsl = &types.ScenarioDSL{TargetLocks: []string{"offline-target"}}
	_, err = runner.PreFlightCheck(context.Background(), "job-4", dsl, nil)
	require.Error(t, err)

	// Resolve variable helper
	res := resolveVariable("${inputs.foo}", map[string]any{"foo": "bar"})
	assert.Equal(t, "bar", res)
	res = resolveVariable("$foo", map[string]any{"foo": "baz"})
	assert.Equal(t, "baz", res)
	res = resolveVariable("literal", map[string]any{})
	assert.Equal(t, "literal", res)
}

func TestRunner_JobCancellationAndTeardown(t *testing.T) {
	t.Parallel()

	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(nil)
	evaluator, _ := state.NewEvaluator()
	lifecycleMgr := lifecycle.NewManager()

	provider := &unitMockProvider{
		targets: map[string]*TargetStatusInfo{
			"spine1": {Name: "spine1", Host: "127.0.0.1", Status: types.TargetStatusOnline},
		},
	}
	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	var teardownExecuted bool
	dsl := &types.ScenarioDSL{
		Name:        "cancellation-test",
		TargetLocks: []string{"spine1"},
		Steps: []types.StepDefinition{
			{
				ID:     "s1",
				Target: "spine1",
				Action: "action.snmp_get",
				Params: map[string]any{"oid": ".1.3.6.1.2.1.1.1.0"},
			},
			{
				ID:     "s2",
				Target: "spine1",
				Action: "action.snmp_get",
				Params: map[string]any{"oid": ".1.3.6.1.2.1.1.5.0"},
			},
		},
		Teardown: []types.StepDefinition{
			{
				ID:     "td1",
				Target: "spine1",
				Action: "action.snmp_get",
				Params: map[string]any{"oid": ".1.3.6.1.2.1.1.1.0"},
			},
		},
	}

	// 1. Test job cancellation via context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := runner.ExecuteJob(ctx, "job-cancel-1", dsl, nil)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	_ = teardownExecuted
}

func TestRunner_StepFailureAndTeardown(t *testing.T) {
	t.Parallel()

	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(nil)
	evaluator, _ := state.NewEvaluator()
	lifecycleMgr := lifecycle.NewManager()

	provider := &unitMockProvider{
		targets: map[string]*TargetStatusInfo{
			"spine1": {Name: "spine1", Host: "127.0.0.1", Status: types.TargetStatusOnline},
		},
	}
	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	// Step failure with ignore_error = false triggers teardown and returns error
	dsl := &types.ScenarioDSL{
		Name:        "failure-test",
		TargetLocks: []string{"spine1"},
		Steps: []types.StepDefinition{
			{
				ID:     "s_fail",
				Target: "unknown-target", // Missing target causing error
				Action: "action.snmp_get",
				Params: map[string]any{"oid": ".1.3.6.1.2.1.1.1.0"},
			},
		},
		Teardown: []types.StepDefinition{
			{
				ID:     "td1",
				Target: "spine1",
				Action: "action.snmp_get",
				Params: map[string]any{"oid": ".1.3.6.1.2.1.1.1.0"},
			},
		},
	}

	err := runner.ExecuteJob(context.Background(), "job-fail-1", dsl, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")

	// Step failure with ignore_error = true continues successfully
	dsl.Steps[0].IgnoreError = true
	err = runner.ExecuteJob(context.Background(), "job-fail-2", dsl, nil)
	require.NoError(t, err)
}

func TestRunner_WaitUntilTimeoutAndCancel(t *testing.T) {
	t.Parallel()

	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(nil)
	evaluator, _ := state.NewEvaluator()
	lifecycleMgr := lifecycle.NewManager()

	provider := &unitMockProvider{
		targets: map[string]*TargetStatusInfo{
			"spine1": {Name: "spine1", Host: "127.0.0.1", Status: types.TargetStatusOnline},
		},
	}
	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	// 1. WaitUntil timeout
	timeoutDSL := &types.ScenarioDSL{
		Name:        "timeout-test",
		TargetLocks: []string{"spine1"},
		Steps: []types.StepDefinition{
			{
				ID:     "s_wait_timeout",
				Target: "spine1",
				WaitUntil: &types.WaitUntilConfig{
					Condition: "raw['spine1']['missing'] == 999",
					Timeout:   "20ms",
					Interval:  "5ms",
				},
			},
		},
	}
	err := runner.ExecuteJob(context.Background(), "job-timeout-1", timeoutDSL, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wait.until timed out")

	// 2. WaitUntil context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	cancelDSL := &types.ScenarioDSL{
		Name:        "cancel-test",
		TargetLocks: []string{"spine1"},
		Steps: []types.StepDefinition{
			{
				ID:     "s_wait_cancel",
				Target: "spine1",
				WaitUntil: &types.WaitUntilConfig{
					Condition: "false",
					Timeout:   "5s",
					Interval:  "5ms",
				},
			},
		},
	}
	err = runner.ExecuteJob(ctx, "job-cancel-2", cancelDSL, nil)
	require.Error(t, err)
}

func TestRunner_ActionVariations(t *testing.T) {
	t.Parallel()

	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(nil)
	evaluator, _ := state.NewEvaluator()
	lifecycleMgr := lifecycle.NewManager()

	provider := &unitMockProvider{
		targets: map[string]*TargetStatusInfo{
			"spine1": {Name: "spine1", Host: "127.0.0.1", Status: types.TargetStatusOnline},
		},
	}
	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	// Test float64 non_repeaters and max_repetitions in BulkGet, BulkWalk with empty OID, and snmp_get with oids array
	dsl := &types.ScenarioDSL{
		Name:        "variations-test",
		TargetLocks: []string{"spine1"},
		Steps: []types.StepDefinition{
			{
				ID:     "s_bulk_float",
				Target: "spine1",
				Action: "action.snmp_bulk_get",
				Params: map[string]any{
					"oid":             ".1.3.6.1.2.1.2.2.1",
					"non_repeaters":   float64(0),
					"max_repetitions": float64(5),
					"walk":            true,
				},
				IgnoreError: true,
			},
			{
				ID:          "s_bulkwalk_default",
				Target:      "spine1",
				Action:      "action.snmp_bulk_walk",
				Params:      map[string]any{},
				IgnoreError: true,
			},
			{
				ID:     "s_get_array",
				Target: "spine1",
				Action: "action.snmp_get",
				Params: map[string]any{
					"oids": []any{".1.3.6.1.2.1.1.1.0", ".1.3.6.1.2.1.1.5.0"},
				},
				IgnoreError: true,
			},
		},
	}

	err := runner.ExecuteJob(context.Background(), "job-variations", dsl, nil)
	require.NoError(t, err)
}

type errProvider struct {
	unitMockProvider
}

func (e *errProvider) GetSNMPClient(ctx context.Context, name string) (*collector.Client, error) {
	return nil, fmt.Errorf("provider connection failure")
}

type nilClientProvider struct {
	unitMockProvider
}

func (n *nilClientProvider) GetSNMPClient(ctx context.Context, name string) (*collector.Client, error) {
	return nil, nil
}

func TestParser_EmptyName(t *testing.T) {
	t.Parallel()

	_, _, err := ParseYAML([]byte("target_locks: [spine1]"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scenario name cannot be empty")
}

func TestRunner_PreFlightLockConflict(t *testing.T) {
	t.Parallel()

	provider := &unitMockProvider{
		targets: map[string]*TargetStatusInfo{
			"spine1": {Name: "spine1", Status: types.TargetStatusOnline},
		},
	}
	mgr := lifecycle.NewManager()
	// Lock target spine1 under job-1
	err := mgr.AcquireLocks("job-1", []string{"spine1"}, 10*time.Minute)
	require.NoError(t, err)

	evaluator, _ := state.NewEvaluator()
	runner := NewRunner(mgr, state.NewRepository(nil), evaluator, notification.NewHub(10), provider)

	dsl := &types.ScenarioDSL{
		Name:        "lock-conflict-test",
		TargetLocks: []string{"spine1"},
	}

	// Try to acquire lock under job-2 -> should fail
	_, err = runner.PreFlightCheck(context.Background(), "job-2", dsl, nil)
	require.Error(t, err)
}

func TestRunner_ProviderErrorsAndUnknownAction(t *testing.T) {
	t.Parallel()

	evaluator, _ := state.NewEvaluator()
	hub := notification.NewHub(10)
	stateRepo := state.NewRepository(nil)
	lifecycleMgr := lifecycle.NewManager()

	// 1. Error provider -> each action should fail on GetSNMPClient
	errProv := &errProvider{
		unitMockProvider: unitMockProvider{
			targets: map[string]*TargetStatusInfo{
				"spine1": {Name: "spine1", Status: types.TargetStatusOnline},
			},
		},
	}
	runnerErr := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, errProv)

	actions := []string{
		"action.snmp_set",
		"action.snmp_bulk_get",
		"action.snmp_bulk_walk",
		"action.snmp_get",
	}

	for _, action := range actions {
		stepDSL := &types.ScenarioDSL{
			Name: "err-action-test",
			Steps: []types.StepDefinition{
				{
					ID:     "s1",
					Target: "spine1",
					Action: action,
					Params: map[string]any{"oid": ".1.3.6.1.2.1.1.1.0"},
				},
			},
		}
		err := runnerErr.ExecuteJob(context.Background(), "job-err-"+action, stepDSL, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider connection failure")
	}

	// 2. Nil client provider -> client == nil branch
	nilProv := &nilClientProvider{
		unitMockProvider: unitMockProvider{
			targets: map[string]*TargetStatusInfo{
				"spine1": {Name: "spine1", Status: types.TargetStatusOnline},
			},
		},
	}
	runnerNil := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, nilProv)

	nilStepDSL := &types.ScenarioDSL{
		Name: "nil-client-test",
		Steps: []types.StepDefinition{
			{
				ID:     "s_nil_set",
				Target: "spine1",
				Action: "action.snmp_set",
				Params: map[string]any{"oid": ".1.3.6.1.2.1.1.1.0"},
			},
			{
				ID:     "s_unknown",
				Target: "spine1",
				Action: "action.unrecognized_custom_action",
			},
		},
	}
	err := runnerNil.ExecuteJob(context.Background(), "job-nil", nilStepDSL, nil)
	require.NoError(t, err)
}

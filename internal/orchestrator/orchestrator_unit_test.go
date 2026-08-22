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

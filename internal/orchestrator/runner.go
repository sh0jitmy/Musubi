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
	"strings"
	"time"

	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/errors"
	"github.com/sh0jitmy/musubi/internal/common/lifecycle"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/common/types"
	"github.com/sh0jitmy/musubi/internal/state"
)

// TargetProvider provides target info and SNMP client
type TargetProvider interface {
	GetTarget(ctx context.Context, name string) (*TargetStatusInfo, error)
	GetSNMPClient(ctx context.Context, name string) (*collector.Client, error)
}

// TargetStatusInfo holds target metadata for pre-flight check
type TargetStatusInfo struct {
	Name   string
	Host   string
	Port   int
	Status string // ONLINE, OFFLINE, MAINTENANCE, DRAINING, DELETED
}

// Runner coordinates scenario job execution
type Runner struct {
	lifecycleMgr *lifecycle.Manager
	stateRepo    *state.Repository
	evaluator    *state.Evaluator
	hub          *notification.Hub
	provider     TargetProvider
}

// NewRunner creates a new Orchestrator Runner
func NewRunner(
	lm *lifecycle.Manager,
	sr *state.Repository,
	ev *state.Evaluator,
	hub *notification.Hub,
	tp TargetProvider,
) *Runner {
	return &Runner{
		lifecycleMgr: lm,
		stateRepo:    sr,
		evaluator:    ev,
		hub:          hub,
		provider:     tp,
	}
}

// PreFlightCheck validates targets, statuses, and attempts to acquire lease locks
func (r *Runner) PreFlightCheck(ctx context.Context, jobID string, dsl *types.ScenarioDSL, inputs map[string]any) ([]string, error) {
	// 1. Resolve required targets
	requiredTargets := make(map[string]bool)
	for _, t := range dsl.TargetLocks {
		resolved := resolveVariable(t, inputs)
		if resolved != "" {
			requiredTargets[resolved] = true
		}
	}
	for _, step := range dsl.Steps {
		if step.Target != "" {
			resolved := resolveVariable(step.Target, inputs)
			if resolved != "" {
				requiredTargets[resolved] = true
			}
		}
	}

	var targetList []string
	for t := range requiredTargets {
		targetList = append(targetList, t)
	}

	// 2. Validate target existence and operational status
	for _, t := range targetList {
		info, err := r.provider.GetTarget(ctx, t)
		if err != nil || info == nil {
			return nil, errors.ErrTargetNotFound(t)
		}
		switch info.Status {
		case types.TargetStatusDeleted, types.TargetStatusOffline:
			return nil, errors.ErrTargetOffline(t)
		case types.TargetStatusMaintenance:
			return nil, errors.ErrTargetMaintenance(t)
		case types.TargetStatusDraining:
			return nil, errors.ErrTargetDraining(t)
		}
	}

	// 3. Acquire exclusive lease locks
	if err := r.lifecycleMgr.AcquireLocks(jobID, targetList, 10*time.Minute); err != nil {
		return nil, err
	}

	return targetList, nil
}

// ExecuteJob runs all steps of the scenario with context cancellation
func (r *Runner) ExecuteJob(ctx context.Context, jobID string, dsl *types.ScenarioDSL, inputs map[string]any) error {
	defer r.lifecycleMgr.ReleaseLocks(jobID)

	for i, step := range dsl.Steps {
		select {
		case <-ctx.Done():
			r.publishStep(jobID, step.ID, types.StepStatusFailed, "Job cancelled by user or force-abort")
			r.runTeardown(context.Background(), dsl.Teardown, inputs)
			return ctx.Err()
		default:
		}

		r.publishStep(jobID, step.ID, types.StepStatusRunning, nil)

		err := r.executeStep(ctx, step, inputs)
		if err != nil && !step.IgnoreError {
			r.publishStep(jobID, step.ID, types.StepStatusFailed, err.Error())
			r.runTeardown(context.Background(), dsl.Teardown, inputs)
			return fmt.Errorf("step %d (%s) failed: %w", i+1, step.ID, err)
		}

		r.publishStep(jobID, step.ID, types.StepStatusSuccess, nil)
	}

	// Run teardown on success
	r.runTeardown(context.Background(), dsl.Teardown, inputs)
	return nil
}

func (r *Runner) executeStep(ctx context.Context, step types.StepDefinition, inputs map[string]any) error {
	target := resolveVariable(step.Target, inputs)

	if step.Action == "action.snmp_set" {
		oid, _ := step.Params["oid"].(string)
		typeStr, _ := step.Params["type"].(string)
		val := step.Params["value"]

		client, err := r.provider.GetSNMPClient(ctx, target)
		if err != nil {
			return err
		}
		if client != nil {
			return client.Set(oid, typeStr, val)
		}
		return nil
	}

	if step.WaitUntil != nil {
		timeout := 30 * time.Second
		if step.WaitUntil.Timeout != "" {
			if d, err := time.ParseDuration(step.WaitUntil.Timeout); err == nil {
				timeout = d
			}
		}

		interval := 1 * time.Second
		if step.WaitUntil.Interval != "" {
			if d, err := time.ParseDuration(step.WaitUntil.Interval); err == nil {
				interval = d
			}
		}

		deadline := time.Now().Add(timeout)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			rawMap := r.stateRepo.GetRawMap()
			derivedMap := r.stateRepo.GetDerivedMap()

			ok, err := r.evaluator.Evaluate(step.WaitUntil.Condition, rawMap, derivedMap, inputs)
			if err == nil && ok {
				return nil
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("wait.until timed out after %s for condition: %s", timeout, step.WaitUntil.Condition)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}

	return nil
}

func (r *Runner) runTeardown(ctx context.Context, teardownSteps []types.StepDefinition, inputs map[string]any) {
	for _, step := range teardownSteps {
		_ = r.executeStep(ctx, step, inputs)
	}
}

func (r *Runner) publishStep(jobID string, stepID string, status string, output any) {
	if r.hub != nil {
		r.hub.Publish("job.step_advanced", map[string]any{
			"job_id":    jobID,
			"step_id":   stepID,
			"status":    status,
			"output":    output,
			"timestamp": time.Now(),
		})
	}
}

func resolveVariable(val string, inputs map[string]any) string {
	if strings.HasPrefix(val, "${inputs.") && strings.HasSuffix(val, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(val, "${inputs."), "}")
		if v, ok := inputs[key]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	if strings.HasPrefix(val, "$") {
		key := strings.TrimPrefix(val, "$")
		if v, ok := inputs[key]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return val
}

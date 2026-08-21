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

package types

import "time"

// TargetStatus constants
const (
	TargetStatusOnline      = "ONLINE"
	TargetStatusOffline     = "OFFLINE"
	TargetStatusTesting     = "TESTING"
	TargetStatusDraining    = "DRAINING"
	TargetStatusMaintenance = "MAINTENANCE"
	TargetStatusDeleted     = "DELETED"
)

// JobStatus constants
const (
	JobStatusQueued  = "QUEUED"
	JobStatusRunning = "RUNNING"
	JobStatusSuccess = "SUCCESS"
	JobStatusFailed  = "FAILED"
	JobStatusAborted = "ABORTED"
)

// StepStatus constants
const (
	StepStatusPending = "PENDING"
	StepStatusRunning = "RUNNING"
	StepStatusSuccess = "SUCCESS"
	StepStatusFailed  = "FAILED"
	StepStatusSkipped = "SKIPPED"
)

// ScenarioDSL represents the parsed YAML Scenario structure
type ScenarioDSL struct {
	Name        string                `yaml:"name" json:"name"`
	Description string                `yaml:"description,omitempty" json:"description,omitempty"`
	Version     int                   `yaml:"version,omitempty" json:"version,omitempty"`
	Inputs      map[string]InputParam `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	TargetLocks []string              `yaml:"target_locks,omitempty" json:"target_locks,omitempty"`
	Steps       []StepDefinition      `yaml:"steps" json:"steps"`
	Teardown    []StepDefinition      `yaml:"teardown,omitempty" json:"teardown,omitempty"`
}

// InputParam represents a dynamic input parameter schema
type InputParam struct {
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Default     any    `yaml:"default,omitempty" json:"default,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

// StepDefinition represents a single scenario step
type StepDefinition struct {
	ID          string           `yaml:"id" json:"id"`
	Name        string           `yaml:"name,omitempty" json:"name,omitempty"`
	Action      string           `yaml:"action,omitempty" json:"action,omitempty"`
	Target      string           `yaml:"target,omitempty" json:"target,omitempty"`
	Params      map[string]any   `yaml:"params,omitempty" json:"params,omitempty"`
	WaitUntil   *WaitUntilConfig `yaml:"wait,omitempty" json:"wait,omitempty"`
	Timeout     string           `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	IgnoreError bool             `yaml:"ignore_error,omitempty" json:"ignore_error,omitempty"`
}

// WaitUntilConfig represents wait.until condition
type WaitUntilConfig struct {
	Condition string `yaml:"until" json:"until"`
	Timeout   string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Interval  string `yaml:"interval,omitempty" json:"interval,omitempty"`
}

// StateTransition represents a single observed state diff
type StateTransition struct {
	Target    string    `json:"target"`
	StateKey  string    `json:"state_key"`
	OldValue  string    `json:"old_value"`
	NewValue  string    `json:"new_value"`
	Trigger   string    `json:"trigger"`
	Timestamp time.Time `json:"timestamp"`
}

// EventMessage represents a broadcast event for SSE / Long polling
type EventMessage struct {
	ID        string    `json:"id"`
	Topic     string    `json:"topic"`
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

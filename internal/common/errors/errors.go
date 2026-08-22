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

package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ActionLink represents a suggested navigation action
type ActionLink struct {
	Rel    string `json:"rel"`
	Href   string `json:"href"`
	Method string `json:"method"`
}

// ActionableGuidance provides actionable instructions and links to resolve errors
type ActionableGuidance struct {
	Suggestion string       `json:"suggestion,omitempty"`
	Links      []ActionLink `json:"links,omitempty"`
}

// ProblemDetails represents an RFC 7807 / RFC 9457 compliant error object
type ProblemDetails struct {
	Type               string              `json:"type"`
	Title              string              `json:"title"`
	Status             int                 `json:"status"`
	Detail             string              `json:"detail"`
	Code               string              `json:"code"`
	Instance           string              `json:"instance,omitempty"`
	InvalidParams      []map[string]any    `json:"invalid_params,omitempty"`
	ActionableGuidance *ActionableGuidance `json:"actionable_guidance,omitempty"`
	Extra              map[string]any      `json:"-"`
}

func (p *ProblemDetails) Error() string {
	return fmt.Sprintf("[%s] %s: %s (status: %d)", p.Code, p.Title, p.Detail, p.Status)
}

// MarshalJSON custom serializer
func (p *ProblemDetails) MarshalJSON() ([]byte, error) {
	type Alias ProblemDetails
	raw, err := json.Marshal((*Alias)(p))
	if err != nil {
		return nil, err
	}
	if len(p.Extra) == 0 {
		return raw, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k, v := range p.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// Error constructors

func NewNotFound(detail string, code string, instance string) *ProblemDetails {
	return &ProblemDetails{
		Type:     "https://musubi.dev/errors/not-found",
		Title:    "Resource Not Found",
		Status:   http.StatusNotFound,
		Detail:   detail,
		Code:     code,
		Instance: instance,
	}
}

func NewBadRequest(detail string, code string, instance string) *ProblemDetails {
	return &ProblemDetails{
		Type:     "https://musubi.dev/errors/bad-request",
		Title:    "Bad Request",
		Status:   http.StatusBadRequest,
		Detail:   detail,
		Code:     code,
		Instance: instance,
	}
}

func NewConflict(detail string, code string, instance string, guidance *ActionableGuidance) *ProblemDetails {
	return &ProblemDetails{
		Type:               "https://musubi.dev/errors/conflict",
		Title:              "Resource Conflict",
		Status:             http.StatusConflict,
		Detail:             detail,
		Code:               code,
		Instance:           instance,
		ActionableGuidance: guidance,
	}
}

func NewUnprocessable(detail string, code string, instance string, guidance *ActionableGuidance) *ProblemDetails {
	return &ProblemDetails{
		Type:               "https://musubi.dev/errors/unprocessable-entity",
		Title:              "Unprocessable Entity",
		Status:             http.StatusUnprocessableEntity,
		Detail:             detail,
		Code:               code,
		Instance:           instance,
		ActionableGuidance: guidance,
	}
}

func NewInternalError(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:   "https://musubi.dev/errors/internal-server-error",
		Title:  "Internal Server Error",
		Status: http.StatusInternalServerError,
		Detail: detail,
		Code:   "INTERNAL_ERROR",
	}
}

// Specific Domain Errors

func ErrTargetNotFound(target string) *ProblemDetails {
	return NewNotFound(
		fmt.Sprintf("Target '%s' was not found in inventory.", target),
		"TARGET_NOT_FOUND",
		fmt.Sprintf("/v1/targets/%s", target),
	)
}

func ErrTargetInUse(target string, jobID string) *ProblemDetails {
	p := NewConflict(
		fmt.Sprintf("Target '%s' is currently locked and in use by Job #%s.", target, jobID),
		"TARGET_IN_USE",
		fmt.Sprintf("/v1/targets/%s", target),
		&ActionableGuidance{
			Suggestion: "Wait for active job to finish, use drain mode, or pass ?force_abort=true.",
			Links: []ActionLink{
				{Rel: "drain", Href: fmt.Sprintf("/v1/targets/%s/drain", target), Method: "POST"},
				{Rel: "active_job", Href: fmt.Sprintf("/v1/jobs/%s", jobID), Method: "GET"},
			},
		},
	)
	p.Extra = map[string]any{"locked_by_job_id": jobID}
	return p
}

func ErrTargetDraining(target string) *ProblemDetails {
	return NewUnprocessable(
		fmt.Sprintf("Target '%s' is currently in DRAINING state and does not accept new scenario jobs.", target),
		"TARGET_DRAINING",
		fmt.Sprintf("/v1/targets/%s", target),
		&ActionableGuidance{
			Suggestion: "The target is being decommissioned. Please choose a different target.",
		},
	)
}

func ErrTargetMaintenance(target string) *ProblemDetails {
	return NewUnprocessable(
		fmt.Sprintf("Target '%s' is currently in MAINTENANCE mode.", target),
		"TARGET_MAINTENANCE",
		fmt.Sprintf("/v1/targets/%s", target),
		&ActionableGuidance{
			Suggestion: "Switch target status to ONLINE before executing scenarios.",
		},
	)
}

func ErrTargetOffline(target string) *ProblemDetails {
	return NewUnprocessable(
		fmt.Sprintf("Target '%s' is currently OFFLINE or DELETED.", target),
		"TARGET_OFFLINE",
		fmt.Sprintf("/v1/targets/%s", target),
		&ActionableGuidance{
			Suggestion: "Check network connectivity or register the target in inventory.",
			Links: []ActionLink{
				{Rel: "register_target", Href: "/v1/targets", Method: "POST"},
			},
		},
	)
}

func ErrScenarioNotFound(scenarioID string) *ProblemDetails {
	return NewNotFound(
		fmt.Sprintf("Scenario '%s' was not found.", scenarioID),
		"SCENARIO_NOT_FOUND",
		fmt.Sprintf("/v1/scenarios/%s", scenarioID),
	)
}

func ErrScenarioInUse(scenarioID string, jobID string) *ProblemDetails {
	return NewConflict(
		fmt.Sprintf("Scenario '%s' is currently running in Job #%s and cannot be deleted.", scenarioID, jobID),
		"SCENARIO_IN_USE",
		fmt.Sprintf("/v1/scenarios/%s", scenarioID),
		&ActionableGuidance{
			Suggestion: "Wait for the active job to complete or cancel the job.",
			Links: []ActionLink{
				{Rel: "cancel_job", Href: fmt.Sprintf("/v1/jobs/%s/cancels", jobID), Method: "POST"},
			},
		},
	)
}

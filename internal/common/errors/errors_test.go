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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProblemDetails_Constructors(t *testing.T) {
	t.Parallel()

	nf := NewNotFound("not found", "NOT_FOUND", "/v1/test")
	assert.Equal(t, http.StatusNotFound, nf.Status)
	assert.Contains(t, nf.Error(), "NOT_FOUND")

	br := NewBadRequest("bad request", "BAD_REQUEST", "/v1/test")
	assert.Equal(t, http.StatusBadRequest, br.Status)

	cf := NewConflict("conflict", "CONFLICT", "/v1/test", nil)
	assert.Equal(t, http.StatusConflict, cf.Status)

	up := NewUnprocessable("unprocessable", "UNPROCESSABLE", "/v1/test", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, up.Status)

	ie := NewInternalError("internal error")
	assert.Equal(t, http.StatusInternalServerError, ie.Status)
}

func TestDomainErrors(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "TARGET_NOT_FOUND", ErrTargetNotFound("spine1").Code)
	assert.Equal(t, "TARGET_IN_USE", ErrTargetInUse("spine1", "job-1").Code)
	assert.Equal(t, "TARGET_DRAINING", ErrTargetDraining("spine1").Code)
	assert.Equal(t, "TARGET_MAINTENANCE", ErrTargetMaintenance("spine1").Code)
	assert.Equal(t, "TARGET_OFFLINE", ErrTargetOffline("spine1").Code)
	assert.Equal(t, "SCENARIO_NOT_FOUND", ErrScenarioNotFound("sc-1").Code)
	assert.Equal(t, "SCENARIO_IN_USE", ErrScenarioInUse("sc-1", "job-1").Code)

	// Extra fields marshal
	inUse := ErrTargetInUse("spine1", "job-1")
	raw, err := json.Marshal(inUse)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "locked_by_job_id")
}

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

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTypes_ConstantsAndStructures(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ONLINE", TargetStatusOnline)
	assert.Equal(t, "OFFLINE", TargetStatusOffline)
	assert.Equal(t, "DRAINING", TargetStatusDraining)
	assert.Equal(t, "MAINTENANCE", TargetStatusMaintenance)
	assert.Equal(t, "DELETED", TargetStatusDeleted)

	assert.Equal(t, "SUCCESS", JobStatusSuccess)
	assert.Equal(t, "RUNNING", JobStatusRunning)
	assert.Equal(t, "FAILED", JobStatusFailed)
	assert.Equal(t, "ABORTED", JobStatusAborted)

	now := time.Now()
	msg := EventMessage{
		ID:        "evt-1",
		Topic:     "job.step_advanced",
		Payload:   "data",
		Timestamp: now,
	}
	assert.Equal(t, "evt-1", msg.ID)
	assert.Equal(t, "job.step_advanced", msg.Topic)
	assert.Equal(t, "data", msg.Payload)
	assert.Equal(t, now, msg.Timestamp)

	st := StateTransition{
		Target:    "spine1",
		StateKey:  "key",
		OldValue:  "val1",
		NewValue:  "val2",
		Trigger:   "trap",
		Timestamp: now,
	}
	assert.Equal(t, "spine1", st.Target)
	assert.Equal(t, "key", st.StateKey)
	assert.Equal(t, "val1", st.OldValue)
	assert.Equal(t, "val2", st.NewValue)
	assert.Equal(t, "trap", st.Trigger)
	assert.Equal(t, now, st.Timestamp)
}

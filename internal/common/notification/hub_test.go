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

package notification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestHub_PubSubAndGetSince(t *testing.T) {
	t.Parallel()

	hub := NewHub(10)

	ch := hub.Subscribe([]string{"target.status_changed", "job.step_advanced"})
	assert.NotNil(t, ch)

	hub.Publish("target.status_changed", map[string]string{"target": "spine1", "status": "ONLINE"})

	select {
	case msg := <-ch:
		assert.Equal(t, "target.status_changed", msg.Topic)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for published message")
	}

	// Topic filtering
	hub.Publish("unrelated.topic", "data")
	select {
	case <-ch:
		t.Fatal("Should not receive filtered topic")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	// GetSince
	allLogs := hub.GetSince("")
	assert.Len(t, allLogs, 2)

	sinceLogs := hub.GetSince(allLogs[0].ID)
	assert.Len(t, sinceLogs, 1)

	hub.Unsubscribe(ch)
}

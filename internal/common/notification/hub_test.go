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

	"github.com/sh0jitmy/musubi/internal/common/types"
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

	// GetSince for last message or non-existent ID -> nil
	assert.Nil(t, hub.GetSince(allLogs[1].ID))
	assert.Nil(t, hub.GetSince("non-existent-id"))

	hub.Unsubscribe(ch)

	// Test maxLogs eviction and subscriber buffer overflow drop
	smallHub := NewHub(2)
	_ = smallHub.Subscribe(nil) // All topics, buffer size 200

	// Exceed maxLogs (2) to trigger eviction
	smallHub.Publish("t1", "m1")
	smallHub.Publish("t2", "m2")
	smallHub.Publish("t3", "m3") // Evicts m1

	logs := smallHub.GetSince("")
	assert.Len(t, logs, 2)
	assert.Equal(t, "t2", logs[0].Topic)
	assert.Equal(t, "t3", logs[1].Topic)

	// Direct channel with buffer size 1 to trigger non-blocking drop
	smallHub.mu.Lock()
	overflowCh := make(chan types.EventMessage, 1)
	overflowCh <- types.EventMessage{ID: "prefill"}
	smallHub.subscribers[overflowCh] = map[string]bool{"*": true}
	smallHub.mu.Unlock()

	// This publish will hit default case because overflowCh is full
	smallHub.Publish("t4", "m4")

	smallHub.CloseAllSubscribers()
}

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
	"sync"
	"time"

	"github.com/sh0jitmy/musubi/internal/common/types"
)

// Hub manages pub/sub subscriptions for SSE and Long Polling
type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan types.EventMessage]map[string]bool
	recentLogs  []types.EventMessage
	maxLogs     int
}

// NewHub creates a new in-memory event Hub
func NewHub(maxHistory int) *Hub {
	return &Hub{
		subscribers: make(map[chan types.EventMessage]map[string]bool),
		recentLogs:  make([]types.EventMessage, 0, maxHistory),
		maxLogs:     maxHistory,
	}
}

// Subscribe registers a listener channel for specified topics
func (h *Hub) Subscribe(topics []string) chan types.EventMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan types.EventMessage, 100)
	topicMap := make(map[string]bool)
	for _, t := range topics {
		topicMap[t] = true
	}
	h.subscribers[ch] = topicMap
	return ch
}

// Unsubscribe removes a listener channel
func (h *Hub) Unsubscribe(ch chan types.EventMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.subscribers[ch]; ok {
		delete(h.subscribers, ch)
		close(ch)
	}
}

// Publish broadcasts an event to matching subscribers
func (h *Hub) Publish(topic string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()

	msg := types.EventMessage{
		ID:        time.Now().Format("20060102150405.000000"),
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	if len(h.recentLogs) >= h.maxLogs {
		h.recentLogs = h.recentLogs[1:]
	}
	h.recentLogs = append(h.recentLogs, msg)

	for ch, topics := range h.subscribers {
		if len(topics) == 0 || topics[topic] || topics["*"] {
			select {
			case ch <- msg:
			default:
				// Non-blocking drop if consumer is too slow
			}
		}
	}
}

// GetSince returns events occurring after a given ID for long-polling
func (h *Hub) GetSince(sinceID string) []types.EventMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if sinceID == "" {
		return h.recentLogs
	}

	for i, m := range h.recentLogs {
		if m.ID == sinceID && i+1 < len(h.recentLogs) {
			return h.recentLogs[i+1:]
		}
	}
	return nil
}

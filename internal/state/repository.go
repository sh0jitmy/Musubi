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

package state

import (
	"fmt"
	"sync"
	"time"

	"github.com/sh0jitmy/musubi/internal/common/types"
)

// TransitionHandler receives state change diffs
type TransitionHandler func(t types.StateTransition)

// Repository manages the in-memory 2-tier state store
type Repository struct {
	mu           sync.RWMutex
	raw          map[string]map[string]any // target -> oid -> value
	derived      map[string]any            // namespace.key -> value
	onTransition TransitionHandler
}

// NewRepository creates a new in-memory State Repository
func NewRepository(handler TransitionHandler) *Repository {
	return &Repository{
		raw:          make(map[string]map[string]any),
		derived:      make(map[string]any),
		onTransition: handler,
	}
}

// SetRaw updates a raw OID value and triggers transition diff callback if changed
func (r *Repository) SetRaw(target string, oid string, newVal any, trigger string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.raw[target]; !ok {
		r.raw[target] = make(map[string]any)
	}

	oldVal, exists := r.raw[target][oid]
	oldStr := fmt.Sprintf("%v", oldVal)
	newStr := fmt.Sprintf("%v", newVal)

	if !exists || oldStr != newStr {
		r.raw[target][oid] = newVal
		if r.onTransition != nil {
			r.onTransition(types.StateTransition{
				Target:    target,
				StateKey:  fmt.Sprintf("raw.%s.%s", target, oid),
				OldValue:  oldStr,
				NewValue:  newStr,
				Trigger:   trigger,
				Timestamp: time.Now(),
			})
		}
		return true
	}
	return false
}

// GetRaw returns the value for a target's OID
func (r *Repository) GetRaw(target string, oid string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if targetMap, ok := r.raw[target]; ok {
		val, exists := targetMap[oid]
		return val, exists
	}
	return nil, false
}

// GetRawMap returns a snapshot of raw states for CEL evaluation
func (r *Repository) GetRawMap() map[string]map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]map[string]any)
	for t, oids := range r.raw {
		sub := make(map[string]any)
		for k, v := range oids {
			sub[k] = v
		}
		snapshot[t] = sub
	}
	return snapshot
}

// SetDerived updates a derived variable
func (r *Repository) SetDerived(key string, val any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.derived[key] = val
}

// GetDerivedMap returns a snapshot of derived states
func (r *Repository) GetDerivedMap() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]any)
	for k, v := range r.derived {
		snapshot[k] = v
	}
	return snapshot
}

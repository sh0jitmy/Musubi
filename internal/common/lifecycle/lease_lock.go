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

package lifecycle

import (
	"context"
	"sync"
	"time"

	"github.com/sh0jitmy/musubi/internal/common/errors"
)

// LockInfo holds information about a target lock
type LockInfo struct {
	TargetID  string
	JobID     string
	Acquired  time.Time
	ExpiresAt time.Time
}

// Manager coordinates target locks, graceful draining, and running job cancellation
type Manager struct {
	mu           sync.RWMutex
	locks        map[string]LockInfo
	draining     map[string]bool
	jobCancels   map[string]context.CancelFunc
	jobTargets   map[string][]string
	drainWaiters map[string][]chan struct{}
}

// NewManager creates a new Lifecycle Manager
func NewManager() *Manager {
	return &Manager{
		locks:        make(map[string]LockInfo),
		draining:     make(map[string]bool),
		jobCancels:   make(map[string]context.CancelFunc),
		jobTargets:   make(map[string][]string),
		drainWaiters: make(map[string][]chan struct{}),
	}
}

// AcquireLocks attempts to acquire exclusive locks on all requested targets
func (m *Manager) AcquireLocks(jobID string, targets []string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// 1. Check if any target is draining
	for _, t := range targets {
		if m.draining[t] {
			return errors.ErrTargetDraining(t)
		}
	}

	// 2. Check if any target is already locked
	for _, t := range targets {
		if lock, exists := m.locks[t]; exists {
			if now.Before(lock.ExpiresAt) && lock.JobID != jobID {
				return errors.ErrTargetInUse(t, lock.JobID)
			}
		}
	}

	// 3. Acquire locks
	for _, t := range targets {
		m.locks[t] = LockInfo{
			TargetID:  t,
			JobID:     jobID,
			Acquired:  now,
			ExpiresAt: now.Add(ttl),
		}
	}
	m.jobTargets[jobID] = targets
	return nil
}

// ReleaseLocks releases all locks held by a job and notifies drain waiters
func (m *Manager) ReleaseLocks(jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	targets := m.jobTargets[jobID]
	for _, t := range targets {
		if lock, exists := m.locks[t]; exists && lock.JobID == jobID {
			delete(m.locks, t)
		}
		// If target is draining and has no active locks, notify waiters
		if m.draining[t] && !m.isTargetLockedInternal(t) {
			for _, ch := range m.drainWaiters[t] {
				close(ch)
			}
			delete(m.drainWaiters, t)
		}
	}
	delete(m.jobTargets, jobID)
	delete(m.jobCancels, jobID)
}

// RegisterJobCancel registers a cancellation function for a running job
func (m *Manager) RegisterJobCancel(jobID string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobCancels[jobID] = cancel
}

// CancelJob aborts a specific running job
func (m *Manager) CancelJob(jobID string) bool {
	m.mu.Lock()
	cancel, exists := m.jobCancels[jobID]
	m.mu.Unlock()

	if exists && cancel != nil {
		cancel()
		return true
	}
	return false
}

// SetDraining marks a target as DRAINING so no new jobs can acquire it
func (m *Manager) SetDraining(target string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.draining[target] = true
}

// IsDraining returns true if a target is in draining state
func (m *Manager) IsDraining(target string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.draining[target]
}

// ForceAbortTarget aborts all active jobs currently using the target and frees its locks
func (m *Manager) ForceAbortTarget(target string) int {
	m.mu.Lock()
	var jobsToCancel []string
	if lock, exists := m.locks[target]; exists {
		jobsToCancel = append(jobsToCancel, lock.JobID)
		delete(m.locks, target)
	}
	m.mu.Unlock()

	for _, jobID := range jobsToCancel {
		m.CancelJob(jobID)
	}
	return len(jobsToCancel)
}

// GetActiveJobsForTarget returns active job IDs using this target
func (m *Manager) GetActiveJobsForTarget(target string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var jobs []string
	if lock, exists := m.locks[target]; exists {
		if time.Now().Before(lock.ExpiresAt) {
			jobs = append(jobs, lock.JobID)
		}
	}
	return jobs
}

// IsTargetLocked returns true if a target currently has an unexpired lease lock
func (m *Manager) IsTargetLocked(target string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if lock, exists := m.locks[target]; exists {
		if time.Now().Before(lock.ExpiresAt) {
			return true, lock.JobID
		}
	}
	return false, ""
}

func (m *Manager) isTargetLockedInternal(target string) bool {
	if lock, exists := m.locks[target]; exists {
		return time.Now().Before(lock.ExpiresAt)
	}
	return false
}

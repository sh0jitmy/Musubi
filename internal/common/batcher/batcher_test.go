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

package batcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestBatcher_BasicFlush(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	flushed := make([]string, 0)

	b := New[string](5, 50*time.Millisecond, func(ctx context.Context, items []string) error {
		mu.Lock()
		flushed = append(flushed, items...)
		mu.Unlock()
		return nil
	})

	for i := 0; i < 3; i++ {
		b.Push("item")
	}

	time.Sleep(100 * time.Millisecond)
	b.Close()

	mu.Lock()
	assert.Len(t, flushed, 3)
	mu.Unlock()
}

func TestBatcher_BufferFullTrigger(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	flushedCount := 0

	b := New[int](3, 1*time.Second, func(ctx context.Context, items []int) error {
		mu.Lock()
		flushedCount += len(items)
		mu.Unlock()
		return nil
	})

	for i := 0; i < 6; i++ {
		b.Push(i)
	}

	time.Sleep(50 * time.Millisecond)
	b.Close()

	mu.Lock()
	assert.Equal(t, 6, flushedCount)
	mu.Unlock()
}

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
	"time"
)

// FlushFunc is called with a batch of items
type FlushFunc[T any] func(ctx context.Context, items []T) error

// Batcher provides high-throughput, thread-safe, non-blocking batch flushing
type Batcher[T any] struct {
	flushFn       FlushFunc[T]
	bufferSize    int
	flushInterval time.Duration
	itemChan      chan T
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// New creates and starts a new Batcher
func New[T any](bufferSize int, flushInterval time.Duration, flushFn FlushFunc[T]) *Batcher[T] {
	b := &Batcher[T]{
		flushFn:       flushFn,
		bufferSize:    bufferSize,
		flushInterval: flushInterval,
		itemChan:      make(chan T, bufferSize*2),
	}
	//nolint:gosec // b.cancel is stored and called during Batcher.Close()
	b.ctx, b.cancel = context.WithCancel(context.Background())

	b.wg.Add(1)
	go b.run()
	return b
}

// Push adds an item to the batch channel (non-blocking if not full)
func (b *Batcher[T]) Push(item T) {
	select {
	case b.itemChan <- item:
	default:
		// Queue full fallback: drop or direct flush
	}
}

// Close gracefully flushes all remaining items and terminates background worker
func (b *Batcher[T]) Close() {
	b.cancel()
	b.wg.Wait()
}

func (b *Batcher[T]) run() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	batch := make([]T, 0, b.bufferSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		items := make([]T, len(batch))
		copy(items, batch)
		batch = batch[:0]
		_ = b.flushFn(context.Background(), items)
	}

	for {
		select {
		case <-b.ctx.Done():
			// Drain remaining in itemChan
			for {
				select {
				case item := <-b.itemChan:
					batch = append(batch, item)
					if len(batch) >= b.bufferSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case item := <-b.itemChan:
			batch = append(batch, item)
			if len(batch) >= b.bufferSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

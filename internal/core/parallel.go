// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"sync"
)

// maxParallel bounds the number of submodules inspected at the same time.
const maxParallel = 8

// forEach calls fn for every index below n, running at most limit calls at
// the same time. The first error cancels the context of the other calls, no
// further calls start, and that error is returned once every started call
// has returned. When ctx ends before all calls started, its error is
// returned.
func forEach(ctx context.Context, n, limit int, fn func(ctx context.Context, i int) error) error {
	inner, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	sem := make(chan struct{}, max(limit, 1))
	started := 0
	for i := range n {
		select {
		case sem <- struct{}{}:
		case <-inner.Done():
		}
		if inner.Err() != nil {
			break
		}
		started++
		wg.Go(func() {
			defer func() { <-sem }()
			if err := fn(inner, i); err != nil {
				mu.Lock()
				if first == nil {
					first = err
					cancel()
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if first != nil {
		return first
	}
	if started < n {
		return ctx.Err()
	}
	return nil
}

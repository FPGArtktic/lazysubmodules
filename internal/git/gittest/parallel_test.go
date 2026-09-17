// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

// recorder is a testing.TB that records failures instead of reporting them.
type recorder struct {
	testing.TB
	mu      sync.Mutex
	errors  []string
	failed  bool
	stopped bool
}

// Errorf records a formatted error.
//
// Context: any goroutine.
// Return: nothing.
func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// Error records an error.
//
// Context: any goroutine.
// Return: nothing.
func (r *recorder) Error(args ...any) {
	r.Errorf("%s", fmt.Sprint(args...))
}

// Fail records a failure.
//
// Context: any goroutine.
// Return: nothing.
func (r *recorder) Fail() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
}

// FailNow records that the test was stopped, and returns.
//
// Context: the test goroutine.
// Return: nothing.
func (r *recorder) FailNow() {
	r.Fail()
	r.stopped = true
}

// stop ends a goroutine started by parallel in the way named by how.
func stop(t testing.TB, how string) {
	switch how {
	case "Fatalf":
		t.Fatalf("stop %d", 1)
	case "Fatal":
		t.Fatal("stop", 2)
	default:
		t.FailNow()
	}
}

func TestParallel(t *testing.T) {
	t.Parallel()
	rec := &recorder{TB: t}
	var done, after atomic.Int32
	count := func(testing.TB) { done.Add(1) }
	fns := []func(testing.TB){count, count}
	for _, how := range []string{"Fatalf", "Fatal", "FailNow"} {
		fns = append(fns, func(t testing.TB) {
			stop(t, how)
			after.Add(1)
		})
	}
	parallel(rec, fns...)
	slices.Sort(rec.errors)
	if !slices.Equal(rec.errors, []string{"stop 1", "stop2"}) || !rec.failed ||
		!rec.stopped || done.Load() != 2 || after.Load() != 0 {
		t.Errorf("errors %q, failed %v, stopped %v, done %d, continued %d", rec.errors,
			rec.failed, rec.stopped, done.Load(), after.Load())
	}

	rec = &recorder{TB: t}
	parallel(rec, count)
	if rec.failed || rec.stopped || len(rec.errors) != 0 || done.Load() != 3 {
		t.Errorf("successful run: errors %q, failed %v, stopped %v, done %d", rec.errors,
			rec.failed, rec.stopped, done.Load())
	}
}

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/task"
	"sync/atomic"
	"unsafe"
)

type semEntry struct {
	f       task.Futex
	waiters atomic.Uint32
}

var semLock task.Mutex
var semMap map[*atomic.Uint32]*semEntry

func getSemEntry(sema *atomic.Uint32) *semEntry {
	semLock.Lock()

	if semMap == nil {
		semMap = make(map[*atomic.Uint32]*semEntry)
	}

	e := semMap[sema]
	if e == nil {
		e = &semEntry{}
		semMap[sema] = e
	}

	semLock.Unlock()
	return e
}

func tryDeleteSem(sema *atomic.Uint32, e *semEntry) {
	if e.waiters.Load() != 0 {
		return
	}
	if sema.Load() != 0 {
		return
	}

	semLock.Lock()
	if e.waiters.Load() == 0 && sema.Load() == 0 {
		delete(semMap, sema)
	}
	semLock.Unlock()
}

//go:linkname semacquire internal/poll.runtime_Semacquire
func semacquire(sema *atomic.Uint32) {
	_ = unsafe.Pointer(nil)

	e := getSemEntry(sema)

	for {
		v := sema.Load()
		if v != 0 {
			if sema.CompareAndSwap(v, v-1) {
				tryDeleteSem(sema, e)
				return
			}
			continue
		}

		e.waiters.Add(1)

		for sema.Load() == 0 {
			e.f.Wait(0)
		}

		e.waiters.Add(^uint32(0)) // -1
	}
}

//go:linkname semrelease internal/poll.runtime_Semrelease
func semrelease(sema *atomic.Uint32) {
	_ = unsafe.Pointer(nil)

	e := getSemEntry(sema)

	old := sema.Add(1) - 1
	if old == 0 {
		e.f.Wake()
	}

	tryDeleteSem(sema, e)
}

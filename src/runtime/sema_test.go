package runtime

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestSemaphoreBasic(t *testing.T) {
	var sem atomic.Uint32
	sem.Store(1)

	semacquire((*uint32)(unsafe.Pointer(&sem)))

	if sem.Load() != 0 {
		t.Fatalf("expected 0, got %d", sem.Load())
	}

	semrelease((*uint32)(unsafe.Pointer(&sem)))

	if sem.Load() != 1 {
		t.Fatalf("expected 1, got %d", sem.Load())
	}
}

func TestSemaphoreBlocking(t *testing.T) {
	var sem atomic.Uint32

	done := make(chan struct{})

	go func() {
		semacquire((*uint32)(unsafe.Pointer(&sem)))
		close(done)
	}()

	select {
	case <-done:
		t.Fatalf("acquire should block")
	case <-time.After(50 * time.Millisecond):
	}

	semrelease((*uint32)(unsafe.Pointer(&sem)))

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("acquire did not wake")
	}
}

func TestSemaphoreConcurrency(t *testing.T) {
	var sem atomic.Uint32
	sem.Store(3)

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers)

	var active atomic.Int32
	var maxActive atomic.Int32

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()

			semacquire((*uint32)(unsafe.Pointer(&sem)))

			n := active.Add(1)

			for {
				old := maxActive.Load()
				if n <= old || maxActive.CompareAndSwap(old, n) {
					break
				}
			}

			time.Sleep(time.Millisecond)

			active.Add(-1)

			semrelease((*uint32)(unsafe.Pointer(&sem)))
		}()
	}

	wg.Wait()

	if maxActive.Load() > 3 {
		t.Fatalf("semaphore limit exceeded: %d", maxActive.Load())
	}
}

func TestSemaphoreCleanup(t *testing.T) {
	var sem atomic.Uint32

	semrelease((*uint32)(unsafe.Pointer(&sem)))
	semacquire((*uint32)(unsafe.Pointer(&sem)))

	time.Sleep(10 * time.Millisecond)

	semLock.Lock()
	_, exists := semMap[&sem]
	semLock.Unlock()

	if exists {
		t.Fatalf("semaphore entry not cleaned up")
	}
}

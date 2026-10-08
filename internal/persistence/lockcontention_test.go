package persistence

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLockExcludesConcurrentHolders: however many writers contend, at most
// one is inside the lock at a time. The pid probe is faked to call every
// holder dead (the state a crashed TUI leaves, or a recycled pid), which is
// exactly when the old pid-file lock let one writer delete another's live
// lock and both proceed.
func TestLockExcludesConcurrentHolders(t *testing.T) {
	store := newLockTestStore(t)
	withProcessAlive(t, func(int) bool { return false })

	var inside, overlaps atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				err := store.withLock(func() error {
					if inside.Add(1) > 1 {
						overlaps.Add(1)
					}
					time.Sleep(200 * time.Microsecond)
					inside.Add(-1)
					return nil
				})
				if err != nil {
					t.Errorf("withLock: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	if n := overlaps.Load(); n > 0 {
		t.Fatalf("%d critical sections overlapped another holder", n)
	}
}

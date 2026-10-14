package notify

import (
	"runtime"
	"testing"
)

// The lock must outlive a garbage collection: an *os.File nobody references
// is closed by its finalizer, which silently drops the flock and lets every
// dashboard and `optimus watch` run its own watcher (duplicate notifications,
// and each one polling tmux every two seconds).
func TestLockSurvivesGC(t *testing.T) {
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	if !TryLock() {
		t.Fatal("first lock refused")
	}
	for i := 0; i < 3; i++ {
		runtime.GC()
	}
	if TryLock() {
		t.Fatal("lock was released by the garbage collector")
	}
}

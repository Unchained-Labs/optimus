package index

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Unchained-Labs/optimus/internal/model"
)

// The web dashboard, the TUI and the CLI save the cache at the same time; a
// torn cache fails to decode and the next load re-reads the whole history.
func TestConcurrentSavesLeaveAValidCache(t *testing.T) {
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			c := cacheFile{Version: cacheVersion, Entries: map[string]entry{}}
			for i := 0; i < 2000+w*100; i++ {
				p := fmt.Sprintf("/s/%d.jsonl", i)
				c.Entries[p] = entry{Key: p, Session: &model.Session{Agent: "claude", ID: p, FirstPrompt: "fix the flaky test in the payment service"}}
			}
			for j := 0; j < 5; j++ {
				if err := saveCache(c); err != nil {
					t.Error(err)
				}
			}
		}(w)
	}
	wg.Wait()
	if got := loadCache(); len(got.Entries) < 2000 {
		t.Fatalf("cache unreadable after concurrent saves: %d entries", len(got.Entries))
	}
}

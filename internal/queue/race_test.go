package queue

import (
	"sync"
	"testing"
)

// TestPriorityQueueConcurrentCheckpointSave exercises the exact access
// pattern used by the crawler's checkpoint system: Items() + AllVisited()
// snapshotting running concurrently with PushURL/PopURL mutations from
// workers. Run with -race to catch data races (Bug B5 regression test).
func TestPriorityQueueConcurrentCheckpointSave(t *testing.T) {
	q := NewPriorityQueueWithMaxSize(100000)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				q.PushURL("https://example.com/url/"+itoa(id)+"/"+itoa(j), 1)
			}
		}(i)
	}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if _, ok := q.PopURL(); !ok {
					break
				}
			}
		}()
	}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = q.Items()
				_ = q.AllVisited()
				_ = q.Size()
			}
		}()
	}

	wg.Wait()
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [32]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = digits[n%10]
		n /= 10
	}
	return string(buf[pos:])
}

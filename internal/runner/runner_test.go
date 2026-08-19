package runner

import (
	"context"
	"testing"
	"time"
)

func TestWaitInactiveReturnsAfterActiveRunFinishes(t *testing.T) {
	r := &Runner{active: make(map[uint]chan struct{})}
	done := make(chan struct{})
	r.active[7] = done

	waited := make(chan error, 1)
	go func() {
		waited <- r.WaitInactive(context.Background(), 7)
	}()

	select {
	case err := <-waited:
		t.Fatalf("WaitInactive returned before the run finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	r.mu.Lock()
	delete(r.active, 7)
	close(done)
	r.mu.Unlock()

	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitInactive did not return after the run finished")
	}
}

package auditlog

import (
	"testing"
	"time"
)

// TestWorkerLoopDrainTimeoutOnEmptyQueue drives the drain-timeout exit of
// workerLoop directly. Close() cannot reach it: Close shuts w.ch before
// w.stopCh, so the worker always leaves through the closed-channel return.
// A pre-closed stopCh over an open, empty queue is the only way in, and the
// queue must stay empty or the outer select races between its two ready arms.
func TestWorkerLoopDrainTimeoutOnEmptyQueue(t *testing.T) {
	stopCh := make(chan struct{})
	close(stopCh)
	w := &Writer{ch: make(chan map[string]any, 1), stopCh: stopCh}
	w.workerWG.Add(1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.workerLoop()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workerLoop did not return after stopCh closed over an empty queue")
	}
	w.workerWG.Wait()

	if got := w.droppedCount.Load(); got != 0 {
		t.Fatalf("droppedCount = %d; want 0, the queue held nothing", got)
	}
	if got := w.drainedCount.Load(); got != 0 {
		t.Fatalf("drainedCount = %d; want 0 on the drain-timeout path", got)
	}
}

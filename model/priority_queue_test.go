package model

import (
	"testing"
	"time"
)

func qm(id string, prio int, terminal bool, at time.Time) *QueuedMessage {
	return &QueuedMessage{ID: id, Priority: prio, Terminal: terminal, EnqueuedAt: at}
}

func TestPriorityQueueOrdering(t *testing.T) {
	q := NewPriorityQueue(6)
	base := time.Now()
	q.Enqueue(qm("a", 1, false, base.Add(0)))
	q.Enqueue(qm("b", 4, false, base.Add(1)))
	q.Enqueue(qm("c", 2, false, base.Add(2)))
	q.Enqueue(qm("d", 4, false, base.Add(3)))

	got := []string{}
	for m := q.Dequeue(); m != nil; m = q.Dequeue() {
		got = append(got, m.ID)
	}
	want := []string{"b", "d", "c", "a"} // priority desc, FIFO within same priority
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %s, want %s (got %v)", i, got[i], want[i], got)
		}
	}
}

func TestPriorityQueueTerminalPreemptsHost(t *testing.T) {
	q := NewPriorityQueue(6)
	base := time.Now()
	q.Enqueue(qm("host_high", 4, false, base.Add(0)))
	q.Enqueue(qm("host_low", 1, false, base.Add(1)))
	q.Enqueue(qm("rtt", 1, true, base.Add(2)))
	if m := q.Dequeue(); m == nil || m.ID != "rtt" {
		t.Fatalf("terminal message must preempt host messages, got %v", m)
	}
	if m := q.Dequeue(); m == nil || m.ID != "host_high" {
		t.Fatalf("next must be the high-priority host, got %v", m)
	}
}

func TestPriorityQueuePurgeStale(t *testing.T) {
	q := NewPriorityQueue(6)
	now := time.Now()
	q.Enqueue(&QueuedMessage{ID: "stale", Priority: 1, EnqueuedAt: now.Add(-10 * time.Second), StalenessLimit: 5 * time.Second})
	q.Enqueue(&QueuedMessage{ID: "fresh", Priority: 1, EnqueuedAt: now, StalenessLimit: time.Hour})
	if n := q.PurgeStale(now); n != 1 {
		t.Fatalf("expected 1 purged, got %d", n)
	}
	if q.Len() != 1 {
		t.Fatalf("expected 1 remaining, got %d", q.Len())
	}
}

func TestPriorityQueueEmptyDequeue(t *testing.T) {
	q := NewPriorityQueue(6)
	if m := q.Dequeue(); m != nil {
		t.Fatalf("expected nil dequeue on empty queue, got %v", m)
	}
}
